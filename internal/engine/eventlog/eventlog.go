package eventlog

import "slices"

// Kind is the type of a simulation event.
type Kind int

const (
	Cordoned Kind = iota
	Skipped
	Refused
	Deferred
	Evicted
	LostReplica
	Placed
	Pending
	NodeDrained
	DrainFailed
)

// Event is one decision taken during the simulation (Section 5.3).
type Event struct {
	Kind   Kind
	Node   string // the drained node
	Pod    string // "namespace/name" of the pod concerned, empty for node events
	Target string // node chosen for a replacement (Placed), otherwise empty
	Reason string // why, e.g. the eviction rule or the rejecting plugin
}

// Observer receives the events of the simulation (Observer pattern).
type Observer interface {
	Notify(e Event)
}

// Log is an Observer that keeps all events in the order they happened
// (component Event log, Section 5.3).
type Log struct {
	events []Event
}

// Notify appends the event to the log.
func (l *Log) Notify(e Event) {
	l.events = append(l.events, e)
}

// Events returns the recorded events in order.
func (l *Log) Events() []Event {
	return slices.Clone(l.events)
}
