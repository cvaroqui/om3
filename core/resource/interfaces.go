package resource

import (
	"context"
	"io"
	"time"

	"github.com/opensvc/om3/v3/core/actionresdeps"
	"github.com/opensvc/om3/v3/core/naming"
	"github.com/opensvc/om3/v3/core/schedule"
	"github.com/opensvc/om3/v3/util/device"
)

type (
	//
	// Aborter implements the Abort func, which can return true to
	// block a start action before any resource has entered Start().
	//
	Aborter interface {
		Abort(ctx context.Context) bool
	}

	//
	// ActionResourceDepser implements the ActionResourceDeps func, which
	// return a list of {action, rid depending on, rid depended on} tuples.
	//
	ActionResourceDepser interface {
		ActionResourceDeps() []actionresdeps.Dep
	}

	// Configurer is an optional interface resource drivers can
	// implement if they want to configure the resource after the
	// manifest data has been loaded. For example, tuning the logger.
	Configurer interface {
		Configure() error
	}

	SetSSHKeyFiler interface {
		SetSSHKeyFile()
	}

	// Mover implements a Move function that is exposed by resource
	// drivers that support live migration (eg container.kvm).
	Mover interface {
		Move(ctx context.Context, to string) error
	}

	// PreMover implements a PreMove function that is called by a moveable
	// driver (eg container.kvm) before starting a move.
	PreMover interface {
		PreMove(ctx context.Context, to string) error
	}

	// PreMoveRollbacker implements a PreMove rollback function that is called
	// by a moveable driver (eg container.kvm) when move has failed.
	PreMoveRollbacker interface {
		PreMoveRollback(ctx context.Context, to string) error
	}

	// PostMover implements a PostMove function that is called by a moveable
	// driver (eg container.kvm) after the move is done.
	PostMover interface {
		PostMove(ctx context.Context, to string) error
	}

	// MoveStorageCopier is implemented by a resource holding disks of a
	// moveable driver (eg container.kvm) that the destination of a move
	// can not reach, as a zfs dataset is imported on one node at a time.
	// The move then mirrors the content of those disks to the destination
	// while the container runs, onto the copy PreMove made there.
	//
	// The disks of the other resources are shared, as on a drbd in dual
	// primary, and copying them would write each block onto itself.
	MoveStorageCopier interface {
		MoveCopiesStorage() bool
	}

	// FinalSyncer is a sync resource able to send its data from an instance
	// stopped, as a switch asks of its source before the destination starts:
	// the replication of a dataset, which a snapshot freezes whether it is
	// mounted or not. A sync reading files through a mount point is not one,
	// the stop having unmounted it.
	FinalSyncer interface {
		// FinalSyncsTo says whether the resource replicates to the node.
		FinalSyncsTo(nodename string) bool
	}

	// PeerSyncer is a sync resource replicating files to peer nodes, read
	// through a mount point: it sends from an instance stopped only when the
	// resource mounting its source is standby, mounted still.
	PeerSyncer interface {
		// SyncsTo says whether the resource replicates to the node.
		SyncsTo(nodename string) bool
		// SyncSource returns the path the resource reads.
		SyncSource() string
	}

	//
	// Runner implements the Run func, which runs a one-shot process
	// Implemented by the resource. The object "run" action causes
	// selected Runners to call Run().
	//
	Runner interface {
		Run(ctx context.Context) error
	}

	//
	// Runninger implements the Running func, which the core calls
	// when evaluating an object instance status to build the "running"
	// list: [{"pid": 123, "rid": "task#1", "session_id": "abcd..."}]
	//
	Runninger interface {
		Running() (RunningInfoList, error)
	}

	//
	// Scheduler implements the Schedules func, which returns the list of
	// schedulable job definition on behalf of the resource.
	//
	Scheduler interface {
		Schedules() schedule.Table
	}

	//
	// StatusInfoer implements the StatusInfo func, which returns a
	// resource specific key-val mapping pushed to the collector on
	// "pushinfo" action.
	//
	StatusInfoer interface {
		StatusInfo(context.Context) map[string]interface{}
	}

	// NetNSPather exposes a NetNSPath method a resource can call to
	// get the string identifying the network namespace for libs like
	// netlink.
	// For example, the container.docker driver's NetNSPath() would return
	// the SandboxKey
	NetNSPather interface {
		NetNSPath(context.Context) (string, error)
	}

	// PIDer exposes a PID method a resource can call to
	// get the head pid of the head process started by the resource.
	// Typically a container resource PID() returns the pid of the
	// first process of the container.
	// PID() must return 0 when no process is running.
	PIDer interface {
		PID(context.Context) int
	}

	// GetHostnamer exposes a GetHostname method a resource can call
	// to get the hostname used by ip resources to obtain a
	// hostname-based DNS record
	GetHostnamer interface {
		GetHostname() string
	}

	shutdowner interface {
		Shutdown(context.Context) error
	}
	starter interface {
		Start(context.Context) error
	}
	startstandbyer interface {
		StartStandby(context.Context) error
	}
	stopstandbyer interface {
		StopStandby(context.Context) error
	}
	stopper interface {
		Stop(context.Context) error
	}
	booter interface {
		Boot(ctx context.Context) error
	}
	resyncer interface {
		Resync(context.Context) error
	}
	splitter interface {
		Split(context.Context) error
	}
	fuller interface {
		Full(context.Context) error
	}
	updater interface {
		Update(context.Context) error
	}
	restorer interface {
		Restore(context.Context) error
	}

	// Quiescer is a resource whose writes a run of a sync resource
	// suspends, for the data it saves to be consistent: quiesced before
	// the run, in the order of a stop, resumed after it, in the order of
	// a start. Quiesce returns how to resume them, nil when the resource
	// quiesces nothing.
	Quiescer interface {
		Quiesce(context.Context) (func(context.Context) error, error)
	}

	// SyncPreparer is a sync resource taking what its transfer copies
	// from, as a snapshot of its source, before the first transfer of a
	// sync update or full action: the resources of the action take it at
	// one moment, under one quiesce, rather than each just before its own
	// transfer, so their copies are of the same point in time.
	SyncPreparer interface {
		// WillPrepareSync says the action would have the resource take
		// something before the transfers: false when it takes nothing,
		// or would transfer nothing.
		WillPrepareSync(ctx context.Context) bool

		// PrepareSync takes it, and returns how to release it once the
		// action is done. The update or full of the resource in the
		// action copies from it.
		PrepareSync(ctx context.Context) (release func() error, err error)
	}

	// SyncSnapshotCopier is a SyncPreparer whose transfer copies the
	// snapshot it takes to the peers, as sync.rsync with snap, sync.zfs
	// and sync.btrfs: its copy is meant to be of the moment of the other
	// copies of the object, so one scheduled apart from them, or left
	// out of a run, is warned of. A resource whose snapshot is its whole
	// job, kept on a schedule of its own, as sync.zfssnap, is not one.
	SyncSnapshotCopier interface {
		// CopiesSyncSnapshot says the configuration has the resource
		// take its snapshot with the ones of the other sync resources
		// of an action, whatever the state of the node: false for a
		// resource taking none, or delaying it to its own transfer.
		CopiesSyncSnapshot() bool
	}

	// HeadSnapshotter is a resource whose head its storage takes
	// point-in-time snapshots of: a run of a sync resource copies the
	// snapshot rather than the head, so the writes it quiesced resume
	// once the snapshot is taken. SnapshotHead returns the directory
	// holding the snapshot and how to release it, and ok false when the
	// storage of this head takes none.
	HeadSnapshotter interface {
		SnapshotHead(ctx context.Context, name string) (dir string, release func() error, ok bool, err error)
	}

	// HeadStager is a resource whose head can be written as a generation
	// apart from the live one, and switched to once written whole: a
	// restore writing into the stage leaves the live head as its last
	// complete restore left it, whatever interrupts it.
	HeadStager interface {
		// StageHead opens a stage named name, as the run restored
		// into it, and returns ok false when the storage of this head
		// stages none: its restore is then written in place.
		StageHead(ctx context.Context, name string) (stage HeadStage, ok bool, err error)

		// RecoverHeadStage finishes the commit, or discards the stage,
		// a killed action left, as the journal of the stage says. The
		// start of the resource and StageHead call it first. It returns
		// ErrHeadStageBusy when the process staging is still running.
		RecoverHeadStage(ctx context.Context) error

		// ResumeHeadStage returns the stage named name a former
		// process opened and left held, for this one to commit or
		// discard it, and ErrHeadStageDiscarded when a recovery
		// discarded it since.
		ResumeHeadStage(ctx context.Context, name string) (HeadStage, error)
	}

	// HeadStage is a generation of a head being written, apart from the
	// live one.
	HeadStage struct {
		// Dir is where the writer writes.
		Dir string

		// Seeded says Dir already holds the committed generation, as a
		// copy-on-write clone of it does: a writer may write a delta,
		// and what it leaves is kept.
		Seeded bool

		// LinkDest is the committed tree, for a writer able to hardlink
		// the files unchanged, empty when Dir is not on its filesystem.
		LinkDest string

		// Seal journals the decision to commit, which a recovery then
		// finishes rather than discards: a restore of several heads
		// seals every stage before committing the first, so an action
		// killed between two commits leaves no head of the former
		// generation beside one of the new.
		Seal func(context.Context) error

		// Commit switches the head to the stage and drops the former
		// generation. It is journaled and idempotent: RecoverHeadStage
		// finishes one a kill interrupted.
		Commit func(context.Context) error

		// Discard drops the stage, the live head left as it is. It
		// fails once the commit switched the head.
		Discard func(context.Context) error

		// Hold leaves the stage open past the process that opened it,
		// for the session it is named after to resume it: a transfer
		// from another node is written into it meanwhile, under no
		// lock, and a start of the head discards it.
		Hold func(context.Context) error
	}

	// ConfigRestorer is a sync resource holding the configuration of the
	// object at the points in time it can restore.
	ConfigRestorer interface {
		// RestoredConfig returns the configuration of the snapshot the
		// restore options of the context name, as the object restores
		// it, and the keywords it changes, none when it is the
		// configuration of the object already.
		RestoredConfig(context.Context) ([]byte, []string, error)
	}

	// Follower is a sync resource of a replica, following the runs the
	// object it is a replica of writes.
	//
	// A follow is two steps, the configuration then the heads, because
	// the first changes what the second acts on: the daemon replaces the
	// configuration, taking the lock of the object, and the heads are
	// restored by the resources of the configuration replaced, under that
	// lock.
	Follower interface {
		// FollowConfig applies the configuration of the latest run of
		// the source when the object does not hold it, and says
		// whether it changed the configuration. A configuration
		// refused is an error, said to the source, and the heads are
		// not followed.
		FollowConfig(context.Context) (bool, error)

		// FollowPending says the latest run of the source is not the
		// one the heads hold: Follow has heads to restore, which need
		// the instance readied for it.
		FollowPending(context.Context) (bool, error)

		// Follow restores the heads from the latest run of the
		// source, when they do not hold it.
		Follow(context.Context) error
	}

	// Promoter is a sync resource of a replica following the runs of its
	// source, which a promotion of the replica asks what it holds, and has
	// record its cluster as the one writing the runs from then on.
	Promoter interface {
		// PromotionState says what the resource holds of its source,
		// and false for a resource following none.
		PromotionState(context.Context) (PromotionState, bool, error)

		// RecordPrimary records the cluster of the node as the primary
		// of the object in the store, taking over from the source
		// followed, the heads holding the run fromRunID, and returns
		// what it recorded.
		RecordPrimary(ctx context.Context, fromRunID string) (PrimaryRecord, error)
	}

	// Fencer is a sync resource writing the snapshots of the object into
	// a store recording which cluster runs it, of the clusters of two
	// sites taking turns.
	Fencer interface {
		// FencedBy says why the object does not run on this cluster,
		// naming the cluster the store records as its primary and
		// the store, and nothing when the store records none, or this
		// one.
		FencedBy(context.Context) (string, error)
	}

	// PromotionState is what a resource following a source says to the
	// promotion of its replica.
	PromotionState struct {
		// Store is the store followed, and SourceClusterID and
		// SourceObjectID the object followed in it.
		Store           string
		SourceClusterID string
		SourceObjectID  string

		// HeldRunID is the run of the source the heads hold whole,
		// empty when they hold none: it was begun at
		// HeldRunCreatedAt, on the clock of the source, and first
		// listed by this node at HeldRunSeenAt, on its own.
		HeldRunID        string
		HeldRunCreatedAt time.Time
		HeldRunSeenAt    time.Time

		// Restore is the restore writing the heads now, nil when
		// none does.
		Restore *RestoreInFlight

		// PromoteWait is how long a promotion waits for a restore
		// that ends within it, as the promote_wait keyword says.
		PromoteWait time.Duration

		// Primary is the primary the store records for the source,
		// nil when it records none: the source is then its own.
		Primary *PrimaryRecord
	}

	// RestoreInFlight is a restore writing the heads of the object, as
	// its progress file tells, every time on the clock of this node.
	RestoreInFlight struct {
		RunID      string
		SessionID  string
		PID        int
		StartedAt  time.Time
		UpdatedAt  time.Time
		BytesTotal int64
		BytesDone  int64
	}

	// PrimaryRecord is the primary of an object, as a store records it.
	PrimaryRecord struct {
		ClusterID       string
		ClusterName     string
		ObjectID        string
		Epoch           uint64
		PromotedFromRun string
		PromotedAt      time.Time
	}

	// SnapshotLister is a sync resource holding restorable points in
	// time.
	SnapshotLister interface {
		Snapshots(context.Context) ([]Snapshot, error)
	}

	// Snapshot is a point in time a sync resource can restore.
	Snapshot struct {
		// ID is om's snapshot id: the session id of the action
		// shared by every resource of one sync update, which
		// --snapshot takes.
		ID string `json:"id"`

		// CreatedAt is when the snapshot was taken.
		CreatedAt time.Time `json:"created_at"`

		// Cluster is the name of the cluster of the node that took it:
		// several clusters may write one store.
		Cluster string `json:"cluster"`

		// Node is the node that took it.
		Node string `json:"node"`

		// Protected says the retention keeps it, and Note is the text
		// it was marked with.
		Protected bool   `json:"protected,omitempty"`
		Note      string `json:"note,omitempty"`

		// Members are what it holds: the heads, by resource id, and
		// the configuration of the object.
		Members []string `json:"members"`

		// Location is where it is: the store holding it.
		Location string `json:"location"`
	}
	datastoreLister interface {
		DatastoreList(context.Context) naming.Paths
	}
	volumeLister interface {
		VolumeList(context.Context) naming.Paths
	}

	// replicaLister is implemented by a resource other clusters restore
	// the object from: the replicas its last status evaluation judged.
	replicaLister interface {
		Replicas() []ReplicaStatus
	}
	toSyncer interface {
		ToSync(context.Context) []string
	}

	// syncScoper is implemented by a toSyncer whose files are the ones of
	// a thing that may differ from node to node, as a container whose
	// name is scoped per node: the files of vm1 are not the ones of the
	// vm2 the same resource runs on another node. The scope is the
	// evaluated name of the thing, so the nodes evaluating the same one
	// share the files, whatever the keyword scoping says.
	syncScoper interface {
		SyncScope() string
	}
	ingester interface {
		Ingest(context.Context) error
	}
	SubDeviceser interface {
		SubDevices(context.Context) device.L
	}

	// Sizer is implemented by a resource driver that knows how much space it
	// holds. It is what a relative resize resolves against, and what decides
	// how much a resize has to grow it.
	//
	// It is not called Size because a driver whose size is configurable holds
	// that keyword in a Size field, and the two are not the same thing: one
	// is what was asked for, this is what is there.
	Sizer interface {
		CurrentSize(ctx context.Context) (int64, error)
	}

	// ResizeTargeter is implemented by a resource that holds no size of its
	// own, but stands for a resource of another object that does: a volume
	// resource stands for the head of the volume it points at.
	//
	// A resize chain reaching such a resource continues in the named object,
	// from that object's head, and the resource itself drops out of the
	// chain: there is nothing in it to change.
	ResizeTargeter interface {
		ResizeTarget(ctx context.Context) (naming.Path, error)
	}

	// ResizeRestsOn is implemented by a resource that rests on another
	// resource of the same object that no device leads to. A logical volume
	// rests on its volume group, but a volume group exposes logical volumes
	// rather than itself, so nothing in the device topology connects the two.
	//
	// It answers the name the resource below answers to, like "vg/data", or
	// "" when there is nothing to name.
	ResizeRestsOn interface {
		ResizeRestsOn(ctx context.Context) string
	}

	// ResizeProvides is implemented by a resource others rest on by name
	// rather than through a device. A volume group answers "vg/<its name>".
	ResizeProvides interface {
		ResizeProvides(ctx context.Context) string
	}

	// ResizeIsReplicated is implemented by a resource whose size is shared
	// with peer nodes, so it can only be resized once every node has grown
	// what is under it: a drbd resource offers what its smallest replica
	// holds.
	//
	// A resize chain is walked in two phases because of it. Every node first
	// grows the links below the replicated one, and only then does the node
	// holding the object up resize the replicated link and what rests on it.
	ResizeIsReplicated interface {
		ResizeIsReplicated() bool
	}

	// ResizeSpansBelow is implemented by a resource grown onto what is under
	// it rather than to a size of its own. A filesystem is: it is told to
	// take up the device it sits on, and the size it reports is that device.
	//
	// A volume group and a drbd resource are grown the same way, keeping a
	// cut of the device for their own metadata. What they report is the
	// device less that cut, which their own plan computes: a link that has
	// taken what is under it asks of it what it holds.
	//
	// Such a resource cannot be told by its size whether it still has to
	// grow. A chain grows from the bottom up, so the device under it already
	// holds the new size by the time it is asked, and comparing the two says
	// there is nothing to do when the filesystem has not been grown at all.
	// So it is grown whenever something below it was.
	ResizeSpansBelow interface {
		ResizeSpansBelow() bool
	}

	// PoolCharger is implemented by a resource taking its storage from
	// something a pool owns: a logical volume from a volume group, a file
	// from a directory.
	//
	// It says what it takes, and what it takes it from, named as a pool names
	// what it is the head of. What owns that head, and whether anything does,
	// is not the resource's to know.
	//
	// The size answered is the one the configuration asks for rather than the
	// one the resource holds: what a volume costs a pool is weighed before
	// anything is provisioned, and a resource that holds nothing yet still
	// costs what it was asked to hold. A resource asking for no size, or for
	// a share of what it rests on, answers zero: what it takes is not a
	// number anything can be rationed by.
	PoolCharger interface {
		PoolCharge() (head string, size int64)
	}

	// Freer is implemented by a resource that can say how much of what it
	// holds nothing has taken yet, which is what "{<rid>.free}" answers.
	//
	// It is the other half of Sizer: a volume group says how big it is and
	// how much of it is unused, and a logical volume carved from it is sized
	// from the second.
	Freer interface {
		CurrentFree(ctx context.Context) (int64, error)
	}

	// IDMapper is implemented by a resource whose processes run with ids
	// of their own, mapped to other ids of the host, as the processes of a
	// rootless container are mapped to the subordinate ids of its user.
	//
	// It answers {<rid>.uid.<id>} and {<rid>.gid.<id>}: the host id a file
	// has to be owned by for the id <id> of the resource to own it.
	IDMapper interface {
		HostUID(id uint32) (uint32, error)
		HostGID(id uint32) (uint32, error)
	}

	// PGPather is implemented by a resource whose processes are placed
	// elsewhere than its process group id says, as a rootless container
	// is placed under the cgroup subtree systemd delegates to its user.
	//
	// PGPath answers the cgroup its processes run in, relative to the
	// cgroup root, or an empty string when it is not known.
	PGPather interface {
		PGPath() string
	}

	// SizeInfoKeyer is implemented by a Sizer whose size is not its own, to
	// name the key it is reported under in the resource info. A fs.directory
	// reports the size of the filesystem holding it, and calling that "size"
	// next to "driver fs.directory" reads as the size of the directory.
	SizeInfoKeyer interface {
		SizeInfoKey() string
	}

	// Resizer is implemented by a resource driver that can change its size.
	//
	// ResizePlan is asked first, of every link of the chain, and changes
	// nothing. It answers the size this resource needs from the resources
	// below it, which is not always the size it was asked for: a raid6 md
	// holding n devices needs to(n-2) from each of them. It errors to refuse,
	// naming what it cannot do, so that a chain is refused whole rather than
	// left half resized - an xfs filesystem cannot grow beyond its device,
	// and finding
	// that out after the device under it has shrunk is data loss.
	//
	// Resize is asked second, of the same links, once every one of them has
	// agreed.
	Resizer interface {
		ResizePlan(ctx context.Context, to int64) (needBelow int64, err error)
		Resize(ctx context.Context, to int64) error
	}

	// ResizeHandsOuter is implemented by a Resizer that can say what it hands
	// out to the link above it when the link below it holds a given size. It
	// is the inverse of ResizePlan: a volume group given a physical volume of
	// below hands out below less its metadata, rounded down to whole extents,
	// where ResizePlan adds the metadata and rounds up.
	//
	// It is what a resize asked of a volume works with. The size of a volume
	// is the size of the bottom of its chain, which is what its pool and the
	// claims of its namespace count, so the size its head has to reach is
	// worked out from it, up the chain, one link at a time. The links are
	// then asked for that head size from the top down, as a resize of the
	// head is, and the bottom ends up asked for about the size it was given.
	//
	// ResizeHandsOut measures what the answer rests on, an extent size or a
	// header offset, once, and answers the function working it out for a
	// size below: a resize refusing a size that grows nothing searches for
	// the smallest one that does, and asks it of many sizes.
	//
	// The function answers no more than the link hands out: ResizePlan asked
	// for the answer asks no more than below of the link below, give or take
	// the rounding to the unit of that link, and it never answers less for
	// more below. A link not implementing it, or with nothing below it, hands
	// out what is below it.
	ResizeHandsOuter interface {
		ResizeHandsOut(ctx context.Context) (func(below int64) int64, error)
	}

	// ResizeUniter is implemented by a Resizer that grows by whole units, a
	// logical volume by extents, a zvol by blocks. A size that grows nothing
	// is refused with the smallest size that does, which is a whole number of
	// them. A resource not implementing it grows by sectors.
	ResizeUniter interface {
		ResizeUnit(ctx context.Context) (int64, error)
	}

	Commander interface {
		CombinedOutput() ([]byte, error)
		Run() error
		Start() error
		StderrPipe() (io.ReadCloser, error)
		Wait() error
	}
	Encaper interface {
		GetHostname() string
		GetOsvcRootPath() string
		EncapCp(context.Context, string, string) error
		EncapCmd(ctx context.Context, args []string, envs []string, stdin io.Reader) (Commander, error)
	}

	// ProjectQuotaUser is a resource holding a directory bounded by a
	// project quota, which says what the directory holds and may hold.
	ProjectQuotaUser interface {
		ProjectQuotaUsage(ctx context.Context) (used, hard int64, ok bool, err error)
	}

	// EncapAPIer is the Encaper whose guest daemon may be reached directly.
	EncapAPIer interface {
		// EncapAPIURL returns the url of the guest daemon, empty when the
		// container offers none. A url does not say the daemon answers.
		EncapAPIURL(ctx context.Context) (string, error)

		// EncapAPIBearer returns the token proving the hypervisor to the
		// guest daemon.
		EncapAPIBearer(ctx context.Context) (string, error)
	}
)

// HandsOutWhatIsBelow is what a ResizeHandsOuter hands out when it hands out
// what is below it, a filesystem counting as its device.
func HandsOutWhatIsBelow(below int64) int64 {
	return below
}
