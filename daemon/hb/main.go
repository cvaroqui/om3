package hb

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/goccy/go-json"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/opensvc/om3/core/clusterhb"
	"github.com/opensvc/om3/core/hbcfg"
	"github.com/opensvc/om3/core/hbtype"
	"github.com/opensvc/om3/core/naming"
	"github.com/opensvc/om3/core/omcrypto"
	"github.com/opensvc/om3/daemon/daemonctx"
	"github.com/opensvc/om3/daemon/daemondata"
	"github.com/opensvc/om3/daemon/daemonenv"
	"github.com/opensvc/om3/daemon/hb/hbctrl"
	"github.com/opensvc/om3/daemon/msgbus"
	"github.com/opensvc/om3/util/funcopt"
	"github.com/opensvc/om3/util/hostname"
	"github.com/opensvc/om3/util/pubsub"
)

type (
	T struct {
		log zerolog.Logger
		txs map[string]hbtype.Transmitter
		rxs map[string]hbtype.Receiver

		ctrl  *hbctrl.C
		ctrlC chan<- any

		readMsgQueue chan *hbtype.Msg

		msgToTxRegister   chan registerTxQueue
		msgToTxUnregister chan string
		msgToTxCtx        context.Context

		ridSignature map[string]string

		sub *pubsub.Subscription

		// ctx is the main context for controller, and started hb drivers
		ctx context.Context

		// cancel is the cancel function for msgToTx, msgFromRx, janitor
		cancel context.CancelFunc
		wg     sync.WaitGroup
	}

	registerTxQueue struct {
		id string
		// msgToSendQueue is the queue on which a tx fetch messages to send
		msgToSendQueue chan []byte
	}
)

func New(opts ...funcopt.O) *T {
	t := &T{}
	t.log = log.Logger.With().Str("sub", "hb").Logger()
	if err := funcopt.Apply(t, opts...); err != nil {
		t.log.Error().Err(err).Msg("hb funcopt.Apply")
		return nil
	}
	t.txs = make(map[string]hbtype.Transmitter)
	t.rxs = make(map[string]hbtype.Receiver)
	t.readMsgQueue = make(chan *hbtype.Msg)
	t.ridSignature = make(map[string]string)
	return t
}

// Start startup the heartbeat components
//
// It starts:
// with ctx:
//   - the hb controller to maintain heartbeat status and peers
//     It is firstly started and lastly stopped
//   - hb drivers
//
// with cancelable context
// - the dispatcher of messages to send to hb tx components
// - the dispatcher of read messages from hb rx components to daemon data
// - the goroutine responsible for hb drivers lifecycle
func (t *T) Start(ctx context.Context) error {
	t.log.Info().Msg("starting hb")

	// we have to start controller routine first (it will be used by hb drivers)
	// It uses main context ctx: it is the last go routine to stop (after t.cancel() & t.wg.Wait())
	t.ctrl = hbctrl.New()
	t.ctrlC = t.ctrl.Start(ctx)

	// t.ctx will be used to start hb drivers
	t.ctx = ctx

	// create cancelable context to cancel other routines
	ctx, cancel := context.WithCancel(ctx)
	t.cancel = cancel
	err := t.msgToTx(ctx)
	if err != nil {
		return err
	}

	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		t.msgFromRx(ctx)
	}()

	t.janitor(ctx)
	t.log.Info().Msg("started hb")
	return nil
}

func (t *T) Stop() error {
	t.log.Info().Msg("stopping hb")
	defer t.log.Info().Msg("stopped hb")

	// this will cancel janitor, msgToTx, msgFromRx and hb drivers context
	t.cancel()

	hbToStop := make([]hbtype.IdStopper, 0)
	var failedIds []string
	for _, hb := range t.txs {
		hbToStop = append(hbToStop, hb)
	}
	for _, hb := range t.rxs {
		hbToStop = append(hbToStop, hb)
	}
	for _, hb := range hbToStop {
		if err := t.stopHb(hb); err != nil {
			t.log.Error().Err(err).Msgf("failure during stop %s", hb.Id())
			failedIds = append(failedIds, hb.Id())
		}
	}
	if len(failedIds) > 0 {
		return fmt.Errorf("failure while stopping heartbeat %s", strings.Join(failedIds, ", "))
	}

	t.wg.Wait()

	// We can now stop the controller
	if err := t.ctrl.Stop(); err != nil {
		t.log.Error().Err(err).Msgf("failure during stop hbctrl")
	}

	return nil
}

func (t *T) stopHb(hb hbtype.IdStopper) error {
	hbId := hb.Id()
	switch hb.(type) {
	case hbtype.Transmitter:
		select {
		case <-t.msgToTxCtx.Done():
			// don't hang up when context is done
		case t.msgToTxUnregister <- hbId:
		}
	}
	t.ctrlC <- hbctrl.CmdUnregister{Id: hbId}
	return hb.Stop()
}

