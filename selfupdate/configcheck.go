package selfupdate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const defaultCheckTimeout = 20 * time.Second

// ConfigCheck asks the program of a prepared release whether it reads a configuration.
type ConfigCheck struct {
	// Config holds the bytes of the configuration. The program only ever sees a copy.
	Config []byte
	// FileName is the plain file name of the copy, such as "config.yaml".
	FileName string
	// Args returns the program's arguments for checking the copy at configPath.
	Args func(configPath string) []string
	// Timeout bounds the run; zero means 20 seconds.
	Timeout time.Duration
}

// ReadsConfig runs the verified program of the release in a fresh private directory, against a copy of
// the configuration, with an environment that points every home, configuration, data, cache, runtime and
// temporary directory into that directory and carries nothing else of the caller but PATH and the Windows
// system root. Exit status 0 means the program reads the configuration, any other exit status means it does
// not. A run that cannot be carried out is an error, not an answer. The program's output is discarded,
// because it is not trusted and may quote the configuration.
func (p *Prepared) ReadsConfig(ctx context.Context, check ConfigCheck) (bool, error) {
	if p == nil || len(p.executable) == 0 || !validPayloadName(p.trust.program) {
		return false, errors.New("invalid prepared release")
	}
	name := p.trust.program
	if runtime.GOOS == "windows" && !strings.HasSuffix(name, ".exe") {
		name += ".exe"
	}
	if !validPayloadName(check.FileName) || check.FileName == name || check.Args == nil {
		return false, errors.New("configuration file name and arguments are required")
	}
	timeout := check.Timeout
	if timeout <= 0 {
		timeout = defaultCheckTimeout
	}
	dir, err := os.MkdirTemp("", "config-check-*")
	if err != nil {
		return false, err
	}
	defer os.RemoveAll(dir)
	if err := os.Chmod(dir, 0o700); err != nil {
		return false, err
	}
	program := filepath.Join(dir, name)
	if err := os.WriteFile(program, p.executable, 0o700); err != nil {
		return false, err
	}
	config := filepath.Join(dir, check.FileName)
	if err := os.WriteFile(config, check.Config, 0o600); err != nil {
		return false, err
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, program, check.Args(config)...)
	cmd.Dir = dir
	cmd.Env = checkEnvironment(dir)
	// Without output pipes a child that inherits them cannot keep Wait waiting.
	cmd.WaitDelay = time.Second
	err = cmd.Run()
	if err == nil {
		return true, nil
	}
	if runCtx.Err() != nil {
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return false, fmt.Errorf("the checked program did not finish within %s", timeout)
		}
		return false, runCtx.Err()
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() > 0 {
		return false, nil
	}
	return false, fmt.Errorf("the checked program could not be run: %w", err)
}

func checkEnvironment(dir string) []string {
	env := []string{
		"HOME=" + dir, "USERPROFILE=" + dir, "APPDATA=" + dir, "LOCALAPPDATA=" + dir,
		"TMPDIR=" + dir, "TEMP=" + dir, "TMP=" + dir,
		"XDG_CONFIG_HOME=" + dir, "XDG_DATA_HOME=" + dir, "XDG_STATE_HOME=" + dir, "XDG_CACHE_HOME=" + dir,
		"XDG_RUNTIME_DIR=" + dir, "NO_COLOR=1",
	}
	for _, keep := range []string{"PATH", "SystemRoot", "windir"} {
		if v, ok := os.LookupEnv(keep); ok {
			env = append(env, keep+"="+v)
		}
	}
	return env
}
