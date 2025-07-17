package ox

import "github.com/opensvc/om3/core/commoncmd"

func init() {
	kind := "svc"

	cmdObject := newCmdSVC()
	cmdObjectCollector := commoncmd.NewCmdObjectCollector(kind)
	cmdObjectCollectorTag := newCmdObjectCollectorTag(kind)
	cmdObjectCompliance := commoncmd.NewCmdObjectCompliance(kind)
	cmdObjectComplianceAttach := newCmdObjectComplianceAttach(kind)
	cmdObjectComplianceDetach := newCmdObjectComplianceDetach(kind)
	cmdObjectComplianceShow := newCmdObjectComplianceShow(kind)
	cmdObjectComplianceList := newCmdObjectComplianceList(kind)
	cmdObjectConfig := commoncmd.NewCmdObjectConfig(kind)
	cmdObjectEdit := newCmdObjectEdit(kind)
	cmdObjectInstance := commoncmd.NewCmdObjectInstance(kind)
	cmdObjectInstanceDevice := commoncmd.NewCmdObjectInstanceDevice(kind)
	cmdObjectInstanceResource := commoncmd.NewCmdObjectInstanceResource(kind)
	cmdObjectInstanceResourceInfo := commoncmd.NewCmdObjectInstanceResourceInfo(kind)
	cmdObjectInstanceSync := commoncmd.NewCmdObjectInstanceSync(kind)
	cmdObjectSchedule := newCmdObjectSchedule(kind)
	cmdObjectResource := commoncmd.NewCmdObjectResource(kind)
	cmdObjectSet := newCmdObjectSet(kind)
	cmdObjectPrint := newCmdObjectPrint(kind)
	cmdObjectPrintConfig := newCmdObjectPrintConfig(kind)
	cmdObjectPush := newCmdObjectPush(kind)
	cmdObjectValidate := newCmdObjectValidate(kind)

	root.AddCommand(
		cmdObject,
	)
	cmdObject.AddGroup(
		commoncmd.NewGroupOrchestratedActions(),
		commoncmd.NewGroupQuery(),
		commoncmd.NewGroupSubsystems(),
	)
	cmdObject.AddCommand(
		cmdObjectCollector,
		cmdObjectCompliance,
		cmdObjectConfig,
		cmdObjectEdit,
		cmdObjectInstance,
		cmdObjectPrint,
		cmdObjectPush,
		cmdObjectResource,
		cmdObjectSet,
		cmdObjectSchedule,
		cmdObjectValidate,
		newCmdObjectAbort(kind),
		newCmdObjectBoot(kind),
		commoncmd.NewCmdObjectClear(kind),
		newCmdObjectCreate(kind),
		newCmdObjectDelete(kind),
		newCmdObjectDeploy(kind),
		newCmdObjectDisable(kind),
		newCmdObjectEnable(kind),
		newCmdObjectEnter(kind),
		newCmdObjectEval(kind),
		newCmdObjectFreeze(kind),
		newCmdObjectGet(kind),
		newCmdObjectGiveback(kind),
		newCmdObjectLogs(kind),
		newCmdObjectList(kind),
		commoncmd.NewCmdObjectMonitor("", kind),
		newCmdObjectPurge(kind),
		newCmdObjectProvision(kind),
		newCmdObjectPRStart(kind),
		newCmdObjectPRStop(kind),
		newCmdObjectRestart(kind),
		newCmdObjectStart(kind),
		newCmdObjectStop(kind),
		newCmdObjectSwitch(kind),
		newCmdObjectTakeover(kind),
		newCmdObjectThaw(kind),
		newCmdObjectUnfreeze(kind),
		newCmdObjectUnprovision(kind),
		newCmdObjectUnset(kind),
		newCmdObjectUpdate(kind),
		newCmdTUI(kind),
	)
	cmdObjectConfig.AddCommand(
		commoncmd.NewCmdObjectConfigDoc(kind),
		newCmdObjectConfigEdit(kind),
		newCmdObjectConfigEval(kind),
		newCmdObjectConfigGet(kind),
		newCmdObjectConfigShow(kind),
		newCmdObjectConfigUpdate(kind),
		newCmdObjectConfigValidate(kind),
	)
	cmdObjectEdit.AddCommand(
		newCmdObjectEditConfig(kind),
	)
	cmdObjectResource.AddCommand(
		newCmdObjectResourceList(kind),
	)
	cmdObjectInstance.AddCommand(
		cmdObjectInstanceDevice,
		cmdObjectInstanceResource,
		cmdObjectInstanceSync,
		newCmdObjectInstanceDelete(kind),
		newCmdObjectInstanceFreeze(kind),
		newCmdObjectInstanceList(kind),
		newCmdObjectInstanceStatus(kind),
		newCmdObjectInstanceProvision(kind),
		newCmdObjectInstancePRStart(kind),
		newCmdObjectInstancePRStop(kind),
		newCmdObjectInstanceRestart(kind),
		newCmdObjectInstanceRun(kind),
		newCmdObjectInstanceShutdown(kind),
		newCmdObjectInstanceStart(kind),
		newCmdObjectInstanceStartStandby(kind),
		newCmdObjectInstanceStop(kind),
		newCmdObjectInstanceUnfreeze(kind),
		newCmdObjectInstanceUnprovision(kind),
		commoncmd.NewCmdObjectInstanceClear(kind, ""),
	)
	cmdObjectInstanceDevice.AddCommand(
		newCmdObjectInstanceDeviceList(kind),
	)
	cmdObjectInstanceResource.AddCommand(
		cmdObjectInstanceResourceInfo,
	)
	cmdObjectInstanceResourceInfo.AddCommand(
		newCmdObjectInstanceResourceInfoList(kind),
		newCmdObjectInstanceResourceInfoPush(kind),
	)
	cmdObjectInstanceSync.AddCommand(
		newCmdObjectInstanceSyncIngest(kind),
		newCmdObjectInstanceSyncFull(kind),
		newCmdObjectInstanceSyncResync(kind),
		newCmdObjectInstanceSyncUpdate(kind),
	)
	cmdObjectSchedule.AddCommand(
		newCmdObjectScheduleList(kind),
	)
	cmdObjectSet.AddCommand(
		newCmdObjectSetProvisioned(kind),
		newCmdObjectSetUnprovisioned(kind),
	)
	cmdObjectPrint.AddCommand(
		cmdObjectPrintConfig,
		newCmdObjectPrintResourceInfo(kind),
		newCmdObjectPrintSchedule(kind),
		newCmdObjectPrintStatus(kind),
	)
	cmdObjectPush.AddCommand(
		newCmdObjectPushResourceInfo(kind),
	)
	cmdObjectValidate.AddCommand(
		newCmdObjectValidateConfig(kind),
	)
	cmdObjectCollector.AddCommand(
		cmdObjectCollectorTag,
	)
	cmdObjectCollectorTag.AddCommand(
		newCmdObjectCollectorTagAttach(kind),
		newCmdObjectCollectorTagDetach(kind),
		newCmdObjectCollectorTagShow(kind),
	)
	cmdObjectCompliance.AddCommand(
		cmdObjectComplianceAttach,
		cmdObjectComplianceDetach,
		cmdObjectComplianceShow,
		cmdObjectComplianceList,
		newCmdObjectComplianceEnv(kind),
		newCmdObjectComplianceAuto(kind),
		newCmdObjectComplianceCheck(kind),
		newCmdObjectComplianceFix(kind),
		newCmdObjectComplianceFixable(kind),
	)
	cmdObjectComplianceAttach.AddCommand(
		newCmdObjectComplianceAttachModuleset(kind),
		newCmdObjectComplianceAttachRuleset(kind),
	)
	cmdObjectComplianceDetach.AddCommand(
		newCmdObjectComplianceDetachModuleset(kind),
		newCmdObjectComplianceDetachRuleset(kind),
	)
	cmdObjectComplianceShow.AddCommand(
		newCmdObjectComplianceShowRuleset(kind),
		newCmdObjectComplianceShowModuleset(kind),
	)
	cmdObjectComplianceList.AddCommand(
		newCmdObjectComplianceListModules(kind),
		newCmdObjectComplianceListModuleset(kind),
		newCmdObjectComplianceListRuleset(kind),
	)
}
