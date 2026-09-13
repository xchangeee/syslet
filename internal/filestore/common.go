package filestore

import (
	"fmt"
	"os"

	"codeberg.org/xchangeee/syslet/internal/util"
	"github.com/spf13/afero"
)

// FileManagers groups the file stores used throughout plan building and apply.
type FileManagers struct {
	// Config manages per-container config files using the BasenameWithHashSuffix naming strategy.
	Config *ContainerConfigFileStore
	// Build manages per-build context files using identity naming (filename stored as-is).
	Build *BuildContextFileStore
}

// isFileChanged reports whether the file at path differs from the desired content or mode.
// Returns (true, "", 0, nil) when the file does not exist.
// Returns (false, existing, existingMode, nil) when content and mode match.
// Returns (true, existing, existingMode, nil) when content or mode differ.
func isFileChanged(afs afero.Fs, path, content string, mode os.FileMode) (changed bool, existing string, existingMode os.FileMode, err error) {
	raw, rerr := afero.ReadFile(afs, path)
	if os.IsNotExist(rerr) {
		return true, "", 0, nil
	}
	if rerr != nil {
		return false, "", 0, fmt.Errorf("reading %s: %w", path, rerr)
	}
	existing = string(raw)
	info, err := afs.Stat(path)
	if err != nil {
		return false, "", 0, fmt.Errorf("stating %s: %w", path, err)
	}
	existingMode = info.Mode().Perm()
	if util.SHA256Hex([]byte(content)) != util.SHA256Hex(raw) || existingMode != mode {
		return true, existing, existingMode, nil
	}
	return false, existing, existingMode, nil
}
