package selfupdate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/castrowithcee/qatlas-kit/fileretry"
)

// ReleaseFiles names copies of the verified release files in a private directory that exists until
// InstallPrepared returns. All paths are absolute.
type ReleaseFiles struct {
	Checksums, Signature, Archive string
	// ArchiveName is the archive's file name in the release, by which Checksums lists it.
	ArchiveName string
	// Rollback reports that the release is older than the running version.
	Rollback bool
}

type companionPlan struct {
	path, staged, backup string
	existed              bool
}

// InstallPrepared installs a verified release at the caller supplied paths: companions first, the program
// last. If the program cannot be replaced, the companions are restored.
func (c *Client) InstallPrepared(ctx context.Context, p *Prepared) (Result, error) {
	if p == nil || p.version == "" || len(p.executable) == 0 || len(p.companions) != len(p.trust.companions) {
		return Result{}, errors.New("invalid prepared release")
	}
	policy, err := c.validate()
	if err != nil {
		return Result{}, err
	}
	version, err := parseSemver(p.version)
	if err != nil || version.compare(policy.minimum) < 0 {
		return Result{}, errors.New("prepared release is below minimum version")
	}
	trust, err := c.binding(p.version)
	if err != nil {
		return Result{}, err
	}
	if !p.trust.matches(trust) {
		return Result{}, errors.New("prepared release belongs to a different trust policy")
	}
	if c.Executable == "" || !filepath.IsAbs(c.Executable) {
		return Result{}, errors.New("absolute executable path is required")
	}
	wantProgram := c.ProgramName
	if runtime.GOOS == "windows" && !strings.HasSuffix(wantProgram, ".exe") {
		wantProgram += ".exe"
	}
	if filepath.Base(c.Executable) != wantProgram {
		return Result{}, &UnsupportedInstallationError{c.Executable, "the path does not name the configured program"}
	}
	info, err := os.Lstat(c.Executable)
	if err != nil {
		return Result{}, err
	}
	if !info.Mode().IsRegular() {
		return Result{}, &UnsupportedInstallationError{c.Executable, "the program is not a regular file"}
	}
	plans := make([]companionPlan, len(c.Companions))
	current := make([][]byte, len(c.Companions))
	for i, companion := range c.Companions {
		if !filepath.IsAbs(companion.Path) || filepath.Clean(companion.Path) == filepath.Clean(c.Executable) {
			return Result{}, fmt.Errorf("companion %s needs a separate absolute path", companion.Entry)
		}
		for _, other := range c.Companions[:i] {
			if filepath.Clean(other.Path) == filepath.Clean(companion.Path) {
				return Result{}, fmt.Errorf("companion path %s is not unique", companion.Path)
			}
		}
		if filepath.Base(companion.Path) != path.Base(companion.Entry) {
			return Result{}, &UnsupportedInstallationError{companion.Path, "the path does not name the companion " + companion.Entry}
		}
		plans[i].path = companion.Path
		info, err := os.Lstat(companion.Path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return Result{}, err
		}
		if !info.Mode().IsRegular() {
			return Result{}, &UnsupportedInstallationError{companion.Path, "the companion is not a regular file"}
		}
		if current[i], err = os.ReadFile(companion.Path); err != nil {
			return Result{}, err
		}
		plans[i].existed = true
	}
	if version.compare(policy.current) < 0 {
		if c.ValidateRollback == nil {
			return Result{}, errors.New("rollback validation is required")
		}
		if err := c.ValidateRollback(ctx, p); err != nil {
			return Result{}, fmt.Errorf("rollback validation: %w", err)
		}
	}
	defer func() {
		for _, plan := range plans {
			if plan.staged != "" {
				os.Remove(plan.staged)
			}
			if plan.backup != "" {
				os.Remove(plan.backup)
			}
		}
	}()
	for i := range plans {
		if plans[i].existed && bytes.Equal(current[i], p.companions[i]) {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(plans[i].path), 0o755); err != nil {
			return Result{}, err
		}
		if plans[i].staged, err = stage(plans[i].path, p.companions[i], 0o644); err != nil {
			return Result{}, err
		}
		if plans[i].existed {
			if plans[i].backup, err = stage(plans[i].path, current[i], 0o644); err != nil {
				return Result{}, err
			}
		}
	}
	stagedBin, err := stage(c.Executable, p.executable, info.Mode().Perm()|0o500)
	if err != nil {
		return Result{}, err
	}
	defer os.Remove(stagedBin)
	var files ReleaseFiles
	if c.BeforeReplace != nil || c.AfterReplace != nil {
		dir, err := os.MkdirTemp("", "release-*")
		if err != nil {
			return Result{}, err
		}
		defer os.RemoveAll(dir)
		if files, err = writeReleaseFiles(dir, p); err != nil {
			return Result{}, err
		}
		files.Rollback = version.compare(policy.current) < 0
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if c.BeforeReplace != nil {
		if err := c.BeforeReplace(ctx, files); err != nil {
			return Result{}, err
		}
	}
	result := Result{Current: c.CurrentVersion, Latest: p.version, UpdateAvailable: version.compare(policy.current) > 0}
	var replaced []int
	for i, plan := range plans {
		if plan.staged == "" {
			continue
		}
		if err := os.Rename(plan.staged, plan.path); err != nil {
			return Result{}, restoreCompanions(plans, replaced, fmt.Errorf("install companion %s: %w", plan.path, err))
		}
		plans[i].staged = ""
		replaced = append(replaced, i)
		syncDir(filepath.Dir(plan.path))
	}
	if runtime.GOOS == "windows" {
		err = replaceWindows(osFileOps, stagedBin, c.Executable)
	} else {
		err = os.Rename(stagedBin, c.Executable)
	}
	if err != nil {
		return Result{}, restoreCompanions(plans, replaced, fmt.Errorf("replace executable: %w", err))
	}
	syncDir(filepath.Dir(c.Executable))
	result.Updated = true
	for _, i := range replaced {
		result.Companions = append(result.Companions, plans[i].path)
	}
	if c.AfterReplace != nil {
		c.AfterReplace(ctx, files)
	}
	return result, nil
}

// restoreCompanions puts back the companions replaced so far, newest first, and reports what it could not.
func restoreCompanions(plans []companionPlan, replaced []int, cause error) error {
	for j := len(replaced) - 1; j >= 0; j-- {
		plan := &plans[replaced[j]]
		var err error
		if plan.backup != "" {
			if err = os.Rename(plan.backup, plan.path); err == nil {
				plan.backup = ""
			}
		} else {
			err = os.Remove(plan.path)
		}
		if err != nil {
			cause = fmt.Errorf("%w; restore %s: %v", cause, plan.path, err)
		}
	}
	return cause
}

func writeReleaseFiles(dir string, p *Prepared) (ReleaseFiles, error) {
	files := ReleaseFiles{
		Checksums:   filepath.Join(dir, checksumsAsset),
		Signature:   filepath.Join(dir, signatureAsset),
		Archive:     filepath.Join(dir, p.archiveName),
		ArchiveName: p.archiveName,
	}
	for name, body := range map[string][]byte{files.Checksums: p.checksums, files.Signature: p.signature, files.Archive: p.archive} {
		if err := os.WriteFile(name, body, 0o600); err != nil {
			return ReleaseFiles{}, err
		}
	}
	return files, nil
}

func stage(target string, body []byte, mode os.FileMode) (string, error) {
	file, err := os.CreateTemp(filepath.Dir(target), ".selfupdate-*")
	if err != nil {
		return "", err
	}
	name := file.Name()
	if err := file.Chmod(mode); err == nil {
		_, err = file.Write(body)
	}
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(name)
		return "", err
	}
	return name, nil
}

