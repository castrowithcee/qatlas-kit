//go:build windows

package localipc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const pipeBuffer = 64 * 1024

var dialWait = 5 * time.Second

const (
	dirRead = iota
	dirWrite
)

type pipeOverlapped struct{ ov windows.Overlapped }

func pipeSecurity() (*windows.SecurityAttributes, error) {
	sid, err := currentUserSID()
	if err != nil {
		return nil, err
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(D;;GA;;;NU)(A;;GA;;;" + sid.String() + ")")
	if err != nil {
		return nil, fmt.Errorf("cannot describe who may open the local pipe: %w", err)
	}
	return &windows.SecurityAttributes{
		Length:             uint32(unsafe.Sizeof(windows.SecurityAttributes{})),
		SecurityDescriptor: sd,
	}, nil
}

func createPipe(path string, first bool, sa *windows.SecurityAttributes) (windows.Handle, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return windows.InvalidHandle, err
	}
	flags := uint32(windows.PIPE_ACCESS_DUPLEX | windows.FILE_FLAG_OVERLAPPED)
	if first {
		flags |= windows.FILE_FLAG_FIRST_PIPE_INSTANCE
	}
	mode := uint32(windows.PIPE_TYPE_BYTE | windows.PIPE_READMODE_BYTE | windows.PIPE_WAIT |
		windows.PIPE_REJECT_REMOTE_CLIENTS)
	return windows.CreateNamedPipe(name, flags, mode, windows.PIPE_UNLIMITED_INSTANCES, pipeBuffer, pipeBuffer, 0, sa)
}

func listen(e Endpoint) (net.Listener, error) {
	path, err := e.Path()
	if err != nil {
		return nil, err
	}
	sa, err := pipeSecurity()
	if err != nil {
		return nil, err
	}
	first, err := createPipe(path, true, sa)
	if err != nil {
		if errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, windows.ERROR_PIPE_BUSY) {
			return nil, ErrRunning
		}
		return nil, fmt.Errorf("cannot create the local pipe %s: %w", path, err)
	}
	closing, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		_ = windows.CloseHandle(first)
		return nil, fmt.Errorf("cannot create the local pipe %s: %w", path, err)
	}
	return &pipeListener{path: path, sa: sa, next: first, closing: closing}, nil
}

type pipeListener struct {
	path string
	sa   *windows.SecurityAttributes

	acceptMu   sync.Mutex
	mu         sync.Mutex
	closed     bool
	next       windows.Handle
	closing    windows.Handle
	connecting *pipeOverlapped
}

func (l *pipeListener) Accept() (net.Conn, error) {
	l.acceptMu.Lock()
	defer l.acceptMu.Unlock()
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil, net.ErrClosed
	}
	h := l.next
	l.next = 0
	l.mu.Unlock()

	err := l.connect(h)
	if errors.Is(err, net.ErrClosed) {
		_ = windows.CloseHandle(h)
		return nil, net.ErrClosed
	}
	next, createErr := createPipe(l.path, false, l.sa)
	if createErr != nil {
		_ = windows.CloseHandle(h)
		return nil, fmt.Errorf("cannot open the next instance of the local pipe: %w", createErr)
	}
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		_ = windows.CloseHandle(next)
		_ = windows.CloseHandle(h)
		return nil, net.ErrClosed
	}
	l.next = next
	l.mu.Unlock()

	if err != nil && !errors.Is(err, windows.ERROR_NO_DATA) {
		_ = windows.CloseHandle(h)
		return nil, fmt.Errorf("cannot accept a client of the local pipe: %w", err)
	}
	conn := newPipeConn(h, l.path, true)
	if err := VerifyPeer(conn); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

func (l *pipeListener) connect(h windows.Handle) error {
	event, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(event)
	op := &pipeOverlapped{}
	op.ov.HEvent = event
	l.connecting = op
	defer func() { l.connecting = nil }()

	err = windows.ConnectNamedPipe(h, &op.ov)
	switch {
	case err == nil, errors.Is(err, windows.ERROR_PIPE_CONNECTED):
		return nil
	case !errors.Is(err, windows.ERROR_IO_PENDING):
		return err
	}
	var n uint32
	which, err := windows.WaitForMultipleObjects([]windows.Handle{event, l.closing}, false, windows.INFINITE)
	if err == nil && which == windows.WAIT_OBJECT_0 {
		return windows.GetOverlappedResult(h, &op.ov, &n, false)
	}
	_ = windows.CancelIoEx(h, &op.ov)
	_ = windows.GetOverlappedResult(h, &op.ov, &n, true)
	switch {
	case err != nil:
		return err
	case which == windows.WAIT_OBJECT_0+1:
		return net.ErrClosed
	}
	return fmt.Errorf("waiting for a client ended with %#x", which)
}

func (l *pipeListener) Close() error {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil
	}
	l.closed = true
	h := l.next
	l.next = 0
	l.mu.Unlock()

	_ = windows.SetEvent(l.closing)
	if h != 0 {
		_ = windows.CloseHandle(h)
	}
	l.acceptMu.Lock()
	_ = windows.CloseHandle(l.closing)
	l.acceptMu.Unlock()
	return nil
}

func (l *pipeListener) Addr() net.Addr { return pipeAddr(l.path) }

type pipeAddr string

func (a pipeAddr) Network() string { return "pipe" }
func (a pipeAddr) String() string  { return string(a) }

