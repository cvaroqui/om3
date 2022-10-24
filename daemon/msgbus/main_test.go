package msgbus

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"opensvc.com/opensvc/core/event"
	"opensvc.com/opensvc/util/pubsub"
)

func TestDaemonPubSub(t *testing.T) {
	bus := pubsub.NewBus(t.Name())
	bus.Start(context.Background())
	defer bus.Stop()
	var (
		eventKinds    = []string{"hb_stale", "hb_beating"}
		expectedKinds = []string{"event_subscribe", "hb_stale", "hb_beating"}
		detectedKinds []string
	)
	defer UnSubEvent(
		bus,
		SubEvent(bus,
			"subscription_name_1",
			func(e event.Event) {
				t.Logf("detected event %s", e.Kind)
				detectedKinds = append(detectedKinds, e.Kind)
			}))
	time.Sleep(1 * time.Millisecond)
	for _, kind := range eventKinds {
		PubEvent(bus, event.Event{Kind: kind})
	}
	time.Sleep(1 * time.Millisecond)
	require.ElementsMatch(t, expectedKinds, detectedKinds)
}

func TestNamespacesAreDeclared(t *testing.T) {
	_ = NsAll
	_ = NsCfg
	_ = NsCfgFile
	_ = NsStatus
	_ = NsSmon
	_ = NsSetSmon
	_ = NsAgg
}