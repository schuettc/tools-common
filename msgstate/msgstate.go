// Package msgstate is the one lifecycle of a message Court sends from a
// family page to an agent: its seven states, the words the page shows for
// each, and the moves between them. Every tool with page messages (diazo,
// galley, casebook, sift, cull) uses it rather than keeping its own table,
// and generates its page's TypeScript type from State, whose Values() makes it
// a union in each tool's wire generator.
package msgstate

import "slices"

// State is where one message stands.
type State string

const (
	// Sent: on the page, not yet in front of the agent.
	Sent State = "sent"
	// WithAgent: the harness reported it landed in the agent's session.
	WithAgent State = "with_agent"
	// Working: the agent acknowledged it, with a line on what it is doing.
	Working State = "working"
	// Answered: settled, with the agent's reply.
	Answered State = "answered"
	// Declined: settled, the agent won't do it, with why.
	Declined State = "declined"
	// Failed: settled, the agent tried and couldn't, with why.
	Failed State = "failed"
	// NoReply: the agent's turn ended with the message unsettled.
	NoReply State = "no_reply"
)

// all is every state in the order the page and the generated type list them.
var all = []State{Sent, WithAgent, Working, Answered, Declined, Failed, NoReply}

var labels = map[State]string{
	Sent:      "sent",
	WithAgent: "with the agent",
	Working:   "working",
	Answered:  "answered",
	Declined:  "declined",
	Failed:    "failed",
	NoReply:   "no reply",
}

// Values is every state, in order. It takes a value receiver so a wire
// generator can call it on State's zero value and emit the type as a union.
func (State) Values() []string {
	out := make([]string, len(all))
	for i, s := range all {
		out[i] = string(s)
	}
	return out
}

// Label is the words the page's pill shows for s; "" for an unknown state.
func (s State) Label() string { return labels[s] }

// Final reports whether s is settled: answered, declined or failed. no_reply
// is not final, since a late reply may still settle it.
func Final(s State) bool { return s == Answered || s == Declined || s == Failed }

// next is the spec's diagram: the states each state may move to.
var next = map[State][]State{
	Sent:      {WithAgent},
	WithAgent: {Working, Answered, Declined, Failed, NoReply},
	Working:   {Answered, Declined, Failed, NoReply},
	// A late reply settles it; Court sending it again puts it back to sent.
	NoReply: {Answered, Declined, Failed, Sent},
}

// Allowed reports whether a message in from may move to to. Anything the
// diagram does not draw is refused: a settled message stays settled, and a
// late landed report never moves a working message back to with_agent. A
// tool that only changes the line under the same state is not moving it, and
// does not ask.
func Allowed(from, to State) bool {
	return slices.Contains(next[from], to)
}

// Acknowledge reports whether the agent's acknowledgement may move a message
// in from to to (working, answered, declined or failed). It is Allowed, plus
// one rule: an acknowledgement proves the message reached the agent, so a
// message still sent (its landed report not in yet) moves as if through
// with_agent. Tools call it for the agent's replies and Allowed for the
// harness's reports.
func Acknowledge(from, to State) bool {
	if from == Sent {
		return Allowed(WithAgent, to) && to != NoReply
	}
	return Allowed(from, to)
}
