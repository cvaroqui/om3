package resource

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The replicas of a resource status are its own in a copy, down to what
// refuses their configuration: changing the copy leaves the status copied as
// it was.
func TestStatusDeepCopyReplicas(t *testing.T) {
	s := &Status{Replicas: []ReplicaStatus{{Cluster: "drp", ConfigErrors: []string{"a"}}}}
	c := s.DeepCopy()
	assert.Equal(t, s.Replicas, c.Replicas)
	c.Replicas[0].Cluster = "other"
	c.Replicas[0].ConfigErrors[0] = "b"
	assert.Equal(t, "drp", s.Replicas[0].Cluster)
	assert.Equal(t, "a", s.Replicas[0].ConfigErrors[0])
	assert.Nil(t, (&Status{}).DeepCopy().Replicas)
}

// A resource no cluster restores from says no replica, and one judged says
// when it breaches only when a time says so.
func TestStatusReplicasJSON(t *testing.T) {
	b, err := json.Marshal(Status{})
	require.NoError(t, err)
	assert.NotContains(t, string(b), "replicas")

	b, err = json.Marshal(Status{Replicas: []ReplicaStatus{{Cluster: "drp", ClusterID: "id", RunID: "run1", SeenRunID: "run2"}}})
	require.NoError(t, err)
	var m struct {
		Replicas []map[string]any `json:"replicas"`
	}
	require.NoError(t, json.Unmarshal(b, &m))
	require.Len(t, m.Replicas, 1)
	assert.Equal(t, map[string]any{"cluster": "drp", "cluster_id": "id", "run_id": "run1", "seen_run_id": "run2", "config_in_sync": false, "config_valid": false}, m.Replicas[0])

	b, err = json.Marshal(ReplicaStatus{RPOBreachedAt: time.Unix(0, 0).UTC(), RestoreError: "x", ConfigErrors: []string{"y"}})
	require.NoError(t, err)
	assert.Contains(t, string(b), `"rpo_breached_at":"1970-01-01T00:00:00Z"`)
	assert.Contains(t, string(b), `"restore_error":"x"`)
	assert.Contains(t, string(b), `"config_errors":["y"]`)
	assert.Contains(t, (&Status{Replicas: []ReplicaStatus{{}}}).Unstructured(), "replicas")
	assert.NotContains(t, (&Status{}).Unstructured(), "replicas")
}
