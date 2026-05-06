package workspace

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// DefaultFileMode is the permission machine-written workspace files get.
const DefaultFileMode os.FileMode = 0o644

// DefaultDirMode is the permission machine-created workspace dirs get.
const DefaultDirMode os.FileMode = 0o755

// AtomicWrite writes data to path atomically, creating any missing
// parent directories with mode [DefaultDirMode].
//
// Implementation: data is written to a sibling temp file in the same
// directory, fsynced, then renamed into place. The temp file is removed
// on any error path so callers do not see half-written turds.
//
// The final file mode is mode (or [DefaultFileMode] when 0). Existing
// files have their content replaced; the rename is atomic on POSIX.
func AtomicWrite(path string, data []byte, mode os.FileMode) error {
	if mode == 0 {
		mode = DefaultFileMode
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, DefaultDirMode); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".fit-agent-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp in %s: %w", dir, err)
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write %s: %w", tmpPath, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("sync %s: %w", tmpPath, err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close %s: %w", tmpPath, err)
	}
	if err := os.Chmod(tmpPath, mode); err != nil {
		cleanup()
		return fmt.Errorf("chmod %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		cleanup()
		return fmt.Errorf("rename %s -> %s: %w", tmpPath, path, err)
	}
	return nil
}

// AtomicWriteFrom is like [AtomicWrite] but streams from r instead of
// holding the full payload in memory. Useful for FIT downloads.
func AtomicWriteFrom(path string, r io.Reader, mode os.FileMode) error {
	if mode == 0 {
		mode = DefaultFileMode
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, DefaultDirMode); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".fit-agent-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp in %s: %w", dir, err)
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }

	if _, err := io.Copy(tmp, r); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("copy to %s: %w", tmpPath, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("sync %s: %w", tmpPath, err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close %s: %w", tmpPath, err)
	}
	if err := os.Chmod(tmpPath, mode); err != nil {
		cleanup()
		return fmt.Errorf("chmod %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		cleanup()
		return fmt.Errorf("rename %s -> %s: %w", tmpPath, path, err)
	}
	return nil
}
