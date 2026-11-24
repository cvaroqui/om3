package ressyncstore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/opensvc/om3/v3/core/backup"
	"github.com/opensvc/om3/v3/core/client"
	"github.com/opensvc/om3/v3/core/object"
	"github.com/opensvc/om3/v3/core/status"
	"github.com/opensvc/om3/v3/core/statusbus"
	"github.com/opensvc/om3/v3/core/xconfig"
	"github.com/opensvc/om3/v3/daemon/api"
	"github.com/opensvc/om3/v3/util/hostname"
	"github.com/opensvc/om3/v3/util/mountinfo"
)

// The sync.store resource of a replica, an object whose replica_of keyword
// names the object of another cluster it is the copy of, follows that object
// instead of writing runs: on the schedule its source writes them on, it
// takes the latest run of the source, applies the configuration it saved,
// and restores its heads, the instance staying stopped.
//
// The configuration comes first, and is the daemon's to apply: it judges
// every keyword the run changes under the grants recorded when the replica
// was created, as a write of its creator, and a configuration refused is not
// applied, its heads not restored, the replica holding the last run it
// followed whole. The heads are restored as a restore in place does, staged
// when their storage stages them.
//
// Each outcome that changes what the replica holds, or what it says of the
// configuration of its source, is written into the store as a receipt, which
// the source reads: a configuration applied or refused, heads restored or a
// restore failing once it touched one. A restore refused before it touched a
// head writes none, the replica holding what its last receipt says.

// followName is the name of the record of what a replica follows, in the var
// directory of the resource.
const followName = "follow.json"

// unknownConfigDigest is the digest a receipt carries for a replica that
// can not say which configuration of its source it holds: it is the digest
// of none, so the source reads its replica as holding another configuration
// than the one it saved.
const unknownConfigDigest = "unknown"

type (
	// followRecord is what a node of a replica keeps of its follows, in
	// the var directory of the resource: the status of the resource reads
	// it, a follow writes it.
	followRecord struct {
		// RunID is the run of the source the heads hold, the last one
		// a follow restored whole, empty when they hold none. It was
		// begun at RunCreatedAt, on the clock of the source, and
		// first listed by this node at RunSeenAt, on its own.
		RunID        string    `json:"run_id,omitempty"`
		RunCreatedAt time.Time `json:"run_created_at,omitzero"`
		RunSeenAt    time.Time `json:"run_seen_at,omitzero"`

		// ConfigRunID is the run of the source whose configuration
		// the object holds, of the digest ConfigDigest.
		ConfigRunID  string `json:"config_run_id,omitempty"`
		ConfigDigest string `json:"config_digest,omitempty"`

		// RefusedRunID is the run of the source whose configuration
		// was refused, for RefusedErrors, empty once a configuration
		// is applied.
		RefusedRunID  string   `json:"refused_run_id,omitempty"`
		RefusedErrors []string `json:"refused_errors,omitempty"`

		// LatestRunID is the latest run of the source this node
		// listed, first at LatestSeenAt on its clock.
		LatestRunID  string    `json:"latest_run_id,omitempty"`
		LatestSeenAt time.Time `json:"latest_seen_at,omitzero"`
	}

	// followEnv is what a follow needs of the node and of the object,
	// which the tests replace.
	followEnv struct {
		// clusterID and clusterName are the cluster of the replica,
		// and node its node following.
		clusterID   string
		clusterName string
		node        string

		// perNode says the node holds a copy of its own, as an
		// instance of a flex replica, and writes its own receipt.
		perNode bool

		// file is where the record of the follows is kept.
		file string

		now func() time.Time

		// drift returns the keywords the configuration of the run
		// changes in the one of the object, none when the object
		// holds it, and the configuration to apply.
		drift func(ctx context.Context, run backup.Run) ([]string, []byte, error)

		// apply has the daemon replace the configuration of the
		// object, judged under the grants of the replica. It returns
		// why the daemon refused it, and an error when it could not
		// be asked.
		apply func(ctx context.Context, body []byte) ([]string, error)

		// restore restores the heads members from the run, and says
		// whether it touched one.
		restore func(ctx context.Context, run backup.Run, members []string) (bool, error)

		// multiNode says the replica has instances on several nodes.
		multiNode bool

		// heads returns the heads the members of the run are
		// restored into.
		heads func(run backup.Run) []followHead

		// leader says the node is the placement leader of the
		// replica, as its instance monitor says.
		leader func(ctx context.Context) (bool, error)

		// fsType returns the type of the filesystem holding the
		// directory dir, empty when it can not be told.
		fsType func(dir string) string

		// validate says whether the configuration of the object
		// validates on this node.
		validate func() (bool, []string)
	}

	// followHead is a head a member of a run is restored into.
	followHead struct {
		rid string
		dir string

		// shared says the resource holding the head is flagged
		// shared: one storage every node of the cluster reaches.
		shared bool
	}

	// followVerdict is what the record of the follows says of a replica.
	followVerdict struct {
		state status.T

		// infos and warns are what to say, and what to warn of.
		infos []string
		warns []string

		// rpoBreach is when the copy the replica holds breaches the
		// recovery point objective, zero when nothing says.
		rpoBreach time.Time

		// changes are the times the verdict changes at with no event
		// to tell.
		changes []time.Time
	}
)

