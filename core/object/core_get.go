package object

import "opensvc.com/opensvc/util/key"

// OptsGet is the options of the Get function of all base objects.
type OptsGet struct {
	OptsLock
	Keyword     string `flag:"kw"`
	Eval        bool   `flag:"eval"`
	Impersonate string `flag:"impersonate"`
}

// Get returns a keyword value
func (t *core) Get(options OptsGet) (interface{}, error) {
	k := key.Parse(options.Keyword)
	if options.Eval {
		v, err := t.config.EvalAs(k, options.Impersonate)
		return v, err
	}
	return t.config.Get(k), nil
}