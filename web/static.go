package web

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"net/http"
	"path"
	"strconv"
	"strings"
)

// contentTypes is the closed list of types Static serves by extension. Text types carry their charset.
var contentTypes = map[string]string{
	".html": "text/html; charset=utf-8", ".css": "text/css; charset=utf-8",
	".js": "text/javascript; charset=utf-8", ".mjs": "text/javascript; charset=utf-8",
	".json": "application/json", ".map": "application/json", ".webmanifest": "application/manifest+json",
	".txt": "text/plain; charset=utf-8", ".wasm": "application/wasm",
	".svg": "image/svg+xml", ".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
	".gif": "image/gif", ".webp": "image/webp", ".avif": "image/avif", ".ico": "image/x-icon",
	".woff": "font/woff", ".woff2": "font/woff2", ".ttf": "font/ttf", ".otf": "font/otf",
}

type staticFile struct {
	data  []byte
	etag  string
	ctype string
}

type static map[string]staticFile

// Static returns a handler that serves the files of fsys, such as an embed.FS, for GET and HEAD. It reads
// every file once, so the answer is a snapshot and the handler never touches fsys again. A request path
// is looked up as a whole among those files: there is no directory listing, no redirect and no way out of
// fsys, and only "/" maps to index.html. Files and directories whose name starts with a dot are not
// served. Every file gets a fixed Content-Type by extension (application/octet-stream for an unknown one,
// which a browser downloads and never interprets) and an ETag of its content, with Cache-Control: no-cache
// so a browser revalidates and gets 304 until the content changes. Anything else answers through Error.
//
// Files cannot carry the CSP nonce, so a served page runs only scripts and styles from its own origin.
func Static(fsys fs.FS) (http.Handler, error) {
	files := static{}
	err := fs.WalkDir(fsys, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if name != "." && strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil // directories are not served; links and devices are never followed
		}
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return err
		}
		ct, ok := contentTypes[strings.ToLower(path.Ext(name))]
		if !ok {
			ct = "application/octet-stream"
		}
		sum := sha256.Sum256(data)
		files[name] = staticFile{data, `"` + hex.EncodeToString(sum[:16]) + `"`, ct}
		return nil
	})
	if err != nil {
		return nil, errors.New("web: static files: " + err.Error())
	}
	return files, nil
}

func (s static) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		Error(w, http.StatusMethodNotAllowed)
		return
	}
	name, ok := strings.CutPrefix(r.URL.Path, "/")
	if name == "" && ok {
		name = "index.html"
	}
	f, found := s[name]
	if !ok || !found {
		Error(w, http.StatusNotFound)
		return
	}
	h := w.Header()
	h.Set("Content-Type", f.ctype)
	h.Set("ETag", f.etag)
	h.Set("Cache-Control", "no-cache")
	if etagMatch(r.Header.Get("If-None-Match"), f.etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	h.Set("Content-Length", strconv.Itoa(len(f.data)))
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodGet {
		_, _ = w.Write(f.data)
	}
}

// etagMatch reports whether an If-None-Match value lists etag or "*", comparing weakly as RFC 9110 asks.
func etagMatch(header, etag string) bool {
	for v := range strings.SplitSeq(header, ",") {
		v = strings.TrimPrefix(strings.TrimSpace(v), "W/")
		if v == "*" || v == etag {
			return true
		}
	}
	return false
}
