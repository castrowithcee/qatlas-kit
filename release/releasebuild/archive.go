package releasebuild

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"io/fs"
	"path"
	"slices"
	"strings"
	"time"
)

type entry struct {
	name       string
	body       []byte
	executable bool
	dir        bool
}

// withDirectories returns files plus an entry for each parent directory, sorted by name, so the archive
// does not depend on the order in which entries were collected.
func withDirectories(files []entry) []entry {
	all := slices.Clone(files)
	seen := map[string]bool{}
	for _, f := range files {
		for dir := path.Dir(f.name); dir != "." && !seen[dir]; dir = path.Dir(dir) {
			seen[dir] = true
			all = append(all, entry{name: dir + "/", dir: true})
		}
	}
	slices.SortFunc(all, func(a, b entry) int { return strings.Compare(a.name, b.name) })
	return all
}

func (e entry) mode() fs.FileMode {
	switch {
	case e.dir:
		return fs.ModeDir | 0o755
	case e.executable:
		return 0o755
	default:
		return 0o644
	}
}

func writeArchive(name string, files []entry, modified time.Time) ([]byte, error) {
	entries := withDirectories(files)
	var buf bytes.Buffer
	if strings.HasSuffix(name, ".zip") {
		w := zip.NewWriter(&buf)
		for _, e := range entries {
			header := &zip.FileHeader{Name: e.name, Method: zip.Deflate, Modified: modified}
			if e.dir {
				header.Method = zip.Store
			}
			header.SetMode(e.mode())
			out, err := w.CreateHeader(header)
			if err != nil {
				return nil, err
			}
			if _, err := out.Write(e.body); err != nil {
				return nil, err
			}
		}
		if err := w.Close(); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	}
	gz, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	w := tar.NewWriter(gz)
	for _, e := range entries {
		header := &tar.Header{Name: e.name, Mode: int64(e.mode().Perm()), ModTime: modified, Typeflag: tar.TypeReg, Size: int64(len(e.body))}
		if e.dir {
			header.Typeflag = tar.TypeDir
		}
		if err := w.WriteHeader(header); err != nil {
			return nil, err
		}
		if _, err := w.Write(e.body); err != nil {
			return nil, err
		}
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
