package release

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-kit/release/releasetest"
)

type tarEntry struct {
	name     string
	typeflag byte
	body     []byte
}

func tarArchive(t *testing.T, entries ...tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	w := tar.NewWriter(gz)
	for _, e := range entries {
		header := &tar.Header{Name: e.name, Mode: 0o755, Typeflag: e.typeflag, Size: int64(len(e.body))}
		if e.typeflag == tar.TypeSymlink {
			header.Linkname, header.Size = "target", 0
		}
		if e.typeflag == tar.TypeDir {
			header.Size = 0
		}
		if err := w.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if header.Size > 0 {
			if _, err := w.Write(e.body); err != nil {
				t.Fatal(err)
			}
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

func sumHex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestProgramSHA256(t *testing.T) {
	program := []byte("program bytes")
	readme := releasetest.File{Name: "share/doc/README", Body: []byte("readme")}
	valid := map[string]struct {
		archiveName string
		archive     []byte
		path        string
	}{
		"tar":        {"sample.tar.gz", releasetest.Archive(t, "sample.tar.gz", readme, releasetest.File{Name: "bin/tool", Body: program}), "bin/tool"},
		"zip":        {"sample.zip", releasetest.Archive(t, "sample.zip", readme, releasetest.File{Name: "bin/tool.exe", Body: program}), "bin/tool.exe"},
		"top level":  {"sample.zip", releasetest.Archive(t, "sample.zip", releasetest.File{Name: "tool", Body: program}), "tool"},
		"tar of dot": {"sample.tar.gz", tarArchive(t, tarEntry{"./", tar.TypeDir, nil}, tarEntry{"./bin/", tar.TypeDir, nil}, tarEntry{"./bin/tool", tar.TypeReg, program}), "bin/tool"},
	}
	for name, tc := range valid {
		got, err := ProgramSHA256(tc.archiveName, tc.archive, tc.path)
		if err != nil || got != sumHex(program) {
			t.Fatalf("%s: %q, %v", name, got, err)
		}
	}

	var dirZip bytes.Buffer
	zw := zip.NewWriter(&dirZip)
	if _, err := zw.Create("bin/tool/"); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	tool := func(name string) releasetest.File { return releasetest.File{Name: name, Body: program} }
	rejected := map[string]struct {
		archiveName string
		archive     []byte
		path, want  string
	}{
		"missing program":    {"sample.tar.gz", releasetest.Archive(t, "sample.tar.gz", readme), "bin/tool", "does not contain"},
		"duplicate program":  {"sample.zip", releasetest.Archive(t, "sample.zip", tool("bin/tool"), tool("bin/tool")), "bin/tool", "duplicate"},
		"duplicate via dot":  {"sample.tar.gz", releasetest.Archive(t, "sample.tar.gz", tool("bin/tool"), tool("./bin/tool")), "bin/tool", "duplicate"},
		"duplicate other":    {"sample.tar.gz", releasetest.Archive(t, "sample.tar.gz", readme, readme, tool("bin/tool")), "bin/tool", "duplicate"},
		"parent entry":       {"sample.tar.gz", releasetest.Archive(t, "sample.tar.gz", tool("../bin/tool")), "bin/tool", "unsafe"},
		"nested parent":      {"sample.zip", releasetest.Archive(t, "sample.zip", tool("bin/../../tool"), tool("bin/tool")), "bin/tool", "unsafe"},
		"absolute entry":     {"sample.tar.gz", releasetest.Archive(t, "sample.tar.gz", tool("/bin/tool")), "bin/tool", "unsafe"},
		"backslash entry":    {"sample.zip", releasetest.Archive(t, "sample.zip", tool(`bin\tool`)), "bin/tool", "unsafe"},
		"unclean entry":      {"sample.tar.gz", releasetest.Archive(t, "sample.tar.gz", tool("bin//tool")), "bin/tool", "unsafe"},
		"empty entry":        {"sample.zip", releasetest.Archive(t, "sample.zip", tool("")), "bin/tool", "unsafe"},
		"symlink program":    {"sample.tar.gz", tarArchive(t, tarEntry{"bin/tool", tar.TypeSymlink, nil}), "bin/tool", "not a regular file"},
		"directory program":  {"sample.tar.gz", tarArchive(t, tarEntry{"bin/tool/", tar.TypeDir, nil}), "bin/tool", "not a regular file"},
		"zip directory":      {"sample.zip", dirZip.Bytes(), "bin/tool", "not a regular file"},
		"oversized program":  {"sample.tar.gz", releasetest.Archive(t, "sample.tar.gz", releasetest.File{Name: "bin/tool", Body: make([]byte, maxProgramBytes+1)}), "bin/tool", "exceeds"},
		"unknown format":     {"sample.tar.xz", releasetest.Archive(t, "sample.tar.gz", tool("bin/tool")), "bin/tool", "unsupported"},
		"corrupt tar":        {"sample.tar.gz", []byte("not gzip"), "bin/tool", "open archive"},
		"corrupt zip":        {"sample.zip", []byte("not zip"), "bin/tool", "open archive"},
		"empty path":         {"sample.tar.gz", releasetest.Archive(t, "sample.tar.gz", tool("bin/tool")), "", "invalid program path"},
		"dot path":           {"sample.tar.gz", releasetest.Archive(t, "sample.tar.gz", tool("bin/tool")), "./bin/tool", "invalid program path"},
		"absolute path":      {"sample.tar.gz", releasetest.Archive(t, "sample.tar.gz", tool("bin/tool")), "/bin/tool", "invalid program path"},
		"parent path":        {"sample.tar.gz", releasetest.Archive(t, "sample.tar.gz", tool("bin/tool")), "../tool", "invalid program path"},
		"directory path":     {"sample.tar.gz", releasetest.Archive(t, "sample.tar.gz", tool("bin/tool")), "bin/", "invalid program path"},
		"backslash path":     {"sample.zip", releasetest.Archive(t, "sample.zip", tool("bin/tool")), `bin\tool`, "invalid program path"},
		"unclean path":       {"sample.zip", releasetest.Archive(t, "sample.zip", tool("bin/tool")), "bin/./tool", "invalid program path"},
		"unsafe after found": {"sample.tar.gz", releasetest.Archive(t, "sample.tar.gz", tool("bin/tool"), tool("../escape")), "bin/tool", "unsafe"},
	}
	for name, tc := range rejected {
		t.Run(name, func(t *testing.T) {
			if _, err := ProgramSHA256(tc.archiveName, tc.archive, tc.path); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}
