package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"time"

	"github.com/opensvc/om3/v3/core/xconfig"
	"github.com/opensvc/om3/v3/util/key"
)

type (
	// Annotator is a store keeping small documents beside the runs of an
	// object: what the clusters reading the runs of another say back to it,
	// as the receipt of a restore. A store without it holds runs only.
	Annotator interface {
		// PutDoc writes the document name of the object objectID of
		// the cluster clusterID, replacing the one it holds.
		PutDoc(ctx context.Context, clusterID, objectID, name string, b []byte) error

		// GetDoc returns the document, and fs.ErrNotExist when the
		// store holds none of that name.
		GetDoc(ctx context.Context, clusterID, objectID, name string) ([]byte, error)

		// Docs returns the names of the documents of the object,
		// sorted.
		Docs(ctx context.Context, clusterID, objectID string) ([]string, error)
	}

	// Receipt is what a cluster restoring the runs of an object of another
	// cluster says of its last restore, in the store, beside the runs: the
	// source cluster reads it to judge the replica, on its own clock.
	//
	// It is kept under the cluster id and the object id of the source, one
	// per replica cluster, each restore replacing the one before: the
	// nodes of a failover replica hold one copy, the one of the node
	// following. A flex replica holds a copy per node, each followed by
	// its node, which writes a receipt of its own, PerNode saying so.
	Receipt struct {
		// ReplicaClusterID and ReplicaClusterName are the cluster that
		// restored, and Node its node that did.
		ReplicaClusterID   string `json:"replica_cluster_id"`
		ReplicaClusterName string `json:"replica_cluster_name"`
		Node               string `json:"node"`

		// PerNode says the receipt is the one of the copy Node holds,
		// one of several the replica cluster keeps, as the instances
		// of a flex replica: it is kept apart from the ones of the
		// other nodes, and judged apart.
		PerNode bool `json:"per_node,omitempty"`

		// SkippedHeads are the members of the run the node did not
		// restore: the heads of the resources flagged shared, one
		// storage every node of the replica cluster reaches, which
		// its placement leader alone restores. The copy of the node
		// holds the run for its other heads, and its shared heads are
		// as fresh as the freshest copy of its cluster skipping none.
		SkippedHeads []string `json:"skipped_heads,omitempty"`

		// Seq counts the receipts the replica wrote for the object,
		// each one more than the one it replaces.
		Seq uint64 `json:"seq"`

		// SeenRunID is the latest completed run of the object the
		// source wrote that the replica listed when it restored: a
		// replica reading the store sees the runs as they complete, so
		// one not seeing the latest is not reading it.
		SeenRunID string `json:"seen_run_id"`

		// RunID is the run the replica restored.
		RunID string `json:"run_id"`

		// ConfigDigest is the digest of the configuration of the run
		// restored, as ConfigDigest computes it: the one the run
		// saved, not the one of the object on the replica, which a
		// restore of the heads leaves as it is. A replica following
		// its source applies the configuration of the run before it
		// restores its heads, making it its own: its receipt then
		// carries the digest of the last configuration it applied,
		// which is not the one of the latest run when it refused
		// that one.
		ConfigDigest string `json:"config_digest"`

		// ConfigValid says the configuration of the object on the
		// replica validates there after the restore, and ConfigErrors
		// what refuses it when it does not. A replica refusing to
		// apply the configuration of the latest run of its source
		// says so here too, with why.
		ConfigValid  bool     `json:"config_valid"`
		ConfigErrors []string `json:"config_errors,omitempty"`

		// Staged says the run is restored aside, not yet into the
		// heads of the object.
		Staged bool `json:"staged"`

		// RestoreError is why the restore failed, empty when it did
		// not.
		RestoreError string `json:"restore_error,omitempty"`

		// WrittenAt is when the receipt was written, on the clock of
		// the replica: it is shown, never compared with a time of
		// another cluster, whose clock is another.
		WrittenAt time.Time `json:"written_at"`
	}
)

