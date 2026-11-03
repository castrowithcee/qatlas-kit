//go:build linux || darwin

package docker

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-kit/output"
)

func stateBody(name, state, health string) string {
	h := ""
	if health != "" {
		h = fmt.Sprintf(`,"Health":{"Status":%q}`, health)
	}
	return fmt.Sprintf(`{"Id":"x","Name":"/%s","State":{"Status":%q%s},"Config":{}}`, name, state, h)
}

// sequence answers each inspect with the next body and repeats the last one.
func sequence(bodies ...string) (http.HandlerFunc, *atomic.Int32) {
	var n atomic.Int32
	return func(w http.ResponseWriter, _ *http.Request) {
		i := int(n.Add(1)) - 1
		_, _ = w.Write([]byte(bodies[min(i, len(bodies)-1)]))
	}, &n
}

func waitClient(t *testing.T, routes map[string]http.HandlerFunc) *Client {
	c := newFake(t, routes).client(t)
	c.minWaitInterval = time.Millisecond
	return c
}

func TestWaitHealthyOutcomes(t *testing.T) {
	plain, _ := sequence(stateBody("plain", "running", ""))
	recovering, polls := sequence(stateBody("rec", "running", "starting"), stateBody("rec", "running", "starting"), stateBody("rec", "running", "healthy"))
	bad, _ := sequence(stateBody("bad", "running", "unhealthy"))
	slow, _ := sequence(stateBody("slow", "running", "starting"))
	restarting, _ := sequence(stateBody("rs", "restarting", ""))
	created, _ := sequence(stateBody("cr", "created", ""))
	exited, _ := sequence(stateBody("ex", "exited", ""))
	dead, _ := sequence(stateBody("de", "dead", ""))
	c := waitClient(t, map[string]http.HandlerFunc{
		"/containers/plain/json": plain, "/containers/rec/json": recovering, "/containers/bad/json": bad,
		"/containers/slow/json": slow, "/containers/rs/json": restarting, "/containers/cr/json": created,
		"/containers/ex/json": exited, "/containers/de/json": dead,
	})
	refs := []string{"slow", "plain", "missing", "ex", "rec", "bad", "rs", "cr", "de"}
	want := []WaitResult{
		{"slow", "slow", false, ReasonStarting},
		{"plain", "plain", true, ""},
		{"missing", "", false, ReasonNotFound},
		{"ex", "ex", false, ReasonExited},
		{"rec", "rec", true, ""},
		{"bad", "bad", false, ReasonUnhealthy},
		{"rs", "rs", false, ReasonRestarting},
		{"cr", "cr", false, ReasonNotRunning},
		{"de", "de", false, ReasonExited},
	}
	got, err := c.WaitHealthy(context.Background(), refs, WaitOptions{Deadline: 300 * time.Millisecond, Interval: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("got %d results", len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("result %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if n := polls.Load(); n != 3 {
		t.Errorf("a healthy container was polled %d times, want 3", n)
	}
}

func TestWaitHealthyFinishesEarly(t *testing.T) {
	up, _ := sequence(stateBody("a", "running", ""))
	gone, _ := sequence(stateBody("b", "exited", ""))
	c := waitClient(t, map[string]http.HandlerFunc{"/containers/a/json": up, "/containers/b/json": gone})
	start := time.Now()
	got, err := c.WaitHealthy(context.Background(), []string{"a", "b"}, WaitOptions{Deadline: 30 * time.Second, Interval: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Errorf("returned after %v, want well before the deadline", d)
	}
	if !got[0].OK || got[1].Reason != ReasonExited {
		t.Errorf("results = %+v", got)
	}
}

func TestWaitHealthyDoesNotPollOKAgain(t *testing.T) {
	up, upPolls := sequence(stateBody("a", "running", ""))
	late, _ := sequence(stateBody("b", "created", ""), stateBody("b", "created", ""), stateBody("b", "running", ""))
	c := waitClient(t, map[string]http.HandlerFunc{"/containers/a/json": up, "/containers/b/json": late})
	got, err := c.WaitHealthy(context.Background(), []string{"a", "b"}, WaitOptions{Deadline: 5 * time.Second, Interval: time.Millisecond})
	if err != nil || !got[0].OK || !got[1].OK {
		t.Fatalf("got %+v, %v", got, err)
	}
	if n := upPolls.Load(); n != 1 {
		t.Errorf("OK container polled %d times", n)
	}
}

func TestWaitHealthyRespectsDeadline(t *testing.T) {
	slow, _ := sequence(stateBody("s", "running", "starting"))
	c := waitClient(t, map[string]http.HandlerFunc{"/containers/s/json": slow})
	start := time.Now()
	got, err := c.WaitHealthy(context.Background(), []string{"s"}, WaitOptions{Deadline: 200 * time.Millisecond, Interval: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d < 200*time.Millisecond || d > 5*time.Second {
		t.Errorf("waited %v", d)
	}
	if got[0].OK || got[0].Reason != ReasonStarting {
		t.Errorf("got %+v", got[0])
	}
}

func TestWaitHealthyContextCancel(t *testing.T) {
	slow, _ := sequence(stateBody("s", "running", "starting"))
	c := waitClient(t, map[string]http.HandlerFunc{"/containers/s/json": slow})
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	got, err := c.WaitHealthy(ctx, []string{"s"}, WaitOptions{Deadline: time.Minute, Interval: 10 * time.Millisecond})
	if got != nil {
		t.Errorf("results = %+v", got)
	}
	e := asError(t, err, output.CodeRuntime)
	if !errors.Is(e, context.Canceled) {
		t.Errorf("cause = %v", e.Unwrap())
	}
}

func TestWaitHealthyEngineFailureIsError(t *testing.T) {
	c := waitClient(t, map[string]http.HandlerFunc{"/containers/boom/json": func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, secretValue, http.StatusInternalServerError)
	}})
	got, err := c.WaitHealthy(context.Background(), []string{"boom"}, WaitOptions{Deadline: time.Second})
	if got != nil {
		t.Errorf("results = %+v", got)
	}
	asError(t, err, output.CodeRuntime)
}

func TestWaitHealthyRefusals(t *testing.T) {
	f := newFake(t, map[string]http.HandlerFunc{})
	c := f.client(t)
	many := make([]string, MaxContainers+1)
	for i := range many {
		many[i] = fmt.Sprintf("c%d", i)
	}
	cases := map[string]struct {
		refs []string
		opts WaitOptions
	}{
		"nil refs":          {nil, WaitOptions{}},
		"empty refs":        {[]string{}, WaitOptions{}},
		"invalid ref":       {[]string{"ok", "../x"}, WaitOptions{}},
		"duplicate":         {[]string{"web", "web"}, WaitOptions{}},
		"duplicate, slash":  {[]string{"web", "/web"}, WaitOptions{}},
		"too many":          {many, WaitOptions{}},
		"negative deadline": {[]string{"web"}, WaitOptions{Deadline: -1}},
		"negative interval": {[]string{"web"}, WaitOptions{Interval: -time.Second}},
	}
	for name, tc := range cases {
		got, err := c.WaitHealthy(context.Background(), tc.refs, tc.opts)
		if err == nil || got != nil {
			t.Errorf("%s: accepted", name)
			continue
		}
		asError(t, err, output.CodeInvalidRequest)
	}
	if got := f.pings.Load(); got != 0 {
		t.Errorf("a refused call reached the server (%d pings)", got)
	}
}
