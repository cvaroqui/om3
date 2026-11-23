package ressyncstore

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/opensvc/om3/v3/core/backup"
	"github.com/opensvc/om3/v3/core/resource"
	"github.com/opensvc/om3/v3/core/status"
	"github.com/opensvc/om3/v3/core/xconfig"
	"github.com/opensvc/om3/v3/util/hostname"
)

// A cluster restoring the runs of an object of another cluster is a replica
// of it: it writes a receipt of each restore into the store, beside the runs,
// and the status of the resource writing the runs reads the receipts to judge
// the replicas.
//
// Every time compared is one of the cluster judging: when its own runs were
// begun and completed, and now. The clock of the replica is another, which no
// comparison can trust, so the time a receipt says it was written at is shown
// and never weighed. What the replica says is which runs: the one it
// restored, and the latest it listed.
//
// A replica late, not reading the store, or holding a configuration to fix is
// a warning of the resource, and nothing more: the object is available where
// it runs whatever becomes of its replicas.

type (
	// replicaVerdict is what the receipts of the replicas say of them.
	replicaVerdict struct {
		replicas []resource.ReplicaStatus

		// warns are what to warn of.
		warns []string

		// changes are the times the verdict changes at with no event
		// to tell, as a replica breaching its max delay.
		changes []time.Time
	}

	// configer is an object telling its configuration.
	configer interface {
		Config() *xconfig.T
	}
)

// noCopyRPOBreach is the RPO breach time of a replica holding no run this
// cluster can date, as one whose restore failed, or one that restored a run
// this cluster does not know: no time says it lost less than everything.
var noCopyRPOBreach = time.Unix(0, 0)

// ackTimeout bounds the writing of a receipt, which a restore interrupted
// still writes.
const ackTimeout = time.Minute

// Replicas implements the replica lister of core/resource: the replicas the
// last status evaluation judged.
func (t *T) Replicas() []resource.ReplicaStatus {
	return t.replicas
}

// judgeReplicas judges the replicas that wrote receipts, at now, on the clock
// of this cluster. runs are the completed runs of the object this cluster
// wrote, oldest first, maxDelay how long after a time a copy made then is
// stale, zero when nothing says, and digestOf the digest of the
// configuration a run saved.
func judgeReplicas(now time.Time, receipts []backup.Receipt, runs []backup.Run, maxDelay func(time.Time) time.Duration, digestOf func(backup.Run) (string, error)) replicaVerdict {
	var v replicaVerdict
	find := func(id string) (backup.Run, bool) {
		i := slices.IndexFunc(runs, func(run backup.Run) bool { return id != "" && run.ID == id })
		if i < 0 {
			return backup.Run{}, false
		}
		return runs[i], true
	}
	short := func(id string) string {
		return backup.Run{ID: id}.ShortID()
	}
	warn := func(format string, args ...any) {
		v.warns = append(v.warns, fmt.Sprintf(format, args...))
	}
	for _, receipt := range receipts {
		name := receipt.ReplicaClusterName
		if name == "" {
			name = receipt.ReplicaClusterID
		}
		rs := resource.ReplicaStatus{
			Cluster:      receipt.ReplicaClusterName,
			ClusterID:    receipt.ReplicaClusterID,
			RunID:        receipt.RunID,
			SeenRunID:    receipt.SeenRunID,
			ConfigValid:  receipt.ConfigValid,
			ConfigErrors: slices.Clone(receipt.ConfigErrors),
			RestoreError: receipt.RestoreError,
		}
		if receipt.RestoreError != "" {
			warn("replica %s: the restore of run %s failed: %s", name, short(receipt.RunID), receipt.RestoreError)
		}
		// The recovery point: the replica holds what the run it restored
		// saved, as old as the run, which this cluster dated.
		restored, known := find(receipt.RunID)
		switch {
		case receipt.RestoreError != "":
			// The heads are emptied before they are written: a
			// restore that failed leaves neither the run it was
			// restoring nor the one before.
			rs.RPOBreachedAt = noCopyRPOBreach
		case !known:
			rs.RPOBreachedAt = noCopyRPOBreach
			warn("replica %s: restored the unknown run %s, not a run of the object this cluster holds, as one its retention dropped", name, short(receipt.RunID))
		default:
			if d := maxDelay(restored.CreatedAt); d > 0 {
				rs.RPOBreachedAt = restored.CreatedAt.Add(d)
				if now.After(rs.RPOBreachedAt) {
					warn("replica %s: holds run %s, begun at %s, more than %s ago", name, restored.ShortID(), restored.CreatedAt.Format(time.RFC3339), d)
				} else {
					v.changes = append(v.changes, rs.RPOBreachedAt)
				}
			}
		}
		// The liveness: a replica reading the store lists the latest run
		// soon after it completes.
		if len(runs) > 0 {
			latest := runs[len(runs)-1]
			if d := maxDelay(latest.CompletedAt); d > 0 && receipt.SeenRunID != latest.ID {
				if deadline := latest.CompletedAt.Add(d); now.After(deadline) {
					warn("replica %s: has not seen run %s, completed at %s, more than %s ago: it no longer reads the store", name, latest.ShortID(), latest.CompletedAt.Format(time.RFC3339), d)
				} else {
					v.changes = append(v.changes, deadline)
				}
			}
		}
		// The configuration: the one the replica restored is the one of
		// the latest run it listed.
		seen, seenKnown := find(receipt.SeenRunID)
		switch {
		case !seenKnown:
			if known || receipt.SeenRunID != receipt.RunID {
				warn("replica %s: listed the unknown run %s, whose configuration can not be compared with the one it restored", name, short(receipt.SeenRunID))
			}
		case !seen.Has(backup.MemberConfig):
			warn("replica %s: run %s holds no configuration to compare the one it restored with", name, seen.ShortID())
		default:
			digest, err := digestOf(seen)
			switch {
			case err != nil:
				warn("replica %s: the configuration of run %s can not be compared with the one it restored: %s", name, seen.ShortID(), err)
			case digest != receipt.ConfigDigest:
				warn("replica %s: its configuration is not the one of run %s, the latest it listed", name, seen.ShortID())
			default:
				rs.ConfigInSync = true
			}
		}
		if !receipt.ConfigValid {
			if len(receipt.ConfigErrors) > 0 {
				warn("replica %s: its configuration does not validate there: %s", name, strings.Join(receipt.ConfigErrors, "; "))
			} else {
				warn("replica %s: its configuration does not validate there", name)
			}
		}
		v.replicas = append(v.replicas, rs)
	}
	return v
}