// receiptPrefix is the prefix of the names of the documents holding receipts,
// each followed by the id of the replica cluster.
const receiptPrefix = "receipt."

// ErrNoDocs is returned by what needs the documents of a store keeping none.
var ErrNoDocs = errors.New("the store keeps no documents")

// DocName returns the name of the document holding the receipt: the one of
// its replica cluster, or the one of its node of that cluster when the
// receipt is of the copy a node holds. A cluster id holds no dot, a node name
// may: the node is what follows the first one.
func (r Receipt) DocName() string {
	if r.PerNode {
		return receiptPrefix + r.ReplicaClusterID + "." + r.Node
	}
	return receiptPrefix + r.ReplicaClusterID
}

// ReadReceipts returns the receipts the replica clusters wrote for the object
// objectID of the cluster clusterID, sorted by replica cluster id then by
// node, the receipt of a cluster ahead of the ones of its nodes. A receipt
// that can not be read is reported in the error, beside the ones that are.
func ReadReceipts(ctx context.Context, a Annotator, clusterID, objectID string) ([]Receipt, error) {
	names, err := a.Docs(ctx, clusterID, objectID)
	if err != nil {
		return nil, err
	}
	l := make([]Receipt, 0)
	var errs error
	for _, name := range names {
		if !strings.HasPrefix(name, receiptPrefix) {
			continue
		}
		b, err := a.GetDoc(ctx, clusterID, objectID, name)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			// removed meanwhile
			continue
		case err != nil:
			errs = errors.Join(errs, fmt.Errorf("%s: %w", name, err))
			continue
		}
		var r Receipt
		if err := json.Unmarshal(b, &r); err != nil {
			errs = errors.Join(errs, fmt.Errorf("%s: %w", name, err))
			continue
		}
		if r.DocName() != name {
			// A cluster writes its own receipt only, under its own
			// id, and a node its own: one saying another is not to
			// be trusted on the rest.
			errs = errors.Join(errs, fmt.Errorf("%s: written for the cluster %q, by the node %q", name, r.ReplicaClusterID, r.Node))
			continue
		}
		l = append(l, r)
	}
	slices.SortFunc(l, func(a, b Receipt) int {
		if c := strings.Compare(a.ReplicaClusterID, b.ReplicaClusterID); c != 0 {
			return c
		}
		if a.PerNode != b.PerNode {
			if b.PerNode {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Node, b.Node)
	})
	return l, errs
}

