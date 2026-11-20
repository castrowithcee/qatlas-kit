//go:build !linux && !darwin

package exteditor

import (
	"errors"
	"os"
	"testing"
)

func TestEditIsUnsupported(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	r := Request{Editor: Editor{Path: self}, Dir: t.TempDir(), Name: "notes.txt", Text: []byte("text"), MaxSize: 10}
	if _, err := Edit(r); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("Edit = %v, want errors.ErrUnsupported", err)
	}
}