// syncDir makes a rename durable; a failure leaves a correct installation, so it is not reported.
func syncDir(dir string) {
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		d.Close()
	}
}

type fileOps struct {
	rename    func(string, string) error
	remove    func(string) error
	transient func(error) bool
	sleep     func(time.Duration)
}

var osFileOps = fileOps{os.Rename, os.Remove, fileretry.Transient, nil}

func (o fileOps) move(from, to string) error {
	return fileretry.Do(func() error { return o.rename(from, to) }, o.transient, o.sleep)
}

func replaceWindows(ops fileOps, staged, target string) error {
	old := target + ".old"
	if err := ops.remove(old); err != nil && !errors.Is(err, os.ErrNotExist) {
		old = target + ".old-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	if err := ops.move(target, old); err != nil {
		return err
	}
	if err := ops.move(staged, target); err != nil {
		if restore := ops.move(old, target); restore != nil {
			return fmt.Errorf("%w; restore old executable: %v", err, restore)
		}
		return err
	}
	_ = ops.remove(old)
	return nil
}

// CleanupLeftovers removes a prior Windows replacement at the explicit executable path.
func CleanupLeftovers(executable string) error {
	if runtime.GOOS != "windows" {
		return nil
	}
	if executable == "" || !filepath.IsAbs(executable) {
		return errors.New("absolute executable path is required")
	}
	err := os.Remove(executable + ".old")
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	matches, err := filepath.Glob(strings.NewReplacer("[", "[[]").Replace(executable) + ".old-*")
	if err != nil {
		return err
	}
	for _, match := range matches {
		if err := os.Remove(match); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}