// replicaSource returns what the object is a replica of, and false when it
// is none: an object whose configuration can not be read is not taken for
// one.
func (t *T) replicaSource() (backup.ReplicaSource, bool, error) {
	cfg := t.objectConfig()
	if cfg == nil {
		return backup.ReplicaSource{}, false, nil
	}
	return object.ReplicaSourceOf(cfg)
}

// objectConfig returns the configuration of the object of the resource, nil
// for a resource of no object, or of one telling none.
func (t *T) objectConfig() *xconfig.T {
	c, ok := t.GetObject().(configer)
	if !ok {
		return nil
	}
	return c.Config()
}

// isReplica says the object is a replica.
func (t *T) isReplica() bool {
	_, ok, _ := t.replicaSource()
	return ok
}

// follows returns the source the resource follows: the one of the object,
// when its store is the one of the resource. A replica follows one store,
// the one its creator was let read the snapshots of its source from, and its
// other sync.store resources neither follow nor write runs.
func (t *T) follows() (backup.ReplicaSource, bool, error) {
	src, ok, err := t.replicaSource()
	if err != nil || !ok {
		return src, false, err
	}
	if !src.SameStore(t.Store) {
		return src, false, nil
	}
	return src, true, nil
}

func loadFollowRecord(file string) followRecord {
	var rec followRecord
	b, err := os.ReadFile(file)
	if err != nil {
		return rec
	}
	// A record that can not be read is one to write again: the replica
	// then follows as one that never did.
	if err := json.Unmarshal(b, &rec); err != nil {
		return followRecord{}
	}
	return rec
}

func saveFollowRecord(file string, rec followRecord) error {
	b, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
		return err
	}
	tmp := file + ".tmp"
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, file)
}

// sees records that the node lists latest as the latest run of the source,
// and says whether the record changed: the time a run is first listed is the
// one this node dates it with, on its own clock.
func (rec *followRecord) sees(latest backup.Run, now time.Time) bool {
	if rec.LatestRunID == latest.ID {
		return false
	}
	rec.LatestRunID = latest.ID
	rec.LatestSeenAt = now.UTC()
	return true
}

// latestOfSource returns the latest completed run of the source in the store
// s, and false when it holds none.
func latestOfSource(ctx context.Context, s backup.Store, src backup.ReplicaSource) (backup.Run, bool, error) {
	run, err := backup.FindRun(ctx, s, backup.RunFilter{ObjectID: src.ObjectID, Cluster: src.ClusterID})
	switch {
	case errors.Is(err, backup.ErrNotFound):
		return run, false, nil
	case err != nil:
		return run, false, err
	}
	return run, true, nil
}

// headMembers returns the members of run holding heads.
func headMembers(run backup.Run) []string {
	l := make([]string, 0, len(run.Members))
	for _, m := range run.Members {
		if m.Name == backup.MemberConfig || m.Name == backup.MemberRefs {
			continue
		}
		l = append(l, m.Name)
	}
	return l
}

