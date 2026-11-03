package docker

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-kit/output"
)

// Bounds of a log stream. A zero LogOptions.MaxLineBytes or MaxLinesPerSecond selects the default.
const (
	DefaultLogLineBytes      = 16 << 10
	MaxLogLineBytes          = 1 << 20
	DefaultLogLinesPerSecond = 1000

	// maxFrameBytes bounds one multiplexed frame; a larger announced length is a damaged stream.
	maxFrameBytes = 1 << 20
	// maxStampBytes bounds the timestamp and its space at the start of a line.
	maxStampBytes = 40
	frameHeadSize = 8
	rawChunkSize  = 32 << 10
)

// Source is the output stream a log line was written to.
type Source string

const (
	SourceStdout Source = "stdout"
	SourceStderr Source = "stderr"
)

// LogOptions selects and bounds the lines Logs returns. The zero value reads every line from the start
// without following.
type LogOptions struct {
	// Since limits the stream to lines at or after this time; zero means from the start. To follow only
	// new lines, set Follow with Since to time.Now().
	Since time.Time
	// Until limits the stream to lines at or before this time; zero means without an end. With Follow the
	// stream ends when the Engine ends it; Logs adds no end of its own.
	Until time.Time
	// Tail limits the stream to the last n lines; zero means all lines.
	Tail int
	// Follow keeps the stream open and delivers new lines until the context ends or the stream is closed.
	Follow bool
	// MaxLineBytes bounds the text of one line. A longer line is cut at a character boundary and marked
	// Truncated. Zero means DefaultLogLineBytes; more than MaxLogLineBytes is refused.
	MaxLineBytes int
	// MaxLinesPerSecond bounds the delivery rate. Next waits instead of dropping lines. Zero means
	// DefaultLogLinesPerSecond; a negative value is refused.
	MaxLinesPerSecond int
}

// LogLine is one line of a container's output. Text is untrusted: a caller masks control characters
// before it shows it.
type LogLine struct {
	Time      time.Time
	Source    Source
	Text      string // without the line end; invalid UTF-8 is replaced by U+FFFD
	Truncated bool   // the line was longer than MaxLineBytes and the rest was dropped
}

var errStreamClosed = &Error{Code: output.CodeRuntime, Message: "the log stream was closed"}

func invalidStream() *Error {
	return &Error{Code: output.CodeRuntime, Message: "the " + subject + " returned an invalid log stream"}
}

// lineBuf collects the bytes of one unfinished line of one source.
type lineBuf struct {
	data []byte
	over bool // bytes beyond the cap were dropped
}

// LogStream delivers log lines in order. Next must not be called concurrently with itself; Close may be
// called at any time from any goroutine.
type LogStream struct {
	ctx  context.Context
	body io.ReadCloser
	br   *bufio.Reader
	tty  bool

	maxLine int
	limiter bucket

	frame []byte
	pos   int
	src   int // index into bufs of the current frame
	bufs  [2]lineBuf
	eof   bool
	err   error

	closed  atomic.Bool
	closeCh chan struct{}
	once    sync.Once
}

