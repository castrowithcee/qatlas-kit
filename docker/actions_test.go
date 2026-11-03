//go:build linux || darwin

package docker

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-kit/output"
)

type recorded struct {
	mu      sync.Mutex
	methods []string
	queries []string
}

func (r *recorded) handler(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		r.methods = append(r.methods, req.Method)
		r.queries = append(r.queries, req.URL.RawQuery)
		r.mu.Unlock()
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

var actionCalls = map[string]func(*Client, context.Context, string, time.Duration) error{
	"start":   func(c *Client, ctx context.Context, ref string, _ time.Duration) error { return c.Start(ctx, ref) },
	"stop":    func(c *Client, ctx context.Context, ref string, g time.Duration) error { return c.Stop(ctx, ref, g) },
	"restart": func(c *Client, ctx context.Context, ref string, g time.Duration) error { return c.Restart(ctx, ref, g) },
}

func TestActionsRequestShape(t *testing.T) {
	for verb, call := range actionCalls {
		rec := &recorded{}
		c := newFake(t, map[string]http.HandlerFunc{"/containers/web-1/" + verb: rec.handler(http.StatusNoContent, "")}).client(t)
		if err := call(c, context.Background(), "/web-1", 0); err != nil {
			t.Fatalf("%s: %v", verb, err)
		}
		if verb != "start" {
			if err := call(c, context.Background(), "web-1", 1500*time.Millisecond); err != nil {
				t.Fatalf("%s: %v", verb, err)
			}
			if err := call(c, context.Background(), "web-1", time.Nanosecond); err != nil {
				t.Fatalf("%s: %v", verb, err)
			}
		}
		for _, m := range rec.methods {
			if m != http.MethodPost {
				t.Errorf("%s: method = %s", verb, m)
			}
		}
		want := []string{""}
		if verb != "start" {
			want = []string{"", "t=2", "t=1"}
		}
		if strings.Join(rec.queries, "|") != strings.Join(want, "|") {
			t.Errorf("%s: queries = %q, want %q", verb, rec.queries, want)
		}
	}
}

func TestActionsNotModified(t *testing.T) {
	for _, verb := range []string{"start", "stop"} {
		rec := &recorded{}
		c := newFake(t, map[string]http.HandlerFunc{"/containers/web/" + verb: rec.handler(http.StatusNotModified, "")}).client(t)
		if err := actionCalls[verb](c, context.Background(), "web", 0); err != nil {
			t.Errorf("%s: 304 = %v, want success", verb, err)
		}
	}
	// A restart always acts, so 304 is not a valid answer.
	rec := &recorded{}
	c := newFake(t, map[string]http.HandlerFunc{"/containers/web/restart": rec.handler(http.StatusNotModified, "")}).client(t)
	asError(t, c.Restart(context.Background(), "web", 0), output.CodeRuntime)
}

func TestActionsFailures(t *testing.T) {
	for verb, call := range actionCalls {
		rec := &recorded{}
		c := newFake(t, map[string]http.HandlerFunc{
			"/containers/boom/" + verb: rec.handler(http.StatusInternalServerError, "internal "+secretValue),
		}).client(t)
		asError(t, call(c, context.Background(), "missing", 0), output.CodeNotFound)
		asError(t, call(c, context.Background(), "boom", 0), output.CodeRuntime)
	}
}

func TestActionsRefusalsSendNothing(t *testing.T) {
	f := newFake(t, map[string]http.HandlerFunc{})
	c := f.client(t)
	for verb, call := range actionCalls {
		for _, ref := range []string{"", "../x", "a/b", "-x", "a b", "a?b", "x\n", strings.Repeat("a", maxRefLength+1)} {
			err := call(c, context.Background(), ref, 0)
			if err == nil {
				t.Errorf("%s(%q) accepted", verb, ref)
				continue
			}
			asError(t, err, output.CodeInvalidRequest)
		}
	}
	for _, g := range []time.Duration{-1, -time.Second, MaxGrace + 1} {
		asError(t, c.Stop(context.Background(), "web", g), output.CodeInvalidRequest)
		asError(t, c.Restart(context.Background(), "web", g), output.CodeInvalidRequest)
	}
	if got := f.pings.Load(); got != 0 {
		t.Errorf("a refused call reached the server (%d pings)", got)
	}
}

func TestStopGraceExceedsClientTimeout(t *testing.T) {
	f := newFake(t, map[string]http.HandlerFunc{"/containers/web/stop": func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(600 * time.Millisecond)
		w.WriteHeader(http.StatusNoContent)
	}})
	c, err := New(Options{Socket: f.socket, Timeout: 200 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	if err := c.Stop(context.Background(), "web", 2*time.Second); err != nil {
		t.Fatalf("Stop with a long grace: %v", err)
	}
}

func TestStopIsBoundedByTimeoutPlusGrace(t *testing.T) {
	var f *fake
	f = newFake(t, map[string]http.HandlerFunc{"/containers/web/stop": func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-f.release:
		}
	}})
	c, err := New(Options{Socket: f.socket, Timeout: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	start := time.Now()
	err = c.Stop(context.Background(), "web", time.Second)
	e := asError(t, err, output.CodeTimeout)
	if !errors.Is(e, context.DeadlineExceeded) {
		t.Errorf("cause = %v", e.Unwrap())
	}
	if d := time.Since(start); d < time.Second {
		t.Errorf("gave up after %v, before the grace", d)
	}
}

func TestActionCancelledContext(t *testing.T) {
	var f *fake
	f = newFake(t, map[string]http.HandlerFunc{"/containers/web/stop": func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-f.release:
		}
	}})
	c := f.client(t)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	e := asError(t, c.Stop(ctx, "web", 5*time.Second), output.CodeRuntime)
	if !errors.Is(e, context.Canceled) {
		t.Errorf("cause = %v", e.Unwrap())
	}
}
