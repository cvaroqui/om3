package omcmd

import (
	"context"
	"fmt"

	"github.com/opensvc/om3/core/naming"
	"github.com/opensvc/om3/core/object"
	"github.com/opensvc/om3/core/objectaction"
)

type (
	CmdObjectCertificateCreate struct {
		OptsGlobal
	}
)

func (t *CmdObjectCertificateCreate) Run(selector, kind string) error {
	mergedSelector := mergeSelector(selector, t.ObjectSelector, kind, "")
	return objectaction.New(
		objectaction.LocalFirst(),
		objectaction.WithLocal(t.Local),
		objectaction.WithColor(t.Color),
		objectaction.WithOutput(t.Output),
		objectaction.WithObjectSelector(mergedSelector),
		objectaction.WithLocalFunc(func(ctx context.Context, p naming.Path) (interface{}, error) {
			o, err := object.New(p)
			if err != nil {
				return nil, err
			}
			store, ok := o.(object.SecureKeystore)
			if !ok {
				return nil, fmt.Errorf("%s is not a secure keystore", o)
			}
			return nil, store.GenCert()
		}),
	).Do()
}
