package vault

import (
	"os"
	"time"

	"github.com/castrowithcee/qatlas-kit/fileretry"
)

// transientFile classifies a file failure a short wait may cure and sleepFile waits between attempts, nil
// meaning time.Sleep. Tests replace both.
var (
	transientFile = fileretry.Transient
	sleepFile     func(time.Duration)
)

// retry runs op, which must change nothing when it fails, and with Options.RetryFiles repeats it while
// the failure is transient. A missing file is an answer, not a lock, so it is never repeated.
func (v *Vault) retry(op func() error) error {
	if !v.opts.RetryFiles {
		return op()
	}
	return fileretry.Do(op, fileretry.Options{Transient: func(err error) bool {
		return !isNotExist(err) && transientFile(err)
	}, Sleep: sleepFile})
}

func (v *Vault) stat(path string) (info os.FileInfo, err error) {
	err = v.retry(func() (err error) {
		info, err = os.Stat(path)
		return err
	})
	return info, err
}

func (v *Vault) readDir(path string) (entries []os.DirEntry, err error) {
	err = v.retry(func() (err error) {
		entries, err = os.ReadDir(path)
		return err
	})
	return entries, err
}

// removeAll removes paths, files or empty directories, ignoring those that do not exist.
func (v *Vault) removeAll(paths ...string) error {
	for _, path := range paths {
		err := v.retry(func() error { return os.Remove(path) })
		if err != nil && !isNotExist(err) {
			return err
		}
	}
	return nil
}
