package resfszfs

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/opensvc/om3/v3/core/resource"
	"github.com/opensvc/om3/v3/util/plog"
)

type (
	// fakeDataset is a dataset of a fakeZfs.
	fakeDataset struct {
		// id tells a dataset from another given its name since.
		id      int
		origin  string
		mounted bool
		props   map[string]string
		snaps   []string

		// content is what the dataset holds.
		content string
	}

	// fakeZfs is a zfs holding datasets, doing what the commands a
	// stager runs do to them.
	fakeZfs struct {
		t        *testing.T
		datasets map[string]*fakeDataset
		nextID   int
		busy     bool
		cmds     []string
	}

	killed struct{ step string }
)

func newFakeZfs(t *testing.T) *fakeZfs {
	z := &fakeZfs{t: t, datasets: make(map[string]*fakeDataset)}
	z.datasets["tank/data"] = &fakeDataset{
		id:      z.id(),
		mounted: true,
		content: "old",
		snaps:   []string{"daily"},
		props: map[string]string{
			"mountpoint":     "/srv/data",
			"compression":    "zstd",
			"refquota":       "1073741824",
			"refreservation": "536870912",
			"opensvc:owner":  "svc1",
		},
	}
	return z
}

func (z *fakeZfs) id() int {
	z.nextID++
	return z.nextID
}

func noDataset(name string) error {
	return fmt.Errorf("exit status 1: cannot open '%s': dataset does not exist", name)
}

func (z *fakeZfs) run(_ context.Context, changes bool, args ...string) (string, error) {
	if changes {
		z.cmds = append(z.cmds, strings.Join(args, " "))
	}
	last := args[len(args)-1]
	ds, snap, isSnap := strings.Cut(last, "@")
	switch args[0] {
	case "list":
		d, ok := z.datasets[ds]
		if !ok || (isSnap && !slices.Contains(d.snaps, snap)) {
			return "", noDataset(last)
		}
		if slices.Contains(args, "-r") {
			l := []string{ds}
			for name := range z.datasets {
				if strings.HasPrefix(name, ds+"/") {
					l = append(l, name)
				}
			}
			return strings.Join(l, "\n") + "\n", nil
		}
		return last + "\n", nil
	case "get":
		d, ok := z.datasets[ds]
		if !ok {
			return "", noDataset(last)
		}
		prop := args[len(args)-2]
		switch prop {
		case "mounted":
			if d.mounted {
				return "yes\n", nil
			}
			return "no\n", nil
		case "origin":
			if d.origin == "" {
				return "-\n", nil
			}
			return d.origin + "\n", nil
		case "encryption":
			return "off\n", nil
		case "all":
			lines := make([]string, 0)
			for k, v := range d.props {
				lines = append(lines, k+"\t"+v)
			}
			return strings.Join(lines, "\n") + "\n", nil
		default:
			if v, ok := d.props[prop]; ok {
				return v + "\n", nil
			}
			return "-\n", nil
		}
	case "snapshot":
		d := z.datasets[ds]
		require.NotContains(z.t, d.snaps, snap)
		d.snaps = append(d.snaps, snap)
	case "clone":
		origin := args[len(args)-2]
		from, _, _ := strings.Cut(origin, "@")
		require.NotContains(z.t, z.datasets, last)
		d := &fakeDataset{id: z.id(), origin: origin, props: make(map[string]string), content: z.datasets[from].content}
		for i, a := range args {
			if a == "-o" {
				k, v, _ := strings.Cut(args[i+1], "=")
				d.props[k] = v
			}
		}
		z.datasets[last] = d
	case "mount":
		z.datasets[last].mounted = true
	case "unmount":
		z.datasets[last].mounted = false
	case "promote":
		d := z.datasets[last]
		from, snap, _ := strings.Cut(d.origin, "@")
		o := z.datasets[from]
		// The clone takes the snapshots of its origin, to the one it
		// is of, and the origin becomes its clone.
		i := slices.Index(o.snaps, snap)
		d.snaps = append(slices.Clone(o.snaps[:i+1]), d.snaps...)
		o.snaps = o.snaps[i+1:]
		o.origin = last + "@" + snap
		d.origin = ""
	case "rename":
		from := args[len(args)-2]
		require.Contains(z.t, z.datasets, from)
		require.NotContains(z.t, z.datasets, last)
		require.False(z.t, z.datasets[from].mounted, "the stager unmounts what it renames")
		z.datasets[last] = z.datasets[from]
		delete(z.datasets, from)
		for _, d := range z.datasets {
			if o, snap, ok := strings.Cut(d.origin, "@"); ok && o == from {
				d.origin = last + "@" + snap
			}
		}
	case "destroy":
		d, ok := z.datasets[ds]
		if !ok {
			return "", noDataset(last)
		}
		for name, other := range z.datasets {
			if (isSnap && other.origin == last) || (!isSnap && strings.HasPrefix(other.origin, ds+"@")) {
				return "", fmt.Errorf("exit status 1: cannot destroy '%s': it has dependent clones: %s", last, name)
			}
		}
		if isSnap {
			d.snaps = slices.DeleteFunc(d.snaps, func(s string) bool { return s == snap })
		} else {
			require.False(z.t, d.mounted, "the stager unmounts what it destroys")
			if len(d.snaps) > 0 && !slices.Contains(args, "-r") {
				return "", fmt.Errorf("exit status 1: cannot destroy '%s': filesystem has children", last)
			}
			delete(z.datasets, ds)
		}
	case "set":
		k, v, _ := strings.Cut(args[1], "=")
		z.datasets[last].props[k] = v
	case "inherit":
		delete(z.datasets[last].props, args[1])
	default:
		z.t.Fatalf("unexpected zfs %s", strings.Join(args, " "))
	}
	return "", nil
}

