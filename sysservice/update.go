package sysservice

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"

	"github.com/castrowithcee/qatlas-kit/selfupdate"
)

// shadowDir is where an administrator's own unit takes precedence over the installed one.
const shadowDir = "/etc/systemd/system"

type options struct {
	root string
	run  func(ctx context.Context, args ...string) error
}

// Option changes how Companion, Restart and Shadowed reach the host.
type Option func(*options)

// WithRoot prepends dir to every host path. It is meant for tests.
func WithRoot(dir string) Option { return func(o *options) { o.root = dir } }

// WithRunner replaces running "systemctl <args>". It is meant for tests.
func WithRunner(run func(ctx context.Context, args ...string) error) Option {
	return func(o *options) { o.run = run }
}

func newOptions(opts []Option) options {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	if o.run == nil {
		o.run = runSystemctl
	}
	return o
}

// hostPath returns the POSIX path p below the root, or p itself without a root.
func (o options) hostPath(p string) string {
	if o.root == "" {
		return p
	}
	return filepath.Join(o.root, filepath.FromSlash(p))
}

// runSystemctl reports only the exit status: systemctl output is untrusted text and stays out of errors.
func runSystemctl(ctx context.Context, args ...string) error {
	return exec.CommandContext(ctx, "systemctl", args...).Run()
}

// Companion returns the unit of program as a selfupdate companion. It does not validate program;
// Restart does.
func Companion(program string, opts ...Option) selfupdate.Companion {
	o := newOptions(opts)
	return selfupdate.Companion{Entry: UnitEntry(program), Path: o.hostPath(InstallPaths(program).Unit)}
}

// Restart restarts the service of program after an installation. It reloads systemd first when result lists
// the unit as written. It does not check for root. An error names the installed version and the failed
// step; after a failed reload the service is not restarted.
func Restart(ctx context.Context, program string, result selfupdate.Result, opts ...Option) error {
	if err := validateProgram(program); err != nil {
		return err
	}
	o := newOptions(opts)
	if slices.Contains(result.Companions, Companion(program, opts...).Path) {
		if err := o.run(ctx, "daemon-reload"); err != nil {
			return fmt.Errorf("version %s is installed, but systemctl daemon-reload failed: %w", result.Latest, err)
		}
	}
	if err := o.run(ctx, "restart", program); err != nil {
		return fmt.Errorf("version %s is installed, but systemctl restart %s failed: %w", result.Latest, program, err)
	}
	return nil
}

// Shadowed reports whether a complete unit of program exists in /etc/systemd/system. A drop-in directory
// does not count, nor does anything but a regular file or a symbolic link.
func Shadowed(program string, opts ...Option) (bool, error) {
	if err := validateProgram(program); err != nil {
		return false, err
	}
	o := newOptions(opts)
	info, err := os.Lstat(o.hostPath(shadowDir + "/" + program + ".service"))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return info.Mode().IsRegular() || info.Mode()&fs.ModeSymlink != 0, nil
}
