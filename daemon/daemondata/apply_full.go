package daemondata

import (
	"context"
	"encoding/json"
	"time"

	"opensvc.com/opensvc/core/cluster"
	"opensvc.com/opensvc/core/event"
	"opensvc.com/opensvc/daemon/msgbus"
	"opensvc.com/opensvc/util/jsondelta"
)

type opApplyRemoteFull struct {
	nodename string
	full     *cluster.NodeData
	done     chan<- bool
}

func (o opApplyRemoteFull) call(ctx context.Context, d *data) {
	d.counterCmd <- idApplyFull
	d.log.Debug().Msgf("opApplyRemoteFull %s", o.nodename)
	d.pending.Cluster.Node[o.nodename] = *o.full
	d.mergedFromPeer[o.nodename] = o.full.Status.Gen[o.nodename]
	d.remotesNeedFull[o.nodename] = false
	if gen, ok := d.pending.Cluster.Node[o.nodename].Status.Gen[d.localNode]; ok {
		d.mergedOnPeer[o.nodename] = gen
	}

	d.pending.Cluster.Node[d.localNode].Status.Gen[o.nodename] = o.full.Status.Gen[o.nodename]
	absolutePatch := jsondelta.Patch{
		jsondelta.Operation{
			OpPath:  jsondelta.OperationPath{"cluster", "node", o.nodename},
			OpValue: jsondelta.NewOptValue(o.full),
			OpKind:  "replace",
		},
	}

	if eventB, err := json.Marshal(absolutePatch); err != nil {
		d.log.Error().Err(err).Msgf("Marshal absolutePatch %s", o.nodename)
	} else {
		eventId++
		msgbus.PubEvent(d.bus, event.Event{
			Kind: "patch",
			ID:   eventId,
			Time: time.Now(),
			Data: eventB,
		})
	}

	d.log.Debug().
		Interface("remotesNeedFull", d.remotesNeedFull).
		Interface("mergedOnPeer", d.mergedOnPeer).
		Interface("pending gen", d.pending.Cluster.Node[o.nodename].Status.Gen).
		Interface("full.gen", o.full.Status.Gen).
		Msgf("opApplyRemoteFull %s", o.nodename)
	select {
	case <-ctx.Done():
	case o.done <- true:
	}
}

func (t T) ApplyFull(nodename string, full *cluster.NodeData) {
	done := make(chan bool)
	t.cmdC <- opApplyRemoteFull{
		nodename: nodename,
		full:     full,
		done:     done,
	}
	<-done
}