func (z *fakeZfs) stager(varDir string) *headStager {
	return &headStager{
		dataset: "tank/data",
		varDir:  varDir,
		log:     plog.NewDefaultLogger(),
		zfs:     z.run,
		liveMounted: func(context.Context) (bool, error) {
			d, ok := z.datasets["tank/data"]
			return ok && d.mounted, nil
		},
		mountLive: func(context.Context) error {
			z.datasets["tank/data"].mounted = true
			return nil
		},
		umountLive: func(context.Context) error {
			if z.busy {
				return fmt.Errorf("target is busy")
			}
			z.datasets["tank/data"].mounted = false
			return nil
		},
	}
}

func (z *fakeZfs) names() []string {
	l := make([]string, 0)
	for name := range z.datasets {
		l = append(l, name)
	}
	slices.Sort(l)
	return l
}

// killAt has the stager die at the step named, as a process killed there.
func killAt(t *testing.T, stager *headStager, step string, fn func()) (died bool) {
	t.Helper()
	stager.step = func(name string) {
		if name == step {
			panic(killed{name})
		}
	}
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(killed); !ok {
				panic(r)
			}
			died = true
		}
	}()
	fn()
	return false
}

func journalStep(t *testing.T, varDir string) string {
	t.Helper()
	j, err := resource.ReadHeadStageJournal(varDir)
	require.NoError(t, err)
	if j == nil {
		return ""
	}
	return j.Step
}

// The stage of a dataset is a clone of it, given the properties set on it
// but the ones that would act on the stage, and its commit makes the clone
// the dataset: another one under the same name, with the properties and the
// snapshots of the former, mounted where it was, the former gone.
func TestStageCommit(t *testing.T) {
	ctx := context.Background()
	z := newFakeZfs(t)
	varDir := t.TempDir()
	former := z.datasets["tank/data"].id
	stager := z.stager(varDir)

	stage, ok, err := stager.stage(ctx, "run1")
	require.NoError(t, err)
	require.True(t, ok)
	assert.True(t, stage.Seeded, "a clone holds what the dataset held")
	assert.Equal(t, stager.stageDir(), stage.Dir)
	clone := z.datasets["tank/data.om-stage"]
	require.NotNil(t, clone)
	assert.True(t, clone.mounted)
	assert.Contains(t, clone.props[propStage], `"step":"staged"`, "the clone carries the journal of its stage")
	assert.NotContains(t, clone.props[propStage], "pid", "the process staging is the node's to tell")
	delete(clone.props, propStage)
	assert.Equal(t, map[string]string{
		"mountpoint":    stager.stageDir(),
		"canmount":      "noauto",
		"compression":   "zstd",
		"refquota":      "1073741824",
		"opensvc:owner": "svc1",
	}, clone.props, "no reservation beside the one of the dataset")
	assert.Equal(t, resource.HeadStageStaged, journalStep(t, varDir))
	clone.content = "new"
	assert.Equal(t, "old", z.datasets["tank/data"].content)
	assert.True(t, z.datasets["tank/data"].mounted, "the head stays mounted while its stage is written")

	require.NoError(t, stage.Commit(ctx))
	assert.Equal(t, []string{"tank/data"}, z.names())
	head := z.datasets["tank/data"]
	assert.NotEqual(t, former, head.id, "the head is another dataset")
	assert.Equal(t, "new", head.content)
	assert.True(t, head.mounted)
	assert.Empty(t, head.origin)
	assert.Equal(t, []string{"daily"}, head.snaps, "the snapshots of the former dataset are the head's")
	assert.Equal(t, map[string]string{
		"mountpoint":     "/srv/data",
		"canmount":       "on",
		"compression":    "zstd",
		"refquota":       "1073741824",
		"refreservation": "536870912",
		"opensvc:owner":  "svc1",
	}, head.props)
	assert.Empty(t, journalStep(t, varDir))
}