// followEnv returns what a follow needs of the node and of the object.
func (t *T) followEnv(s backup.Store) followEnv {
	env := followEnv{
		node:     hostname.Hostname(),
		file:     filepath.Join(t.VarDir(), followName),
		now:      time.Now,
		validate: t.validateConfig,
	}
	env.clusterID, env.clusterName = t.cluster()
	env.perNode = t.receiptPerNode()
	env.drift = func(ctx context.Context, run backup.Run) ([]string, []byte, error) {
		return t.followedDrift(ctx, s, run)
	}
	env.apply = t.applyFollowedConfig
	env.multiNode = t.replicaNodes() > 1
	env.heads = t.followHeadsOf
	env.leader = t.isPlacementLeader
	env.fsType = fsTypeOf
	env.restore = func(ctx context.Context, run backup.Run, members []string) (bool, error) {
		mode, err := t.stageMode("")
		if err != nil {
			return false, err
		}
		// Each member into the head of the resource of its rid, as
		// the deploy the replica was created by restored them: the
		// resources of a replica are the ones of its source, and a
		// head may be elsewhere on its nodes, by a keyword scoped
		// for them.
		touched, err := t.restoreInPlace(ctx, s, run, members, true, mode)
		if err != nil {
			return touched, err
		}
		// The keys the source references are the ones the replica
		// starts on: the ones of the run replace the ones it holds.
		return touched, t.restoreRefs(ctx, s, run, object.RefsOverwrite)
	}
	return env
}

// followedDrift returns the keywords the configuration the run saved
// changes in the one of the object, as the replica applies it, sorted, and
// the configuration to apply.
func (t *T) followedDrift(ctx context.Context, s backup.Store, run backup.Run) ([]string, []byte, error) {
	b, err := backup.ReadMember(ctx, s, run, backup.MemberConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("read the configuration of run %s: %w", run.ShortID(), err)
	}
	c, ok := t.GetObjectDriver().(configer)
	if !ok {
		return nil, nil, fmt.Errorf("the configuration of %s can not be read", t.Path)
	}
	body, err := object.FollowedConfig(t.Path, c.Config(), b)
	if err != nil {
		return nil, nil, fmt.Errorf("the configuration of run %s: %w", run.ShortID(), err)
	}
	followed, err := xconfig.NewObject("", body)
	if err != nil {
		return nil, nil, fmt.Errorf("the configuration of run %s: %w", run.ShortID(), err)
	}
	return keysDrift(backup.ConfigKeys(followed), backup.ConfigKeys(c.Config())), body, nil
}

// keysDrift returns the keywords two configurations differ on, sorted.
func keysDrift(a, b map[string]string) []string {
	l := make([]string, 0)
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			l = append(l, k)
		}
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			l = append(l, k)
		}
	}
	slices.Sort(l)
	return l
}

// applyFollowedConfig has the daemon replace the configuration of the
// object with body, as one a replica follows: judged under the grants
// recorded when the replica was created, and checked as any configuration
// replaced. It returns why the daemon refused it, and an error when the
// daemon could not be asked: nothing but the daemon judges a configuration
// followed, so a follow with the daemon down applies none.
func (t *T) applyFollowedConfig(ctx context.Context, body []byte) ([]string, error) {
	c, err := client.New()
	if err != nil {
		return nil, err
	}
	follow := true
	resp, err := c.PutObjectConfigFileWithBodyWithResponse(ctx, t.Path.Namespace, t.Path.Kind, t.Path.Name, &api.PutObjectConfigFileParams{Follow: &follow}, "application/octet-stream", bytes.NewReader(body))
	switch {
	case err != nil:
		return nil, fmt.Errorf("the daemon judges the configuration a replica follows: %w", err)
	case resp.StatusCode() == http.StatusNoContent:
		return nil, nil
	case resp.JSON400 != nil:
		return []string{resp.JSON400.Detail}, nil
	case resp.JSON403 != nil:
		return []string{resp.JSON403.Detail}, nil
	case resp.JSON409 != nil:
		return []string{resp.JSON409.Detail}, nil
	case resp.JSON500 != nil:
		return nil, fmt.Errorf("apply the configuration: %s", resp.JSON500)
	default:
		return nil, fmt.Errorf("apply the configuration: unexpected response: %s", resp.Status())
	}
}

// FollowConfig implements resource.Follower: the configuration of the
// latest run of the source applied, when the object does not hold it.
func (t *T) FollowConfig(ctx context.Context) (bool, error) {
	src, ok, err := t.follows()
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}
	s, err := t.store()
	if err != nil {
		return false, err
	}
	defer s.Close()
	return t.followConfig(ctx, s, src, t.followEnv(s))
}

