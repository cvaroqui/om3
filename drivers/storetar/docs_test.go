package storetar

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/opensvc/om3/v3/core/backup"
)

// A document is read back as written, replaced by the next one of its name,
// and listed by name: one not there is told as fs.ErrNotExist.
func TestDocsRoundTrip(t *testing.T) {
	ctx := context.Background()
	storeDir := t.TempDir()
	s := newStore(t, "file://"+storeDir)

	names, err := s.Docs(ctx, "c1", "oid")
	require.NoError(t, err)
	assert.Empty(t, names, "an object with no document")
	_, err = s.GetDoc(ctx, "c1", "oid", "receipt.c2")
	assert.ErrorIs(t, err, fs.ErrNotExist)

	require.NoError(t, s.PutDoc(ctx, "c1", "oid", "receipt.c2", []byte(`{"seq":1}`)))
	require.NoError(t, s.PutDoc(ctx, "c1", "oid", "receipt.c3", []byte(`{"seq":7}`)))
	require.NoError(t, s.PutDoc(ctx, "c1", "oid", "receipt.c2", []byte(`{"seq":2}`)))
	require.NoError(t, s.PutDoc(ctx, "c1", "other", "receipt.c2", []byte(`{"seq":9}`)))

	b, err := s.GetDoc(ctx, "c1", "oid", "receipt.c2")
	require.NoError(t, err)
	assert.Equal(t, `{"seq":2}`, string(b), "the last written")
	names, err = s.Docs(ctx, "c1", "oid")
	require.NoError(t, err)
	assert.Equal(t, []string{"receipt.c2", "receipt.c3"}, names, "of this object only, sorted")
	assert.FileExists(t, filepath.Join(storeDir, "c1", "oid", "docs", "receipt.c2.json"))

	entries, err := os.ReadDir(filepath.Join(storeDir, "c1", "oid", "docs"))
	require.NoError(t, err)
	for _, e := range entries {
		assert.False(t, strings.HasSuffix(e.Name(), tmpSuffix), "no temporary file left: %s", e.Name())
	}
}

// A document name is one path element, and not the one of a manifest: it
// names a file of the store.
func TestDocsRefuseNames(t *testing.T) {
	ctx := context.Background()
	s := newStore(t, "file://"+t.TempDir())
	for _, name := range []string{"", ".", "..", "a/b", "../x", "a\\b", "manifest"} {
		assert.Error(t, s.PutDoc(ctx, "c1", "oid", name, []byte("{}")), "put %q", name)
		_, err := s.GetDoc(ctx, "c1", "oid", name)
		assert.Error(t, err, "get %q", name)
		assert.NotErrorIs(t, err, fs.ErrNotExist, "get %q: refused, not looked for", name)
	}
	assert.Error(t, s.PutDoc(ctx, "../c1", "oid", "doc", []byte("{}")))
	assert.Error(t, s.PutDoc(ctx, "c1", "a/b", "doc", []byte("{}")))
	_, err := s.Docs(ctx, "c1", "..")
	assert.Error(t, err)
}

// The directory of the documents of an object is not a run: the listings of
// the runs, the recount of the object and its stats are the same with it,
// whatever file it holds, and no run is written as it.
func TestListingsIgnoreDocs(t *testing.T) {
	ctx := context.Background()
	storeDir := t.TempDir()
	s := newStore(t, "file://"+storeDir)
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "a.txt"), "a", 0640)

	write := func(id string, tm time.Time) backup.Run {
		run := newRun(id, tm)
		require.NoError(t, s.Backup(ctx, run, "fs#1", src, src, nil))
		require.NoError(t, s.Complete(ctx, run))
		return run
	}
	run1 := write("run1", time.Date(2026, 10, 20, 2, 0, 0, 0, time.UTC))

	runsBefore, err := s.Runs(ctx, backup.RunFilter{})
	require.NoError(t, err)
	statsBefore, err := s.ObjectStats(ctx)
	require.NoError(t, err)

	require.NoError(t, s.PutDoc(ctx, "c1", "oid", "receipt.c2", []byte(`{"seq":1}`)))
	// A manifest in the directory of the documents, which PutDoc refuses
	// to write, as another writer of the store could leave one.
	docs := filepath.Join(storeDir, "c1", "oid", docsDir)
	manifest, err := os.ReadFile(filepath.Join(storeDir, "c1", "oid", "run1", manifestName))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(docs, manifestName), manifest, 0600))
	// The documents of an object no run of which is in the store.
	require.NoError(t, s.PutDoc(ctx, "c1", "norun", "receipt.c2", []byte(`{"seq":1}`)))

	for _, filter := range []backup.RunFilter{{}, {Incomplete: true}, {ObjectID: "oid"}, {Path: "ns1/svc/svc1"}, {Cluster: "c1"}, {Cluster: "cluster1"}, {Since: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}} {
		runs, err := s.Runs(ctx, filter)
		require.NoError(t, err, "%+v", filter)
		assert.Equal(t, runsBefore, runs, "%+v", filter)
	}
	runs, err := s.Runs(ctx, backup.RunFilter{ObjectID: "norun"})
	require.NoError(t, err)
	assert.Empty(t, runs)

	l, err := objectRuns(localFS{}, filepath.Join(storeDir, "c1", "oid"))
	require.NoError(t, err)
	require.Len(t, l, 1)
	assert.Equal(t, "run1", l[0].ID)

	stats, err := s.ObjectStats(ctx)
	require.NoError(t, err)
	assert.Equal(t, statsBefore, stats)

	// A run completed and one removed recount the object: the documents
	// are neither counted nor removed.
	run2 := write("run2", time.Date(2026, 10, 21, 2, 0, 0, 0, time.UTC))
	stats, err = s.ObjectStats(ctx)
	require.NoError(t, err)
	require.Len(t, stats, 1)
	assert.Equal(t, 2, stats[0].Runs)
	require.NoError(t, s.Delete(ctx, []backup.Run{run1, run2}))
	stats, err = s.ObjectStats(ctx)
	require.NoError(t, err)
	require.Len(t, stats, 1)
	assert.Equal(t, 0, stats[0].Runs)
	b, err := s.GetDoc(ctx, "c1", "oid", "receipt.c2")
	require.NoError(t, err)
	assert.Equal(t, `{"seq":1}`, string(b), "the documents outlive the runs")
	require.NoError(t, s.Check(ctx, backup.RunFilter{}))

	assert.Error(t, s.Backup(ctx, newRun(docsDir, time.Now()), "fs#1", src, src, nil), "no run named as the directory of the documents")
}