// Logs opens the log of a container named by ID or name. Lines carry the Engine's timestamp and their
// source; a container with a terminal has a single stream, reported as SourceStdout. A line of one source
// that spans several frames is joined. The stream ends with io.EOF when the log ends, or never with Follow.
// Request timeouts apply to the answer's headers only: afterwards the context and Close end the stream.
func (c *Client) Logs(ctx context.Context, ref string, opts LogOptions) (*LogStream, error) {
	segment, err := checkRef(ref)
	if err != nil {
		return nil, err
	}
	if opts.MaxLineBytes < 0 || opts.MaxLineBytes > MaxLogLineBytes || opts.MaxLinesPerSecond < 0 ||
		opts.Tail < 0 || !opts.Since.IsZero() && opts.Since.Unix() < 0 ||
		!opts.Until.IsZero() && (opts.Until.Unix() < 0 || opts.Until.Before(opts.Since)) {
		return nil, &Error{Code: output.CodeInvalidRequest, Message: "the log options are not valid"}
	}
	maxLine := opts.MaxLineBytes
	if maxLine == 0 {
		maxLine = DefaultLogLineBytes
	}
	rate := opts.MaxLinesPerSecond
	if rate == 0 {
		rate = DefaultLogLinesPerSecond
	}

	answer, err := c.inspect(ctx, ref)
	if err != nil {
		return nil, err
	}
	version, err := c.apiVersion(ctx)
	if err != nil {
		return nil, err
	}
	query := url.Values{"stdout": {"1"}, "stderr": {"1"}, "timestamps": {"1"}, "tail": {"all"}}
	if opts.Tail > 0 {
		query.Set("tail", strconv.Itoa(opts.Tail))
	}
	if !opts.Since.IsZero() {
		query.Set("since", unixStamp(opts.Since))
	}
	if !opts.Until.IsZero() {
		query.Set("until", unixStamp(opts.Until))
	}
	if opts.Follow {
		query.Set("follow", "1")
	}
	target := "http://" + host + "/v" + version + "/containers/" + segment + "/logs?" + query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, &Error{Code: output.CodeInvalidRequest, Message: "the request could not be built"}
	}
	resp, err := c.stream.Do(req)
	if err != nil {
		return nil, transportError(ctx, err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, statusError(resp.StatusCode)
	}
	return &LogStream{
		ctx:     ctx,
		body:    resp.Body,
		br:      bufio.NewReaderSize(resp.Body, rawChunkSize),
		tty:     answer.Config.Tty,
		maxLine: maxLine,
		limiter: newBucket(rate),
		closeCh: make(chan struct{}),
	}, nil
}

// unixStamp formats t as the Engine expects it: seconds with a nine-digit fraction.
func unixStamp(t time.Time) string {
	n := strconv.Itoa(t.Nanosecond())
	return strconv.FormatInt(t.Unix(), 10) + "." + strings.Repeat("0", 9-len(n)) + n
}

// Next returns the next line, or io.EOF at the end of the log. After an error every call returns it again.
func (s *LogStream) Next() (LogLine, error) {
	if s.err != nil {
		return LogLine{}, s.err
	}
	line, err := s.next()
	if err != nil {
		s.err = err
		return LogLine{}, err
	}
	if err := s.limiter.wait(s.ctx, s.closeCh); err != nil {
		s.err = err
		return LogLine{}, err
	}
	return line, nil
}

func (s *LogStream) next() (LogLine, error) {
	for {
		for s.pos < len(s.frame) {
			rest := s.frame[s.pos:]
			b := &s.bufs[s.src]
			i := bytes.IndexByte(rest, '\n')
			if i < 0 {
				b.add(rest, s.maxLine+maxStampBytes)
				s.pos = len(s.frame)
				break
			}
			b.add(rest[:i], s.maxLine+maxStampBytes)
			s.pos += i + 1
			return s.finish(s.src)
		}
		if s.eof {
			// The log ended without a final line end: deliver what is left.
			for i := range s.bufs {
				if len(s.bufs[i].data) > 0 || s.bufs[i].over {
					return s.finish(i)
				}
			}
			return LogLine{}, io.EOF
		}
		if err := s.fill(); errors.Is(err, io.EOF) {
			s.eof = true
		} else if err != nil {
			return LogLine{}, err
		}
	}
}

func (b *lineBuf) add(p []byte, limit int) {
	if room := limit - len(b.data); len(p) > room {
		p, b.over = p[:room], true
	}
	b.data = append(b.data, p...)
}

