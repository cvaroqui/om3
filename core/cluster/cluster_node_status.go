package cluster

import (
	"time"

	"opensvc.com/opensvc/core/instance"
	"opensvc.com/opensvc/core/nodesinfo"
	"opensvc.com/opensvc/core/status"
	"opensvc.com/opensvc/util/san"
)

type (
	NodeStatus struct {
		Agent           string                      `json:"agent"`
		API             uint64                      `json:"api"`
		Arbitrators     map[string]ArbitratorStatus `json:"arbitrators"`
		Compat          uint64                      `json:"compat"`
		Env             string                      `json:"env"`
		Frozen          time.Time                   `json:"frozen"`
		Gen             map[string]uint64           `json:"gen"`
		MinAvailMemPct  uint64                      `json:"min_avail_mem"`
		MinAvailSwapPct uint64                      `json:"min_avail_swap"`
		Speaker         bool                        `json:"speaker"`
		Labels          nodesinfo.Labels            `json:"labels"`
		Paths           san.Paths                   `json:"paths"`
	}

	// NodeServices groups instances configuration digest and status
	NodeServices struct {
		Config map[string]instance.Config  `json:"config"`
		Status map[string]instance.Status  `json:"status"`
		Smon   map[string]instance.Monitor `json:"smon"`
	}

	// ArbitratorStatus describes the internet name of an arbitrator and
	// if it is join-able.
	ArbitratorStatus struct {
		Name   string   `json:"name"`
		Status status.T `json:"status"`
	}
)

func (nodeStatus *NodeStatus) DeepCopy() *NodeStatus {
	result := *nodeStatus
	newArbitrator := make(map[string]ArbitratorStatus)
	for n, v := range nodeStatus.Arbitrators {
		newArbitrator[n] = v
	}
	result.Arbitrators = newArbitrator

	newGen := make(map[string]uint64)
	for n, v := range nodeStatus.Gen {
		newGen[n] = v
	}
	result.Gen = newGen
	result.Labels = nodeStatus.Labels.DeepCopy()
	result.Paths = nodeStatus.Paths.DeepCopy()

	return &result
}