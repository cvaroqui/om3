package node

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type (
	// Monitor describes the in-daemon states of a node
	Monitor struct {
		GlobalExpect MonitorGlobalExpect `json:"global_expect"`
		LocalExpect  MonitorLocalExpect  `json:"local_expect"`
		State        MonitorState        `json:"state"`

		GlobalExpectUpdatedAt time.Time `json:"global_expect_updated_at"`
		LocalExpectUpdatedAt  time.Time `json:"local_expect_updated_at"`
		StateUpdatedAt        time.Time `json:"state_updated_at"`
		UpdatedAt             time.Time `json:"updated_at"`

		OrchestrationID     uuid.UUID `json:"orchestration_id"`
		OrchestrationIsDone bool      `json:"orchestration_is_done"`
		SessionID           uuid.UUID `json:"session_id"`

		IsPreserved bool `json:"preserved"`
	}

	// MonitorUpdate is embedded in the SetNodeMonitor message to
	// change some Monitor values. A nil value does not change the
	// current value.
	MonitorUpdate struct {
		State        *MonitorState        `json:"state"`
		LocalExpect  *MonitorLocalExpect  `json:"local_expect"`
		GlobalExpect *MonitorGlobalExpect `json:"global_expect"`

		// CandidateOrchestrationID is a candidate orchestration id for a new imon orchestration.
		CandidateOrchestrationID uuid.UUID `json:"orchestration_id"`
	}
)

var (
	ErrInvalidGlobalExpect = errors.New("invalid node monitor global expect")
	ErrInvalidLocalExpect  = errors.New("invalid node monitor local expect")
	ErrInvalidState        = errors.New("invalid node monitor state")
	ErrSameGlobalExpect    = errors.New("node monitor global expect is already set to the same value")
	ErrSameLocalExpect     = errors.New("node monitor local expect is already set to the same value")
	ErrSameState           = errors.New("node monitor state is already set to the same value")
)

func (t MonitorState) IsDoing() bool {
	return strings.HasSuffix(t.String(), "ing")
}

func (t MonitorState) IsRankable() bool {
	_, ok := MonitorStateUnrankable[t]
	return !ok
}

func (n *Monitor) DeepCopy() *Monitor {
	var d Monitor
	d = *n
	return &d
}

func (t MonitorState) String() string {
	return MonitorStateStrings[t]
}

func (t MonitorState) MarshalText() ([]byte, error) {
	if s, ok := MonitorStateStrings[t]; !ok {
		return nil, fmt.Errorf("unexpected node.MonitorState value: %d", t)
	} else {
		return []byte(s), nil
	}
}

func (t *MonitorState) UnmarshalText(b []byte) error {
	s := string(b)
	if v, ok := MonitorStateValues[s]; !ok {
		return fmt.Errorf("unexpected node.MonitorState value: %s", b)
	} else {
		*t = v
		return nil
	}
}

func (t MonitorLocalExpect) String() string {
	return MonitorLocalExpectStrings[t]
}

func (t MonitorLocalExpect) MarshalText() ([]byte, error) {
	if s, ok := MonitorLocalExpectStrings[t]; !ok {
		return nil, fmt.Errorf("unexpected node.MonitorLocalExpect value: %d", t)
	} else {
		return []byte(s), nil
	}
}

func (t *MonitorLocalExpect) UnmarshalText(b []byte) error {
	s := string(b)
	if v, ok := MonitorLocalExpectValues[s]; !ok {
		return fmt.Errorf("unexpected node.MonitorLocalExpect value: %s", b)
	} else {
		*t = v
		return nil
	}
}

func (t MonitorGlobalExpect) String() string {
	return MonitorGlobalExpectStrings[t]
}

func (t MonitorGlobalExpect) MarshalText() ([]byte, error) {
	if s, ok := MonitorGlobalExpectStrings[t]; !ok {
		return nil, fmt.Errorf("unexpected MonitorGlobalExpect value: %d", t)
	} else {
		return []byte(s), nil
	}
}

func (t *MonitorGlobalExpect) UnmarshalText(b []byte) error {
	s := string(b)
	if v, ok := MonitorGlobalExpectValues[s]; !ok {
		return fmt.Errorf("unexpected node.MonitorGlobalExpect value: %s", b)
	} else {
		*t = v
		return nil
	}
}

func (t MonitorUpdate) String() string {
	s := fmt.Sprintf("CandidateOrchestrationID=%s", t.CandidateOrchestrationID)
	if t.State != nil {
		s += fmt.Sprintf(" State=%s", t.State)
	}
	if t.LocalExpect != nil {
		s += fmt.Sprintf(" LocalExpect=%s", t.LocalExpect)
	}
	if t.GlobalExpect != nil {
		s += fmt.Sprintf(" GlobalExpect=%s", t.GlobalExpect)
	}
	return fmt.Sprintf("node.MonitorUpdate{%s}", s)
}
