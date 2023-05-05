package daemonapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/opensvc/om3/core/node"
	"github.com/opensvc/om3/daemon/api"
	"github.com/opensvc/om3/daemon/daemonctx"
	"github.com/opensvc/om3/daemon/daemondata"
	"github.com/opensvc/om3/daemon/daemonlogctx"
	"github.com/opensvc/om3/daemon/msgbus"
	"github.com/opensvc/om3/util/hostname"
	"github.com/opensvc/om3/util/pubsub"
)

func (a *DaemonApi) PostDaemonStop(w http.ResponseWriter, r *http.Request) {
	log := daemonlogctx.Logger(r.Context()).With().Str("func", "PostDaemonStop").Logger()
	log.Debug().Msg("starting")

	ctx := r.Context()
	daemon := daemonctx.Daemon(ctx)

	maintenance := func() {
		log.Info().Msg("announce maintenance state")
		bus := pubsub.BusFromContext(ctx)
		state := node.MonitorStateMaintenance
		bus.Pub(&msgbus.SetNodeMonitor{
			Node: hostname.Hostname(),
			Value: node.MonitorUpdate{
				State: &state,
			},
		}, labelApi)
		time.Sleep(2 * daemondata.PropagationInterval())
	}

	if daemon.Running() {
		maintenance()

		log.Info().Msg("daemon stopping")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(api.ResponseText("daemon stopping"))
		w.WriteHeader(http.StatusOK)
		go func() {
			// Give time for response received by client before stop daemon
			time.Sleep(50 * time.Millisecond)
			if err := daemon.Stop(); err != nil {
				log.Error().Err(err).Msg("daemon stop failure")
				return
			}
			log.Info().Msg("daemon stopped")
		}()
	} else {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(api.ResponseText("no daemon to stop"))
		w.WriteHeader(http.StatusOK)
	}
}
