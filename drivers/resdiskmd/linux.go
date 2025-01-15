//go:build linux

package resdiskmd

import "github.com/opensvc/om3/util/md"

func (t *T) md() MDDriver {
	d := md.New(
		t.Name(),
		t.UUID,
		md.WithLogger(t.Log()),
	)
	return d
}
