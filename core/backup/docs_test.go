package backup

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// memDocs is an Annotator keeping its documents in memory.
type memDocs map[string][]byte

func (t memDocs) PutDoc(_ context.Context, clusterID, objectID, name string, b []byte) error {
	t[clusterID+"/"+objectID+"/"+name] = b
	return nil
}

func (t memDocs) GetDoc(_ context.Context, clusterID, objectID, name string) ([]byte, error) {
	b, ok := t[clusterID+"/"+objectID+"/"+name]
	if !ok {
		return nil, fmt.Errorf("%s: %w", name, fs.ErrNotExist)
	}
	return b, nil
}

func (t memDocs) Docs(_ context.Context, clusterID, objectID string) ([]string, error) {
	l := make([]string, 0)
	for k := range t {
		if name, ok := strings.CutPrefix(k, clusterID+"/"+objectID+"/"); ok {
			l = append(l, name)
		}
	}
	slices.Sort(l)
	return l, nil
}

// A replica keeps one receipt per object of a source cluster, each write
// replacing the one before and counting one more, dated as it is written.
func TestWriteAndReadReceipts(t *testing.T) {
	ctx := context.Background()
	docs := memDocs{}

	l, err := ReadReceipts(ctx, docs, "prod", "oid")
	require.NoError(t, err)
	assert.Empty(t, l)

	require.Error(t, WriteReceipt(ctx, docs, "prod", "oid", Receipt{}), "a receipt of no cluster")

	before := time.Now()
	require.NoError(t, WriteReceipt(ctx, docs, "prod", "oid", Receipt{ReplicaClusterID: "drp", RunID: "run1", Seq: 40}))
	require.NoError(t, WriteReceipt(ctx, docs, "prod", "oid", Receipt{ReplicaClusterID: "drp", RunID: "run2"}))
	require.NoError(t, WriteReceipt(ctx, docs, "prod", "oid", Receipt{ReplicaClusterID: "drp2", RunID: "run1"}))
	require.NoError(t, WriteReceipt(ctx, docs, "prod", "other", Receipt{ReplicaClusterID: "drp", RunID: "run9"}))

	l, err = ReadReceipts(ctx, docs, "prod", "oid")
	require.NoError(t, err)
	require.Len(t, l, 2, "one per replica cluster, of this object")
	assert.Equal(t, "drp", l[0].ReplicaClusterID)
	assert.Equal(t, "run2", l[0].RunID, "the last written")
	assert.Equal(t, uint64(2), l[0].Seq, "one more than the one replaced, whatever the writer said")
	assert.False(t, l[0].WrittenAt.Before(before.Add(-time.Second)))
	assert.Equal(t, "drp2", l[1].ReplicaClusterID)
	assert.Equal(t, uint64(1), l[1].Seq)

	// The receipts that can not be trusted are told, beside the others:
	// one not a receipt, and one saying another cluster than the one its
	// name does.
	docs["prod/oid/receipt.bad"] = []byte("{")
	b, err := json.Marshal(Receipt{ReplicaClusterID: "drp"})
	require.NoError(t, err)
	docs["prod/oid/receipt.forged"] = b
	docs["prod/oid/primary"] = []byte("{}")
	l, err = ReadReceipts(ctx, docs, "prod", "oid")
	assert.ErrorContains(t, err, "receipt.bad")
	assert.ErrorContains(t, err, "receipt.forged")
	assert.Len(t, l, 2)

	// A former receipt that can not be read is replaced.
	docs["prod/oid/receipt.drp"] = []byte("{")
	require.NoError(t, WriteReceipt(ctx, docs, "prod", "oid", Receipt{ReplicaClusterID: "drp", RunID: "run3"}))
	b, err = docs.GetDoc(ctx, "prod", "oid", Receipt{ReplicaClusterID: "drp"}.DocName())
	require.NoError(t, err)
	var r Receipt
	require.NoError(t, json.Unmarshal(b, &r))
	assert.Equal(t, uint64(1), r.Seq)
}

// The fields of a receipt are named in snake case in the store.
func TestReceiptJSON(t *testing.T) {
	b, err := json.Marshal(Receipt{ConfigErrors: []string{"x"}, RestoreError: "y"})
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	for _, k := range []string{"replica_cluster_id", "replica_cluster_name", "node", "seq", "seen_run_id", "run_id", "config_digest", "config_valid", "config_errors", "staged", "restore_error", "written_at"} {
		assert.Contains(t, m, k)
	}
	assert.Len(t, m, 12)
}

// The digest of a configuration is the one of its keywords and their values:
// how it is written does not change it, nor does restore_from, and any other
// keyword does.
func TestConfigDigest(t *testing.T) {
	a, err := ConfigDigest([]byte("[DEFAULT]\nid = 1\nnodes = n1 n2\n\n[fs#1]\ntype = flag\n"))
	require.NoError(t, err)
	assert.Len(t, a, 64)
	b, err := ConfigDigest([]byte("[fs#1]\ntype=flag\n[DEFAULT]\nnodes = n1 n2\nrestore_from = s1 ns1/svc/svc1 run1\nid = 1\n"))
	require.NoError(t, err)
	assert.Equal(t, a, b, "written differently, and deployed from a snapshot")
	c, err := ConfigDigest([]byte("[DEFAULT]\nid = 1\nnodes = n1 n2\n\n[fs#1]\ntype = flag\n\n[fs#2]\ntype = flag\n"))
	require.NoError(t, err)
	assert.NotEqual(t, a, c, "a section more")
	d, err := ConfigDigest([]byte("[DEFAULT]\nid = 1\nnodes = n1\n\n[fs#1]\ntype = flag\n"))
	require.NoError(t, err)
	assert.NotEqual(t, a, d, "a value changed")
}
