package backup

import (
	"fmt"
	"strings"
)

// ReplicaSource is what the replica_of keyword of an object says: the
// object is a replica, following the runs the object ObjectID of the cluster
// ClusterID writes into the store Store.
//
// The object followed is named by its id, not by its path: the runs of an
// object are kept under its id, and an object purged and created again at the
// same path on the source cluster is another object, whose runs the replica
// does not follow.
type ReplicaSource struct {
	// Store is the reference of the store, as the store keyword of a
	// sync.store resource names it: one word for a store of the cluster
	// configuration, "from <sec> store#<name>" for one of a sec.
	Store string

	// ClusterID and ObjectID are the cluster writing the runs, and the
	// object the runs are of.
	ClusterID string
	ObjectID  string
}

// ParseReplicaSource reads the value of a replica_of keyword: "<store>
// <source cluster id> <source object id>".
func ParseReplicaSource(s string) (ReplicaSource, error) {
	fields := strings.Fields(s)
	if len(fields) != 3 && len(fields) != 5 {
		return ReplicaSource{}, fmt.Errorf("replica_of %q: want <store> <source cluster id> <source object id>", s)
	}
	last := len(fields) - 2
	return ReplicaSource{
		Store:     strings.Join(fields[:last], " "),
		ClusterID: fields[last],
		ObjectID:  fields[last+1],
	}, nil
}

// String returns the source as the replica_of keyword holds it.
func (t ReplicaSource) String() string {
	return fmt.Sprintf("%s %s %s", t.Store, t.ClusterID, t.ObjectID)
}

// SameStore says ref names the store the replica follows, however its words
// are spaced.
func (t ReplicaSource) SameStore(ref string) bool {
	return strings.Join(strings.Fields(ref), " ") == strings.Join(strings.Fields(t.Store), " ")
}
