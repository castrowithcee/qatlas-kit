package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	releaseverify "github.com/castrowithcee/qatlas-kit/release"
)

const (
	maxArchiveBytes    = 128 << 20
	maxExecutableBytes = 64 << 20
	maxCompanionBytes  = 2 << 20
	stallTimeout       = 30 * time.Second
)

// Prepared holds a downloaded and verified release and can only be made by Prepare outside this package.
type Prepared struct {
	version, archiveName          string
	checksums, signature, archive []byte
	executable                    []byte
	companions                    [][]byte
	trust                         trustBinding
}

type trustBinding struct {
	repository, namespace, archive, program string
	companions                              []string
	keys                                    [][]byte
}

// Version is the tag of the prepared release.
func (p *Prepared) Version() string { return p.version }

// ArchiveName is the file name of the archive, by which checksums.txt lists it.
func (p *Prepared) ArchiveName() string { return p.archiveName }

// Checksums returns a copy of the verified checksums.txt.
func (p *Prepared) Checksums() []byte { return bytes.Clone(p.checksums) }

// Signature returns a copy of the signature over checksums.txt.
func (p *Prepared) Signature() []byte { return bytes.Clone(p.signature) }

// Archive returns a copy of the verified archive.
func (p *Prepared) Archive() []byte { return bytes.Clone(p.archive) }

func (c *Client) binding(version string) (trustBinding, error) {
	if c.Namespace == "" || len(c.TrustedKeys) == 0 || !validPayloadName(c.ProgramName) {
		return trustBinding{}, errors.New("namespace, trusted keys and program name are required")
	}
	b := trustBinding{repository: c.RepositoryAPI, namespace: c.Namespace, archive: c.ArchiveName(version), program: c.ProgramName}
	program := programEntry(b.archive, b.program)
	for _, companion := range c.Companions {
		if name, ok := entryPath(companion.Entry); !ok || name == "" || name != companion.Entry {
			return trustBinding{}, fmt.Errorf("invalid companion entry %q", companion.Entry)
		}
		if companion.Entry == program || slices.Contains(b.companions, companion.Entry) {
			return trustBinding{}, fmt.Errorf("companion entry %q is not unique", companion.Entry)
		}
		b.companions = append(b.companions, companion.Entry)
	}
	for _, key := range c.TrustedKeys {
		if key == nil {
			return trustBinding{}, errors.New("trusted key is nil")
		}
		b.keys = append(b.keys, bytes.Clone(key.Marshal()))
	}
	return b, nil
}

func (b trustBinding) matches(other trustBinding) bool {
	if b.repository != other.repository || b.namespace != other.namespace || b.archive != other.archive || b.program != other.program ||
		!slices.Equal(b.companions, other.companions) || len(b.keys) != len(other.keys) {
		return false
	}
	for i := range b.keys {
		if !bytes.Equal(b.keys[i], other.keys[i]) {
			return false
		}
	}
	return true
}

