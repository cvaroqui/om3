package backup

import (
	"context"
	"io/fs"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The replica_of keyword names the store, by one word or as a store of a
// sec, then the cluster and the object followed.
func TestParseReplicaSource(t *testing.T) {
	src, err := ParseReplicaSource("store1 cid oid")
	require.NoError(t, err)
	assert.Equal(t, ReplicaSource{Store: "store1", ClusterID: "cid", ObjectID: "oid"}, src)
	assert.Equal(t, "store1 cid oid", src.String())
	assert.True(t, src.SameStore(" store1 "))
	assert.False(t, src.SameStore("store2"))

	src, err = ParseReplicaSource("from ns1/sec/s1 store#a cid oid")
	require.NoError(t, err)
	assert.Equal(t, "from ns1/sec/s1 store#a", src.Store)
	assert.Equal(t, "cid", src.ClusterID)
	assert.Equal(t, "oid", src.ObjectID)
	assert.True(t, src.SameStore("from  ns1/sec/s1  store#a"))

	_, err = ParseReplicaSource("store1 cid")
	assert.Error(t, err)
}

// What the daemon sets on a replica is not a difference between its
// configuration and the one of its source: the two have the same digest.
func TestConfigDigestIgnoresWhatMakesAReplica(t *testing.T) {
	source, err := ConfigDigest([]byte("[DEFAULT]\nnodes = p1\ndrpnodes = d1\n\n[fs#1]\ntype = flag\n"))
	require.NoError(t, err)
	replica, err := ConfigDigest([]byte("[DEFAULT]\nnodes = p1\ndrpnodes = d1\nrestore_from = s1 a/svc/b c\nreplica_of = s1 cid oid\nreplica_grants = admin:a\n\n[fs#1]\ntype = flag\n"))
	require.NoError(t, err)
	assert.Equal(t, source, replica)
}

// receiptDocs is a store of documents kept in memory.
type receiptDocs map[string][]byte

func (d receiptDocs) PutDoc(_ context.Context, clusterID, objectID, name string, b []byte) error {
	d[clusterID+"/"+objectID+"/"+name] = b
	return nil
}

func (d receiptDocs) GetDoc(_ context.Context, clusterID, objectID, name string) ([]byte, error) {
	b, ok := d[clusterID+"/"+objectID+"/"+name]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return b, nil
}

func (d receiptDocs) Docs(_ context.Context, clusterID, objectID string) ([]string, error) {
	l := make([]string, 0)
	for k := range d {
		if name, ok := strings.CutPrefix(k, clusterID+"/"+objectID+"/"); ok {
			l = append(l, name)
		}
	}
	slices.Sort(l)
	return l, nil
}

// Each node of a flex replica writes the receipt of the copy it holds, kept
// apart from the ones of the other nodes and counted apart, where the nodes
// of a failover replica share the receipt of their cluster. A receipt under
// the name of another node is not trusted.
func TestReceiptsPerNode(t *testing.T) {
	ctx := context.Background()
	docs := receiptDocs{}
	write := func(r Receipt) {
		t.Helper()
		require.NoError(t, WriteReceipt(ctx, docs, "prod", "oid", r))
	}
	write(Receipt{ReplicaClusterID: "drp", Node: "d2.example.com", PerNode: true, RunID: "run2"})
	write(Receipt{ReplicaClusterID: "drp", Node: "d1", PerNode: true, RunID: "run1"})
	write(Receipt{ReplicaClusterID: "drp", Node: "d1", PerNode: true, RunID: "run2"})
	write(Receipt{ReplicaClusterID: "other", Node: "o1", RunID: "run1"})
	write(Receipt{ReplicaClusterID: "other", Node: "o2", RunID: "run2"})
	assert.Error(t, WriteReceipt(ctx, docs, "prod", "oid", Receipt{ReplicaClusterID: "drp", PerNode: true}), "the copy of a node names the node")

	l, err := ReadReceipts(ctx, docs, "prod", "oid")
	require.NoError(t, err)
	require.Len(t, l, 3)
	assert.Equal(t, "d1", l[0].Node)
	assert.Equal(t, "run2", l[0].RunID)
	assert.EqualValues(t, 2, l[0].Seq, "the node counts its own receipts")
	assert.Equal(t, "d2.example.com", l[1].Node)
	assert.EqualValues(t, 1, l[1].Seq)
	assert.Equal(t, "other", l[2].ReplicaClusterID)
	assert.Equal(t, "o2", l[2].Node, "the nodes of a failover replica replace the receipt of their cluster")
	assert.EqualValues(t, 2, l[2].Seq)
	assert.Equal(t, "receipt.drp.d2.example.com", l[1].DocName())
	assert.Equal(t, "receipt.other", l[2].DocName())

	// d1 writing under the name of d2.
	docs["prod/oid/receipt.drp.d3"] = docs["prod/oid/receipt.drp.d1"]
	l, err = ReadReceipts(ctx, docs, "prod", "oid")
	assert.Error(t, err)
	assert.Len(t, l, 3)
}
