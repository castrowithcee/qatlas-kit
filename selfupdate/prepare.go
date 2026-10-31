package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"time"

	releaseverify "github.com/castrowithcee/qatlas-kit/release"
)

const (
	maxArchiveBytes    = 128 << 20
	maxExecutableBytes = 64 << 20
	maxManpageBytes    = 2 << 20
	stallTimeout       = 30 * time.Second
)

// Prepared holds verified payloads and can only be made by Prepare outside this package.
type Prepared struct {
	version             string
	executable, manpage []byte
	trust               trustBinding
}

type trustBinding struct {
	repository, namespace, archive, program, manpage string
	keys                                             [][]byte
}

func (c *Client) binding(version string) (trustBinding, error) {
	if c.Namespace == "" || len(c.TrustedKeys) == 0 || !validPayloadName(c.ProgramName) || !validPayloadName(c.ManpageName) {
		return trustBinding{}, errors.New("namespace, trusted keys, program and manpage names are required")
	}
	b := trustBinding{repository: c.RepositoryAPI, namespace: c.Namespace, archive: c.ArchiveName(version), program: c.ProgramName, manpage: c.ManpageName}
	for _, key := range c.TrustedKeys {
		if key == nil {
			return trustBinding{}, errors.New("trusted key is nil")
		}
		b.keys = append(b.keys, bytes.Clone(key.Marshal()))
	}
	return b, nil
}

func (b trustBinding) matches(other trustBinding) bool {
	if b.repository != other.repository || b.namespace != other.namespace || b.archive != other.archive || b.program != other.program || b.manpage != other.manpage || len(b.keys) != len(other.keys) {
		return false
	}
	for i := range b.keys {
		if !bytes.Equal(b.keys[i], other.keys[i]) {
			return false
		}
	}
	return true
}

func (p *Prepared) Version() string { return p.version }
func (p *Prepared) Manpage() []byte { return bytes.Clone(p.manpage) }

// Prepare downloads and verifies the selected release after caller confirmation.
func (c *Client) Prepare(ctx context.Context, target Target) (*Prepared, error) {
	policy, err := c.validate()
	if err != nil {
		return nil, err
	}
	version, ok := c.eligible(target.rel, policy)
	if !ok || target.Version != target.rel.TagName || version.compare(policy.minimum) < 0 {
		return nil, errors.New("ineligible target release")
	}
	trust, err := c.binding(target.Version)
	if err != nil {
		return nil, err
	}
	name := trust.archive
	urls := make(map[string]string)
	for _, asset := range target.rel.Assets {
		urls[asset.Name] = asset.APIURL
	}
	checksums, err := c.download(ctx, urls["checksums.txt"], maxMetadataBytes)
	if err != nil {
		return nil, fmt.Errorf("download checksums: %w", err)
	}
	signature, err := c.download(ctx, urls["checksums.txt.sig"], maxMetadataBytes)
	if err != nil {
		return nil, fmt.Errorf("download signature: %w", err)
	}
	archive, err := c.download(ctx, urls[name], maxArchiveBytes)
	if err != nil {
		return nil, fmt.Errorf("download archive: %w", err)
	}
	if err := releaseverify.VerifyArchive(checksums, signature, archive, c.Namespace, name, c.TrustedKeys); err != nil {
		return nil, fmt.Errorf("verify release: %w", err)
	}
	binary, manpage, err := extractPayload(archive, name, c.ProgramName, c.ManpageName)
	if err != nil {
		return nil, err
	}
	return &Prepared{version: target.Version, executable: binary, manpage: manpage, trust: trust}, nil
}

