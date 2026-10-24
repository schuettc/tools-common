package msgstate

import (
	"slices"
	"testing"
)

func TestValuesPinsTheSevenStatesInOrder(t *testing.T) {
	want := []string{"sent", "with_agent", "working", "answered", "declined", "failed", "no_reply"}
	if got := State("").Values(); !slices.Equal(got, want) {
		t.Fatalf("Values() = %q, want %q", got, want)
	}
}

func TestLabelPinsEachStatesWords(t *testing.T) {
	want := map[State]string{
		Sent:      "sent",
		WithAgent: "with the agent",
		Working:   "working",
		Answered:  "answered",
		Declined:  "declined",
		Failed:    "failed",
		NoReply:   "no reply",
	}
	for s, label := range want {
		if got := s.Label(); got != label {
			t.Errorf("%s.Label() = %q, want %q", s, got, label)
		}
	}
	if got := State("delivered").Label(); got != "" {
		t.Errorf("unknown state's label = %q, want empty", got)
	}
}

func TestFinalIsTheThreeSettledStates(t *testing.T) {
	for _, v := range State("").Values() {
		s := State(v)
		want := s == Answered || s == Declined || s == Failed
		if got := Final(s); got != want {
			t.Errorf("Final(%s) = %v, want %v", s, got, want)
		}
	}
}

// TestAllowedPinsTheSpecDiagram checks every pair of states against the
// spec's diagram: anything not listed here is refused, including working back
// to with_agent (a late landed report) and a move out of a settled state.
func TestAllowedPinsTheSpecDiagram(t *testing.T) {
	allowed := map[[2]State]bool{
		{Sent, WithAgent}:     true,
		{WithAgent, Working}:  true,
		{WithAgent, Answered}: true,
		{WithAgent, Declined}: true,
		{WithAgent, Failed}:   true,
		{WithAgent, NoReply}:  true,
		{Working, Answered}:   true,
		{Working, Declined}:   true,
		{Working, Failed}:     true,
		{Working, NoReply}:    true,
		{NoReply, Answered}:   true,
		{NoReply, Declined}:   true,
		{NoReply, Failed}:     true,
		{NoReply, Sent}:       true,
	}
	for _, f := range State("").Values() {
		for _, to := range State("").Values() {
			from, to := State(f), State(to)
			if got, want := Allowed(from, to), allowed[[2]State{from, to}]; got != want {
				t.Errorf("Allowed(%s, %s) = %v, want %v", from, to, got, want)
			}
		}
	}
	if Allowed(Working, WithAgent) {
		t.Error("a late landed report must not move working back to with_agent")
	}
	if Allowed("delivered", WithAgent) || Allowed(Sent, "delivered") {
		t.Error("an unknown state must not be allowed either way")
	}
}

// TestAcknowledgeTreatsAnAnswerAsProofItLanded: the agent can answer a message
// before the harness's landed report reaches the tool (pi lists a held message
// in its status; Claude Code's report is its own process). An acknowledgement
// proves the message reached the agent, so from sent it passes through
// with_agent; everything else follows Allowed.
func TestAcknowledgeTreatsAnAnswerAsProofItLanded(t *testing.T) {
	for _, to := range []State{Working, Answered, Declined, Failed} {
		if !Acknowledge(Sent, to) {
			t.Errorf("Acknowledge(sent, %s) refused", to)
		}
		if !Acknowledge(WithAgent, to) || !Acknowledge(NoReply, to) && to != Working {
			t.Errorf("Acknowledge refused a move Allowed draws, to %s", to)
		}
	}
	for _, c := range [][2]State{{Sent, WithAgent}, {Sent, NoReply}, {Answered, Working}, {Working, WithAgent}, {NoReply, Working}} {
		if Acknowledge(c[0], c[1]) {
			t.Errorf("Acknowledge(%s, %s) allowed", c[0], c[1])
		}
	}
}
