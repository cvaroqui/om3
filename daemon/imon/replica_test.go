package imon

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/opensvc/om3/v3/core/instance"
)

// The daemon takes no start initiative for a replica, whatever the
// orchestrate of its configuration, which is the one of its source: neither
// the start of an orchestrate=start object at the boot of its node, nor the
// start of an orchestrate=ha object found down. The same object not a
// replica is started.
func TestTheDaemonNeverStartsAReplica(t *testing.T) {
	for _, orchestrate := range []string{"start", "ha"} {
		t.Run(orchestrate, func(t *testing.T) {
			m := newBootStartTestManager(instance.MonitorStateIdle)
			m.state.IsHALeader = true
			m.objStatus.Orchestrate = orchestrate
			m.instConfig.ActorConfig = &instance.ActorConfig{Orchestrate: orchestrate}
			m.orchestrateNone()
			require.Equal(t, instance.MonitorStateReady, m.state.State, "an object of the cluster is readied to start")

			m = newBootStartTestManager(instance.MonitorStateIdle)
			m.state.IsHALeader = true
			m.objStatus.Orchestrate = orchestrate
			m.instConfig.ActorConfig = &instance.ActorConfig{Orchestrate: orchestrate, DRP: true}
			require.True(t, m.isReplica())
			m.orchestrateNone()
			require.Equal(t, instance.MonitorStateIdle, m.state.State, "a replica is left stopped")
		})
	}
}

// An orchestration starting a replica is refused upfront, saying how a
// replica starts: its instances would refuse the start. The ones stopping,
// freezing, provisioning or removing it are not.
func TestAnOrchestrationStartingAReplicaIsRefused(t *testing.T) {
	m := newBootStartTestManager(instance.MonitorStateIdle)
	m.instConfig.ActorConfig = &instance.ActorConfig{DRP: true}
	for _, ge := range []instance.MonitorGlobalExpect{
		instance.MonitorGlobalExpectStarted,
		instance.MonitorGlobalExpectRestarted,
		instance.MonitorGlobalExpectPlaced,
		instance.MonitorGlobalExpectPlacedAt,
	} {
		assert.Contains(t, m.replicaRefusal(ge), "a replica starts by its promotion", "%s", ge)
	}
	for _, ge := range []instance.MonitorGlobalExpect{
		instance.MonitorGlobalExpectStopped,
		instance.MonitorGlobalExpectFrozen,
		instance.MonitorGlobalExpectProvisioned,
		instance.MonitorGlobalExpectUnprovisioned,
		instance.MonitorGlobalExpectPurged,
		instance.MonitorGlobalExpectDeleted,
	} {
		assert.Empty(t, m.replicaRefusal(ge), "%s", ge)
	}
	m.instConfig.ActorConfig = &instance.ActorConfig{}
	assert.Empty(t, m.replicaRefusal(instance.MonitorGlobalExpectStarted), "an object of the cluster starts")
}
