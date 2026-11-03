//go:build linux || darwin

package docker

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-kit/output"
)

const stamp = "2026-01-02T03:04:05.123456789Z "

func frame(source byte, payload string) []byte {
	head := []byte{source, 0, 0, 0, 0, 0, 0, 0}
	binary.BigEndian.PutUint32(head[4:], uint32(len(payload)))
	return append(head, payload...)
}

func cat(parts ...[]byte) []byte { return bytes.Join(parts, nil) }

// logRoutes serves the inspect answer and the log body for container "web".
func logRoutes(tty bool, logs http.HandlerFunc) map[string]http.HandlerFunc {
	inspect := `{"Id":"web","Name":"/web","Config":{"Tty":false}}`
	if tty {
		inspect = `{"Id":"web","Name":"/web","Config":{"Tty":true}}`
	}
	return map[string]http.HandlerFunc{
		"/containers/web/json": jsonHandler(inspect),
		"/containers/web/logs": logs,
	}
}

func bodyHandler(body []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(body) }
}

// readAll returns the lines until the stream ends and the error that ended it.
func readAll(s *LogStream) ([]LogLine, error) {
	var lines []LogLine
	for {
		l, err := s.Next()
		if err != nil {
			return lines, err
		}
		lines = append(lines, l)
	}
}