// finish turns the collected bytes of one source into a line.
func (s *LogStream) finish(i int) (LogLine, error) {
	b := &s.bufs[i]
	data, over := b.data, b.over
	b.data, b.over = b.data[:0], false

	head := data
	if len(head) > maxStampBytes {
		head = head[:maxStampBytes]
	}
	sp := bytes.IndexByte(head, ' ')
	if sp < 0 {
		return LogLine{}, invalidStream()
	}
	stamp, err := time.Parse(time.RFC3339Nano, string(data[:sp]))
	if err != nil {
		return LogLine{}, invalidStream()
	}
	// Replacement can lengthen the text, so the limit applies afterwards.
	text := strings.ToValidUTF8(string(data[sp+1:]), "�")
	truncated := over
	if len(text) > s.maxLine {
		cut := s.maxLine
		for cut > 0 && !utf8.RuneStart(text[cut]) {
			cut--
		}
		text, truncated = text[:cut], true
	}
	source := SourceStdout
	if i == 1 {
		source = SourceStderr
	}
	return LogLine{Time: stamp, Source: source, Text: text, Truncated: truncated}, nil
}

// fill loads the next frame, or chunk of a raw stream, into s.frame. It returns io.EOF only at a frame
// boundary.
func (s *LogStream) fill() error {
	s.frame, s.pos, s.src = s.frame[:0], 0, 0
	if s.tty {
		if cap(s.frame) < rawChunkSize {
			s.frame = make([]byte, 0, rawChunkSize)
		}
		n, err := s.br.Read(s.frame[:rawChunkSize])
		s.frame = s.frame[:n]
		if err != nil && n == 0 {
			return s.readError(err, false)
		}
		return nil
	}
	var head [frameHeadSize]byte
	if _, err := io.ReadFull(s.br, head[:]); err != nil {
		return s.readError(err, true)
	}
	if head[0] != 1 && head[0] != 2 || head[1]|head[2]|head[3] != 0 {
		return invalidStream()
	}
	size := binary.BigEndian.Uint32(head[4:])
	if size > maxFrameBytes {
		return invalidStream()
	}
	if cap(s.frame) < int(size) {
		s.frame = make([]byte, size)
	}
	s.frame = s.frame[:size]
	if _, err := io.ReadFull(s.br, s.frame); err != nil {
		s.frame = s.frame[:0]
		return s.readError(err, true)
	}
	s.src = int(head[0]) - 1
	return nil
}

// readError classifies a failed read. An end inside a frame is a damaged stream, an end between frames is
// io.EOF.
func (s *LogStream) readError(err error, framed bool) error {
	switch {
	case s.closed.Load():
		return errStreamClosed
	case s.ctx.Err() != nil:
		return contextError(s.ctx.Err())
	case err == io.EOF:
		return io.EOF
	case framed && err == io.ErrUnexpectedEOF:
		return invalidStream()
	}
	return &Error{Code: output.CodeRuntime, Message: "the log stream was interrupted"}
}

// Close ends the stream and unblocks a pending Next. It is safe to call more than once.
func (s *LogStream) Close() error {
	s.once.Do(func() {
		s.closed.Store(true)
		close(s.closeCh)
		_ = s.body.Close()
	})
	return nil
}

// bucket is a token bucket that holds up to one second of the rate. It belongs to one stream, which calls it
// from Next only.
type bucket struct {
	rate   float64
	tokens float64
	last   time.Time
}

func newBucket(perSecond int) bucket {
	return bucket{rate: float64(perSecond), tokens: float64(perSecond), last: time.Now()}
}

func (b *bucket) wait(ctx context.Context, closeCh <-chan struct{}) error {
	for {
		now := time.Now()
		b.tokens = min(b.rate, b.tokens+now.Sub(b.last).Seconds()*b.rate)
		b.last = now
		if b.tokens >= 1 {
			b.tokens--
			return nil
		}
		timer := time.NewTimer(time.Duration((1 - b.tokens) / b.rate * float64(time.Second)))
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return contextError(ctx.Err())
		case <-closeCh:
			timer.Stop()
			return errStreamClosed
		}
	}
}
