//go:build linux || darwin

package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-kit/output"
)

const secretEnv = "EXAMPLE_TOKEN=invented-secret-value"

const secretValue = "invented-secret-value"

type fake struct {
	socket  string
	version string // Api-Version of the ping; "-" omits the header
	pings   atomic.Int32
	routes  map[string]http.HandlerFunc
	release chan struct{}
	// pingEntered, if set, receives a value when a ping arrives, which then waits for release.
	pingEntered chan struct{}
}

// newFake serves routes (keyed by the path below /v<version>) on a socket of its own.
func newFake(t *testing.T, routes map[string]http.HandlerFunc) *fake {
	t.Helper()
	// A short path: the socket path limit is 104 bytes on macOS.
	dir, err := os.MkdirTemp("", "dk")
	if err != nil {
		t.Fatal(err)
	}
	f := &fake{socket: filepath.Join(dir, "s"), version: "1.45", routes: routes, release: make(chan struct{})}
	l, err := net.Listen("unix", f.socket)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/_ping", func(w http.ResponseWriter, r *http.Request) {
		f.pings.Add(1)
		if f.pingEntered != nil {
			f.pingEntered <- struct{}{}
			select {
			case <-r.Context().Done():
			case <-f.release:
			}
		}
		if f.version != "-" {
			w.Header().Set("Api-Version", f.version)
		}
		_, _ = w.Write([]byte("OK"))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_, rest, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/v"), "/")
		h, ok := f.routes["/"+rest]
		if !ok {
			http.NotFound(w, r)
			return
		}
		h(w, r)
	})
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(func() {
		close(f.release)
		_ = srv.Close()
		_ = os.RemoveAll(dir)
	})
	return f
}

func (f *fake) client(t *testing.T) *Client {
	t.Helper()
	c, err := New(Options{Socket: f.socket, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}

func jsonHandler(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }
}

const listBody = `[
 {"Id":"aaaaaaaaaaaa","Names":["/shop-web-1","/other/alias"],"Image":"example/web:1","State":"running","Status":"Up 2 hours","Labels":{"com.docker.compose.project":"shop","com.docker.compose.project.working_dir":"/srv/shop"}},
 {"Id":"bbbbbbbbbbbb","Names":["/loose"],"Image":"example/x","State":"exited","Status":"Exited (0) 1 hour ago","Labels":{}},
 {"Id":"cccccccccccc","Names":["/gone"],"Image":"example/y","State":"running","Status":"Up","Labels":{}}
]`

const inspectBody = `{"Id":"aaaaaaaaaaaa","Name":"/shop-web-1","Image":"sha256:1","RestartCount":3,
 "State":{"Status":"running","StartedAt":"2026-01-02T03:04:05.5Z","Health":{"Status":"healthy"}},
 "Config":{"Image":"example/web:1","Env":["EXAMPLE_TOKEN=invented-secret-value","PLAIN=1","EMPTY="],"Labels":{"k":"v"}},
 "Mounts":[{"Type":"bind","Source":"/srv/shop/data","Destination":"/data","RW":false},{"Type":"volume","Name":"vol1","Source":"/var/lib/docker/volumes/vol1/_data","Destination":"/v","RW":true}],
 "NetworkSettings":{"Ports":{"80/tcp":[{"HostIp":"0.0.0.0","HostPort":"8080"}],"53/udp":null},"Networks":{"shop_default":{"IPAddress":"172.18.0.2"}}}}`

func shopRoutes() map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"/containers/json":              jsonHandler(listBody),
		"/containers/aaaaaaaaaaaa/json": jsonHandler(inspectBody),
		"/containers/shop-web-1/json":   jsonHandler(inspectBody),
		"/containers/bbbbbbbbbbbb/json": jsonHandler(`{"Id":"bbbbbbbbbbbb","Name":"/loose","RestartCount":0,"State":{"Status":"exited","StartedAt":"0001-01-01T00:00:00Z"},"Config":{"Env":null},"Mounts":[]}`),
		// cccccccccccc is not routed: it vanished after the list.
	}
}

