package object

import (
	"path/filepath"

	"github.com/opensvc/fcntllock"
	"github.com/opensvc/flock"
	"opensvc.com/opensvc/core/actioncontext"
	"opensvc.com/opensvc/util/xsession"
)

func (t *Base) lockPath(group string) (path string) {
	if group == "" {
		group = "generic"
	}
	path = filepath.Join(VarDir(t.Path), "lock", group)
	return
}

func (t *Base) lockAction(action actioncontext.Properties, options OptsLock) (func(), error) {
	unlock := func() {}
	if !action.MustLock {
		return unlock, nil
	}
	if options.Disable {
		// --nolock handling
		return unlock, nil
	}
	p := t.lockPath(action.LockGroup)
	lock := flock.New(p, xsession.ID, fcntllock.New)
	err := lock.Lock(options.Timeout, action.Name)
	if err != nil {
		return unlock, err
	}
	unlock = func() { _ = lock.UnLock() }

	// the config may have changed since we first read it.
	// ex:
	//  set --kw env.a=a &
	//  set --kw env.b=b
	//
	// These parallel commands end up with either a or b set,
	// because the 2 process load the config cache before locking.
	t.reloadConfig()

	return unlock, nil
}
