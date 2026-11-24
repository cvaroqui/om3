package object

import (
	"context"
	"fmt"
	"slices"

	"github.com/opensvc/om3/v3/core/actioncontext"
	"github.com/opensvc/om3/v3/core/backup"
	"github.com/opensvc/om3/v3/core/keyop"
	"github.com/opensvc/om3/v3/core/naming"
	"github.com/opensvc/om3/v3/core/xconfig"
	"github.com/opensvc/om3/v3/util/key"
)

// A replica is the copy of an object another cluster runs, kept on this
// cluster to take over from it in a disaster: its replica_of keyword names
// the object it follows. Its configuration is the one of its source, word
// for word, so what tells the two apart is that keyword, which the daemon
// sets, and the nodes: the instances of a replica are the drpnodes.

var (
	// KeyReplicaOf and KeyReplicaGrants are the keywords the daemon sets
	// on a replica: the object it follows, and the grants the
	// configurations it follows are judged under.
	KeyReplicaOf     = key.New("DEFAULT", "replica_of")
	KeyReplicaGrants = key.New("DEFAULT", "replica_grants")

	keyNodes    = key.New("DEFAULT", "nodes")
	keyDRPNodes = key.New("DEFAULT", "drpnodes")
)

// ReplicaSourceOf returns what the object of the configuration cfg is a
// replica of, and false when it is none.
func ReplicaSourceOf(cfg *xconfig.T) (backup.ReplicaSource, bool, error) {
	s := cfg.Get(KeyReplicaOf)
	if s == "" {
		return backup.ReplicaSource{}, false, nil
	}
	src, err := backup.ParseReplicaSource(s)
	if err != nil {
		return src, true, err
	}
	return src, true, nil
}

// ScopeKey returns the keyword naming the nodes the instances of the object
// of the configuration cfg are on: drpnodes for a replica, whose nodes are
// the ones of the cluster of its source, and nodes for any other object.
func ScopeKey(cfg *xconfig.T) key.T {
	if cfg.Get(KeyReplicaOf) != "" {
		return keyDRPNodes
	}
	return keyNodes
}

// IsReplica says the object is a replica of an object of another cluster.
func (t *core) IsReplica() bool {
	return t.config.Get(KeyReplicaOf) != ""
}

// withoutReplicaNodes returns nodes, the nodes keyword evaluated, as a
// replica reads it: without its drpnodes.
//
// The nodes of the configuration of a replica are the ones of the cluster of
// its source, which this cluster does not know: they evaluate to none, and
// the nodes keyword then falls back on the node evaluating it. A replica
// instance would be one of the nodes, read the values scoped @nodes, the
// ones of the source, and never the ones scoped @drpnodes written for it. A
// node hosting a replica is one of its drpnodes, and not of its nodes,
// whatever the nodes keyword evaluates to here: two clusters naming their
// nodes alike included.
//
// The drpnodes are read as written: evaluating a scoped keyword asks for the
// nodes, which is asking this.
func (t *core) withoutReplicaNodes(nodes []string) []string {
	if !t.IsReplica() {
		return nodes
	}
	v, err := xconfig.PeersConverter.Convert(t.config.Get(keyDRPNodes))
	if err != nil {
		return nodes
	}
	drp, _ := v.([]string)
	return slices.DeleteFunc(slices.Clone(nodes), func(s string) bool {
		return slices.Contains(drp, s)
	})
}

// checkReplicaStart refuses the start of a replica, unless forced: a
// replica holds the data of the last run it followed, for a promotion to
// start it on once its source is known lost. Started beside its source, it
// would run the object twice, and the next follow would write under it.
func (t *actor) checkReplicaStart(ctx context.Context) error {
	if !t.IsReplica() {
		return nil
	}
	if actioncontext.IsForce(ctx) {
		t.log.Warnf("start a replica, as forced: its source may be running, and it no longer follows it while started")
		return nil
	}
	return fmt.Errorf("%s is a replica: a replica starts by its promotion, or with --force", t.path)
}

// FollowedConfig returns the configuration run, the one a run of its source
// saved, as the replica at p holding the configuration current applies it.
//
// What the source recorded of the things it made does not come with it, the
// id it was created with, the disk its provision created: the replica keeps
// the ones it recorded of its own. It keeps the keywords the daemon set on
// it too, which say what it is on this cluster: the snapshot its provision
// restores from, the object it follows and the grants it follows it under.
// Every other keyword is the one of the source: a value of the replica's own
// is written in the configuration of the source, scoped @drpnodes.
func FollowedConfig(p naming.Path, current *xconfig.T, run []byte) ([]byte, error) {
	oc, err := NewConfigurer(p, WithConfigData(run), WithVolatile(true))
	if err != nil {
		return nil, err
	}
	cfg := oc.Config()
	cfg.MaterializeDefaultSection()
	cfg.UnsetRecorded()
	for _, option := range backup.DaemonSetOptions {
		if err := cfg.PrepareUnset(key.New("DEFAULT", option)); err != nil {
			return nil, err
		}
	}
	if err := cfg.PrepareUnset(keyID); err != nil {
		return nil, err
	}
	sets := make([]keyop.T, 0)
	for _, section := range current.SectionStrings() {
		if section != "DEFAULT" && !cfg.HasSectionString(section) {
			// What the replica recorded of a resource the source
			// no longer has goes with the resource.
			continue
		}
		for _, option := range current.Keys(section) {
			k := key.New(section, option)
			keep := section == "DEFAULT" && (option == keyID.Option || slices.Contains(backup.DaemonSetOptions, option))
			if !keep && current.Referrer != nil {
				kw := current.Referrer.KeywordLookup(k, current.SectionType(k))
				keep = kw != nil && kw.Recorded
			}
			if keep {
				sets = append(sets, *keyop.New(k, keyop.Set, current.Get(k), 0))
			}
		}
	}
	if err := cfg.PrepareSet(sets...); err != nil {
		return nil, err
	}
	return cfg.Dump()
}
