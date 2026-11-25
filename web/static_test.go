package web

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func staticFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html":      {Data: []byte("<h1>home</h1>")},
		"assets/app.js":   {Data: []byte("console.log(1)")},
		"assets/APP.CSS":  {Data: []byte("a{}")},
		"assets/data.xyz": {Data: []byte("<script>alert(1)</script>")},
		"docs/index.html": {Data: []byte("<h1>docs</h1>")},
		".hidden/secret":  {Data: []byte("hidden")},
		".env":            {Data: []byte("hidden")},
		"assets/.dotfile": {Data: []byte("hidden")},
		"assets/noext":    {Data: []byte("x")},
		"assets/icon.SVG": {Data: []byte("<svg/>")},
	}
}

func staticServer(t *testing.T, opts Options) *Server {
	t.Helper()
	h, err := Static(staticFS())
	if err != nil {
		t.Fatal(err)
	}
	opts.Routes = append(opts.Routes, Route{Pattern: "GET /", Handler: h, Public: true})
	return newServer(t, opts)
}

func staticGet(s *Server, method, p string, hdr ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, p, nil)
	r.Host = "localhost"
	for i := 0; i+1 < len(hdr); i += 2 {
		r.Header.Set(hdr[i], hdr[i+1])
	}
	return do(s, r)
}

func TestStaticServesWithFixedHeaders(t *testing.T) {
	s := staticServer(t, Options{})
	for _, tc := range []struct{ path, ctype, body string }{
		{"/", "text/html; charset=utf-8", "<h1>home</h1>"},
		{"/index.html", "text/html; charset=utf-8", "<h1>home</h1>"},
		{"/assets/app.js", "text/javascript; charset=utf-8", "console.log(1)"},
		{"/assets/APP.CSS", "text/css; charset=utf-8", "a{}"},
		{"/assets/icon.SVG", "image/svg+xml", "<svg/>"},
		{"/assets/data.xyz", "application/octet-stream", "<script>alert(1)</script>"},
		{"/assets/noext", "application/octet-stream", "x"},
	} {
		rec := staticGet(s, "GET", tc.path)
		h := rec.Header()
		if rec.Code != 200 || rec.Body.String() != tc.body || h.Get("Content-Type") != tc.ctype {
			t.Errorf("%s: %d %q %q", tc.path, rec.Code, h.Get("Content-Type"), rec.Body)
		}
		if h.Get("X-Content-Type-Options") != "nosniff" || !strings.Contains(h.Get("Content-Security-Policy"), "nonce-") ||
			h.Get("Cross-Origin-Resource-Policy") != "same-origin" || h.Get("Cache-Control") != "no-cache" {
			t.Errorf("%s: headers %v", tc.path, h)
		}
	}
}

func TestStaticRefusesWhatIsNotAFile(t *testing.T) {
	s := staticServer(t, Options{})
	for _, p := range []string{
		"/assets", "/assets/", "/docs", "/docs/", "/missing", "/index.html/", "/.env", "/.hidden/secret",
		"/assets/.dotfile", "/%2e%2e/go.mod", "/assets/%2e%2e/%2e%2e/etc/passwd", "/assets/..%2f..%2fx",
		"/%2Fetc/passwd", "//etc/passwd", "/assets%5C..%5Capp.js", "/assets/app.js%00", "/assets/app.js%00.png",
		"/C:/Windows/win.ini", "/..", "/../index.html",
	} {
		rec := staticGet(s, "GET", p)
		loc := rec.Header().Get("Location")
		redirectOut := rec.Code >= 300 && rec.Code < 400 && (!strings.HasPrefix(loc, "/") || strings.HasPrefix(loc, "//"))
		if rec.Code == 200 || redirectOut {
			t.Errorf("%s: %d location %q", p, rec.Code, loc)
		}
		if rec.Code >= 400 && (strings.Contains(rec.Body.String(), "passwd") || strings.Contains(rec.Body.String(), "assets") ||
			!strings.Contains(rec.Body.String(), "<h1>")) {
			t.Errorf("%s: error page %q", p, rec.Body)
		}
		if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s: no nosniff", p)
		}
	}
	if rec := staticGet(s, "GET", "/missing"); rec.Code != 404 {
		t.Errorf("missing: %d", rec.Code)
	}
}

func TestStaticDirectHandlerNeverLeavesTheFiles(t *testing.T) {
	h, _ := Static(staticFS())
	// ServeMux cleans these paths first; the handler must hold even when it is reached without it.
	for _, p := range []string{"/../index.html", "/assets/../index.html", "assets/app.js", "/docs/", "/./index.html"} {
		r := httptest.NewRequest("GET", "/", nil)
		r.URL.Path = p
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != 404 {
			t.Errorf("%s: %d", p, rec.Code)
		}
	}
}

func TestStaticMethods(t *testing.T) {
	s := staticServer(t, Options{})
	rec := staticGet(s, "HEAD", "/assets/app.js")
	if rec.Code != 200 || rec.Body.Len() != 0 || rec.Header().Get("Content-Length") != "14" {
		t.Errorf("HEAD: %d %q %v", rec.Code, rec.Body, rec.Header())
	}
	h, _ := Static(staticFS())
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/", nil))
	if rec.Code != 405 || rec.Header().Get("Allow") != "GET, HEAD" {
		t.Errorf("POST: %d %v", rec.Code, rec.Header())
	}
	if rec := staticGet(s, "POST", "/"); rec.Code != 405 {
		t.Errorf("POST via server: %d", rec.Code)
	}
}

func TestStaticCachesByContent(t *testing.T) {
	s := staticServer(t, Options{})
	first := staticGet(s, "GET", "/assets/app.js")
	etag := first.Header().Get("ETag")
	if etag == "" || etag == staticGet(s, "GET", "/index.html").Header().Get("ETag") {
		t.Fatalf("etag %q", etag)
	}
	for _, inm := range []string{etag, "W/" + etag, `"x", ` + etag, "*"} {
		rec := staticGet(s, "GET", "/assets/app.js", "If-None-Match", inm)
		if rec.Code != 304 || rec.Body.Len() != 0 || rec.Header().Get("ETag") != etag ||
			rec.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("If-None-Match %q: %d %q", inm, rec.Code, rec.Body)
		}
	}
	if rec := staticGet(s, "GET", "/assets/app.js", "If-None-Match", `"other"`); rec.Code != 200 {
		t.Errorf("stale etag: %d", rec.Code)
	}
	// Same content under another name has the same tag; changed content does not.
	h2, _ := Static(fstest.MapFS{"assets/app.js": {Data: []byte("console.log(2)")}})
	rec := httptest.NewRecorder()
	h2.ServeHTTP(rec, httptest.NewRequest("GET", "/assets/app.js", nil))
	if rec.Header().Get("ETag") == etag {
		t.Error("etag did not change with content")
	}
}

func TestStaticIgnoresRange(t *testing.T) {
	s := staticServer(t, Options{})
	rec := staticGet(s, "GET", "/assets/app.js", "Range", "bytes=0-3")
	if rec.Code != 200 || rec.Body.String() != "console.log(1)" {
		t.Errorf("range: %d %q", rec.Code, rec.Body)
	}
}

func TestStaticReportsUnreadableFS(t *testing.T) {
	if _, err := Static(failFS{}); err == nil {
		t.Error("no error for an unreadable file system")
	}
}

type failFS struct{}

func (failFS) Open(string) (fs.File, error) { return nil, http.ErrMissingFile }
