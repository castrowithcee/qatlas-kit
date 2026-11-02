package httpx_test

import (
	"testing"

	"github.com/castrowithcee/qatlas-kit/httpx"
)

func TestBoundCursorRoundTripsAndRefusesOtherBindings(t *testing.T) {
	binding := httpx.CursorBinding("records", "person", "name")
	cursor := httpx.EncodeCursor(binding, "abc123")
	if inner, ok := httpx.DecodeCursor(binding, cursor, 100); !ok || inner != "abc123" {
		t.Fatalf("DecodeCursor() = %q, %v", inner, ok)
	}
	altered := []byte(cursor)
	if altered[len(altered)-1] == 'A' {
		altered[len(altered)-1] = 'B'
	} else {
		altered[len(altered)-1] = 'A'
	}
	forged := httpx.EncodeCursor(httpx.CursorBinding("records", "company", "name"), "abc123")
	for name, c := range map[string]string{
		"other binding": forged, "altered": string(altered), "cut short": cursor[:len(cursor)-2],
		"empty": "", "garbage": "!!", "short": "AAAA",
	} {
		if _, ok := httpx.DecodeCursor(binding, c, 100); ok {
			t.Errorf("%s was accepted", name)
		}
	}
	if _, ok := httpx.DecodeCursor(binding, cursor, 5); ok {
		t.Error("an overlong cursor was accepted")
	}
}