func (t *T) followConfig(ctx context.Context, s backup.Store, src backup.ReplicaSource, env followEnv) (bool, error) {
	if promotedHere(ctx, s, src, env.clusterID) {
		// The promotion is to finish, the heads its own.
		return false, nil
	}
	latest, found, err := latestOfSource(ctx, s, src)
	if err != nil {
		return false, err
	}
	if !found {
		return false, nil
	}
	rec := loadFollowRecord(env.file)
	before := rec
	rec.sees(latest, env.now())
	save := func() {
		if !recordsEqual(before, rec) {
			if err := saveFollowRecord(env.file, rec); err != nil {
				t.Log().Warnf("record the follow: %s", err)
			}
		}
	}
	defer save()
	if !latest.Has(backup.MemberConfig) {
		return false, nil
	}
	drift, body, err := env.drift(ctx, latest)
	if err != nil {
		return false, err
	}
	digest := func() string {
		b, err := backup.ReadMember(ctx, s, latest, backup.MemberConfig)
		if err != nil {
			return unknownConfigDigest
		}
		d, err := backup.ConfigDigest(b)
		if err != nil {
			return unknownConfigDigest
		}
		return d
	}
	if len(drift) == 0 {
		if rec.ConfigRunID != latest.ID {
			rec.ConfigRunID, rec.ConfigDigest = latest.ID, digest()
		}
		rec.RefusedRunID, rec.RefusedErrors = "", nil
		return false, nil
	}
	t.Log().Infof("apply the configuration of run %s of the cluster %s from store %s, changing %s", latest.ShortID(), latest.ClusterName, s.Ref(), strings.Join(drift, " "))
	refusal, err := env.apply(ctx, body)
	if err != nil {
		return false, err
	}
	if len(refusal) > 0 {
		for i, s := range refusal {
			refusal[i] = fmt.Sprintf("the configuration of run %s is refused: %s", latest.ShortID(), s)
		}
		// A refusal said already is not said again at every follow:
		// the source reads the same receipt until it changes.
		said := rec.RefusedRunID == latest.ID && slices.Equal(rec.RefusedErrors, refusal)
		rec.RefusedRunID, rec.RefusedErrors = latest.ID, refusal
		if !said {
			t.acknowledgeConfig(ctx, s, src, latest, rec, env, false, refusal)
		}
		return false, fmt.Errorf("%s: the replica keeps its configuration, and its heads are not restored", strings.Join(refusal, "; "))
	}
	rec.ConfigRunID, rec.ConfigDigest = latest.ID, digest()
	rec.RefusedRunID, rec.RefusedErrors = "", nil
	t.acknowledgeConfig(ctx, s, src, latest, rec, env, true, nil)
	return true, nil
}

func recordsEqual(a, b followRecord) bool {
	return a.RunID == b.RunID && a.RunCreatedAt.Equal(b.RunCreatedAt) && a.RunSeenAt.Equal(b.RunSeenAt) &&
		a.ConfigRunID == b.ConfigRunID && a.ConfigDigest == b.ConfigDigest &&
		a.RefusedRunID == b.RefusedRunID && slices.Equal(a.RefusedErrors, b.RefusedErrors) &&
		a.LatestRunID == b.LatestRunID && a.LatestSeenAt.Equal(b.LatestSeenAt)
}

// acknowledgeConfig writes into the store the receipt of a configuration of
// the run latest applied, or refused for errs: the replica holds the run of
// its heads still, the one its record says, or else the one its last receipt
// said, and the configuration of its record. A receipt that can not be
// written is said, and fails nothing.
func (t *T) acknowledgeConfig(ctx context.Context, s backup.Store, src backup.ReplicaSource, latest backup.Run, rec followRecord, env followEnv, valid bool, errs []string) {
	if env.clusterID == "" || env.clusterID == src.ClusterID {
		return
	}
	a, ok := s.(backup.Annotator)
	if !ok {
		t.Log().Debugf("no receipt of the configuration of run %s: store %s keeps no documents", latest.ShortID(), s.Ref())
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), ackTimeout)
	defer cancel()
	receipt := backup.Receipt{
		ReplicaClusterID:   env.clusterID,
		ReplicaClusterName: env.clusterName,
		Node:               env.node,
		PerNode:            env.perNode,
		SeenRunID:          latest.ID,
		RunID:              rec.RunID,
		ConfigDigest:       rec.ConfigDigest,
		ConfigValid:        valid,
		ConfigErrors:       errs,
	}
	if receipt.RunID == "" || receipt.ConfigDigest == "" {
		// A replica that never followed holds what its provision
		// restored, which the receipt of the provision says.
		if prev, ok := readReceipt(ctx, a, src, env); ok {
			if receipt.RunID == "" && prev.RestoreError == "" {
				receipt.RunID = prev.RunID
			}
			if receipt.ConfigDigest == "" {
				receipt.ConfigDigest = prev.ConfigDigest
			}
		}
	}
	if receipt.ConfigDigest == "" {
		receipt.ConfigDigest = unknownConfigDigest
	}
	if err := backup.WriteReceipt(ctx, a, src.ClusterID, src.ObjectID, receipt); err != nil {
		t.Log().Warnf("write the receipt of the configuration of run %s for the cluster %s into store %s: %s", latest.ShortID(), latest.ClusterName, s.Ref(), err)
		return
	}
	t.Log().Infof("write the receipt of the configuration of run %s into store %s, for the cluster %s to see this replica", latest.ShortID(), s.Ref(), latest.ClusterName)
}