func asError(t *testing.T, err error, code output.Code) *Error {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("error = %v, want *Error", err)
	}
	if e.Code != code {
		t.Fatalf("code = %q (%v), want %q", e.Code, e, code)
	}
	if strings.Contains(e.Error(), secretValue) {
		t.Fatalf("message leaks a value: %q", e.Error())
	}
	return e
}

func TestListInspectGroupUsingPath(t *testing.T) {
	f := newFake(t, shopRoutes())
	c := f.client(t)
	ctx := context.Background()

	cs, err := c.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 2 || cs[0].Name != "loose" || cs[1].Name != "shop-web-1" {
		t.Fatalf("List() = %+v", cs)
	}
	web := cs[1]
	if web.Health != "healthy" || web.RestartCount != 3 || web.State != "running" || web.Status != "Up 2 hours" ||
		web.Image != "example/web:1" || web.StartedAt.Year() != 2026 || web.Labels[labelProject] != "shop" {
		t.Errorf("web = %+v", web)
	}
	if !cs[0].StartedAt.IsZero() || cs[0].Health != "" {
		t.Errorf("loose = %+v", cs[0])
	}
	projects, loose := Group(cs)
	if len(projects) != 1 || projects[0].Name != "shop" || len(loose) != 1 {
		t.Errorf("Group() = %+v, %+v", projects, loose)
	}

	byName, err := c.Inspect(ctx, "/shop-web-1")
	if err != nil || byName.ID != "aaaaaaaaaaaa" {
		t.Fatalf("Inspect by name = %+v, %v", byName, err)
	}
	d, err := c.Inspect(ctx, "aaaaaaaaaaaa")
	if err != nil {
		t.Fatal(err)
	}
	if d.Name != "shop-web-1" || len(d.Ports) != 2 || d.Ports[0] != (Port{PrivatePort: 53, Protocol: "udp"}) ||
		d.Ports[1] != (Port{PrivatePort: 80, PublicPort: 8080, Protocol: "tcp", HostIP: "0.0.0.0"}) {
		t.Errorf("details = %+v", d)
	}
	if len(d.Mounts) != 2 || !d.Mounts[0].ReadOnly || d.Mounts[1].ReadOnly || d.Mounts[1].Name != "vol1" ||
		len(d.Networks) != 1 || d.Networks[0].IPAddress != "172.18.0.2" {
		t.Errorf("details = %+v", d)
	}
	if want := "EMPTY PLAIN EXAMPLE_TOKEN"; len(d.EnvNames) != 3 || strings.Join(d.EnvNames, " ") != "EMPTY EXAMPLE_TOKEN PLAIN" {
		t.Errorf("EnvNames = %v, want %s sorted", d.EnvNames, want)
	}

	used, err := c.UsingPath(ctx, "/srv/shop")
	if err != nil || len(used) != 1 || used[0].Name != "shop-web-1" {
		t.Fatalf("UsingPath = %+v, %v", used, err)
	}
	used, err = c.UsingPath(ctx, "/srv/sho")
	if err != nil || len(used) != 0 {
		t.Fatalf("UsingPath(/srv/sho) = %+v, %v", used, err)
	}
	_, err = c.UsingPath(ctx, "srv/shop")
	asError(t, err, output.CodeInvalidRequest)
}

func TestEnvValuesNeverAppear(t *testing.T) {
	c := newFake(t, shopRoutes()).client(t)
	d, err := c.Inspect(context.Background(), "aaaaaaaaaaaa")
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(d)
	for name, text := range map[string]string{"%+v": fmt.Sprintf("%+v", d), "%#v": fmt.Sprintf("%#v", d), "json": string(encoded)} {
		if strings.Contains(text, secretValue) {
			t.Errorf("%s output holds the value: %s", name, text)
		}
	}

	broken := map[string]string{
		"truncated": `{"Config":{"Env":["` + secretEnv + `"]},"State":`,
		"too large": `{"Config":{"Env":["` + secretEnv + `"],"Labels":{"pad":"` + strings.Repeat("x", inspectLimit) + `"}}}`,
		"bad entry": `{"Config":{"Env":["` + secretEnv + `", 7]}}`,
	}
	for name, body := range broken {
		c := newFake(t, map[string]http.HandlerFunc{"/containers/abc/json": jsonHandler(body)}).client(t)
		_, err := c.Inspect(context.Background(), "abc")
		if err == nil {
			t.Errorf("%s: no error", name)
			continue
		}
		asError(t, err, output.CodeRuntime)
	}
}