func (c *Client) download(ctx context.Context, raw string, limit int64) ([]byte, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	timer := time.AfterFunc(stallTimeout, cancel)
	defer timer.Stop()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/octet-stream")
	request.Header.Set("User-Agent", c.UserAgent)
	client := &http.Client{}
	if c.HTTPClient != nil {
		*client = *c.HTTPClient
	}
	client.Timeout = 0
	client.CheckRedirect = func(next *http.Request, previous []*http.Request) error {
		host := next.URL.Hostname()
		if len(previous) >= 10 || next.URL.Scheme != "https" || !(host == "github.com" || strings.HasSuffix(host, ".github.com") || strings.HasSuffix(host, ".githubusercontent.com")) {
			return errors.New("untrusted asset redirect")
		}
		next.Header.Del("Authorization")
		return nil
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("asset returned %s", response.Status)
	}
	var out bytes.Buffer
	buf := make([]byte, 32<<10)
	for {
		n, readErr := response.Body.Read(buf)
		if n > 0 {
			timer.Reset(stallTimeout)
			if int64(out.Len()+n) > limit {
				return nil, errors.New("asset exceeds size limit")
			}
			out.Write(buf[:n])
		}
		if readErr == io.EOF {
			return out.Bytes(), nil
		}
		if readErr != nil {
			return nil, readErr
		}
	}
}

func validPayloadName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, "/\\")
}

func extractPayload(body []byte, archiveName, program, manpageName string) ([]byte, []byte, error) {
	binName := "bin/" + program
	if strings.HasSuffix(archiveName, ".zip") && !strings.HasSuffix(program, ".exe") {
		binName += ".exe"
	}
	manName := "share/man/man1/" + manpageName
	var binary, manpage []byte
	seenBin, seenMan := false, false
	capture := func(name string, regular bool, r io.Reader) error {
		if err := validateArchivePath(name); err != nil {
			return err
		}
		if name != binName && name != manName {
			return nil
		}
		if !regular {
			return fmt.Errorf("payload %q is not regular", name)
		}
		if name == binName {
			if seenBin {
				return fmt.Errorf("duplicate executable %q", name)
			}
			seenBin = true
			var err error
			binary, err = readPayloadEntry(r, maxExecutableBytes)
			return err
		}
		if seenMan {
			return fmt.Errorf("duplicate manpage %q", name)
		}
		seenMan = true
		var err error
		manpage, err = readPayloadEntry(r, maxManpageBytes)
		return err
	}
	if strings.HasSuffix(archiveName, ".zip") {
		z, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
		if err != nil {
			return nil, nil, err
		}
		for _, f := range z.File {
			if f.FileInfo().IsDir() {
				if err := validateArchivePath(f.Name); err != nil {
					return nil, nil, err
				}
				continue
			}
			r, err := f.Open()
			if err != nil {
				return nil, nil, err
			}
			err = capture(f.Name, f.Mode().IsRegular(), r)
			r.Close()
			if err != nil {
				return nil, nil, err
			}
		}
	} else if strings.HasSuffix(archiveName, ".tar.gz") {
		gz, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			return nil, nil, err
		}
		defer gz.Close()
		t := tar.NewReader(gz)
		for {
			h, err := t.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, nil, err
			}
			if err := capture(h.Name, h.Typeflag == tar.TypeReg || h.Typeflag == tar.TypeRegA, t); err != nil {
				return nil, nil, err
			}
		}
	} else {
		return nil, nil, errors.New("unsupported archive format")
	}
	if !seenBin || len(binary) == 0 {
		return nil, nil, errors.New("archive has no executable")
	}
	if !strings.HasSuffix(archiveName, ".zip") && (!seenMan || len(manpage) == 0) {
		return nil, nil, errors.New("archive has no manpage")
	}
	return binary, manpage, nil
}

func readPayloadEntry(r io.Reader, limit int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, errors.New("archive entry exceeds size limit")
	}
	return body, nil
}

func validateArchivePath(name string) error {
	clean := strings.TrimSuffix(name, "/")
	if strings.HasPrefix(name, "/") || strings.Contains(name, "\\") || path.Clean(clean) != clean || clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("unsafe archive entry %q", name)
	}
	return nil
}
