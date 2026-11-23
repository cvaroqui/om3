package object

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/opensvc/om3/v3/core/backup"
	"github.com/opensvc/om3/v3/core/resource"
)

// markStore is a store whose restore writes a file into the head, after
// calling during, and fails with err when set.
type markStore struct {
	backup.Store
	during func()
	err    error
}

func (t markStore) Restore(_ context.Context, _ backup.Run, _ string, dst string) error {
	if t.during != nil {
		t.during()
	}
	if t.err != nil {
		return t.err
	}
	return os.WriteFile(filepath.Join(dst, "data"), []byte("restored"), 0600)
}

// A provision restoring a head from a snapshot marks it incomplete while it
// writes it: restored, the mark is cleared, and a restore failing leaves it,
// with the reason, for a start to refuse the head.
func TestProvisionRestoreMarksTheHeadIncomplete(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	varDir := filepath.Join(t.TempDir(), "fs#1")
	run := backup.Run{ID: "30aa87df-6b62-476c-886a-2b2eb429eaf1"}
	marked := func() bool {
		_, ok := resource.HeadIncomplete(varDir)
		return ok
	}

	store := markStore{during: func() {
		assert.True(t, marked(), "marked before the head is written")
	}}
	require.NoError(t, restoreMarked(ctx, store, run, "fs#1", dir, varDir))
	assert.FileExists(t, filepath.Join(dir, "data"))
	assert.False(t, marked(), "cleared once restored")

	store.err = errors.New("the store is gone")
	err := restoreMarked(ctx, store, run, "fs#1", dir, varDir)
	require.ErrorContains(t, err, "restore fs#1: the store is gone")
	reason, ok := resource.HeadIncomplete(varDir)
	require.True(t, ok, "a restore failing leaves the mark")
	assert.Contains(t, reason, "30aa87df")
}