func TestOversizedListIsRefused(t *testing.T) {
	body := `[{"Id":"aaaaaaaaaaaa","Labels":{"pad":"` + strings.Repeat("x", listLimit) + `"}}]`
	c := newFake(t, map[string]http.HandlerFunc{"/containers/json": jsonHandler(body)}).client(t)
	_, err := c.List(context.Background())
	asError(t, err, output.CodeRuntime)
}

func TestRedirectIsNotFollowed(t *testing.T) {
	var followed atomic.Bool
	c := newFake(t, map[string]http.HandlerFunc{
		"/containers/json": func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/v1.45/elsewhere", http.StatusFound)
		},
		"/elsewhere": func(http.ResponseWriter, *http.Request) { followed.Store(true) },
	}).client(t)
	_, err := c.List(context.Background())
	asError(t, err, output.CodeRuntime)
	if followed.Load() {
		t.Error("the redirect was followed")
	}
}

func TestUnreachableSocket(t *testing.T) {
	dir, err := os.MkdirTemp("", "dk")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	c, err := New(Options{Socket: filepath.Join(dir, "none")})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.List(context.Background())
	asError(t, err, output.CodeUnreachable)
}

func TestPermissionDenied(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores socket permissions")
	}
	f := newFake(t, shopRoutes())
	if err := os.Chmod(f.socket, 0); err != nil {
		t.Fatal(err)
	}
	_, err := f.client(t).List(context.Background())
	e := asError(t, err, output.CodePermission)
	if !errors.Is(e, os.ErrPermission) {
		t.Errorf("cause = %v", e.Unwrap())
	}
}

