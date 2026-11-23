package resource

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// A head a restore writes in place is emptied first: from then on, and until
// its members are restored, it holds neither what it held nor what the run
// saved. The restore marks it incomplete meanwhile, in the var directory of
// its resource, so a restore killed or failing leaves a head a start refuses
// to run on, rather than one it runs on half-written.

// headIncompleteName is the name of the marker, in the var directory of the
// resource holding the head.
const headIncompleteName = "head_incomplete"

// MarkHeadIncomplete marks the head of the resource whose var directory is
// varDir as incomplete, for reason. The marker is on the disk when it
// returns: what follows empties the head.
func MarkHeadIncomplete(varDir, reason string) error {
	if err := os.MkdirAll(varDir, 0755); err != nil {
		return err
	}
	return writeFileDurably(filepath.Join(varDir, headIncompleteName), []byte(reason+"\n"))
}

// ClearHeadIncomplete removes the marker of the head, once restored whole.
func ClearHeadIncomplete(varDir string) error {
	err := os.Remove(filepath.Join(varDir, headIncompleteName))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// HeadIncomplete says whether the head of the resource whose var directory
// is varDir is marked incomplete, and the reason it was marked for.
func HeadIncomplete(varDir string) (string, bool) {
	b, err := os.ReadFile(filepath.Join(varDir, headIncompleteName))
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(b)), true
}

// writeFileDurably writes b as the file p, through a temporary file renamed
// over it, both synced: a reader sees the former content or the new one,
// after a crash too.
func writeFileDurably(p string, b []byte) error {
	dir := filepath.Dir(p)
	f, err := os.CreateTemp(dir, "."+filepath.Base(p)+".*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() { _ = os.Remove(tmp) }()
	if _, err := f.Write(b); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, p); err != nil {
		return err
	}
	return syncDir(dir)
}

// syncDir syncs the directory dir, making the renames in it durable.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