// A dataset inheriting its mount point has its stage inherit it again once
// it is the head.
func TestStageCommitInheritsTheMountpoint(t *testing.T) {
	ctx := context.Background()
	z := newFakeZfs(t)
	delete(z.datasets["tank/data"].props, "mountpoint")
	stage, ok, err := z.stager(t.TempDir()).stage(ctx, "run1")
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, stage.Commit(ctx))
	assert.NotContains(t, z.datasets["tank/data"].props, "mountpoint")
}

// A stage discarded leaves the dataset as it was, and nothing of the stage.
func TestStageDiscard(t *testing.T) {
	ctx := context.Background()
	z := newFakeZfs(t)
	varDir := t.TempDir()
	former := z.datasets["tank/data"].id
	stage, ok, err := z.stager(varDir).stage(ctx, "run1")
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, stage.Discard(ctx))
	assert.Equal(t, []string{"tank/data"}, z.names())
	assert.Equal(t, former, z.datasets["tank/data"].id)
	assert.Equal(t, []string{"daily"}, z.datasets["tank/data"].snaps)
	assert.Empty(t, journalStep(t, varDir))
}

// A dataset still used refuses its unmount: the commit fails before the
// clone is promoted, and the stage is discarded, the head mounted as it was.
func TestStageCommitOfABusyHead(t *testing.T) {
	ctx := context.Background()
	z := newFakeZfs(t)
	varDir := t.TempDir()
	former := z.datasets["tank/data"].id
	stage, ok, err := z.stager(varDir).stage(ctx, "run1")
	require.NoError(t, err)
	require.True(t, ok)
	z.busy = true
	require.Error(t, stage.Commit(ctx))
	require.NoError(t, stage.Discard(ctx))
	assert.Equal(t, []string{"tank/data"}, z.names())
	assert.Equal(t, former, z.datasets["tank/data"].id)
	assert.True(t, z.datasets["tank/data"].mounted)
	assert.Empty(t, journalStep(t, varDir))
}

// stageKillPoints are the steps a stager is killed at, and the generation
// the head is of once recovered.
var stageKillPoints = []struct {
	step string
	want string
}{
	{"snapshot", "old"},
	{"clone", "old"},
	{"mount", "old"},
	{"seal", "old"},
	{"umount-stage", "new"},
	{"umount-live", "new"},
	{"promote", "new"},
	{"rename-old", "new"},
	{"rename-stage", "new"},
	{"set-mountpoint", "new"},
	{"set-canmount", "new"},
	{"mount-live", "new"},
	{"switched", "new"},
	{"destroy-old", "new"},
	{"destroy-snapshot", "new"},
	{"set-refreservation", "new"},
	{"done", "new"},
}

// A stager killed at any step leaves datasets a recovery makes the head of:
// the former dataset when the commit was not decided, the clone from the
// decision on, mounted, with its properties, and nothing else.
func TestStageKilledThenRecovered(t *testing.T) {
	ctx := context.Background()
	for _, c := range stageKillPoints {
		t.Run(c.step, func(t *testing.T) {
			z := newFakeZfs(t)
			varDir := t.TempDir()
			former := z.datasets["tank/data"].id
			stager := z.stager(varDir)
			died := killAt(t, stager, c.step, func() {
				stage, ok, err := stager.stage(ctx, "run1")
				require.NoError(t, err)
				require.True(t, ok)
				z.datasets["tank/data.om-stage"].content = "new"
				require.NoError(t, stage.Commit(ctx))
			})
			require.True(t, died, "the stager goes through the step %s", c.step)

			// Another process recovers, as the start of the
			// resource: a stager of its own.
			require.NoError(t, z.stager(varDir).recover(ctx))
			assert.Equal(t, []string{"tank/data"}, z.names())
			head := z.datasets["tank/data"]
			assert.Equal(t, c.want, head.content)
			assert.Equal(t, c.want == "old", head.id == former)
			assert.Empty(t, head.origin)
			assert.Equal(t, []string{"daily"}, head.snaps)
			assert.Equal(t, "/srv/data", head.props["mountpoint"])
			assert.Equal(t, "536870912", head.props["refreservation"])
			assert.Equal(t, "zstd", head.props["compression"])
			if c.want == "new" {
				assert.True(t, head.mounted, "a commit finished mounts the head")
				assert.Equal(t, "on", head.props["canmount"])
			}
			assert.Empty(t, journalStep(t, varDir))
			assert.NotContains(t, head.props, propStage, "no journal left on the dataset")
			require.NoError(t, z.stager(varDir).recover(ctx), "nothing left to recover")
		})
	}
}

