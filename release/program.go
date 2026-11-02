package release

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
)

const maxProgramBytes = 64 << 20

// ProgramSHA256 returns the lowercase hex SHA-256 of the regular file programPath, a slash-separated
// path such as "bin/tool", inside archive. The suffix of archiveName selects .tar.gz or .zip.
func ProgramSHA256(archiveName string, archive []byte, programPath string) (string, error) {
	if name, ok := entryPath(programPath); !ok || name == "" || name != programPath {
		return "", fmt.Errorf("invalid program path %q", programPath)
	}
	p := &programScan{path: programPath, seen: map[string]bool{}}
	switch {
	case strings.HasSuffix(archiveName, ".tar.gz"):
		gz, err := gzip.NewReader(bytes.NewReader(archive))
		if err != nil {
			return "", fmt.Errorf("open archive: %w", err)
		}
		defer gz.Close()
		reader := tar.NewReader(gz)
		for {
			header, err := reader.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return "", fmt.Errorf("read archive: %w", err)
			}
			if err := p.entry(header.Name, header.FileInfo().Mode().IsRegular(), reader); err != nil {
				return "", err
			}
		}
	case strings.HasSuffix(archiveName, ".zip"):
		reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
		if err != nil {
			return "", fmt.Errorf("open archive: %w", err)
		}
		for _, file := range reader.File {
			if err := p.zipEntry(file); err != nil {
				return "", err
			}
		}
	default:
		return "", fmt.Errorf("unsupported archive format %s", archiveName)
	}
	if p.sum == nil {
		return "", fmt.Errorf("archive does not contain %s", programPath)
	}
	return hex.EncodeToString(p.sum), nil
}

type programScan struct {
	path string
	seen map[string]bool
	sum  []byte
}

func (p *programScan) zipEntry(file *zip.File) error {
	name, ok := entryPath(file.Name)
	if !ok || name != p.path {
		return p.entry(file.Name, false, nil)
	}
	entry, err := file.Open()
	if err != nil {
		return fmt.Errorf("open %s: %w", file.Name, err)
	}
	defer entry.Close()
	return p.entry(file.Name, file.Mode().IsRegular(), entry)
}

func (p *programScan) entry(raw string, regular bool, body io.Reader) error {
	name, ok := entryPath(raw)
	if !ok {
		return fmt.Errorf("unsafe archive entry %q", raw)
	}
	if name == "" {
		return nil
	}
	if p.seen[name] {
		return fmt.Errorf("duplicate archive entry %q", name)
	}
	p.seen[name] = true
	if name != p.path {
		return nil
	}
	if !regular {
		return fmt.Errorf("%s is not a regular file", name)
	}
	data, err := io.ReadAll(io.LimitReader(body, maxProgramBytes+1))
	if err != nil {
		return fmt.Errorf("read %s: %w", name, err)
	}
	if len(data) > maxProgramBytes {
		return fmt.Errorf("%s exceeds %d bytes", name, maxProgramBytes)
	}
	sum := sha256.Sum256(data)
	p.sum = sum[:]
	return nil
}

// entryPath returns the canonical form of an archive entry name, or "" for the archive root. A single
// leading "./" is accepted because tar archives built from a directory with "." carry it.
func entryPath(raw string) (string, bool) {
	name := strings.TrimSuffix(strings.TrimPrefix(raw, "./"), "/")
	if name == "" || name == "." {
		return "", raw == "./" || raw == "."
	}
	if strings.HasPrefix(name, "/") || strings.Contains(name, "\\") || path.Clean(name) != name || name == ".." || strings.HasPrefix(name, "../") {
		return "", false
	}
	return name, true
}
