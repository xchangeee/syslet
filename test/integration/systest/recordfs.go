//go:build integration

package systest

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/afero"

	"codeberg.org/xchangeee/syslet/test/oplog"
)

// The filesystem is the third channel of side effects syslet.Apply produces,
// alongside systemd and podman, and the only one with no fake of its own: the
// stores write through a real afero.Fs. Without a record of those writes the
// harness can see that a container was stopped and started but not that its new
// config was on disk in between — which is the entire point of stopping it
// first.
//
// recordingFs is a transparent afero.Fs decorator that appends every mutating
// call to the shared oplog timeline, classified by which store's directory the
// path falls under. It changes no behavior; it only makes the writes visible
// to the phase-order check in order.go.

// fsChannel labels filesystem entries on the shared timeline.
const fsChannel = "fs"

// fsZone maps one store's directory subtree to the operation names its writes
// and deletions are recorded under.
type fsZone struct {
	prefix   string
	writeOp  string
	deleteOp string
}

// fsRecorder classifies a path into a zone and records the operation.
//
// Zones are held longest-prefix-first so that nested directories — a config
// store rooted inside the quadlet directory, say — resolve to the innermost
// store rather than the outer one.
type fsRecorder struct {
	log   *oplog.Log
	zones []fsZone
}

// newFsRecorder builds a recorder for the three store subtrees. Paths outside
// all of them — the validation staging directory, most notably — are not
// recorded, because they are scratch space rather than host state.
func newFsRecorder(log *oplog.Log, quadletDir, configDir, buildDir string) *fsRecorder {
	zones := []fsZone{
		{prefix: filepath.Clean(quadletDir), writeOp: "write-unit", deleteOp: "delete-unit"},
		{prefix: filepath.Clean(configDir), writeOp: "write-config", deleteOp: "delete-config"},
		{prefix: filepath.Clean(buildDir), writeOp: "write-build", deleteOp: "delete-build"},
	}
	sort.Slice(zones, func(i, j int) bool { return len(zones[i].prefix) > len(zones[j].prefix) })
	return &fsRecorder{log: log, zones: zones}
}

// record appends one filesystem event, ignoring paths outside every zone.
func (r *fsRecorder) record(path string, deleting bool) {
	clean := filepath.Clean(path)
	for _, z := range r.zones {
		if clean != z.prefix && !strings.HasPrefix(clean, z.prefix+string(filepath.Separator)) {
			continue
		}
		op := z.writeOp
		if deleting {
			op = z.deleteOp
		}
		r.log.Record(fsChannel, op, clean)
		return
	}
}

// isWrite reports whether an OpenFile flag set will modify the file. A
// read-only open is not an effect and must not land on the timeline.
func isWrite(flag int) bool {
	return flag&(os.O_WRONLY|os.O_RDWR|os.O_APPEND|os.O_CREATE|os.O_TRUNC) != 0
}

// recordingFs decorates an afero.Fs, recording mutations on the shared
// timeline. Reads pass straight through.
//
// Mkdir and Chmod are deliberately not recorded: in every store they accompany
// a write to the same subtree that is already recorded, so logging them would
// only duplicate entries in failure output.
type recordingFs struct {
	afero.Fs
	rec *fsRecorder
}

// newRecordingFs wraps fs, preserving symlink support when the underlying
// filesystem has it.
//
// The wrapper must not claim symlink support the underlying filesystem lacks:
// ContainerConfigFileStore type-asserts afero.Linker to decide whether
// versioned configDirs are possible at all, and afero.MemMapFs deliberately
// fails that assertion. Since a Go type either has the method or does not,
// support is preserved by returning a different wrapper type rather than by
// forwarding conditionally.
func newRecordingFs(fs afero.Fs, rec *fsRecorder) afero.Fs {
	base := recordingFs{Fs: fs, rec: rec}
	linker, canLink := fs.(afero.Linker)
	reader, canRead := fs.(afero.LinkReader)
	if canLink && canRead {
		return recordingLinkerFs{recordingFs: base, linker: linker, reader: reader}
	}
	return base
}

// Create records a write and creates the file.
func (f recordingFs) Create(name string) (afero.File, error) {
	f.rec.record(name, false)
	return f.Fs.Create(name)
}

// OpenFile records a write when the flags call for one, then opens the file.
func (f recordingFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if isWrite(flag) {
		f.rec.record(name, false)
	}
	return f.Fs.OpenFile(name, flag, perm)
}

// Remove records a deletion and removes the file.
func (f recordingFs) Remove(name string) error {
	f.rec.record(name, true)
	return f.Fs.Remove(name)
}

// RemoveAll records a deletion and removes the subtree.
func (f recordingFs) RemoveAll(path string) error {
	f.rec.record(path, true)
	return f.Fs.RemoveAll(path)
}

// Rename records a write at the destination, which is where the effect lands —
// the configDir ..data swap is a rename, and it is the new symlink that matters.
func (f recordingFs) Rename(oldname, newname string) error {
	f.rec.record(newname, false)
	return f.Fs.Rename(oldname, newname)
}

// recordingLinkerFs is recordingFs for an underlying filesystem that supports
// symlinks, such as afero.OsFs behind WithOSConfigStore.
type recordingLinkerFs struct {
	recordingFs
	linker afero.Linker
	reader afero.LinkReader
}

// SymlinkIfPossible records a write at the link itself and creates it.
func (f recordingLinkerFs) SymlinkIfPossible(oldname, newname string) error {
	f.rec.record(newname, false)
	return f.linker.SymlinkIfPossible(oldname, newname)
}

// ReadlinkIfPossible reads a link; reads are not recorded.
func (f recordingLinkerFs) ReadlinkIfPossible(name string) (string, error) {
	return f.reader.ReadlinkIfPossible(name)
}
