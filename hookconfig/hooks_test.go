package hookconfig_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/subaud-tools/tools-common/hookconfig"
)

func document(t *testing.T, text string) map[string]any {
	t.Helper()
	var d map[string]any
	if err := json.Unmarshal([]byte(text), &d); err != nil {
		t.Fatal(err)
	}
	return d
}

const mixed = `{"permissions":{"allow":["Read"]},"hooks":{
 "Stop":[
  {"hooks":[{"type":"command","command":"muster hook Stop claude"},{"type":"command","command":"echo unrelated"},{"type":"prompt","command":"muster hook Stop claude"}],"extra":"keep"},
  {"matcher":"","hooks":[{"type":"command","command":"~/.local/bin/muster hook Stop claude"}]},
  {"matcher":"deliberate","hooks":[{"type":"command","command":"muster hook Stop claude"}]}
 ],
 "SessionStart":[{"matcher":"startup|resume","hooks":[{"type":"command","command":"muster hook SessionStart claude"}]}],
 "SessionEnd":[{"hooks":[{"type":"command","command":"muster hook SessionEnd claude"}]}]
}}`

var stop = hookconfig.Removal{Event: "Stop", Commands: []string{"muster hook Stop claude", "~/.local/bin/muster hook Stop claude"}}

func TestRemoveOnlyExactOwnedCommandHooks(t *testing.T) {
	d := document(t, mixed)
	want := document(t, `{"permissions":{"allow":["Read"]},"hooks":{
 "Stop":[
  {"hooks":[{"type":"command","command":"echo unrelated"},{"type":"prompt","command":"muster hook Stop claude"}],"extra":"keep"},
  {"matcher":"deliberate","hooks":[{"type":"command","command":"muster hook Stop claude"}]}
 ],
 "SessionStart":[{"matcher":"startup|resume","hooks":[{"type":"command","command":"muster hook SessionStart claude"}]}],
 "SessionEnd":[{"hooks":[{"type":"command","command":"muster hook SessionEnd claude"}]}]
 }}`)
	if !hookconfig.Remove(d, []hookconfig.Removal{stop}) {
		t.Fatal("removal reported no change")
	}
	if !reflect.DeepEqual(d, want) {
		t.Fatalf("got %#v; want %#v", d, want)
	}
	if hookconfig.Remove(d, []hookconfig.Removal{stop}) {
		t.Fatal("repeat removal changed document")
	}
}

func TestCountOwnedAliasesWithoutChangingSettings(t *testing.T) {
	d := document(t, mixed)
	before := document(t, mixed)
	if n := hookconfig.Count(d, stop); n != 2 {
		t.Fatalf("owned Stop registrations = %d, want 2", n)
	}
	if !reflect.DeepEqual(d, before) {
		t.Fatal("count changed settings")
	}
}
