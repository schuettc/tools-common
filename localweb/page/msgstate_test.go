package page

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/subaud-tools/tools-common/msgstate"
)

// updateStates rewrites assets/msgstate.js instead of asserting it:
// go test ./localweb/page -run TestMessageStatesFile -update
var updateStates = flag.Bool("update", false, "rewrite assets/msgstate.js from tools-common/msgstate")

// messageStatesJS is the page's copy of msgstate's list: the states in order,
// each with its label, as JSON in an ES module (the kit's assets import only
// ./<module>.js). Only this test imports msgstate; the page package does not.
func messageStatesJS(t *testing.T) []byte {
	t.Helper()
	type entry struct {
		State string `json:"state"`
		Label string `json:"label"`
	}
	var list []entry
	for _, v := range msgstate.State("").Values() {
		list = append(list, entry{v, msgstate.State(v).Label()})
	}
	b, err := json.Marshal(list)
	if err != nil {
		t.Fatal(err)
	}
	return []byte("// Generated from tools-common/msgstate; do not edit. Regenerate with\n" +
		"// go test ./localweb/page -run TestMessageStatesFile -update\n" +
		"export const MESSAGE_STATES = " + string(b) + ";\n")
}

func TestMessageStatesFile(t *testing.T) {
	want := messageStatesJS(t)
	path := filepath.Join("assets", "msgstate.js")
	if *updateStates {
		if err := os.WriteFile(path, want, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(want) {
		t.Fatalf("%s drifted from tools-common/msgstate (err %v); regenerate with -update.\nwant:\n%s\ngot:\n%s", path, err, want, got)
	}
}