func (t *T) startHb(hb hbcfg.Confer) error {
	var errs error
	if err := t.startHbRx(hb); err != nil {
		errs = errors.Join(errs, err)
	}
	if err := t.startHbTx(hb); err != nil {
		errs = errors.Join(errs, err)
	}
	return errs
}

func (t *T) startHbTx(hb hbcfg.Confer) error {
	tx := hb.Tx()
	if tx == nil {
		return fmt.Errorf("nil tx for %s", hb.Name())
	}
	t.ctrlC <- hbctrl.CmdRegister{Id: tx.Id(), Type: hb.Type()}
	localDataC := make(chan []byte)
	if err := tx.Start(t.ctrlC, localDataC); err != nil {
		t.log.Error().Err(err).Msgf("starting %s", tx.Id())
		t.ctrlC <- hbctrl.CmdSetState{Id: tx.Id(), State: "failed"}
		return err
	}
	select {
	case <-t.msgToTxCtx.Done():
		// don't hang up when context is done
	case t.msgToTxRegister <- registerTxQueue{id: tx.Id(), msgToSendQueue: localDataC}:
		t.txs[hb.Name()] = tx
	}
	return nil
}

func (t *T) startHbRx(hb hbcfg.Confer) error {
	rx := hb.Rx()
	if rx == nil {
		return fmt.Errorf("nil rx for %s", hb.Name())
	}
	t.ctrlC <- hbctrl.CmdRegister{Id: rx.Id(), Type: hb.Type()}
	if err := rx.Start(t.ctrlC, t.readMsgQueue); err != nil {
		t.ctrlC <- hbctrl.CmdSetState{Id: rx.Id(), State: "failed"}
		t.log.Error().Err(err).Msgf("starting %s", rx.Id())
		return err
	}
	t.rxs[hb.Name()] = rx
	return nil
}

func (t *T) stopHbRid(rid string) error {
	errCount := 0
	failures := make([]string, 0)
	if tx, ok := t.txs[rid]; ok {
		if err := t.stopHb(tx); err != nil {
			failures = append(failures, "tx")
			errCount++
		} else {
			delete(t.txs, rid)
		}
	}
	if rx, ok := t.rxs[rid]; ok {
		if err := t.stopHb(rx); err != nil {
			failures = append(failures, "rx")
			errCount++
		} else {
			delete(t.rxs, rid)
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("stop hb rid %s error for " + strings.Join(failures, ", "))
	}
	return nil
}

// rescanHb updates the running heartbeats from existing configuration
//
// To avoid hold resources, the updates are done in this order:
// 1- stop the running heartbeats that don't anymore exist in configuration
// 2- stop the running heartbeats where configuration has been changed
// 3- start the configuration changed stopped heartbeats
// 4- start the new configuration heartbeats
func (t *T) rescanHb(ctx context.Context) error {
	var errs error
	ridHb, err := t.getHbConfigured(ctx)
	if err != nil {
		return err
	}
	ridSignatureNew := make(map[string]string)
	for rid, hb := range ridHb {
		ridSignatureNew[rid] = hb.Signature()
	}

	for rid := range t.ridSignature {
		if _, ok := ridSignatureNew[rid]; ok {
			continue
		}
		t.log.Info().Msgf("heartbeat config deleted %s => stopping", rid)
		if err := t.stopHbRid(rid); err == nil {
			delete(t.ridSignature, rid)
		} else {
			errs = errors.Join(errs, err)
		}
	}
	// Stop first to release connexion holders
	stoppedRids := make(map[string]string)
	for rid, newSig := range ridSignatureNew {
		if sig, ok := t.ridSignature[rid]; ok {
			if sig != newSig {
				t.log.Info().Msgf("heartbeat config changed %s => stopping", rid)
				if err := t.stopHbRid(rid); err != nil {
					errs = errors.Join(errs, err)
					continue
				}
				stoppedRids[rid] = newSig
			}
		}
	}
	for rid, newSig := range stoppedRids {
		t.log.Info().Msgf("heartbeat config changed %s => starting (from stoppped)", rid)
		if err := t.startHb(ridHb[rid]); err != nil {
			errs = errors.Join(errs, err)
		}
		t.ridSignature[rid] = newSig
	}
	for rid, newSig := range ridSignatureNew {
		if _, ok := t.ridSignature[rid]; !ok {
			t.log.Info().Msgf("heartbeat config new %s => starting", rid)
			if err := t.startHb(ridHb[rid]); err != nil {
				errs = errors.Join(errs, err)
				continue
			}
		}
		t.ridSignature[rid] = newSig
	}
	return errs
}

// msgToTx starts the goroutine to multiplex data messages to hb tx drivers
//
// It ends when ctx is done
func (t *T) msgToTx(ctx context.Context) error {
	msgC := make(chan hbtype.Msg)
	databus := daemondata.FromContext(ctx)
	if err := databus.SetHBSendQ(msgC); err != nil {
		return fmt.Errorf("msgToTx can't set daemondata HBSendQ")
	}
	t.msgToTxRegister = make(chan registerTxQueue)
	t.msgToTxUnregister = make(chan string)
	t.msgToTxCtx = ctx
	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		defer t.log.Info().Msg("multiplexer message to hb tx drivers stopped")
		t.log.Info().Msg("multiplexer message to hb tx drivers started")
		defer func() {
			if err := databus.SetHBSendQ(nil); err != nil {
				t.log.Error().Err(err).Msg("msgToTx can't unset daemondata HBSendQ")
			}
		}()
		registeredTxMsgQueue := make(map[string]chan []byte)
		defer func() {
			tC := time.After(daemonenv.DrainChanDuration)
			for {
				select {
				case <-tC:
					return
				case <-msgC:
					t.log.Debug().Msgf("msgToTx drop msg (done context)")
				case <-t.msgToTxRegister:
				case <-t.msgToTxUnregister:
				}
			}
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case c := <-t.msgToTxRegister:
				t.log.Debug().Msgf("add %s to hb transmitters", c.id)
				registeredTxMsgQueue[c.id] = c.msgToSendQueue
			case txId := <-t.msgToTxUnregister:
				t.log.Debug().Msgf("remove %s from hb transmitters", txId)
				delete(registeredTxMsgQueue, txId)
			case msg := <-msgC:
				var rMsg *omcrypto.Message
				if b, err := json.Marshal(msg); err != nil {
					err = fmt.Errorf("marshal failure %s for msg %v", err, msg)
					continue
				} else {
					rMsg = omcrypto.NewMessage(b)
				}
				b, err := rMsg.Encrypt()
				if err != nil {
					continue
				}
				for _, txQueue := range registeredTxMsgQueue {
					select {
					case <-ctx.Done():
						// don't hang up when context is done
						return
					case txQueue <- b:
					}
				}
			}
		}
	}()
	return nil
}

