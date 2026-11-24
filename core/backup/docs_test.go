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

// A promotion records the cluster promoted as the primary under the
// identity of the source it takes over from, and under its own: each cluster
// reads the document kept under its own identity. The way back is another
// promotion, of the former source over the cluster promoted, one epoch
// later. A promotion asked again keeps its epoch.
func TestPromoteThereAndBack(t *testing.T) {
	ctx := context.Background()
	docs := memDocs{}
	prod := Primary{ClusterID: "prod-id", ClusterName: "prod", ObjectID: "oa"}
	drp := Primary{ClusterID: "drp-id", ClusterName: "drp", ObjectID: "ob", PromotedFromRun: "run1"}

	_, found, err := ReadPrimary(ctx, docs, "prod-id", "oa")
	require.NoError(t, err)
	assert.False(t, found, "an object nobody was promoted over records no primary")

	p, err := Promote(ctx, docs, "prod-id", "oa", drp)
	require.NoError(t, err)
	assert.EqualValues(t, 1, p.Epoch)
	assert.False(t, p.PromotedAt.IsZero())
	for _, id := range [][2]string{{"prod-id", "oa"}, {"drp-id", "ob"}} {
		got, found, err := ReadPrimary(ctx, docs, id[0], id[1])
		require.NoError(t, err)
		require.True(t, found, "%v", id)
		assert.True(t, got.Names("drp-id"), "%v", id)
		assert.False(t, got.Names("prod-id"), "%v", id)
		assert.Equal(t, "ob", got.ObjectID, "the source finds the object to follow")
		assert.Equal(t, "run1", got.PromotedFromRun)
	}

	// Asked again, as after a failure between the record and the rest.
	again, err := Promote(ctx, docs, "prod-id", "oa", drp)
	require.NoError(t, err)
	assert.EqualValues(t, 1, again.Epoch, "a promotion asked again is the same promotion")
	assert.True(t, again.PromotedAt.Equal(p.PromotedAt))

	// The record of the cluster promoted lost: asking again writes it.
	delete(docs, "drp-id/ob/primary")
	_, err = Promote(ctx, docs, "prod-id", "oa", drp)
	require.NoError(t, err)
	_, found, err = ReadPrimary(ctx, docs, "drp-id", "ob")
	require.NoError(t, err)
	assert.True(t, found)

	// Back: the former source, a replica of the cluster promoted, is
	// promoted over it.
	prod.PromotedFromRun = "run9"
	back, err := Promote(ctx, docs, "drp-id", "ob", prod)
	require.NoError(t, err)
	assert.EqualValues(t, 2, back.Epoch)
	for _, id := range [][2]string{{"prod-id", "oa"}, {"drp-id", "ob"}} {
		got, _, err := ReadPrimary(ctx, docs, id[0], id[1])
		require.NoError(t, err)
		assert.True(t, got.Names("prod-id"), "%v", id)
		assert.EqualValues(t, 2, got.Epoch, "%v", id)
	}

	_, err = Promote(ctx, docs, "prod-id", "oa", prod)
	assert.ErrorContains(t, err, "over itself")
}

// A primary that can not be read is an error: a cluster not knowing who
// writes the runs is told so, and neither that it does nor that nobody was
// promoted.
func TestReadPrimaryUnreadable(t *testing.T) {
	ctx := context.Background()
	docs := memDocs{"c/o/primary": []byte("{")}
	_, found, err := ReadPrimary(ctx, docs, "c", "o")
	assert.Error(t, err)
	assert.False(t, found)
	docs["c/o/primary"] = []byte("{}")
	_, found, err = ReadPrimary(ctx, docs, "c", "o")
	assert.ErrorContains(t, err, "names no cluster")
	assert.False(t, found)
}