func dial(ctx context.Context, e Endpoint) (net.Conn, error) {
	path, err := e.Path()
	if err != nil {
		return nil, err
	}
	limit := time.Now().Add(dialWait)
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, fmt.Errorf("%s is not a named pipe: %w", path, fs.ErrNotExist)
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !time.Now().Before(limit) {
			return nil, os.ErrDeadlineExceeded
		}
		h, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING,
			windows.FILE_FLAG_OVERLAPPED|windows.SECURITY_SQOS_PRESENT|windows.SECURITY_IDENTIFICATION, 0)
		if err == nil {
			if kind, err := windows.GetFileType(h); err != nil || kind != windows.FILE_TYPE_PIPE {
				_ = windows.CloseHandle(h)
				return nil, fmt.Errorf("%s is not a named pipe: %w", path, fs.ErrNotExist)
			}
			conn := newPipeConn(h, path, false)
			if err := VerifyPeer(conn); err != nil {
				_ = conn.Close()
				return nil, fmt.Errorf("%s: %w", e.label(), err)
			}
			return conn, nil
		}
		if !errors.Is(err, windows.ERROR_PIPE_BUSY) {
			return nil, &os.PathError{Op: "open", Path: path, Err: err}
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

type pipeConn struct {
	h      windows.Handle
	path   string
	server bool

	mu       sync.Mutex
	closed   bool
	deadline [2]time.Time
	ops      map[*pipeOp]struct{}
	inflight sync.WaitGroup
}

type pipeOp struct {
	pipeOverlapped
	dir     int
	timer   *time.Timer
	expired bool
}

func newPipeConn(h windows.Handle, path string, server bool) *pipeConn {
	return &pipeConn{h: h, path: path, server: server, ops: map[*pipeOp]struct{}{}}
}

func (c *pipeConn) Read(b []byte) (int, error) { return c.do(dirRead, b) }

func (c *pipeConn) Write(b []byte) (int, error) {
	total := 0
	for total < len(b) {
		n, err := c.do(dirWrite, b[total:])
		total += n
		if err != nil {
			return total, err
		}
		if n == 0 {
			return total, io.ErrShortWrite
		}
	}
	return total, nil
}

func (c *pipeConn) do(dir int, b []byte) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}
	event, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(event)
	op := &pipeOp{dir: dir}
	op.ov.HEvent = event

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return 0, net.ErrClosed
	}
	deadline := c.deadline[dir]
	if !deadline.IsZero() && !time.Now().Before(deadline) {
		c.mu.Unlock()
		return 0, os.ErrDeadlineExceeded
	}
	c.ops[op] = struct{}{}
	c.inflight.Add(1)
	c.arm(op)
	c.mu.Unlock()

	var n uint32
	if dir == dirWrite {
		err = windows.WriteFile(c.h, b, &n, &op.ov)
	} else {
		err = windows.ReadFile(c.h, b, &n, &op.ov)
	}
	if err == nil || errors.Is(err, windows.ERROR_IO_PENDING) {
		// A deadline or Close that came between the check above and the start of the operation found
		// nothing to cancel yet; it is cancelled here instead.
		c.mu.Lock()
		if c.closed || op.expired {
			_ = windows.CancelIoEx(c.h, &op.ov)
		}
		c.mu.Unlock()
		err = windows.GetOverlappedResult(c.h, &op.ov, &n, true)
	}

	c.mu.Lock()
	delete(c.ops, op)
	if op.timer != nil {
		op.timer.Stop()
	}
	expired, closed := op.expired, c.closed
	c.mu.Unlock()
	c.inflight.Done()

	switch {
	case err == nil:
		return int(n), nil
	case errors.Is(err, windows.ERROR_OPERATION_ABORTED) && closed:
		return int(n), net.ErrClosed
	case errors.Is(err, windows.ERROR_OPERATION_ABORTED) && expired:
		return int(n), os.ErrDeadlineExceeded
	case errors.Is(err, windows.ERROR_BROKEN_PIPE), errors.Is(err, windows.ERROR_PIPE_NOT_CONNECTED),
		errors.Is(err, windows.ERROR_NO_DATA):
		// The other end closed: an end of file for a read, a broken pipe for a write.
		if dir == dirRead {
			return int(n), io.EOF
		}
		return int(n), syscall.EPIPE
	}
	return int(n), err
}

func (c *pipeConn) arm(op *pipeOp) {
	if op.timer != nil {
		op.timer.Stop()
		op.timer = nil
	}
	deadline := c.deadline[op.dir]
	if deadline.IsZero() {
		return
	}
	op.timer = time.AfterFunc(time.Until(deadline), func() { c.expire(op) })
}

func (c *pipeConn) expire(op *pipeOp) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, live := c.ops[op]; !live {
		return
	}
	deadline := c.deadline[op.dir]
	if deadline.IsZero() || time.Now().Before(deadline) {
		return
	}
	op.expired = true
	_ = windows.CancelIoEx(c.h, &op.ov)
}

func (c *pipeConn) setDeadline(t time.Time, dirs ...int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return net.ErrClosed
	}
	for _, dir := range dirs {
		c.deadline[dir] = t
	}
	for op := range c.ops {
		for _, dir := range dirs {
			if op.dir == dir {
				c.arm(op)
			}
		}
	}
	return nil
}

func (c *pipeConn) SetDeadline(t time.Time) error      { return c.setDeadline(t, dirRead, dirWrite) }
func (c *pipeConn) SetReadDeadline(t time.Time) error  { return c.setDeadline(t, dirRead) }
func (c *pipeConn) SetWriteDeadline(t time.Time) error { return c.setDeadline(t, dirWrite) }

func (c *pipeConn) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	_ = windows.CancelIoEx(c.h, nil)
	c.mu.Unlock()
	c.inflight.Wait()
	return windows.CloseHandle(c.h)
}

func (c *pipeConn) LocalAddr() net.Addr  { return pipeAddr(c.path) }
func (c *pipeConn) RemoteAddr() net.Addr { return pipeAddr(c.path) }