// msgFromRx get hbrx decoded messages from readMsgQueue, and
// forward the decoded hb message to daemondata HBRecvMsgQ.
//
// When multiple hb rx are running, we can get multiple times the same hb message,
// but only one hb decoded message is forwarded to daemondata HBRecvMsgQ
//
// It ends when ctx is done
func (t *T) msgFromRx(ctx context.Context) {
	defer t.log.Info().Msg("message receiver from hb rx drivers stopped")
	t.log.Info().Msg("message receiver from hb rx drivers started")
	count := 0.0
	statTicker := time.NewTicker(60 * time.Second)
	defer statTicker.Stop()
	dataMsgRecvQ := daemonctx.HBRecvMsgQ(ctx)
	msgTimes := make(map[string]time.Time)
	msgTimeDuration := 10 * time.Minute
	defer func() {
		tC := time.After(daemonenv.DrainChanDuration)
		for {
			select {
			case <-tC:
				return
			case <-t.readMsgQueue:
				t.log.Debug().Msgf("msgFromRx drop msg (done context)")
			}
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-statTicker.C:
			t.log.Debug().Msgf("received message: %.2f/s, goroutines %d", count/10, runtime.NumGoroutine())
			count = 0
			for peer, updated := range msgTimes {
				if now.Sub(updated) > msgTimeDuration {
					delete(msgTimes, peer)
				}
			}
		case msg := <-t.readMsgQueue:
			peer := msg.Nodename
			if msgTimes[peer].Equal(msg.UpdatedAt) {
				t.log.Debug().Msgf("drop already processed msg %s from %s gens: %v", msg.Kind, msg.Nodename, msg.Gen)
				continue
			}
			select {
			case <-ctx.Done():
				// don't hang up when context is done
				return
			case dataMsgRecvQ <- msg:
				t.log.Debug().Msgf("processed msg type %s from %s gens: %v", msg.Kind, msg.Nodename, msg.Gen)
				msgTimes[peer] = msg.UpdatedAt
				count++
			}
		}
	}
}

func (t *T) startSubscriptions(ctx context.Context) {
	bus := pubsub.BusFromContext(ctx)
	t.sub = bus.Sub("hb")
	t.sub.AddFilter(&msgbus.InstanceConfigUpdated{}, pubsub.Label{"path", naming.Cluster.String()})
	t.sub.AddFilter(&msgbus.DaemonCtl{})
	t.sub.Start()
}

