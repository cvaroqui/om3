package smon

import (
	"opensvc.com/opensvc/core/topology"
)

func (o *smon) orchestrateHA() {
	if o.svcAgg.Orchestrate != "ha" {
		return
	}
	o.orchestrateHAStart()
	o.orchestrateHAStop()
}

func (o *smon) orchestrateHAStop() {
	if o.svcAgg.Topology != topology.Flex {
		return
	}
	if v, _ := o.isExtraInstance(); !v {
		return
	}
	o.stop()
}

func (o *smon) orchestrateHAStart() {
	if v, _ := o.isStartable(); !v {
		return
	}
	o.orchestrateStarted()
}