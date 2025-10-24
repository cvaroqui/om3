package ccfg

import (
	"strings"

	"github.com/opensvc/om3/core/cluster"
	"github.com/opensvc/om3/core/clusternode"
	"github.com/opensvc/om3/core/network"
	"github.com/opensvc/om3/core/object"
	"github.com/opensvc/om3/daemon/msgbus"
	"github.com/opensvc/om3/util/pubsub"
	"github.com/opensvc/om3/util/stringslice"
)

// onConfigFileUpdated reloads the config parser and emits the updated
// node.Config data in a NodeConfigUpdated event, so other go routines
// can just subscribe to this event to maintain the cache of keywords
// they care about.
func (t *Manager) onConfigFileUpdated(c *msgbus.ConfigFileUpdated) {
	t.pubClusterConfig()
}

func (t *Manager) pubClusterConfig() {
	previousNodes := t.state.Nodes

	state, err := object.SetClusterConfig()
	if err != nil {
		t.log.Errorf("%s", err)
	}
	for _, issue := range state.Issues {
		t.log.Warnf("issue: %s", issue)
	}
	t.handleConfigChanges()

	t.state = *state.DeepCopy()
	labelLocalhost := pubsub.Label{"node", t.localhost}

	removed, added := stringslice.Diff(previousNodes, state.Nodes)
	if len(added) > 0 {
		t.log.Debugf("added nodes: %s", added)
	}
	if len(removed) > 0 {
		t.log.Debugf("removed nodes: %s", removed)
	}
	cluster.ConfigData.Set(&state)
	clusternode.Set(state.Nodes)

	t.publisher.Pub(&msgbus.ClusterConfigUpdated{Node: t.localhost, Value: state, NodesAdded: added, NodesRemoved: removed}, labelLocalhost)

	for _, v := range added {
		t.publisher.Pub(&msgbus.JoinSuccess{AddedNode: v}, labelLocalhost, pubsub.Label{"added_node", v})
	}
	for _, v := range removed {
		t.publisher.Pub(&msgbus.LeaveSuccess{RemovedNode: v}, labelLocalhost, pubsub.Label{"removed_node", v})
	}
}

func (t *Manager) handleConfigChanges() {
	clu, err := object.NewCluster()
	if err != nil {
		t.log.Errorf("%s", err)
		return
	}
	var change bool

	for _, name := range clu.Config().SectionStrings() {
		if strings.HasPrefix(name, "network#") || name == "cni" {
			lastSig, _ := t.networkSigs[name]
			sig := clu.Config().SectionSig(name)
			if sig != lastSig {
				change = true
				t.log.Infof("configuration section %s changed (sig %s is now %s)", name, lastSig, sig)
				t.networkSigs[name] = sig
			}
		}
	}
	if change {
		if n, err := object.NewNode(object.WithLogger(t.log)); err != nil {
			t.log.Errorf("allocate Node for network setup: %s", err)
		} else {
			t.log.Infof("reconfigure networks")
			if err := network.Setup(n); err != nil {
				t.log.Infof("reconfigure networks: %s", err.Error())
			}
		}
	}
	return
}