// A pool failing over while its dataset is staged or switched is recovered
// by the node importing it, which has no journal file of the stage: the
// journal the datasets carry tells it what to discard, or to finish. A stage
// killed before its clone was made left a snapshot only, which the next
// stage replaces.
func TestStageKilledThenRecoveredOnAnotherNode(t *testing.T) {
	ctx := context.Background()
	for _, c := range stageKillPoints {
		t.Run(c.step, func(t *testing.T) {
			z := newFakeZfs(t)
			former := z.datasets["tank/data"].id
			stager := z.stager(t.TempDir())
			died := killAt(t, stager, c.step, func() {
				stage, ok, err := stager.stage(ctx, "run1")
				require.NoError(t, err)
				require.True(t, ok)
				z.datasets["tank/data.om-stage"].content = "new"
				require.NoError(t, stage.Commit(ctx))
			})
			require.True(t, died, "the stager goes through the step %s", c.step)

			// The other node: a var directory of its own, empty.
			varDir := t.TempDir()
			require.NoError(t, z.stager(varDir).recover(ctx))
			assert.Equal(t, []string{"tank/data"}, z.names())
			head := z.datasets["tank/data"]
			assert.Equal(t, c.want, head.content)
			assert.Equal(t, c.want == "old", head.id == former)
			assert.Empty(t, head.origin)
			assert.Equal(t, "/srv/data", head.props["mountpoint"])
			assert.Equal(t, "536870912", head.props["refreservation"])
			assert.Equal(t, "zstd", head.props["compression"])
			if c.want == "new" {
				assert.True(t, head.mounted, "a commit finished mounts the head")
				assert.Equal(t, "on", head.props["canmount"])
				assert.Equal(t, []string{"daily"}, head.snaps)
			}
			assert.NotContains(t, head.props, propStage, "no journal left on the dataset")
			assert.Empty(t, journalStep(t, varDir))

			// The next stage goes through, whatever was left.
			stage, ok, err := z.stager(varDir).stage(ctx, "run2")
			require.NoError(t, err)
			require.True(t, ok)
			require.NoError(t, stage.Discard(ctx))
			assert.Equal(t, []string{"tank/data"}, z.names())
			assert.Equal(t, []string{"daily"}, z.datasets["tank/data"].snaps)
		})
	}
}

// The journal the datasets carry is the authority: a journal file saying a
// former step, as one written before the node lost the pool to another that
// went further, does not undo what the datasets say was decided.
func TestStageRecoveryTrustsTheDatasets(t *testing.T) {
	ctx := context.Background()
	z := newFakeZfs(t)
	varDir := t.TempDir()
	stager := z.stager(varDir)
	var staged resource.HeadStageJournal
	died := killAt(t, stager, "promote", func() {
		stage, ok, err := stager.stage(ctx, "run1")
		require.NoError(t, err)
		require.True(t, ok)
		j, err := resource.ReadHeadStageJournal(varDir)
		require.NoError(t, err)
		staged = *j
		z.datasets["tank/data.om-stage"].content = "new"
		require.NoError(t, stage.Commit(ctx))
	})
	require.True(t, died)
	// The file as it was before the commit was decided, of a process
	// gone.
	staged.PID = 1 << 30
	require.NoError(t, staged.Write(varDir))

	require.NoError(t, z.stager(varDir).recover(ctx))
	assert.Equal(t, []string{"tank/data"}, z.names())
	assert.Equal(t, "new", z.datasets["tank/data"].content, "the commit decided is finished, not discarded")
	assert.Empty(t, journalStep(t, varDir))
}

// A dataset with children, or the root of a pool, stages none, and is left
// untouched.
func TestStageRefused(t *testing.T) {
	ctx := context.Background()
	z := newFakeZfs(t)
	z.datasets["tank/data/child"] = &fakeDataset{id: z.id()}
	_, ok, err := z.stager(t.TempDir()).stage(ctx, "run1")
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Empty(t, z.cmds)

	z = newFakeZfs(t)
	stager := z.stager(t.TempDir())
	stager.dataset = "tank"
	_, ok, err = stager.stage(ctx, "run1")
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Empty(t, z.cmds)
}

// A snapshot taken of the dataset while its stage was written goes with the
// former generation.
func TestStageCommitDropsTheSnapshotsTakenSince(t *testing.T) {
	ctx := context.Background()
	z := newFakeZfs(t)
	stage, ok, err := z.stager(t.TempDir()).stage(ctx, "run1")
	require.NoError(t, err)
	require.True(t, ok)
	z.datasets["tank/data"].snaps = append(z.datasets["tank/data"].snaps, "during")
	require.NoError(t, stage.Commit(ctx))
	assert.Equal(t, []string{"tank/data"}, z.names())
	assert.Equal(t, []string{"daily"}, z.datasets["tank/data"].snaps)
}