// WriteReceipt writes the receipt r of its replica cluster for the object
// objectID of the cluster clusterID, the source, replacing the one the
// replica wrote before: its Seq is one more than that one's, and its
// WrittenAt is now.
func WriteReceipt(ctx context.Context, a Annotator, clusterID, objectID string, r Receipt) error {
	if r.ReplicaClusterID == "" {
		return fmt.Errorf("a receipt names the cluster writing it")
	}
	if r.PerNode && r.Node == "" {
		return fmt.Errorf("a receipt of the copy of a node names the node")
	}
	name := r.DocName()
	r.Seq = 1
	if b, err := a.GetDoc(ctx, clusterID, objectID, name); err == nil {
		var prev Receipt
		// A former receipt that can not be read is replaced, the
		// count starting again.
		if json.Unmarshal(b, &prev) == nil {
			r.Seq = prev.Seq + 1
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	r.WrittenAt = time.Now().UTC()
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return a.PutDoc(ctx, clusterID, objectID, name, b)
}

// Acknowledge writes into the store s the receipt r of a restore of run, a
// run of another cluster than the one restoring, which r names. It fills the
// runs of the receipt: the one restored, and the latest completed run of the
// object its cluster wrote, as the store lists them now. The digest of the
// configuration of the run is read from the store when r holds none.
//
// It writes nothing, and says so with false, for a run of the cluster
// restoring: a cluster restoring its own runs is not a replica. A store
// keeping no documents returns ErrNoDocs.
func Acknowledge(ctx context.Context, s Store, run Run, r Receipt) (bool, error) {
	if run.ClusterID == "" || r.ReplicaClusterID == "" || run.ClusterID == r.ReplicaClusterID {
		return false, nil
	}
	a, ok := s.(Annotator)
	if !ok {
		return false, ErrNoDocs
	}
	r.RunID = run.ID
	r.SeenRunID = run.ID
	// The runs are listed after the restore, not before it: the receipt
	// says what the replica sees as it writes it.
	if runs, err := s.Runs(ctx, RunFilter{ObjectID: run.ObjectID, Cluster: run.ClusterID}); err == nil && len(runs) > 0 {
		r.SeenRunID = runs[len(runs)-1].ID
	}
	if r.ConfigDigest == "" && run.Has(MemberConfig) {
		b, err := ReadMember(ctx, s, run, MemberConfig)
		if err != nil {
			return false, fmt.Errorf("read the configuration of run %s: %w", run.ShortID(), err)
		}
		if r.ConfigDigest, err = ConfigDigest(b); err != nil {
			return false, fmt.Errorf("the configuration of run %s: %w", run.ShortID(), err)
		}
	}
	if err := WriteReceipt(ctx, a, run.ClusterID, run.ObjectID, r); err != nil {
		return false, err
	}
	return true, nil
}

// DaemonSetOptions are the keywords of the DEFAULT section the daemon sets
// on an object created from a store, and nobody else: the snapshot its
// provision restores from, the object it is a replica of, and the grants its
// followed configurations are judged under. They say what the object is on
// its cluster, not what its configuration is.
var DaemonSetOptions = []string{"restore_from", "replica_of", "replica_grants"}

// ConfigKeys returns the keywords of cfg and their values, as written, the
// DaemonSetOptions aside: the once of a deploy from a snapshot, or what makes
// a replica of an object, is not a difference between two configurations.
func ConfigKeys(cfg *xconfig.T) map[string]string {
	m := make(map[string]string)
	for _, section := range cfg.SectionStrings() {
		for _, option := range cfg.Keys(section) {
			if section == "DEFAULT" && slices.Contains(DaemonSetOptions, option) {
				continue
			}
			k := key.New(section, option)
			m[k.String()] = cfg.Get(k)
		}
	}
	return m
}

// ConfigDigest returns the digest of the configuration b, of its keywords and
// their values as ConfigKeys returns them: two configurations written
// differently, as with their sections in another order, have the same digest
// when they set the same keywords to the same values.
func ConfigDigest(b []byte) (string, error) {
	cfg, err := xconfig.NewObject("", b)
	if err != nil {
		return "", err
	}
	m := ConfigKeys(cfg)
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	h := sha256.New()
	for _, k := range keys {
		// Lengths ahead of the texts: no value holding a separator
		// makes two configurations read the same.
		fmt.Fprintf(h, "%d %d %s%s", len(k), len(m[k]), k, m[k])
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Primary is the cluster entitled to write the runs of an object the
// clusters of two sites take turns running, as the store says it: the
// promotion of a replica writes it, and a cluster it does not name writes no
// run of the object from then on, so the cluster a disaster took away does
// not write over its successor when it comes back.
//
// The object is not one object: each cluster holds its own, with an id of
// its own, and writes its runs under its cluster id and that id. So the
// document is kept under each of these identities the promotion knows, the
// one of the source it takes over from, and its own: a cluster reads the one
// kept under its own identity, whichever it is, and finds there who writes,
// and under which identity the runs to follow are. An identity holding no
// document is the primary of its runs, as before any promotion.
type Primary struct {
	// ClusterID and ClusterName are the cluster promoted, and ObjectID
	// the id of the object there: its runs are kept under both.
	ClusterID   string `json:"cluster_id"`
	ClusterName string `json:"cluster_name"`
	ObjectID    string `json:"object_id"`

	// Epoch counts the promotions of the object, each one more than the
	// highest the promotion read.
	Epoch uint64 `json:"epoch"`

	// PromotedFromRun is the run of the source the heads of the replica
	// held when it was promoted, empty when they held none whole.
	PromotedFromRun string `json:"promoted_from_run"`

	// PromotedAt is when, on the clock of the cluster promoted: it is
	// shown, never compared with a time of another cluster.
	PromotedAt time.Time `json:"promoted_at"`
}

// primaryName is the name of the document holding the primary of an object.
const primaryName = "primary"

// Names says the primary is the cluster clusterID.
func (t Primary) Names(clusterID string) bool {
	return clusterID != "" && t.ClusterID == clusterID
}

// ReadPrimary returns the primary recorded under the object objectID of the
// cluster clusterID, and false when none is: the cluster writing the runs is
// then their primary. A document that can not be read is an error, never a
// primary nor the absence of one: a cluster not knowing who writes does not.
func ReadPrimary(ctx context.Context, a Annotator, clusterID, objectID string) (Primary, bool, error) {
	var p Primary
	b, err := a.GetDoc(ctx, clusterID, objectID, primaryName)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return p, false, nil
	case err != nil:
		return p, false, err
	}
	if err := json.Unmarshal(b, &p); err != nil {
		return p, false, fmt.Errorf("the %s document of the object %s of the cluster %s: %w", primaryName, objectID, clusterID, err)
	}
	if p.ClusterID == "" {
		return p, false, fmt.Errorf("the %s document of the object %s of the cluster %s names no cluster", primaryName, objectID, clusterID)
	}
	return p, true, nil
}

// WritePrimary records p as the primary under the object objectID of the
// cluster clusterID, replacing the one recorded.
func WritePrimary(ctx context.Context, a Annotator, clusterID, objectID string, p Primary) error {
	if p.ClusterID == "" || p.ObjectID == "" {
		return fmt.Errorf("a primary names its cluster and its object")
	}
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return a.PutDoc(ctx, clusterID, objectID, primaryName, b)
}

// Promote records the cluster and the object of self as the primary taking
// over from the object objectID of the cluster clusterID, the source its
// replica followed, and returns the primary recorded.
//
// The document of the source is written first: it is the one fencing the
// cluster taken over from, and the one of the cluster promoted, which lets
// it write runs, is of no use before. A promotion interrupted between the
// two, or after them, is finished by asking it again: the source naming the
// cluster promoted already, its epoch is kept, and what is missing written.
func Promote(ctx context.Context, a Annotator, clusterID, objectID string, self Primary) (Primary, error) {
	if self.ClusterID == "" || self.ObjectID == "" {
		return self, fmt.Errorf("a primary names its cluster and its object")
	}
	if self.ClusterID == clusterID {
		return self, fmt.Errorf("a cluster is not promoted over itself")
	}
	source, sourceFound, err := ReadPrimary(ctx, a, clusterID, objectID)
	if err != nil {
		return self, err
	}
	own, ownFound, err := ReadPrimary(ctx, a, self.ClusterID, self.ObjectID)
	if err != nil {
		return self, err
	}
	if sourceFound && source.ClusterID == self.ClusterID && source.ObjectID == self.ObjectID {
		// Promoted already: the fence is up, the rest may not be.
		if !ownFound || own != source {
			if err := WritePrimary(ctx, a, self.ClusterID, self.ObjectID, source); err != nil {
				return source, err
			}
		}
		return source, nil
	}
	self.Epoch = max(source.Epoch, own.Epoch) + 1
	self.PromotedAt = time.Now().UTC()
	if err := WritePrimary(ctx, a, clusterID, objectID, self); err != nil {
		return self, err
	}
	return self, WritePrimary(ctx, a, self.ClusterID, self.ObjectID, self)
}
