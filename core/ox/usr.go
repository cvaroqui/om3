package ox

func init() {
	kind := "usr"

	cmdObject := newCmdUsr()
	cmdObjectEdit := newCmdObjectEdit(kind)
	cmdObjectInstance := newCmdObjectInstance(kind)
	cmdObjectSet := newCmdObjectSet(kind)
	cmdObjectPrint := newCmdObjectPrint(kind)
	cmdObjectPrintConfig := newCmdObjectPrintConfig(kind)
	cmdObjectValidate := newCmdObjectValidate(kind)

	root.AddCommand(
		cmdObject,
	)
	cmdObject.AddCommand(
		cmdObjectEdit,
		cmdObjectInstance,
		cmdObjectPrint,
		cmdObjectSet,
		cmdObjectValidate,
		newCmdKeystoreAdd(kind),
		newCmdKeystoreChange(kind),
		newCmdKeystoreDecode(kind),
		newCmdKeystoreKeys(kind),
		newCmdKeystoreRemove(kind),
		newCmdKeystoreRename(kind),
		newCmdObjectCreate(kind),
		newCmdObjectDelete(kind),
		newCmdObjectEval(kind),
		newCmdObjectGet(kind),
		newCmdObjectLogs(kind),
		newCmdObjectLs(kind),
		newCmdObjectMonitor(kind),
		newCmdObjectPurge(kind),
		newCmdObjectStatus(kind),
		newCmdObjectUnset(kind),
		newCmdObjectUpdate(kind),
		newCmdSecGenCert(kind),
		newCmdSecPKCS(kind),
		newCmdTUI(kind),
	)
	cmdObjectEdit.AddCommand(
		newCmdObjectEditConfig(kind),
	)
	cmdObjectInstance.AddCommand(
		newCmdObjectInstanceLs(kind),
	)
	cmdObjectPrint.AddCommand(
		cmdObjectPrintConfig,
		newCmdObjectPrintStatus(kind),
	)
	cmdObjectValidate.AddCommand(
		newCmdObjectValidateConfig(kind),
	)
}