// readReceipt returns the receipt the replica wrote last for the source, the
// one of its cluster or the one of its node, and false when it wrote none
// that can be read.
func readReceipt(ctx context.Context, a backup.Annotator, src backup.ReplicaSource, env followEnv) (backup.Receipt, bool) {
	var r backup.Receipt
	name := backup.Receipt{ReplicaClusterID: env.clusterID, Node: env.node, PerNode: env.perNode}.DocName()
	b, err := a.GetDoc(ctx, src.ClusterID, src.ObjectID, name)
	if err != nil {
		return r, false
	}
	if json.Unmarshal(b, &r) != nil {
		return r, false
	}
	return r, true
}

// adopt takes the run latest for the one the heads hold when the record of
// the follows says none and the last receipt of this node says it restored
// that run: the provision of a replica restores the snapshot it was created
// from, which the first follow does not restore again.
func (t *T) adopt(ctx context.Context, s backup.Store, src backup.ReplicaSource, latest backup.Run, rec *followRecord, env followEnv) bool {
	if rec.RunID != "" {
		return false
	}
	a, ok := s.(backup.Annotator)
	if !ok {
		return false
	}
	prev, ok := readReceipt(ctx, a, src, env)
	if !ok || prev.Node != env.node || prev.RunID != latest.ID || prev.RestoreError != "" {
		return false
	}
	rec.RunID, rec.RunCreatedAt, rec.RunSeenAt = latest.ID, latest.CreatedAt, rec.LatestSeenAt
	return true
}

// FollowPending implements resource.Follower: the heads do not hold the
// latest run of the source.
func (t *T) FollowPending(ctx context.Context) (bool, error) {
	src, ok, err := t.follows()
	if err != nil || !ok {
		return false, err
	}
	s, err := t.store()
	if err != nil {
		return false, err
	}
	defer s.Close()
	env := t.followEnv(s)
	if promotedHere(ctx, s, src, env.clusterID) {
		return false, nil
	}
	latest, found, err := latestOfSource(ctx, s, src)
	if err != nil || !found {
		return false, err
	}
	rec := loadFollowRecord(env.file)
	rec.sees(latest, env.now())
	return rec.RunID != latest.ID && !t.adopt(ctx, s, src, latest, &rec, env), nil
}

// Follow implements resource.Follower: the heads restored from the latest
// run of the source, when they do not hold it.
func (t *T) Follow(ctx context.Context) error {
	src, ok, err := t.follows()
	if err != nil {
		return err
	}
	if !ok {
		if t.isReplica() {
			t.Log().Infof("nothing to follow: the replica follows store %s, not %s", src.Store, t.Store)
		}
		return nil
	}
	s, err := t.store()
	if err != nil {
		return err
	}
	defer s.Close()
	return t.followHeads(ctx, s, src, t.followEnv(s))
}

