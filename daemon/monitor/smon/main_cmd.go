package smon

import (
	"opensvc.com/opensvc/core/instance"
	"opensvc.com/opensvc/core/status"
	"opensvc.com/opensvc/daemon/monitor/moncmd"
)

// cmdSvcAggUpdated updateIfChange state global expect from aggregated status
func (o *smon) cmdSvcAggUpdated(c moncmd.MonSvcAggUpdated) {
	if c.SrcEv != nil {
		switch srcCmd := (*c.SrcEv).(type) {
		case moncmd.InstStatusUpdated:
			srcNode := srcCmd.Node
			if _, ok := o.instStatus[srcNode]; ok {
				instStatus := srcCmd.Status
				o.instStatus[srcNode] = instStatus
			}
		case moncmd.CfgUpdated:
			if srcCmd.Node == o.localhost {
				cfgNodes := make(map[string]struct{})
				for _, node := range srcCmd.Config.Scope {
					cfgNodes[node] = struct{}{}
					if _, ok := o.instStatus[node]; !ok {
						o.instStatus[node] = instance.Status{Avail: status.Undef}
					}
				}
				for node := range o.instStatus {
					if _, ok := cfgNodes[node]; !ok {
						o.log.Info().Msgf("drop not anymore in local config status from node %s", node)
						delete(o.instStatus, node)
					}
				}
				o.scopeNodes = append([]string{}, srcCmd.Config.Scope...)
			}
		}
	}
	o.svcAgg = c.SvcAgg
	o.orchestrate()
}

func (o *smon) cmdSetSmonClient(c instance.Monitor) {
	strVal := c.GlobalExpect
	if strVal == statusIdle {
		strVal = "unset"
	}
	o.log.Info().Msgf("client request global expect to %s %+v", strVal, c)
	if o.state.Status != statusIdle {
		msg := "can't set global expect to " + strVal + ", state is not idle: " + o.state.Status
		o.log.Info().Msg(msg)
		return
	}
	if o.state.GlobalExpect != globalExpectUnset {
		msg := "can't set global expect to " + strVal + ", global expect is already set: " + o.state.GlobalExpect
		o.log.Info().Msg(msg)
		return
	}
	if c.GlobalExpect != o.state.GlobalExpect {
		o.change = true
		o.state.GlobalExpect = c.GlobalExpect
		o.state.GlobalExpectUpdated = c.GlobalExpectUpdated
		o.updateIfChange()
		o.orchestrate()
	}
}

func (o *smon) cmdSmonUpdated(c moncmd.SmonUpdated) {
	node := c.Node
	if node == o.localhost {
		return
	}
	instSmon := c.Status
	o.log.Debug().Msgf("updated instance smon from node %s  -> %s", node, instSmon.GlobalExpect)
	o.instSmon[node] = instSmon
	o.convergeGlobalExpectFromRemote()
	o.updateIfChange()
	o.orchestrate()
	o.updateIfChange()
}

func (o *smon) needOrchestrate(c cmdOrchestrate) {
	if o.state.Status == c.state {
		o.change = true
		o.state.Status = c.newState
		o.updateIfChange()
	}
	o.orchestrate()
}
