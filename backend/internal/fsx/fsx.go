// Package fsx provides root-relative access to /sys and /proc.
//
// Production uses the real filesystem root.  Tests and verification runs can
// point Root at a temporary directory containing fake sys/ and proc/ trees via
// the --root flag or UTH_POWER_ROOT environment variable.
package fsx

import (
	"os"
	"path/filepath"
)

// Root anchors every filesystem lookup.  An empty or "/" root means the real
// host filesystem.
type Root struct {
	path string
}

// New returns a Root for the given directory.  The directory may be relative;
// callers normally pass "/" or an absolute temporary directory.
func New(root string) *Root {
	if root == "" {
		root = "/"
	}
	return &Root{path: filepath.Clean(root)}
}

// Path joins elements onto the root.
func (r *Root) Path(elem ...string) string {
	parts := make([]string, 0, len(elem)+1)
	parts = append(parts, r.path)
	parts = append(parts, elem...)
	return filepath.Join(parts...)
}

// Sys returns a path under <root>/sys.
func (r *Root) Sys(elem ...string) string {
	return r.Path(append([]string{"sys"}, elem...)...)
}

// Proc returns a path under <root>/proc.
func (r *Root) Proc(elem ...string) string {
	return r.Path(append([]string{"proc"}, elem...)...)
}

// ReadFile reads a path below the root.
func (r *Root) ReadFile(elem ...string) ([]byte, error) {
	return os.ReadFile(r.Path(elem...))
}

// ReadDir lists a directory below the root.
func (r *Root) ReadDir(elem ...string) ([]os.DirEntry, error) {
	return os.ReadDir(r.Path(elem...))
}

// IsDefault reports whether this is the real host filesystem root.  It is
// used to keep automatic host probes (nvidia-smi) out of hermetic runs.
func (r *Root) IsDefault() bool {
	return r.path == string(filepath.Separator)
}