func (t *T) followHeads(ctx context.Context, s backup.Store, src backup.ReplicaSource, env followEnv) error {
	if promotedHere(ctx, s, src, env.clusterID) {
		// A promotion recorded this cluster as the primary, and did
		// not go on to make of the replica an object of its own: its
		// heads are the ones the object starts on, which a follow
		// would write a run of the cluster taken over from into.
		t.Log().Infof("nothing to follow: store %s records this cluster as the primary of the object, promoted: finish the promotion with 'om %s promote'", s.Ref(), t.Path)
		return nil
	}
	latest, found, err := latestOfSource(ctx, s, src)
	if err != nil {
		return err
	}
	if !found {
		t.Log().Infof("nothing to follow: store %s holds no run of the object %s of the cluster %s", s.Ref(), src.ObjectID, src.ClusterID)
		return nil
	}
	rec := loadFollowRecord(env.file)
	before := rec
	rec.sees(latest, env.now())
	save := func() error {
		if recordsEqual(before, rec) {
			return nil
		}
		return saveFollowRecord(env.file, rec)
	}
	if rec.RunID == latest.ID || t.adopt(ctx, s, src, latest, &rec, env) {
		t.Log().Infof("nothing to follow: the heads hold run %s, the latest of the cluster %s in store %s", latest.ShortID(), latest.ClusterName, s.Ref())
		return save()
	}
	// The heads of a run are restored under its configuration: the
	// resources holding them are the ones it names.
	if latest.Has(backup.MemberConfig) {
		drift, _, err := env.drift(ctx, latest)
		if err != nil {
			return errors.Join(err, save())
		}
		if len(drift) > 0 {
			return errors.Join(fmt.Errorf("the configuration of run %s is not applied, differing on %s: its heads are not restored", latest.ShortID(), strings.Join(drift, " ")), save())
		}
	}
	members, skipped, err := t.planFollow(ctx, latest, env)
	if err != nil {
		return errors.Join(err, save())
	}
	t.Log().Infof("follow run %s of the cluster %s, begun at %s, from store %s", latest.ShortID(), latest.ClusterName, latest.CreatedAt.Format(time.RFC3339), s.Ref())
	touched, err := env.restore(ctx, latest, members)
	t.acknowledgeFrom(ctx, s, latest, env.clusterID, env.clusterName, env.node, env.perNode, skipped, env.validate, touched, err)
	if err != nil {
		if touched {
			// A head was emptied, written or switched, and not
			// all of them: the heads hold no run whole.
			rec.RunID, rec.RunCreatedAt, rec.RunSeenAt = "", time.Time{}, time.Time{}
		}
		return errors.Join(err, save())
	}
	rec.RunID, rec.RunCreatedAt, rec.RunSeenAt = latest.ID, latest.CreatedAt.UTC(), rec.LatestSeenAt
	return save()
}

// judgeFollow judges a replica from the record of its follows, at now, on
// the clock of its node. latest is the latest run of the source the store
// lists, when found, and maxDelay how long after a time a copy made then is
// stale, zero when nothing says.
//
// The recovery point of the replica is the run its heads hold, which two
// clocks date: the source began it at a time of its own, and this node first
// listed it at a time of its own. The earlier of the two is taken: a source
// whose clock runs ahead dates its runs in the future of this node, and the
// copy would look fresher than it is, when this node knows it held nothing
// newer than what it listed then. A source whose clock runs behind makes the
// copy look older, which errs on the side of saying so.
//
// started are the resources using the heads found up, not standby: the
// replica is started, and no longer follows.
func judgeFollow(now time.Time, rec followRecord, latest backup.Run, found bool, started []string, maxDelay func(time.Time) time.Duration, origin string) followVerdict {
	v := followVerdict{state: status.NotApplicable}
	warn := func(format string, args ...any) {
		v.warns = append(v.warns, fmt.Sprintf(format, args...))
		v.state.Add(status.Warn)
	}
	if len(started) > 0 {
		warn("the replica is started, %s up: it no longer follows its source until stopped", strings.Join(started, " "))
	}
	short := func(id string) string {
		return backup.Run{ID: id}.ShortID()
	}
	switch {
	case rec.RunID == "":
		warn("the replica holds no run of its source: none followed whole yet")
		v.rpoBreach = noCopyRPOBreach
	default:
		held := rec.RunCreatedAt
		if !rec.RunSeenAt.IsZero() && (held.IsZero() || rec.RunSeenAt.Before(held)) {
			held = rec.RunSeenAt
		}
		d := maxDelay(held)
		switch {
		case d == 0:
			v.infos = append(v.infos, fmt.Sprintf("holds run %s of its source, no schedule and no max delay", short(rec.RunID)))
		case now.After(held.Add(d)):
			v.rpoBreach = held.Add(d)
			warn("holds run %s of its source, of %s, more than %s ago (%s)", short(rec.RunID), held.Format(time.RFC3339), d, origin)
		default:
			v.rpoBreach = held.Add(d)
			v.changes = append(v.changes, v.rpoBreach)
			v.infos = append(v.infos, fmt.Sprintf("holds run %s of its source, of %s", short(rec.RunID), held.Format(time.RFC3339)))
			v.state.Add(status.Up)
		}
	}
	if found && latest.ID != rec.RunID && rec.LatestRunID == latest.ID {
		// A run the source completed is followed within the max
		// delay of when this node first listed it.
		if d := maxDelay(rec.LatestSeenAt); d > 0 {
			if deadline := rec.LatestSeenAt.Add(d); now.After(deadline) {
				warn("run %s of the source, listed since %s, is not followed after %s (%s)", latest.ShortID(), rec.LatestSeenAt.Format(time.RFC3339), d, origin)
			} else {
				v.changes = append(v.changes, deadline)
			}
		}
	}
	if rec.RefusedRunID != "" && (!found || rec.RefusedRunID == latest.ID) {
		warn("%s", strings.Join(rec.RefusedErrors, "; "))
	}
	return v
}

