package httpx_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-kit/httpx"
)

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("broken") }

func TestClassifyStatus(t *testing.T) {
	texts := httpx.StatusTexts{Subject: "Acme", Auth: "auth text", Permission: "permission text", NotFound: "missing text"}
	redirect := "Acme answered with a redirect, which is not followed for this request"
	tests := []struct {
		status  int
		class   httpx.Class
		message string
	}{
		{401, httpx.ClassAuth, "auth text"},
		{403, httpx.ClassPermission, "permission text"},
		{404, httpx.ClassNotFound, "missing text"},
		{429, httpx.ClassRateLimited, "Acme rate-limited the operation"},
		{503, httpx.ClassUnreachable, "Acme is unavailable or in maintenance"},
		{504, httpx.ClassTimeout, "Acme did not answer in time"},
		{301, httpx.ClassRemoteError, redirect},
		{399, httpx.ClassRemoteError, redirect},
		{400, httpx.ClassRemoteError, "Acme rejected the operation (HTTP 400)"},
		{500, httpx.ClassRemoteError, "Acme rejected the operation (HTTP 500)"},
		{502, httpx.ClassRemoteError, "Acme rejected the operation (HTTP 502)"},
	}
	for _, test := range tests {
		got := httpx.ClassifyStatus("acme.read", test.status, texts)
		if got.Class != test.class || got.Message != test.message || got.Op != "acme.read" || got.Original != nil {
			t.Errorf("status %d = %+v, want class %s message %q", test.status, got, test.class, test.message)
		}
		if got.Error() != "acme.read: "+test.message {
			t.Errorf("status %d: Error() = %q", test.status, got.Error())
		}
	}
}

func TestReadJSON(t *testing.T) {
	type answer struct {
		Name string `json:"name"`
	}
	t.Run("success", func(t *testing.T) {
		var out answer
		if err := httpx.ReadJSON("op", "Acme", strings.NewReader(`{"name":"x"}`), 12, &out); err != nil || out.Name != "x" {
			t.Fatalf("err = %v, out = %+v", err, out)
		}
	})
	const tooLarge = "the Acme response could not be read within the size limit"
	tests := []struct {
		name    string
		body    func() *strings.Reader
		limit   int64
		message string
	}{
		{"over limit", func() *strings.Reader { return strings.NewReader(`{"name":"x"}`) }, 11, tooLarge},
		{"invalid JSON", func() *strings.Reader { return strings.NewReader(`not json`) }, 100,
			"Acme returned an invalid response"},
		{"wrong shape", func() *strings.Reader { return strings.NewReader(`{"name":7}`) }, 100,
			"Acme returned an invalid response"},
	}
	for _, test := range tests {
		var out answer
		err := httpx.ReadJSON("op", "Acme", test.body(), test.limit, &out)
		var failure *httpx.Error
		if !errors.As(err, &failure) || failure.Class != httpx.ClassInvalidResponse || failure.Op != "op" ||
			failure.Message != test.message {
			t.Errorf("%s: err = %+v, want invalid-response %q", test.name, err, test.message)
		}
	}
	var out answer
	err := httpx.ReadJSON("op", "Acme", failingReader{}, 100, &out)
	var failure *httpx.Error
	if !errors.As(err, &failure) || failure.Class != httpx.ClassInvalidResponse || failure.Message != tooLarge {
		t.Errorf("read failure: err = %+v", err)
	}
}
