package msgbus

import (
	"github.com/opensvc/om3/core/instance"
	"github.com/opensvc/om3/core/node"
)

// onInstanceConfigDeleted removes cluster.node.<node>.instance.<path>.config
func (data *ClusterData) onInstanceConfigDeleted(c *InstanceConfigDeleted) {
	s := c.Path.String()
	if inst, ok := data.Cluster.Node[c.Node].Instance[s]; ok && inst.Config != nil {
		inst.Config = nil
		data.Cluster.Node[c.Node].Instance[s] = inst
	}
}

// onInstanceConfigUpdated updates cluster.node.<node>.instance.<path>.config
func (data *ClusterData) onInstanceConfigUpdated(c *InstanceConfigUpdated) {
	s := c.Path.String()
	value := c.Value.DeepCopy()
	if cnode, ok := data.Cluster.Node[c.Node]; ok {
		if cnode.Instance == nil {
			cnode.Instance = make(map[string]instance.Instance)
		}
		inst := cnode.Instance[s]
		inst.Config = value
		cnode.Instance[s] = inst
		data.Cluster.Node[c.Node] = cnode
	} else {
		data.Cluster.Node[c.Node] = node.Node{Instance: map[string]instance.Instance{s: {Config: value}}}
	}
}