// janitor starts the goroutine responsible for hb drivers lifecycle.
//
// It ends when ctx is done.
//
// It watches cluster InstanceConfigUpdated and DaemonCtl to (re)start hb drivers
// When a hb driver is started, it will use the main context t.ctx.
func (t *T) janitor(ctx context.Context) {
	t.startSubscriptions(ctx)
	started := make(chan bool)

	if err := t.rescanHb(ctx); err != nil {
		t.log.Error().Err(err).Msg("initial rescan on janitor hb start")
	}

	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		started <- true
		defer func() {
			if err := t.sub.Stop(); err != nil {
				t.log.Error().Err(err).Msg("subscription stop")
			}
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case i := <-t.sub.C:
				switch msg := i.(type) {
				case *msgbus.InstanceConfigUpdated:
					if msg.Node != hostname.Hostname() {
						continue
					}
					t.log.Info().Msg("rescan heartbeat configurations (local cluster config changed)")
					_ = t.rescanHb(t.ctx)
					t.log.Info().Msg("rescan heartbeat configurations done")
				case *msgbus.DaemonCtl:
					hbId := msg.Component
					action := msg.Action
					if !strings.HasPrefix(hbId, "hb#") {
						continue
					}
					switch msg.Action {
					case "stop":
						t.daemonCtlStop(hbId, action)
					case "start":
						t.daemonCtlStart(t.ctx, hbId, action)
					}
				}
			}
		}
	}()
	<-started
}

func (t *T) daemonCtlStart(ctx context.Context, hbId string, action string) {
	var rid string
	if strings.HasSuffix(hbId, ".rx") {
		rid = strings.TrimSuffix(hbId, ".rx")
	} else if strings.HasSuffix(hbId, ".tx") {
		rid = strings.TrimSuffix(hbId, ".tx")
	} else {
		t.log.Info().Msgf("daemonctl %s found no component for %s", action, hbId)
		return
	}
	h, err := t.getHbConfiguredComponent(ctx, rid)
	if err != nil {
		t.log.Info().Msgf("daemonctl %s found no component for %s (rid: %s)", action, hbId, rid)
		return
	}
	if strings.HasSuffix(hbId, ".rx") {
		if err := t.startHbRx(h); err != nil {
			t.log.Error().Err(err).Msgf("daemonctl %s %s failure", action, hbId)
			return
		}
	} else {
		if err := t.startHbTx(h); err != nil {
			t.log.Error().Err(err).Msgf("daemonctl %s %s failure", action, hbId)
			return
		}
	}
}

func (t *T) daemonCtlStop(hbId string, action string) {
	var hbI interface{}
	var found bool
	if strings.HasSuffix(hbId, ".rx") {
		rid := strings.TrimSuffix(hbId, ".rx")
		if hbI, found = t.rxs[rid]; !found {
			t.log.Info().Msgf("daemonctl %s %s found no %s.rx component", action, hbId, rid)
			return
		}
	} else if strings.HasSuffix(hbId, ".tx") {
		rid := strings.TrimSuffix(hbId, ".tx")
		if hbI, found = t.txs[rid]; !found {
			t.log.Info().Msgf("daemonctl %s %s found no %s.tx component", action, hbId, rid)
			return
		}
	} else {
		t.log.Info().Msgf("daemonctl %s %s found no component", action, hbId)
		return
	}
	t.log.Info().Msgf("ask to %s %s", action, hbId)
	switch hbI.(type) {
	case hbtype.Transmitter:
		select {
		case <-t.msgToTxCtx.Done():
		// don't hang up when context is done
		case t.msgToTxUnregister <- hbId:
		}
	}
	if err := hbI.(hbtype.IdStopper).Stop(); err != nil {
		t.log.Error().Err(err).Msgf("daemonctl %s %s failure", action, hbId)
	} else {
		t.ctrlC <- hbctrl.CmdSetState{Id: hbI.(hbtype.IdStopper).Id(), State: "stopped"}
	}
}

func (t *T) getHbConfigured(ctx context.Context) (ridHb map[string]hbcfg.Confer, err error) {
	var node *clusterhb.T
	ridHb = make(map[string]hbcfg.Confer)
	node, err = clusterhb.New()
	if err != nil {
		return ridHb, err
	}
	for _, h := range node.Hbs() {
		h.Configure(ctx)
		ridHb[h.Name()] = h
	}
	return ridHb, nil
}

func (t *T) getHbConfiguredComponent(ctx context.Context, rid string) (c hbcfg.Confer, err error) {
	var node *clusterhb.T
	node, err = clusterhb.New()
	if err != nil {
		t.log.Error().Err(err).Msgf("clusterhb.NewPath")
		return
	}
	for _, h := range node.Hbs() {
		h.Configure(ctx)
		if h.Name() == rid {
			c = h
			return
		}
	}
	err = fmt.Errorf("not found rid")
	return
}
