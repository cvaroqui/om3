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
	// per replica cluster, each restore replacing the one before.
	Receipt struct {
		// ReplicaClusterID and ReplicaClusterName are the cluster that
		// restored, and Node its node that did.
		ReplicaClusterID   string `json:"replica_cluster_id"`
		ReplicaClusterName string `json:"replica_cluster_name"`
		Node               string `json:"node"`

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
		// its source will apply the configuration of the run, making
		// it its own.
		ConfigDigest string `json:"config_digest"`

		// ConfigValid says the configuration of the object on the
		// replica validates there after the restore, and ConfigErrors
		// what refuses it when it does not.
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

// ReceiptName returns the name of the document holding the receipt of the
// replica cluster.
func ReceiptName(replicaClusterID string) string {
	return receiptPrefix + replicaClusterID
}

// ReadReceipts returns the receipts the replica clusters wrote for the object
// objectID of the cluster clusterID, sorted by replica cluster id. A receipt
// that can not be read is reported in the error, beside the ones that are.
func ReadReceipts(ctx context.Context, a Annotator, clusterID, objectID string) ([]Receipt, error) {
	names, err := a.Docs(ctx, clusterID, objectID)
	if err != nil {
		return nil, err
	}
	l := make([]Receipt, 0)
	var errs error
	for _, name := range names {
		replica, ok := strings.CutPrefix(name, receiptPrefix)
		if !ok {
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
		if r.ReplicaClusterID != replica {
			// A cluster writes its own receipt only, under its own
			// id: one saying another id is not to be trusted on the
			// rest.
			errs = errors.Join(errs, fmt.Errorf("%s: written for the cluster %q", name, r.ReplicaClusterID))
			continue
		}
		l = append(l, r)
	}
	slices.SortFunc(l, func(a, b Receipt) int { return strings.Compare(a.ReplicaClusterID, b.ReplicaClusterID) })
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
	name := ReceiptName(r.ReplicaClusterID)
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

// ConfigKeys returns the keywords of cfg and their values, as written,
// restore_from aside: the once of a deploy from a snapshot is not a
// difference between two configurations.
func ConfigKeys(cfg *xconfig.T) map[string]string {
	m := make(map[string]string)
	for _, section := range cfg.SectionStrings() {
		for _, option := range cfg.Keys(section) {
			if section == "DEFAULT" && option == "restore_from" {
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
