package daemondata

import (
	"context"

	"github.com/opensvc/om3/core/instance"
	"github.com/opensvc/om3/core/node"
	"github.com/opensvc/om3/core/pool"
	"github.com/opensvc/om3/daemon/daemonsubsystem"
	"github.com/opensvc/om3/daemon/hbcache"
	"github.com/opensvc/om3/daemon/msgbus"
	"github.com/opensvc/om3/util/pubsub"
)

type (
	opDropPeerNode struct {
		errC
		node string
	}

	opGetClusterNodeData struct {
		errC
		Node string
		Data chan<- *node.Node
	}
)

// DropPeerNode is a public method to drop peer node from t. It uses private call
// to func (d *data) dropPeer.
// It is used by a node stale detector to call drop peer on stale <peer> detection.
func (t T) DropPeerNode(peerNode string) error {
	err := make(chan error, 1)
	op := opDropPeerNode{
		errC: err,
		node: peerNode,
	}
	t.cmdC <- op
	return <-err
}

func (o opDropPeerNode) call(ctx context.Context, d *data) error {
	d.dropPeer(o.node)
	return nil
}

// ClusterNodeData returns deep copy of cluster node data for node n.
// It returns nil when node n is not found in cluster data.
func (t T) ClusterNodeData(n string) *node.Node {
	nData := make(chan *node.Node, 1)
	err := make(chan error, 1)
	t.cmdC <- opGetClusterNodeData{
		errC: err,
		Node: n,
		Data: nData,
	}
	if <-err != nil {
		return nil
	}
	return <-nData
}

func (o opGetClusterNodeData) call(ctx context.Context, d *data) error {
	if nData, ok := d.clusterData.Cluster.Node[o.Node]; ok {
		o.Data <- nData.DeepCopy()
	} else {
		o.Data <- nil
	}
	return nil
}

// dropPeer handle actions needed when <peer> node is dropped
//
// It drops <peer> from hbcache
// It drops <peer> from instance data holders and publish associated msgbus.Instance<xxx>Deleted
// It drops <peer> node data holder and publish associated msgbus.Node<xxx>Deleted
// It delete <peer> d.clusterData.Cluster.Node
// It calls setDaemonHeartbeat()
// It publish ForgetPeer
func (d *data) dropPeer(peer string) {
	d.log.Infof("drop peer node %s", peer)
	peerLabels := []pubsub.Label{{"node", peer}, {"from", "peer"}}

	hbcache.DropPeer(peer)

	// unset and publish deleted <peer> components instance and node (found from
	// instance and node data holders).
	d.log.Infof("unset and publish deleted peer %s components", peer)
	for p := range instance.ConfigData.GetByNode(peer) {
		instance.ConfigData.Unset(p, peer)
		d.publisher.Pub(&msgbus.InstanceConfigDeleted{Node: peer, Path: p}, append(peerLabels, pubsub.Label{"namespace", p.Namespace}, pubsub.Label{"path", p.String()})...)
	}
	for p := range instance.StatusData.GetByNode(peer) {
		instance.StatusData.Unset(p, peer)
		d.publisher.Pub(&msgbus.InstanceStatusDeleted{Node: peer, Path: p}, append(peerLabels, pubsub.Label{"namespace", p.Namespace}, pubsub.Label{"path", p.String()})...)
	}
	for p := range instance.MonitorData.GetByNode(peer) {
		instance.MonitorData.Unset(p, peer)
		d.publisher.Pub(&msgbus.InstanceMonitorDeleted{Node: peer, Path: p}, append(peerLabels, pubsub.Label{"namespace", p.Namespace}, pubsub.Label{"path", p.String()})...)
	}
	for _, p := range pool.StatusData.GetByNode(peer) {
		pool.StatusData.Unset(p.Name, peer)
		d.publisher.Pub(&msgbus.NodePoolStatusDeleted{Node: peer, Name: p.Name}, peerLabels...)
	}
	if v := node.MonitorData.GetByNode(peer); v != nil {
		node.DropNode(peer)
		// TODO: find a way to clear parts of cluster.node.<peer>.Status
		d.publisher.Pub(&msgbus.NodeMonitorDeleted{Node: peer}, peerLabels...)

		daemonsubsystem.DropNode(peer)
		d.publisher.Pub(&msgbus.DaemonCollectorUpdated{Node: peer}, peerLabels...)
		d.publisher.Pub(&msgbus.DaemonDataUpdated{Node: peer}, peerLabels...)
		d.publisher.Pub(&msgbus.DaemonDnsUpdated{Node: peer}, peerLabels...)
		d.publisher.Pub(&msgbus.DaemonHeartbeatUpdated{Node: peer}, peerLabels...)
		d.publisher.Pub(&msgbus.DaemonListenerUpdated{Node: peer}, peerLabels...)
		d.publisher.Pub(&msgbus.DaemonRunnerImonUpdated{Node: peer}, peerLabels...)
		d.publisher.Pub(&msgbus.DaemonSchedulerUpdated{Node: peer}, peerLabels...)
	}

	// delete peer from internal caches
	delete(d.hbGens, peer)
	delete(d.hbGens[d.localNode], peer)
	delete(d.hbPatchMsgUpdated, peer)
	delete(d.hbMsgPatchLength, peer)
	delete(d.hbMsgType, peer)
	delete(d.previousRemoteInfo, peer)

	// delete peer d.clusterData.Cluster.Node...
	if d.clusterData.Cluster.Node[d.localNode].Status.Gen != nil {
		delete(d.clusterData.Cluster.Node[d.localNode].Status.Gen, peer)
	}
	delete(d.clusterData.Cluster.Node, peer)

	d.setDaemonHeartbeat()
	d.publisher.Pub(&msgbus.ForgetPeer{Node: peer}, peerLabels...)
}