func openLogs(t *testing.T, routes map[string]http.HandlerFunc, opts LogOptions) *LogStream {
	t.Helper()
	s, err := newFake(t, routes).client(t).Logs(context.Background(), "web", opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestLogsMultiplexed(t *testing.T) {
	body := cat(
		frame(1, stamp+"one\n"),
		frame(2, stamp+"err "),
		frame(1, stamp+"two\n"+stamp+"par"),
		frame(2, "t two\n"),
		frame(1, "tial\n"),
		frame(1, ""),
		frame(1, stamp+"no end"),
	)
	lines, err := readAll(openLogs(t, logRoutes(false, bodyHandler(body)), LogOptions{}))
	if err != io.EOF {
		t.Fatalf("end = %v", err)
	}
	want := []LogLine{
		{Source: SourceStdout, Text: "one"},
		{Source: SourceStdout, Text: "two"},
		{Source: SourceStderr, Text: "err t two"},
		{Source: SourceStdout, Text: "partial"},
		{Source: SourceStdout, Text: "no end"},
	}
	if len(lines) != len(want) {
		t.Fatalf("lines = %+v", lines)
	}
	for i, w := range want {
		g := lines[i]
		if g.Source != w.Source || g.Text != w.Text || g.Truncated || g.Time.Nanosecond() != 123456789 || g.Time.Year() != 2026 {
			t.Errorf("line %d = %+v, want %+v", i, g, w)
		}
	}
}

func TestLogsTTYRaw(t *testing.T) {
	// A raw stream has no frames; a header-like start must not be read as one.
	body := []byte(stamp + "first\n" + stamp + "second")
	lines, err := readAll(openLogs(t, logRoutes(true, bodyHandler(body)), LogOptions{}))
	if err != io.EOF || len(lines) != 2 || lines[0].Text != "first" || lines[1].Text != "second" ||
		lines[0].Source != SourceStdout || lines[1].Source != SourceStdout {
		t.Fatalf("lines = %+v, err = %v", lines, err)
	}
}

func TestLogsQuery(t *testing.T) {
	since := time.Unix(1700000000, 5000)
	tests := []struct {
		opts LogOptions
		want url.Values
	}{
		{LogOptions{}, url.Values{"stdout": {"1"}, "stderr": {"1"}, "timestamps": {"1"}, "tail": {"all"}}},
		{LogOptions{Since: since, Tail: 25, Follow: true}, url.Values{"stdout": {"1"}, "stderr": {"1"}, "timestamps": {"1"},
			"tail": {"25"}, "since": {"1700000000.000005000"}, "follow": {"1"}}},
	}
	for _, tt := range tests {
		var got atomic.Value
		routes := logRoutes(false, func(w http.ResponseWriter, r *http.Request) {
			got.Store(r.URL.Query())
			_, _ = w.Write(nil)
		})
		s := openLogs(t, routes, tt.opts)
		if _, err := readAll(s); err != io.EOF {
			t.Fatal(err)
		}
		q := got.Load().(url.Values)
		if q.Encode() != tt.want.Encode() {
			t.Errorf("query = %v, want %v", q, tt.want)
		}
	}
}

func TestLogsDamagedFrames(t *testing.T) {
	good := frame(1, stamp+"ok\n")
	hdr := func(b0, b1, b2, b3 byte, n uint32) []byte {
		h := []byte{b0, b1, b2, b3, 0, 0, 0, 0}
		binary.BigEndian.PutUint32(h[4:], n)
		return h
	}
	tests := map[string][]byte{
		"stdin source":    cat(good, frame(0, stamp+"x\n")),
		"unknown source":  cat(good, frame(3, stamp+"x\n")),
		"reserved byte 1": cat(good, hdr(1, 1, 0, 0, 2), []byte("x\n")),
		"reserved byte 3": cat(good, hdr(1, 0, 0, 9, 2), []byte("x\n")),
		"oversized frame": cat(good, hdr(1, 0, 0, 0, maxFrameBytes+1)),
		"huge frame":      cat(good, hdr(2, 0, 0, 0, 0xFFFFFFFF)),
		"truncated head":  cat(good, []byte{1, 0, 0}),
		"truncated body":  cat(good, hdr(1, 0, 0, 0, 20), []byte(stamp)),
		"no timestamp":    cat(good, frame(1, "plain text\n")),
		"bad timestamp":   cat(good, frame(1, "2026-13-45T99:00:00Z x\n")),
		"timestamp only":  cat(good, frame(1, "2026-01-02T03:04:05Z\n")),
	}
	for name, body := range tests {
		s := openLogs(t, logRoutes(false, bodyHandler(body)), LogOptions{})
		lines, err := readAll(s)
		if len(lines) != 1 || lines[0].Text != "ok" {
			t.Errorf("%s: lines = %+v", name, lines)
		}
		e := asError(t, err, output.CodeRuntime)
		if !strings.Contains(e.Message, "invalid log stream") || strings.Contains(e.Message, "plain text") {
			t.Errorf("%s: message = %q", name, e.Message)
		}
		if _, again := s.Next(); again != err {
			t.Errorf("%s: error is not sticky: %v", name, again)
		}
	}
}

func TestLogsLongLineIsTruncated(t *testing.T) {
	long := strings.Repeat("ä", 100) // two bytes each
	body := cat(
		frame(1, stamp+long[:50]),
		frame(1, long[50:]+"\n"),
		frame(1, stamp+"next\n"),
		frame(2, stamp+strings.Repeat("x", 5000)+"\n"),
	)
	lines, err := readAll(openLogs(t, logRoutes(false, bodyHandler(body)), LogOptions{MaxLineBytes: 51}))
	if err != io.EOF || len(lines) != 3 {
		t.Fatalf("lines = %d, err = %v", len(lines), err)
	}
	if !lines[0].Truncated || len(lines[0].Text) > 51 || !utf8.ValidString(lines[0].Text) ||
		lines[0].Text != strings.Repeat("ä", 25) {
		t.Errorf("long = %q truncated=%v", lines[0].Text, lines[0].Truncated)
	}
	if lines[1].Truncated || lines[1].Text != "next" {
		t.Errorf("next = %+v", lines[1])
	}
	if !lines[2].Truncated || len(lines[2].Text) != 51 || lines[2].Source != SourceStderr {
		t.Errorf("stderr = %+v", lines[2])
	}
}

func TestLogsInvalidUTF8(t *testing.T) {
	body := frame(1, stamp+"a\xff\xfeb\n")
	lines, err := readAll(openLogs(t, logRoutes(false, bodyHandler(body)), LogOptions{MaxLineBytes: 4}))
	if err != io.EOF || len(lines) != 1 || lines[0].Text != "a�" || !lines[0].Truncated {
		t.Fatalf("lines = %+v, err = %v", lines, err)
	}
}

func TestLogsLineBufferIsBounded(t *testing.T) {
	// A line far over the limit, spread over many frames, must not be buffered.
	var parts [][]byte
	parts = append(parts, frame(1, stamp))
	chunk := strings.Repeat("y", 60000)
	for range 20 {
		parts = append(parts, frame(1, chunk))
	}
	parts = append(parts, frame(1, "\n"), frame(1, stamp+"after\n"))
	s := openLogs(t, logRoutes(false, bodyHandler(cat(parts...))), LogOptions{MaxLineBytes: 100})
	lines, err := readAll(s)
	if err != io.EOF || len(lines) != 2 || !lines[0].Truncated || lines[1].Text != "after" {
		t.Fatalf("lines = %d, err = %v", len(lines), err)
	}
	if got := cap(s.bufs[0].data); got > 100+maxStampBytes+64 {
		t.Errorf("buffer capacity = %d", got)
	}
}

func TestLogsRateIsEnforcedByBackpressure(t *testing.T) {
	var body []byte
	for range 30 {
		body = append(body, frame(1, stamp+"x\n")...)
	}
	s := openLogs(t, logRoutes(false, bodyHandler(body)), LogOptions{MaxLinesPerSecond: 20})
	start := time.Now()
	lines, err := readAll(s)
	elapsed := time.Since(start)
	if err != io.EOF || len(lines) != 30 {
		t.Fatalf("lines = %d, err = %v", len(lines), err)
	}
	// 20 lines are the burst; the other 10 take 0.5 s.
	if elapsed < 400*time.Millisecond {
		t.Errorf("30 lines took %v, want at least 400ms", elapsed)
	}
}

func TestLogsRateWaitHonorsContextAndClose(t *testing.T) {
	var body []byte
	for range 5 {
		body = append(body, frame(1, stamp+"x\n")...)
	}
	f := newFake(t, logRoutes(false, bodyHandler(body)))
	c := f.client(t)

	ctx, cancel := context.WithCancel(context.Background())
	s, err := c.Logs(ctx, "web", LogOptions{MaxLinesPerSecond: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Next(); err != nil {
		t.Fatal(err)
	}
	time.AfterFunc(50*time.Millisecond, cancel)
	start := time.Now()
	_, err = s.Next()
	e := asError(t, err, output.CodeRuntime)
	if !errors.Is(e, context.Canceled) || time.Since(start) > 900*time.Millisecond {
		t.Errorf("err = %v after %v", err, time.Since(start))
	}

	s2, err := c.Logs(context.Background(), "web", LogOptions{MaxLinesPerSecond: 1})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = s2.Next()
	time.AfterFunc(50*time.Millisecond, func() { _ = s2.Close() })
	start = time.Now()
	_, err = s2.Next()
	asError(t, err, output.CodeRuntime)
	if time.Since(start) > 900*time.Millisecond {
		t.Errorf("Close took %v to end Next", time.Since(start))
	}
}

// blockingLogs writes one line, flushes and then holds the stream open.
func blockingLogs(f **fake) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(frame(1, stamp+"first\n"))
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-(*f).release:
		}
	}
}

func TestLogsContextCancelWhileFollowing(t *testing.T) {
	var f *fake
	f = newFake(t, logRoutes(false, blockingLogs(&f)))
	ctx, cancel := context.WithCancel(context.Background())
	s, err := f.client(t).Logs(ctx, "web", LogOptions{Follow: true})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if l, err := s.Next(); err != nil || l.Text != "first" {
		t.Fatalf("first = %+v, %v", l, err)
	}
	done := make(chan error, 1)
	go func() { _, err := s.Next(); done <- err }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if e := asError(t, err, output.CodeRuntime); !errors.Is(e, context.Canceled) {
			t.Errorf("cause = %v", e.Unwrap())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Next stayed blocked after cancel")
	}
}

func TestLogsCloseWhileBlocked(t *testing.T) {
	var f *fake
	f = newFake(t, logRoutes(false, blockingLogs(&f)))
	s, err := f.client(t).Logs(context.Background(), "web", LogOptions{Follow: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Next(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := s.Next(); done <- err }()
	time.Sleep(50 * time.Millisecond)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		asError(t, err, output.CodeRuntime)
	case <-time.After(3 * time.Second):
		t.Fatal("Next stayed blocked after Close")
	}
}

func TestLogsFollowHasNoOverallTimeout(t *testing.T) {
	routes := logRoutes(false, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(frame(1, stamp+"early\n"))
		w.(http.Flusher).Flush()
		time.Sleep(600 * time.Millisecond)
		_, _ = w.Write(frame(1, stamp+"late\n"))
	})
	f := newFake(t, routes)
	c, err := New(Options{Socket: f.socket, Timeout: 200 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	s, err := c.Logs(context.Background(), "web", LogOptions{Follow: true})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	lines, err := readAll(s)
	if err != io.EOF || len(lines) != 2 || lines[1].Text != "late" {
		t.Fatalf("lines = %+v, err = %v", lines, err)
	}
}

func TestLogsHeaderTimeout(t *testing.T) {
	routes := logRoutes(false, func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(3 * time.Second):
		}
	})
	f := newFake(t, routes)
	c, err := New(Options{Socket: f.socket, Timeout: 200 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_, err = c.Logs(context.Background(), "web", LogOptions{})
	asError(t, err, output.CodeTimeout)
}

func TestLogsRefusals(t *testing.T) {
	var requests atomic.Int32
	routes := logRoutes(false, bodyHandler(nil))
	count := routes["/containers/web/json"]
	routes["/containers/web/json"] = func(w http.ResponseWriter, r *http.Request) { requests.Add(1); count(w, r) }
	f := newFake(t, routes)
	c := f.client(t)
	bad := map[string]LogOptions{
		"line limit over maximum": {MaxLineBytes: MaxLogLineBytes + 1},
		"negative line limit":     {MaxLineBytes: -1},
		"negative rate":           {MaxLinesPerSecond: -1},
		"negative tail":           {Tail: -1},
		"since before epoch":      {Since: time.Unix(-5, 0)},
	}
	for name, opts := range bad {
		_, err := c.Logs(context.Background(), "web", opts)
		if err == nil {
			t.Errorf("%s: no error", name)
			continue
		}
		asError(t, err, output.CodeInvalidRequest)
	}
	for _, ref := range []string{"", "../etc", "a b", "web/x", strings.Repeat("a", 300)} {
		_, err := c.Logs(context.Background(), ref, LogOptions{})
		asError(t, err, output.CodeInvalidRequest)
	}
	if requests.Load() != 0 || f.pings.Load() != 0 {
		t.Errorf("a refused call sent requests: %d inspects, %d pings", requests.Load(), f.pings.Load())
	}
	// The maximum itself is accepted.
	s, err := c.Logs(context.Background(), "web", LogOptions{MaxLineBytes: MaxLogLineBytes})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
}

func TestLogsNotFound(t *testing.T) {
	c := newFake(t, shopRoutes()).client(t)
	_, err := c.Logs(context.Background(), "cccccccccccc", LogOptions{})
	asError(t, err, output.CodeNotFound)

	routes := logRoutes(false, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "no such container: "+secretValue, http.StatusNotFound)
	})
	_, err = newFake(t, routes).client(t).Logs(context.Background(), "web", LogOptions{})
	asError(t, err, output.CodeNotFound)
}
