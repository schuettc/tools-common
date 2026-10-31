package msgstate_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/subaud-tools/tools-common/msgstate"
	"github.com/subaud-tools/tools-common/wiregen"
)

// The field names are what a server sends the kit's thread.
func TestMessageMarshalsItsWireNames(t *testing.T) {
	when := time.Date(2026, 10, 30, 22, 0, 0, 0, time.UTC)
	got, err := json.Marshal(msgstate.Message{
		ID: 7, Thread: "DCSN1", Who: "court", Text: "go", When: when,
		State: msgstate.Working, ReplyTo: 6, Line: "reading",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"id":7,"thread":"DCSN1","who":"court","text":"go","when":"2026-10-30T22:00:00Z","state":"working","reply_to":6,"line":"reading"}`
	if string(got) != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

// An agent's row has no lifecycle: its state, reply-to and line stay off the wire.
func TestMessageLeavesEmptyFieldsOff(t *testing.T) {
	got, err := json.Marshal(msgstate.Message{ID: 8, Thread: "t", Who: "s1", Text: "done"})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"id":8,"thread":"t","who":"s1","text":"done","when":"0001-01-01T00:00:00Z"}`
	if string(got) != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestWiregenAcceptsMessage(t *testing.T) {
	got, err := wiregen.Generate([]any{msgstate.Message{}}, wiregen.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"export interface Message {", "state?: MessageState;", "reply_to?: number;", "export type MessageState ="} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in:\n%s", want, got)
		}
	}
}
