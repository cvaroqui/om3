package om

func init() {
	kind := "ccfg"

	cmdObject := newCmdCcfg()
	cmdObjectConfig := newCmdObjectConfig(kind)
	cmdObjectEdit := newCmdObjectEdit(kind)
	cmdObjectSet := newCmdObjectSet(kind)
	cmdObjectSSH := newCmdObjectSSH(kind)
	cmdObjectPrint := newCmdObjectPrint(kind)
	cmdObjectPrintConfig := newCmdObjectPrintConfig(kind)
	cmdObjectValidate := newCmdObjectValidate(kind)

	root.AddCommand(
		cmdObject,
	)
	cmdObject.AddCommand(
		cmdObjectConfig,
		cmdObjectEdit,
		cmdObjectSet,
		cmdObjectSSH,
		cmdObjectPrint,
		cmdObjectValidate,
		newCmdClusterAbort(),
		newCmdClusterFreeze(),
		newCmdClusterLogs(),
		newCmdClusterThaw(),
		newCmdClusterUnfreeze(),
		newCmdObjectCreate(kind),
		newCmdObjectDoc(kind),
		newCmdObjectEval(kind),
		newCmdObjectGet(kind),
		newCmdObjectLogs(kind),
		newCmdObjectLs(kind),
		newCmdObjectMonitor(kind),
		newCmdObjectStatus(kind),
		newCmdObjectUnset(kind),
	)
	cmdObjectConfig.AddCommand(
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
	cmdObjectPrint.AddCommand(
		cmdObjectPrintConfig,
		newCmdObjectPrintStatus(kind),
	)
	cmdObjectPrintConfig.AddCommand(
		newCmdObjectPrintConfigMtime(kind),
	)
	cmdObjectSSH.AddCommand(
		newCmdClusterSSHTrust(),
	)
	cmdObjectValidate.AddCommand(
		newCmdObjectValidateConfig(kind),
	)
}