// statusFollow is the status of the resource of a replica: how fresh the run
// its heads hold is, whether the source has a run it does not follow, and
// whether it refuses the configuration of its source.
func (t *T) statusFollow(ctx context.Context, src backup.ReplicaSource) status.T {
	if !src.SameStore(t.Store) {
		t.StatusLog().Info("no run from a replica, which follows store %s", src.Store)
		return status.NotApplicable
	}
	s, err := t.store()
	if err != nil {
		t.StatusLog().Error("%s", err)
		return status.Warn
	}
	defer s.Close()
	file := filepath.Join(t.VarDir(), followName)
	rec := loadFollowRecord(file)
	now := time.Now()
	latest, found, err := latestOfSource(ctx, s, src)
	if err != nil {
		t.StatusLog().Warn("the runs of the source can not be listed in store %s: %s", s.Ref(), err)
	}
	if found && rec.sees(latest, now) {
		// The first time this node lists a run is what it dates the
		// run with: the status records it, as a follow does.
		if err := saveFollowRecord(file, rec); err != nil {
			t.StatusLog().Warn("record the latest run of the source: %s", err)
		}
	}
	if clusterID, _ := t.cluster(); promotedHere(ctx, s, src, clusterID) {
		t.StatusLog().Warn("store %s records this cluster as the primary of the object, and the object is a replica still: it no longer follows, finish its promotion with 'om %s promote'", s.Ref(), t.Path)
		return status.Warn
	}
	v := judgeFollow(now, rec, latest, found, t.startedConsumers(ctx), t.GetMaxDelay, t.MaxDelayOrigin())
	if !v.rpoBreach.IsZero() {
		t.StatusLog().RPOBreachesAt(v.rpoBreach)
	}
	for _, tm := range v.changes {
		t.StatusLog().ChangesAt(tm)
	}
	for _, s := range v.infos {
		t.StatusLog().Info("%s", s)
	}
	for _, s := range v.warns {
		t.StatusLog().Warn("%s", s)
	}
	if err != nil {
		v.state.Add(status.Warn)
	}
	return v.state
}

// startedConsumers returns the resources using the heads the status being
// evaluated found up, the standby ones aside, which are up on a replica
// stopped: sorted, none for a replica stopped.
func (t *T) startedConsumers(ctx context.Context) []string {
	sb := statusbus.FromContext(ctx)
	l := make([]string, 0)
	for _, r := range t.GetObjectDriver().ResourcesByDrivergroups(consumerGroups) {
		if r.IsDisabled() || r.IsStandby() {
			continue
		}
		if sb.Get(r.RID()).Is(status.Up) {
			l = append(l, r.RID())
		}
	}
	slices.Sort(l)
	return l
}

// sharedFSTypes are the types of the filesystems several nodes mount at
// once: a head on one is the same storage on every node of the replica.
var sharedFSTypes = []string{"nfs", "nfs4", "gfs2", "ocfs2", "glusterfs", "fuse.glusterfs", "cephfs", "ceph"}

