package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"io"
	"strings"
	"testing"
)

type zeros struct{}

func (zeros) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

type byteCount int64

func (c *byteCount) Write(p []byte) (int, error) {
	*c += byteCount(len(p))
	return len(p), nil
}

// zeroPayload returns a .tar.gz with a program of size zero bytes and a small companion, without holding
// the program in memory.
func zeroPayload(t *testing.T, program, companion string, size int64) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz, err := gzip.NewWriterLevel(&buf, gzip.BestSpeed)
	if err != nil {
		t.Fatal(err)
	}
	w := tar.NewWriter(gz)
	for _, f := range []struct {
		name string
		size int64
	}{{program, size}, {companion, 1}} {
		if err := w.WriteHeader(&tar.Header{Name: f.name, Mode: 0o755, Size: f.size, Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := io.CopyN(w, zeros{}, f.size); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestProgramSizeLimit(t *testing.T) {
	if maxExecutableBytes != 256<<20 {
		t.Fatalf("program limit = %d, want 256 MiB", maxExecutableBytes)
	}
	program, man := "bin/sample", "share/man/man1/sample.1"
	files, err := extractPayload("a.tar.gz", zeroPayload(t, program, man, maxExecutableBytes), program, []string{man})
	if err != nil || len(files[program]) != maxExecutableBytes {
		t.Fatalf("program at the limit: %d bytes, %v", len(files[program]), err)
	}
	if _, err := extractPayload("a.tar.gz", zeroPayload(t, program, man, maxExecutableBytes+1), program, []string{man}); err == nil || !strings.Contains(err.Error(), "exceeds size limit") {
		t.Fatalf("program above the limit: %v", err)
	}
}

// An archive download must admit the largest program even when the archive stores it uncompressed.
func TestArchiveLimitAdmitsLargestProgram(t *testing.T) {
	var size byteCount
	w := zip.NewWriter(&size)
	for _, f := range []struct {
		name string
		size int64
	}{{"bin/sample.exe", maxExecutableBytes}, {"share/man/man1/sample.1", maxCompanionBytes}} {
		entry, err := w.CreateHeader(&zip.FileHeader{Name: f.name, Method: zip.Store})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.CopyN(entry, zeros{}, f.size); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if int64(size) > maxArchiveBytes {
		t.Fatalf("stored archive of %d bytes exceeds the archive limit %d", size, maxArchiveBytes)
	}
}
