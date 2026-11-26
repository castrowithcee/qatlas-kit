package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-kit/redact"
)

func codeRequest(code string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "http://localhost/in", strings.NewReader(url.Values{CodeField: {code}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return r
}

func verify(t *testing.T, o *OnceCode, r *http.Request) bool {
	t.Helper()
	ok, err := o.Verify(r)
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

func TestOnceCodeIsUsedOnce(t *testing.T) {
	o := NewOnceCode(OnceCodeOptions{})
	code := o.Generate()
	if len(code) != 9 || code[4] != '-' {
		t.Fatalf("code form %q", code)
	}
	for _, c := range strings.ReplaceAll(code, "-", "") {
		if !strings.ContainsRune(codeAlphabet, c) {
			t.Fatalf("symbol %q outside the alphabet", c)
		}
	}
	if verify(t, o, codeRequest("2222-2222")) && code != "2222-2222" {
		t.Fatal("wrong code accepted")
	}
	if !verify(t, o, codeRequest(code)) {
		t.Fatal("a wrong guess consumed the code")
	}
	if verify(t, o, codeRequest(code)) {
		t.Fatal("code accepted twice")
	}
}

func TestOnceCodeNormalisesInput(t *testing.T) {
	o := NewOnceCode(OnceCodeOptions{})
	code := o.Generate()
	typed := " " + strings.ToLower(strings.ReplaceAll(code, "-", "")) + " "
	if !verify(t, o, codeRequest(typed)) {
		t.Errorf("%q refused", typed)
	}
}

func TestOnceCodeExpires(t *testing.T) {
	clock := &testClock{t: time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)}
	o := NewOnceCode(OnceCodeOptions{})
	o.now = clock.now
	code := o.Generate()
	clock.t = clock.t.Add(defaultCodeTTL)
	if verify(t, o, codeRequest(code)) {
		t.Error("expired code accepted")
	}
	// An expired or used code is not replaced by a request, only by the application.
	if verify(t, o, codeRequest(code)) {
		t.Error("expired code came back")
	}
	code = o.Generate()
	clock.t = clock.t.Add(defaultCodeTTL - time.Second)
	if !verify(t, o, codeRequest(code)) {
		t.Error("code refused inside its lifetime")
	}
}

func TestOnceCodeNewCodeEndsOld(t *testing.T) {
	o := NewOnceCode(OnceCodeOptions{})
	first := o.Generate()
	second := o.Generate()
	if first == second {
		t.Fatal("same code twice")
	}
	if verify(t, o, codeRequest(first)) {
		t.Error("old code still valid")
	}
	if !verify(t, o, codeRequest(second)) {
		t.Error("new code refused")
	}
}

func TestOnceCodeNeverFromURLOrWithoutCode(t *testing.T) {
	o := NewOnceCode(OnceCodeOptions{})
	if verify(t, o, codeRequest("")) {
		t.Error("empty code accepted before any Generate")
	}
	code := o.Generate()
	r := httptest.NewRequest(http.MethodPost, "http://localhost/in?code="+code, nil)
	if verify(t, o, r) {
		t.Error("code from the URL accepted")
	}
	if verify(t, o, codeRequest("")) {
		t.Error("empty code accepted")
	}
}

func TestOnceCodeGoesToRedactor(t *testing.T) {
	var red redact.Redactor
	o := NewOnceCode(OnceCodeOptions{Redactor: &red})
	code := o.Generate()
	for _, s := range []string{code, strings.ReplaceAll(code, "-", "")} {
		if got := red.Apply("code " + s); strings.Contains(got, s) {
			t.Errorf("%q not redacted: %q", s, got)
		}
	}
}
