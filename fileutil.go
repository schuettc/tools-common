package tools

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// WriteFileAtomic replaces path's contents durably: it creates the parent
// directory (0755) if missing, writes a unique temp file in the same
// directory, fsyncs and closes it, sets perm exactly (independent of umask),
// then renames it over path. If anything before the rename fails, the temp
// file is removed and path is untouched. If the rename itself fails, the temp
// file is KEPT and its name is in the error, so no content is lost.
//
// The family's one atomic write (superset of the copies in galley, kempt,
// muster and tackle; tackle's fsync + unique temp name).
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+"-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	fail := func(err error) error {
		_ = tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return fail(err)
	}
	if err := tmp.Sync(); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Chmod(name, perm); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("rename %s to %s (temp kept): %w", name, path, err)
	}
	return nil
}

// PIDAlive is the family's one liveness rule (from galley registry.Alive):
// signal 0 asks the kernel whether pid exists; EPERM still means it exists.
// pid <= 0 is never alive. A recycled PID is the accepted false positive.
func PIDAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = p.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM)
}