func TestVersionNegotiation(t *testing.T) {
	tests := []struct {
		version string
		code    output.Code
		sent    string
	}{
		{"1.40", output.CodeRuntime, ""},
		{"-", output.CodeRuntime, ""},
		{"abc", output.CodeRuntime, ""},
		{"1.41", "", "1.41"},
		{"1.45", "", "1.45"},
		{"1.99", "", MaxAPIVersion},
	}
	for _, tt := range tests {
		var path atomic.Value
		f := newFake(t, nil)
		f.version = tt.version
		f.routes = map[string]http.HandlerFunc{"/containers/json": func(w http.ResponseWriter, r *http.Request) {
			path.Store(r.URL.Path)
			_, _ = w.Write([]byte("[]"))
		}}
		_, err := f.client(t).List(context.Background())
		if tt.code != "" {
			e := asError(t, err, tt.code)
			if tt.version == "1.40" && !errors.Is(e, ErrUnsupportedAPIVersion) {
				t.Errorf("1.40: cause = %v", e.Unwrap())
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: %v", tt.version, err)
		}
		if got, want := path.Load(), "/v"+tt.sent+"/containers/json"; got != want {
			t.Errorf("%s: path = %v, want %s", tt.version, got, want)
		}
	}
}

func TestVersionIsCachedOnlyOnSuccess(t *testing.T) {
	f := newFake(t, map[string]http.HandlerFunc{"/containers/json": jsonHandler("[]")})
	c := f.client(t)
	f.version = "1.30"
	if _, err := c.List(context.Background()); err == nil {
		t.Fatal("old version accepted")
	}
	f.version = "1.45"
	for range 3 {
		if _, err := c.List(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if got := f.pings.Load(); got != 2 {
		t.Errorf("pings = %d, want 2 (one failed, one cached)", got)
	}
}

func TestConcurrentCallsPingOnce(t *testing.T) {
	f := newFake(t, shopRoutes())
	c := f.client(t)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.List(context.Background()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if got := f.pings.Load(); got != 1 {
		t.Errorf("pings = %d, want 1", got)
	}
}

func TestCancelledContext(t *testing.T) {
	var f *fake
	f = newFake(t, map[string]http.HandlerFunc{"/containers/json": func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-f.release:
		}
	}})
	c := f.client(t)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	_, err := c.List(ctx)
	e := asError(t, err, output.CodeRuntime)
	if !errors.Is(e, context.Canceled) {
		t.Errorf("cause = %v", e.Unwrap())
	}
}

func TestDeadlineIsTimeout(t *testing.T) {
	var f *fake
	f = newFake(t, map[string]http.HandlerFunc{"/containers/json": func(_ http.ResponseWriter, r *http.Request) {
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
	_, err = c.List(context.Background())
	e := asError(t, err, output.CodeTimeout)
	if !errors.Is(e, context.DeadlineExceeded) {
		t.Errorf("cause = %v", e.Unwrap())
	}
}

func TestInvalidReferencesAreRefusedBeforeRequest(t *testing.T) {
	f := newFake(t, shopRoutes())
	c := f.client(t)
	for _, ref := range []string{"", "../x", "a/b", "/", "//x", "-x", ".x", "a b", "a?b", "a%2Fb", "x\n", strings.Repeat("a", maxRefLength+1)} {
		if _, err := c.Inspect(context.Background(), ref); err == nil {
			t.Errorf("Inspect(%q) accepted", ref)
		} else {
			asError(t, err, output.CodeInvalidRequest)
		}
		if _, err := c.Stats(context.Background(), ref); err == nil {
			t.Errorf("Stats(%q) accepted", ref)
		}
	}
	if got := f.pings.Load(); got != 0 {
		t.Errorf("a refused reference reached the server (%d pings)", got)
	}
}

func TestNotFoundAndServerError(t *testing.T) {
	c := newFake(t, map[string]http.HandlerFunc{
		"/containers/boom/json": func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "internal secret "+secretValue, http.StatusInternalServerError)
		},
	}).client(t)
	_, err := c.Inspect(context.Background(), "missing")
	asError(t, err, output.CodeNotFound)
	_, err = c.Inspect(context.Background(), "boom")
	asError(t, err, output.CodeRuntime)
}

func TestStats(t *testing.T) {
	var query atomic.Value
	c := newFake(t, map[string]http.HandlerFunc{"/containers/abc/stats": func(w http.ResponseWriter, r *http.Request) {
		query.Store(r.URL.RawQuery)
		_, _ = w.Write([]byte(`{"cpu_stats":{"cpu_usage":{"total_usage":300},"system_cpu_usage":1200,"online_cpus":2},
 "precpu_stats":{"cpu_usage":{"total_usage":100},"system_cpu_usage":200},
 "memory_stats":{"usage":1000,"limit":8000,"stats":{"inactive_file":400}}}`))
	}}).client(t)
	s, err := c.Stats(context.Background(), "abc")
	if err != nil {
		t.Fatal(err)
	}
	if s.CPUPercent != 40 || s.MemoryUsed != 600 || s.MemoryLimit != 8000 {
		t.Errorf("Stats = %+v", s)
	}
	if query.Load() != "stream=false" {
		t.Errorf("query = %v", query.Load())
	}
}

func TestNewRefusesBadOptions(t *testing.T) {
	for _, o := range []Options{{}, {Socket: "docker.sock"}, {Socket: "/x", Timeout: -time.Second}} {
		_, err := New(o)
		asError(t, err, output.CodeConfigInvalid)
	}
}

func TestWaiterForPingHonorsItsContext(t *testing.T) {
	f := newFake(t, shopRoutes())
	f.pingEntered = make(chan struct{}, 1)
	c := f.client(t)
	first := make(chan error, 1)
	go func() { _, err := c.List(context.Background()); first <- err }()
	<-f.pingEntered

	ctx, stop := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer stop()
	begin := time.Now()
	_, err := c.List(ctx)
	asError(t, err, output.CodeTimeout)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	_, err = c.List(ctx)
	if e := asError(t, err, output.CodeRuntime); !errors.Is(e, context.Canceled) {
		t.Errorf("cause = %v", e.Unwrap())
	}
	if time.Since(begin) > 2*time.Second {
		t.Error("waiters were held back by the first ping")
	}
	select {
	case err := <-first:
		t.Fatalf("first caller returned early: %v", err)
	default:
	}
}