// Prepare downloads and verifies the selected release after caller confirmation. It verifies the
// signature over checksums.txt before it downloads the archive, and the archive before it reads it.
func (c *Client) Prepare(ctx context.Context, target Target) (*Prepared, error) {
	policy, err := c.validate()
	if err != nil {
		return nil, err
	}
	version, ok := c.eligible(target.rel, policy)
	if !ok || target.Version != target.rel.TagName || version.compare(policy.minimum) < 0 {
		return nil, &TargetError{target.Version, TargetIneligible}
	}
	if version.compare(policy.current) == 0 {
		return nil, &TargetError{target.Version, TargetRunning}
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
	checksums, err := c.download(ctx, policy, urls[checksumsAsset], maxMetadataBytes, nil)
	if err != nil {
		return nil, fmt.Errorf("download checksums: %w", err)
	}
	signature, err := c.download(ctx, policy, urls[signatureAsset], maxMetadataBytes, nil)
	if err != nil {
		return nil, fmt.Errorf("download signature: %w", err)
	}
	if err := releaseverify.VerifyChecksums(checksums, signature, c.Namespace, c.TrustedKeys); err != nil {
		return nil, fmt.Errorf("verify release: %w", err)
	}
	want, err := releaseverify.ChecksumFor(checksums, name)
	if err != nil {
		return nil, fmt.Errorf("verify release: %w", err)
	}
	archive, err := c.download(ctx, policy, urls[name], maxArchiveBytes, c.Progress)
	if err != nil {
		return nil, fmt.Errorf("download archive: %w", err)
	}
	if got := sha256.Sum256(archive); hex.EncodeToString(got[:]) != want {
		return nil, fmt.Errorf("verify release: archive checksum does not match %s", name)
	}
	program := programEntry(name, trust.program)
	programSum, err := releaseverify.ProgramSHA256(name, archive, program)
	if err != nil {
		return nil, fmt.Errorf("verify archive: %w", err)
	}
	files, err := extractPayload(name, archive, program, trust.companions)
	if err != nil {
		return nil, err
	}
	if got := sha256.Sum256(files[program]); hex.EncodeToString(got[:]) != programSum {
		return nil, errors.New("verify archive: program differs between reads")
	}
	p := &Prepared{version: target.Version, archiveName: name, checksums: checksums, signature: signature, archive: archive,
		executable: files[program], trust: trust}
	for _, entry := range trust.companions {
		p.companions = append(p.companions, files[entry])
	}
	return p, nil
}

func (c *Client) download(ctx context.Context, p policy, raw string, limit int64, progress func(done, total int64)) ([]byte, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var stalled atomic.Bool
	timer := time.AfterFunc(stallTimeout, func() {
		stalled.Store(true)
		cancel()
	})
	defer timer.Stop()
	request, err := c.newRequest(ctx, p, raw, "application/octet-stream")
	if err != nil {
		return nil, err
	}
	client := &http.Client{}
	if c.HTTPClient != nil {
		*client = *c.HTTPClient
	}
	// The stall timer bounds the download; a total timeout would also end a slow download that moves.
	client.Timeout = 0
	client.CheckRedirect = assetRedirect
	body, err := receive(client, request, limit, func(done, total int64) {
		timer.Reset(stallTimeout)
		if progress != nil {
			progress(done, total)
		}
	})
	if err != nil && stalled.Load() {
		return nil, fmt.Errorf("no data received for %s", stallTimeout)
	}
	return body, err
}

// assetRedirect follows a release asset to GitHub's download hosts only, and never with the token.
func assetRedirect(next *http.Request, previous []*http.Request) error {
	host := next.URL.Hostname()
	if len(previous) >= 10 || next.URL.Scheme != "https" || !(host == "github.com" || strings.HasSuffix(host, ".github.com") || strings.HasSuffix(host, ".githubusercontent.com")) {
		return errors.New("untrusted asset redirect")
	}
	next.Header.Del("Authorization")
	return nil
}

func receive(client *http.Client, request *http.Request, limit int64, received func(done, total int64)) ([]byte, error) {
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
			if int64(out.Len()+n) > limit {
				return nil, errors.New("asset exceeds size limit")
			}
			out.Write(buf[:n])
			received(int64(out.Len()), response.ContentLength)
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

func programEntry(archiveName, program string) string {
	if strings.HasSuffix(archiveName, ".zip") && !strings.HasSuffix(program, ".exe") {
		program += ".exe"
	}
	return "bin/" + program
}

// extractPayload returns the program and companion entries of archive by canonical name. Every entry must
// have a safe path that no other entry shares, and every wanted entry must be one non-empty regular file.
func extractPayload(archiveName string, archive []byte, program string, companions []string) (map[string][]byte, error) {
	s := payloadScan{limits: map[string]int64{program: maxExecutableBytes}, seen: map[string]bool{}, files: map[string][]byte{}}
	for _, entry := range companions {
		s.limits[entry] = maxCompanionBytes
	}
	switch {
	case strings.HasSuffix(archiveName, ".tar.gz"):
		gz, err := gzip.NewReader(bytes.NewReader(archive))
		if err != nil {
			return nil, fmt.Errorf("open archive: %w", err)
		}
		defer gz.Close()
		reader := tar.NewReader(gz)
		for {
			header, err := reader.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, fmt.Errorf("read archive: %w", err)
			}
			if err := s.entry(header.Name, header.FileInfo().Mode().IsRegular(), func() (io.ReadCloser, error) { return io.NopCloser(reader), nil }); err != nil {
				return nil, err
			}
		}
	case strings.HasSuffix(archiveName, ".zip"):
		reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
		if err != nil {
			return nil, fmt.Errorf("open archive: %w", err)
		}
		for _, file := range reader.File {
			if err := s.entry(file.Name, file.Mode().IsRegular(), file.Open); err != nil {
				return nil, err
			}
		}
	default:
		return nil, errors.New("unsupported archive format")
	}
	for name := range s.limits {
		if len(s.files[name]) == 0 {
			return nil, fmt.Errorf("archive has no %s", name)
		}
	}
	return s.files, nil
}

type payloadScan struct {
	limits map[string]int64
	seen   map[string]bool
	files  map[string][]byte
}

func (s *payloadScan) entry(raw string, regular bool, open func() (io.ReadCloser, error)) error {
	name, ok := entryPath(raw)
	if !ok {
		return fmt.Errorf("unsafe archive entry %q", raw)
	}
	if name == "" {
		return nil
	}
	if s.seen[name] {
		return fmt.Errorf("duplicate archive entry %q", name)
	}
	s.seen[name] = true
	limit, wanted := s.limits[name]
	if !wanted {
		return nil
	}
	if !regular {
		return fmt.Errorf("archive entry %q is not a regular file", name)
	}
	r, err := open()
	if err != nil {
		return fmt.Errorf("open %s: %w", name, err)
	}
	defer r.Close()
	body, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return fmt.Errorf("read %s: %w", name, err)
	}
	if int64(len(body)) > limit {
		return fmt.Errorf("archive entry %q exceeds size limit", name)
	}
	s.files[name] = body
	return nil
}

// entryPath returns the canonical name of an archive entry, or "" for the archive root, with the rule of
// release.ProgramSHA256: exactly one leading "./" is accepted because tar archives of "." carry it.
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
