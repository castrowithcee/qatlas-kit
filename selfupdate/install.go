package selfupdate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/castrowithcee/qatlas-kit/fileretry"
)

// InstallPrepared installs a verified release at caller supplied paths.
func (c *Client) InstallPrepared(ctx context.Context, p *Prepared) error {
	if p == nil || p.version == "" || len(p.executable) == 0 {
		return errors.New("invalid prepared release")
	}
	policy, err := c.validate()
	if err != nil {
		return err
	}
	version, err := parseSemver(p.version)
	if err != nil || version.compare(policy.minimum) < 0 {
		return errors.New("prepared release is below minimum version")
	}
	if !validPayloadName(c.ProgramName) || !validPayloadName(c.ManpageName) {
		return errors.New("program and manpage names are required")
	}
	if c.Executable == "" || !filepath.IsAbs(c.Executable) {
		return errors.New("absolute executable path is required")
	}
	wantProgram := c.ProgramName
	if runtime.GOOS == "windows" && !strings.HasSuffix(wantProgram, ".exe") {
		wantProgram += ".exe"
	}
	if filepath.Base(c.Executable) != wantProgram {
		return errors.New("executable path does not name the configured program")
	}
	info, err := os.Lstat(c.Executable)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("executable target is not a regular file")
	}
	if len(p.manpage) > 0 {
		if c.ManpagePath == "" || !filepath.IsAbs(c.ManpagePath) || c.ManpagePath == c.Executable {
			return errors.New("separate absolute manpage path is required")
		}
		if filepath.Base(c.ManpagePath) != c.ManpageName {
			return errors.New("manpage path does not name the configured manpage")
		}
		manInfo, err := os.Lstat(c.ManpagePath)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err == nil && !manInfo.Mode().IsRegular() {
			return errors.New("manpage target is not a regular file")
		}
	}
	if version.compare(policy.current) < 0 {
		if c.ValidateRollback == nil {
			return errors.New("rollback validation is required")
		}
		if err := c.ValidateRollback(ctx, bytes.Clone(p.executable)); err != nil {
			return fmt.Errorf("rollback validation: %w", err)
		}
	}
	stagedBin, err := stage(c.Executable, p.executable, info.Mode().Perm()|0o500)
	if err != nil {
		return err
	}
	defer os.Remove(stagedBin)
	var stagedMan, backupMan string
	if len(p.manpage) > 0 {
		stagedMan, err = stage(c.ManpagePath, p.manpage, 0o644)
		if err != nil {
			return err
		}
		defer os.Remove(stagedMan)
		if original, readErr := os.ReadFile(c.ManpagePath); readErr == nil {
			backupMan, err = stage(c.ManpagePath, original, 0o644)
			if err != nil {
				return err
			}
			defer os.Remove(backupMan)
		} else if !errors.Is(readErr, os.ErrNotExist) {
			return readErr
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if stagedMan != "" {
		if err := os.Rename(stagedMan, c.ManpagePath); err != nil {
			return fmt.Errorf("install manpage: %w", err)
		}
	}
	if runtime.GOOS == "windows" {
		err = replaceWindows(osFileOps, stagedBin, c.Executable)
	} else {
		err = os.Rename(stagedBin, c.Executable)
	}
	if err != nil {
		if stagedMan != "" {
			if backupMan != "" {
				if backErr := os.Rename(backupMan, c.ManpagePath); backErr != nil {
					return fmt.Errorf("replace executable: %w; restore manpage: %v", err, backErr)
				}
			} else if backErr := os.Remove(c.ManpagePath); backErr != nil {
				return fmt.Errorf("replace executable: %w; remove new manpage: %v", err, backErr)
			}
		}
		return fmt.Errorf("replace executable: %w", err)
	}
	return nil
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
