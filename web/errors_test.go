package web

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestErrorPageNamesOnlyTheStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	Error(rec, http.StatusNotFound)
	body := rec.Body.String()
	for _, want := range []string{`<html lang="en">`, `<meta charset="utf-8">`, `name="viewport"`,
		"<title>404 Not Found</title>", "<main>", "<h1>Not Found</h1>"} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q:\n%s", want, body)
		}
	}
	if rec.Code != http.StatusNotFound || rec.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("status %d type %q", rec.Code, rec.Header().Get("Content-Type"))
	}

	rec = httptest.NewRecorder()
	Error(rec, http.StatusOK)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status 200 became %d", rec.Code)
	}
}

func TestUnknownPathAndMethodGetErrorPages(t *testing.T) {
	s := newServer(t, Options{Login: LoginOff, Routes: []Route{{Pattern: "GET /items", Handler: noContent}}})

	rec := do(s, get("/hidden-path-example"))
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "<h1>Not Found</h1>") ||
		strings.Contains(rec.Body.String(), "hidden-path-example") {
		t.Errorf("unknown path: status %d body %q", rec.Code, rec.Body.String())
	}

	// A handler that changes state is registered for POST and is not reached by GET.
	rec = do(s, httptest.NewRequest(http.MethodPost, "http://localhost/items", nil))
	if rec.Code != http.StatusMethodNotAllowed || !strings.Contains(rec.Body.String(), "<h1>Method Not Allowed</h1>") ||
		rec.Header().Get("Allow") == "" {
		t.Errorf("wrong method: status %d allow %q body %q", rec.Code, rec.Header().Get("Allow"), rec.Body.String())
	}
}

func TestPathRedirectsOfTheMuxPass(t *testing.T) {
	s := newServer(t, Options{Login: LoginOff, Routes: []Route{{Pattern: "GET /a/", Handler: noContent}}})
	for path, want := range map[string]string{"/a": "/a/", "/x/../a/": "/a/", "/x/../y": "/y"} {
		rec := do(s, get(path))
		if rec.Code < 300 || rec.Code > 399 || rec.Header().Get("Location") != want {
			t.Errorf("%s: status %d location %q, want a redirect to %q", path, rec.Code, rec.Header().Get("Location"), want)
		}
	}
}

func TestRedirectStaysOnTheServer(t *testing.T) {
	rec := httptest.NewRecorder()
	if err := Redirect(rec, get("/"), "/items?page=2", http.StatusSeeOther); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/items?page=2" {
		t.Fatalf("status %d location %q", rec.Code, rec.Header().Get("Location"))
	}

	for _, target := range []string{"//evil.example.test/", "https://evil.example.test/", `/\evil.example.test`,
		"/\t/evil.example.test", "/\n/evil.example.test", "items", "", "javascript:alert(1)", "?next=/"} {
		rec := httptest.NewRecorder()
		if err := Redirect(rec, get("/"), target, http.StatusFound); !errors.Is(err, ErrRedirectTarget) {
			t.Errorf("target %q: %v", target, err)
		}
		if rec.Header().Get("Location") != "" || rec.Body.Len() != 0 {
			t.Errorf("target %q: wrote a response", target)
		}
	}
	if err := Redirect(httptest.NewRecorder(), get("/"), "/items", http.StatusOK); err == nil {
		t.Error("status 200 accepted as a redirect")
	}
}
