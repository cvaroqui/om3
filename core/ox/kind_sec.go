package ox

import "github.com/opensvc/om3/core/commoncmd"

func init() {
	kind := "sec"

	cmdObject := newCmdSec()
	cmdObjectCertificate := newCmdObjectCertificate(kind)
	cmdObjectConfig := commoncmd.NewCmdObjectConfig(kind)
	cmdObjectEdit := newCmdObjectEdit(kind)
	cmdObjectGen := newCmdObjectGen(kind)
	cmdObjectKey := commoncmd.NewCmdObjectKey(kind)
	cmdObjectInstance := commoncmd.NewCmdObjectInstance(kind)
	cmdObjectSet := newCmdObjectSet(kind)
	cmdObjectPrint := newCmdObjectPrint(kind)
	cmdObjectPrintConfig := newCmdObjectPrintConfig(kind)
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
		cmdObjectCertificate,
		cmdObjectConfig,
		cmdObjectEdit,
		cmdObjectGen,
		cmdObjectKey,
		cmdObjectInstance,
		cmdObjectPrint,
		cmdObjectSet,
		cmdObjectValidate,
		newCmdDataStoreAdd(kind),
		newCmdDataStoreChange(kind),
		newCmdDataStoreDecode(kind),
		newCmdDataStoreKeys(kind),
		newCmdDataStoreInstall(kind),
		newCmdDataStoreRemove(kind),
		newCmdDataStoreRename(kind),
		newCmdObjectCreate(kind),
		newCmdObjectDelete(kind),
		newCmdObjectEval(kind),
		newCmdObjectGet(kind),
		newCmdObjectLogs(kind),
		newCmdObjectList(kind),
		commoncmd.NewCmdObjectMonitor("", kind),
		newCmdObjectPurge(kind),
		newCmdObjectUnset(kind),
		newCmdObjectUpdate(kind),
		newCmdObjectPKCS(kind),
		newCmdTUI(kind),
	)
	cmdObjectCertificate.AddCommand(
		newCmdObjectCertificateCreate(kind),
		newCmdObjectCertificatePKCS(kind),
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
	cmdObjectGen.AddCommand(
		newCmdObjectGenCert(kind),
	)
	cmdObjectKey.AddCommand(
		newCmdObjectKeyAdd(kind),
		newCmdObjectKeyChange(kind),
		newCmdObjectKeyDecode(kind),
		newCmdObjectKeyEdit(kind),
		newCmdObjectKeyInstall(kind),
		newCmdObjectKeyList(kind),
		newCmdObjectKeyRemove(kind),
		newCmdObjectKeyRename(kind),
	)
	cmdObjectInstance.AddCommand(
		newCmdObjectInstanceList(kind),
		newCmdObjectInstanceDelete(kind),
	)
	cmdObjectPrint.AddCommand(
		cmdObjectPrintConfig,
	)
	cmdObjectValidate.AddCommand(
		newCmdObjectValidateConfig(kind),
	)
}