// statusReplicas judges the replicas of the object from the receipts they
// wrote into the store s, logs what to warn of, and returns the status they
// add to the one of the resource: warn when a replica needs attention, and
// nothing otherwise. A replica never makes the resource down: the object is
// available where it runs whatever becomes of its replicas.
//
// An object no cluster restores has no receipt, and nothing is said, as of
// one whose store keeps no documents, which can hold no receipt: nothing
// tells yet an object expecting a replica from one that has none. The runs
// of the object are listed only when it has a receipt.
func (t *T) statusReplicas(ctx context.Context, s backup.Store) status.T {
	t.replicas = nil
	a, ok := s.(backup.Annotator)
	if !ok {
		return status.NotApplicable
	}
	clusterID, _ := t.cluster()
	objectID := t.ObjectID.String()
	if clusterID == "" {
		return status.NotApplicable
	}
	receipts, err := backup.ReadReceipts(ctx, a, clusterID, objectID)
	if err != nil {
		// Not a warning of the resource: the store not answering fails
		// the next run, which is what says so.
		t.StatusLog().Warn("replica health unknown: %s", err)
	}
	if len(receipts) == 0 {
		return status.NotApplicable
	}
	runs, err := s.Runs(ctx, backup.RunFilter{ObjectID: objectID, Cluster: clusterID})
	if err != nil {
		t.StatusLog().Warn("replica health unknown: %s", err)
		return status.NotApplicable
	}
	v := judgeReplicas(time.Now(), receipts, runs, t.GetMaxDelay, func(run backup.Run) (string, error) {
		b, err := backup.ReadMember(ctx, s, run, backup.MemberConfig)
		if err != nil {
			return "", err
		}
		return backup.ConfigDigest(b)
	})
	t.replicas = v.replicas
	for _, tm := range v.changes {
		t.StatusLog().ChangesAt(tm)
	}
	if len(v.warns) == 0 {
		return status.NotApplicable
	}
	for _, s := range v.warns {
		t.StatusLog().Warn("%s", s)
	}
	return status.Warn
}

// validateConfig says whether the configuration of the object validates on
// this node, and what refuses it when it does not.
func (t *T) validateConfig() (bool, []string) {
	c, ok := t.GetObjectDriver().(configer)
	if !ok {
		return false, []string{"the configuration can not be read"}
	}
	alerts, err := c.Config().Validate()
	if err != nil {
		return false, []string{err.Error()}
	}
	alerts = alerts.Errors()
	if len(alerts) == 0 {
		return true, nil
	}
	l := make([]string, len(alerts))
	for i, alert := range alerts {
		l[i] = alert.StringWithoutMeta()
	}
	return false, l
}

// acknowledge writes into the store the receipt of the restore of run, done
// or failed with restoreErr, when run is one of another cluster: this cluster
// is then a replica of the object, which the cluster writing the runs judges
// from the receipt. A receipt that can not be written is said and does not
// fail the restore, which is done.
//
// touched says the restore emptied or wrote a head. A restore refused before
// it touched one writes no receipt: the instance holds what it held, the run
// of its last receipt, which the source keeps judging it on. One failing
// after says so, the instance holding neither run whole.
func (t *T) acknowledge(ctx context.Context, s backup.Store, run backup.Run, touched bool, restoreErr error) {
	if restoreErr != nil && !touched {
		return
	}
	clusterID, clusterName := t.cluster()
	t.acknowledgeAs(ctx, s, run, clusterID, clusterName, t.validateConfig, touched, restoreErr)
}

func (t *T) acknowledgeAs(ctx context.Context, s backup.Store, run backup.Run, clusterID, clusterName string, validate func() (bool, []string), touched bool, restoreErr error) {
	if restoreErr != nil && !touched {
		return
	}
	if run.ClusterID == "" || clusterID == "" || run.ClusterID == clusterID {
		return
	}
	receipt := backup.Receipt{
		ReplicaClusterID:   clusterID,
		ReplicaClusterName: clusterName,
		Node:               hostname.Hostname(),
	}
	if restoreErr != nil {
		receipt.RestoreError = restoreErr.Error()
	}
	receipt.ConfigValid, receipt.ConfigErrors = validate()
	// A restore interrupted, by its timeout or by the user, still says it
	// failed.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), ackTimeout)
	defer cancel()
	written, err := backup.Acknowledge(ctx, s, run, receipt)
	switch {
	case errors.Is(err, backup.ErrNoDocs):
		t.Log().Debugf("no receipt of run %s for the cluster %s: store %s keeps no documents", run.ShortID(), run.ClusterName, s.Ref())
	case err != nil:
		t.Log().Warnf("write the receipt of run %s for the cluster %s into store %s: %s", run.ShortID(), run.ClusterName, s.Ref(), err)
	case written:
		t.Log().Infof("write the receipt of run %s into store %s, for the cluster %s to see this replica", run.ShortID(), s.Ref(), run.ClusterName)
	}
}