// planFollow returns the members of the run this node restores, and the
// ones it leaves to another: the heads of a flex replica, each instance of
// which follows the copy it holds.
//
// A head flagged shared is one storage every node of the replica reaches,
// which each node restoring would write at once: its placement leader alone
// restores it, as it alone provisions it, and the other nodes skip it, a
// follow asked by hand included. The receipt of a node skipping heads says
// which, so the source does not take its copy for holding them.
//
// A head on a filesystem several nodes mount at once, not flagged shared,
// is refused before anything is written, when the replica has several
// nodes: every node would restore the same directory. The refusal is one of
// the configuration, to fix on the source, and writes no receipt, the
// replica holding what its last one says.
func (t *T) planFollow(ctx context.Context, run backup.Run, env followEnv) (members, skipped []string, err error) {
	all := headMembers(run)
	if !env.perNode || env.heads == nil {
		return all, nil, nil
	}
	heads := env.heads(run)
	shared := make([]string, 0)
	unmarked := make([]string, 0)
	for _, h := range heads {
		switch {
		case h.shared:
			shared = append(shared, h.rid)
		case env.multiNode && h.dir != "" && env.fsType != nil && slices.Contains(sharedFSTypes, env.fsType(h.dir)):
			unmarked = append(unmarked, fmt.Sprintf("%s (%s, %s)", h.rid, h.dir, env.fsType(h.dir)))
		}
	}
	if len(unmarked) > 0 {
		return nil, nil, fmt.Errorf("%s: on a filesystem the nodes of the replica mount at once, which each of them would restore: set 'shared = true' on the resource in the configuration of the source, for the placement leader alone to restore it", strings.Join(unmarked, ", "))
	}
	if len(shared) == 0 {
		return all, nil, nil
	}
	leader, err := env.leader(ctx)
	if err != nil {
		// Not knowing, the node does not write a storage another
		// node may be writing.
		t.Log().Warnf("the placement leader can not be told, the shared heads %s are left to it: %s", strings.Join(shared, " "), err)
		leader = false
	}
	if leader {
		return all, nil, nil
	}
	for _, m := range all {
		if slices.Contains(shared, m) {
			skipped = append(skipped, m)
		} else {
			members = append(members, m)
		}
	}
	t.Log().Debugf("skip the shared heads %s, which the placement leader restores", strings.Join(skipped, " "))
	return members, skipped, nil
}

// followHeadsOf returns the heads the members of run are restored into,
// each the one of the resource of its rid. A member of no resource is left
// to the restore to refuse.
func (t *T) followHeadsOf(run backup.Run) []followHead {
	o := t.GetObjectDriver()
	l := make([]followHead, 0)
	for _, rid := range headMembers(run) {
		r := o.ResourceByID(rid)
		if r == nil {
			continue
		}
		h := followHead{rid: rid, shared: r.IsShared()}
		if i, ok := r.(header); ok {
			h.dir = i.Head()
		}
		l = append(l, h)
	}
	return l
}

// replicaNodes returns how many nodes host the instances of the replica,
// its drpnodes.
func (t *T) replicaNodes() int {
	cfg := t.objectConfig()
	if cfg == nil {
		return 0
	}
	return len(cfg.GetStrings(object.ScopeKey(cfg)))
}

// isPlacementLeader says the node is the placement leader of the object, as
// the monitor of its instance says, which the scheduler reads too.
func (t *T) isPlacementLeader(ctx context.Context) (bool, error) {
	c, err := client.New()
	if err != nil {
		return false, err
	}
	resp, err := c.GetInstanceWithResponse(ctx, hostname.Hostname(), t.Path.Namespace, t.Path.Kind, t.Path.Name)
	switch {
	case err != nil:
		return false, err
	case resp.JSON200 == nil || resp.JSON200.Data.Monitor == nil:
		return false, fmt.Errorf("the daemon tells no monitor of the instance: %s", resp.Status())
	}
	return resp.JSON200.Data.Monitor.IsLeader, nil
}

// fsTypeOf returns the type of the filesystem holding dir, the one of the
// deepest mount it is under, empty when the mounts can not be read.
func fsTypeOf(dir string) string {
	entries, err := mountinfo.Read()
	if err != nil {
		return ""
	}
	dir = filepath.Clean(dir)
	best, fsType := -1, ""
	for _, e := range entries {
		target := filepath.Clean(e.Target)
		// The last mount of a mount point is the one shown.
		if within(dir, target) && len(target) >= best {
			best, fsType = len(target), e.FSType
		}
	}
	return fsType
}
