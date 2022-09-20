package cluster

import (
	"encoding/json"
	"time"

	"opensvc.com/opensvc/core/instance"
	"opensvc.com/opensvc/core/object"
	"opensvc.com/opensvc/core/path"
	"opensvc.com/opensvc/core/status"
)

type (

	// MonitorThreadStatus describes the OpenSVC daemon monitor thread state,
	// which is responsible for the node DataSets aggregation and
	// decision-making.
	MonitorThreadStatus struct {
		ThreadStatus
		Routines int `json:"routines"`
	}

	// TNodeData holds a node DataSet.
	TNodeData struct {
		Instance map[string]instance.Instance `json:"instance"`
		Monitor  NodeMonitor                  `json:"monitor"`
		Stats    NodeStatusStats              `json:"stats"`
		Status   TNodeStatus                  `json:"status"`
		//Locks map[string]Lock `json:"locks"`
	}

	TNodeStatus struct {
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
		Labels          map[string]string           `json:"labels"`
	}

	// NodeStatusStats describes systems (cpu, mem, swap) resource usage of a node
	// and an opensvc-specific score.
	NodeStatusStats struct {
		Load15M      float64 `json:"load_15m"`
		MemAvailPct  uint64  `json:"mem_avail"`
		MemTotalMB   uint64  `json:"mem_total"`
		Score        uint    `json:"score"`
		SwapAvailPct uint64  `json:"swap_avail"`
		SwapTotalMB  uint64  `json:"swap_total"`
	}

	// NodeMonitor describes the in-daemon states of a node
	NodeMonitor struct {
		GlobalExpect        string    `json:"global_expect"`
		Status              string    `json:"status"`
		StatusUpdated       time.Time `json:"status_updated"`
		GlobalExpectUpdated time.Time `json:"global_expect_updated"`
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

// GetNodeStatus extracts from the cluster dataset all information relative
// to node status.
func (s *Status) GetNodeStatus(nodename string) *TNodeStatus {
	if nodeData, ok := s.Cluster.Node[nodename]; ok {
		return &nodeData.Status
	}
	return nil
}

// GetObjectStatus extracts from the cluster dataset all information relative
// to an object.
func (s *Status) GetObjectStatus(p path.T) object.Status {
	ps := p.String()
	data := object.NewStatus()
	data.Path = p
	data.Object, _ = s.Cluster.Object[ps]
	for nodename, ndata := range s.Cluster.Node {
		instanceStates := instance.States{}
		instanceStates.Node.Frozen = ndata.Status.Frozen
		instanceStates.Node.Name = nodename
		inst, ok := ndata.Instance[ps]
		if !ok {
			continue
		}
		if inst.Status == nil || inst.Config == nil {
			continue
		}

		instanceStates.Status = *inst.Status
		instanceStates.Config = *inst.Config
		data.Instances[nodename] = instanceStates
		for _, relative := range instanceStates.Status.Parents {
			ps := relative.String()
			data.Parents[ps] = s.Cluster.Object[ps]
		}
		for _, relative := range instanceStates.Status.Children {
			ps := relative.String()
			data.Children[ps] = s.Cluster.Object[ps]
		}
		for _, relative := range instanceStates.Status.Slaves {
			ps := relative.String()
			data.Slaves[ps] = s.Cluster.Object[ps]
		}
	}
	return *data
}

func (n *TNodeData) DeepCopy() TNodeData {
	b, err := json.Marshal(n)
	if err != nil {
		return TNodeData{}
	}
	nodeStatus := TNodeData{}
	if err := json.Unmarshal(b, &nodeStatus); err != nil {
		return TNodeData{}
	}
	return nodeStatus
}
