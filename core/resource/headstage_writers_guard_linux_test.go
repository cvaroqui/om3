//go:build linux

package resource

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/opensvc/om3/v3/util/plog"
)

// The writers of a stage are killed as root: the directory must be one the
// stagers name a stage, never the live head or the root a caller's mistake
// would pass.
func TestKillHeadStageWritersRefusesADirectoryNotAStage(t *testing.T) {
	for dir, ok := range map[string]bool{
		"/var/lib/opensvc/ns/svc/s1/fs#1/head_stage": true,
		"/srv/data/.app.om-stage":                    true,
		"/mnt/top/subvol.om-stage":                   true,
		"/":                                          false,
		"":                                           false,
		"relative/head_stage":                        false,
		"/srv/data":                                  false,
		"/srv/.om-stage":                             false,
		"/srv/head_stage_top":                        false,
	} {
		assert.Equal(t, ok, IsHeadStageDir(dir), dir)
	}
	for _, dir := range []string{"/", "/srv/data", t.TempDir()} {
		assert.ErrorContains(t, KillHeadStageWriters(dir, plog.NewDefaultLogger()), "not the directory of a stage", dir)
	}
}
