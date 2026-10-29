package sessiontheme

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/subaud-tools/tools-common/wiregen"
)

func TestQueryJSON(t *testing.T) {
	out, err := wiregen.Generate([]any{Query{}}, wiregen.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, union := range []string{
		"export type Hue =\n  | 'blue'\n  | 'teal'\n  | 'green'\n  | 'olive'\n  | 'amber'\n  | 'rust'\n  | 'rose'\n  | 'purple';",
		"export type Shade = 'midnight' | 'deep' | 'muted' | 'bright';",
	} {
		if !strings.Contains(out, union) {
			t.Errorf("session theme wire contract missing enum union:\n%s\ngenerated:\n%s", union, out)
		}
	}
	data, err := json.Marshal(Query{Assigned: false})
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"assigned":false}` {
		t.Fatalf("unassigned query: %s", data)
	}
	// A consumer's literal snake_case payload must retain stable session and group identity.
	input := `{"assigned":true,"theme":{"session_id":"remembered-1","group_id":"window-1","assignment":{"hue":"rose","shade":"muted"},"palette":{"background":"#010203","foreground":"#ffffff","panel":"#020304","selected_background":"#030405","muted":"#eeeeee","border":"#dddddd","accent":"#cccccc","accent_text":"#000000","success":"#b4e5ac","warning":"#f3d18a","error":"#ffa9a9","cursor":"#bbbbbb","cursor_text":"#111111","ansi":["#000000","#111111","#222222","#333333","#444444","#555555","#666666","#777777","#888888","#999999","#aaaaaa","#bbbbbb","#cccccc","#dddddd","#eeeeee","#ffffff"]},"header":{"background":"#010203","foreground":"#ffffff","accent":"#cccccc"}}}`
	var q Query
	if err := json.Unmarshal([]byte(input), &q); err != nil {
		t.Fatal(err)
	}
	if !q.Assigned || q.Theme == nil || q.Theme.SessionID != "remembered-1" || q.Theme.GroupID != "window-1" || q.Theme.Assignment != (Assignment{"rose", "muted"}) {
		t.Fatalf("lost resolved identity: %+v", q)
	}
	output, err := json.Marshal(q)
	if err != nil {
		t.Fatal(err)
	}
	var want, got any
	if err := json.Unmarshal([]byte(input), &want); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(output, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("wire fields lost: %s", output)
	}
}
