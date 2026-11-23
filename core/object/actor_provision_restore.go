package object

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/opensvc/om3/v3/core/backup"
	"github.com/opensvc/om3/v3/core/resource"
	"github.com/opensvc/om3/v3/core/status"
	"github.com/opensvc/om3/v3/core/statusbus"
	"github.com/opensvc/om3/v3/util/hostname"
	"github.com/opensvc/om3/v3/util/key"
)

// provisionRestore is the snapshot a provision restores the heads it
// provisions from, as the restore_from keyword names it.
type provisionRestore struct {
	store backup.Driver
	run   backup.Run

	// attempted says the provision began restoring a head, restored or
	// not. A provision failing before writes nothing into a head, and
	// no receipt.
	attempted bool
}

// ackTimeout bounds the writing of a receipt, which a provision interrupted
// still writes.
const ackTimeout = time.Minute

// provisionRestoreSource returns the snapshot restore_from names, nil when it
// names none.
func (t *actor) provisionRestoreSource(ctx context.Context) (*provisionRestore, error) {
	s := t.config.GetString(key.Parse("restore_from"))
	if s == "" {
		return nil, nil
	}
	// The store reference is one word for a store of the cluster
	// configuration, from <sec> store#<name> for one of a sec.
	fields := strings.Fields(s)
	if len(fields) != 3 && len(fields) != 5 {
		return nil, fmt.Errorf("restore_from %q: want <store> <object path> <snapshot id>", s)
	}
	n, err := t.Node()
	if err != nil {
		return nil, err
	}
	last := len(fields) - 2
	store, err := n.Store(strings.Join(fields[:last], " "), t.path)
	if err != nil {
		return nil, fmt.Errorf("restore_from: %w", err)
	}
	// The id names the run: the path says what it was of.
	run, err := backup.FindRun(ctx, store, backup.RunFilter{ID: fields[last+1]})
	if err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("restore_from: %w", err)
	}
	return &provisionRestore{store: store, run: run}, nil
}

// restoreProvisionedHead restores into the head of r the member of the
// snapshot named after it, r being one the provision moved from
// unprovisioned to provisioned: its head holds nothing yet. A head the
// provision did not start, as a shared one off the placement leader, is left
// to the node starting it.
func (t *actor) restoreProvisionedHead(ctx context.Context, src *provisionRestore, r resource.Driver) error {
	rid := r.RID()
	if !src.run.Has(rid) {
		return nil
	}
	h, ok := r.(interface{ Head() string })
	if !ok {
		return nil
	}
	dir := h.Head()
	switch st := statusbus.FromContext(ctx).Get(rid); {
	case dir == "":
		return fmt.Errorf("restore %s: no head directory", rid)
	case st.Is(status.Up, status.StandbyUp):
	case st.Is(status.NotApplicable):
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			t.log.Attr("rid", rid).Infof("%s: skip the restore, its head %s is not there", rid, dir)
			return nil
		}
	default:
		t.log.Attr("rid", rid).Infof("%s: skip the restore, not started here", rid)
		return nil
	}
	t.log.Attr("rid", rid).Infof("%s: restore from snapshot %s of %s in store %s into %s", rid, src.run.ShortID(), src.run.Path, src.store.Ref(), dir)
	src.attempted = true
	return restoreMarked(ctx, src.store, src.run, rid, dir, r.VarDir())
}

// restoreMarked restores into dir, the head of the resource rid, its member
// of run, the head marked incomplete in varDir, the var directory of the
// resource, from before it is written until it is restored: a provision
// failing or killed meanwhile leaves a head a start refuses to run on, until
// a restore of it, or a forced start. The unprovision of the resource clears
// the mark with the head.
//
// The head is not staged: just provisioned, it holds nothing a stage would
// keep, and a provision failing is one to do again.
func restoreMarked(ctx context.Context, store backup.Store, run backup.Run, rid, dir, varDir string) error {
	reason := fmt.Sprintf("the provision restoring snapshot %s into it did not finish: restore it, or provision it again", run.ShortID())
	if err := resource.MarkHeadIncomplete(varDir, reason); err != nil {
		return fmt.Errorf("mark %s incomplete: %w", rid, err)
	}
	if err := store.Restore(ctx, run, rid, dir); err != nil {
		return fmt.Errorf("restore %s: %w", rid, err)
	}
	return resource.ClearHeadIncomplete(varDir)
}

// acknowledgeProvisionRestore writes into the store the receipt of the
// restore a provision did, or failed with provisionErr, from a snapshot of
// another cluster: this cluster is then a replica of the object the snapshot
// is of, which the cluster writing its runs judges from the receipt.
//
// A provision restoring no head writes none, as the one of a node the shared
// heads are not started on, one of an instance provisioned already, or one
// failing before it wrote into a head: it changes nothing of what the
// instance holds. A receipt that can not be written
// is said and does not fail the provision.
func (t *actor) acknowledgeProvisionRestore(ctx context.Context, src *provisionRestore, provisionErr error) {
	if src == nil || !src.attempted {
		return
	}
	n, err := t.Node()
	if err != nil {
		return
	}
	clusterID, clusterName := n.Cluster()
	if src.run.ClusterID == "" || clusterID == "" || src.run.ClusterID == clusterID {
		return
	}
	receipt := backup.Receipt{
		ReplicaClusterID:   clusterID,
		ReplicaClusterName: clusterName,
		Node:               hostname.Hostname(),
	}
	if provisionErr != nil {
		receipt.RestoreError = provisionErr.Error()
	}
	switch alerts, err := t.config.Validate(); {
	case err != nil:
		receipt.ConfigErrors = []string{err.Error()}
	case alerts.HasError():
		for _, alert := range alerts.Errors() {
			receipt.ConfigErrors = append(receipt.ConfigErrors, alert.StringWithoutMeta())
		}
	default:
		receipt.ConfigValid = true
	}
	// A provision interrupted, by its timeout or by the user, still says
	// it failed.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), ackTimeout)
	defer cancel()
	written, err := backup.Acknowledge(ctx, src.store, src.run, receipt)
	switch {
	case errors.Is(err, backup.ErrNoDocs):
		t.log.Debugf("no receipt of snapshot %s for the cluster %s: store %s keeps no documents", src.run.ShortID(), src.run.ClusterName, src.store.Ref())
	case err != nil:
		t.log.Warnf("write the receipt of snapshot %s for the cluster %s into store %s: %s", src.run.ShortID(), src.run.ClusterName, src.store.Ref(), err)
	case written:
		t.log.Infof("write the receipt of snapshot %s into store %s, for the cluster %s to see this replica", src.run.ShortID(), src.store.Ref(), src.run.ClusterName)
	}
}
