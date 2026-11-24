package scheduler

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/opensvc/om3/v3/core/instance"
	"github.com/opensvc/om3/v3/core/schedule"
)

// The schedule of the sync resource of a replica runs its follow, one exec
// per resource: the updates of an object due at once run together, a follow
// does not.
func TestTheFollowOfAReplicaIsScheduled(t *testing.T) {
	assert.True(t, slices.Contains(ObjectActions, "sync_follow"))
	e := schedule.Entry{Path: testPath, Config: schedule.Config{Action: "sync_follow", Key: "sync#1.schedule", RequireProvisioned: true}}
	argv, err := CmdArgs(e)
	require.NoError(t, err)
	assert.Equal(t, []string{testPath.String(), "instance", "follow", "--rid", "sync#1"}, argv)
}

// A job of the placement leader only, as the follow of a failover replica,
// is skipped on the other nodes, and on a node whose instance monitor does
// not say yet: the node leading when the job is due runs it.
func TestAJobOfThePlacementLeaderIsSkippedElsewhere(t *testing.T) {
	s := newTestScheduler(t)
	e := schedule.Entry{Node: s.localhost, Path: testPath, Config: schedule.Config{Action: "sync_follow", Key: "sync#1.schedule", RequireLeader: true}}
	assert.Equal(t, "placement leader not yet evaluated", s.notLeaderReason(e))

	instance.MonitorData.Set(testPath, s.localhost, &instance.Monitor{IsLeader: false})
	t.Cleanup(func() { instance.MonitorData.Unset(testPath, s.localhost) })
	assert.Equal(t, "not the placement leader", s.notLeaderReason(e))

	instance.MonitorData.Set(testPath, s.localhost, &instance.Monitor{IsLeader: true})
	assert.Empty(t, s.notLeaderReason(e))

	// A job any node runs, as the follow of a flex replica.
	instance.MonitorData.Set(testPath, s.localhost, &instance.Monitor{IsLeader: false})
	e.RequireLeader = false
	assert.Empty(t, s.notLeaderReason(e))
}
