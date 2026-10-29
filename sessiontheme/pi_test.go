package sessiontheme

import (
	"encoding/json"
	"os"
	"testing"
)

func TestPiThemeContract(t *testing.T) {
	data, err := os.ReadFile("testdata/pi-theme.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Properties map[string]struct {
			Required   []string
			Properties map[string]json.RawMessage
		}
	}
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	tokens := schema.Properties["colors"]
	for _, h := range Hue("").Values() {
		for _, s := range Shade("").Values() {
			t.Run(string(h)+"-"+string(s), func(t *testing.T) {
				p, err := Resolve(Assignment{Hue(h), Shade(s)})
				if err != nil {
					t.Fatal(err)
				}
				name := string(h) + "-" + string(s)
				data, err := PiTheme(name, p)
				if err != nil {
					t.Fatal(err)
				}
				var doc struct {
					Name       string            `json:"name"`
					Appearance string            `json:"appearance"`
					Colors     map[string]string `json:"colors"`
				}
				if err := json.Unmarshal(data, &doc); err != nil {
					t.Fatal(err)
				}
				if doc.Name != name || doc.Appearance != "dark" {
					t.Fatal("wrong theme identity or appearance")
				}
				for _, key := range tokens.Required {
					if _, ok := doc.Colors[key]; !ok {
						t.Errorf("missing required token %s", key)
					}
				}
				backgrounds := map[string]bool{"selectedBg": true, "searchMatchBg": true, "userMessageBg": true, "customMessageBg": true, "toolPendingBg": true, "toolSuccessBg": true, "toolErrorBg": true}
				for key, c := range doc.Colors {
					if _, ok := tokens.Properties[key]; !ok {
						t.Errorf("unsupported token %s", key)
					}
					if !hexColor.MatchString(c) {
						t.Errorf("invalid color %s=%q", key, c)
					}
					if !backgrounds[key] {
						for _, bg := range []string{p.Background, doc.Colors["selectedBg"], doc.Colors["userMessageBg"], doc.Colors["customMessageBg"], doc.Colors["toolPendingBg"], doc.Colors["toolSuccessBg"], doc.Colors["toolErrorBg"], doc.Colors["searchMatchBg"]} {
							if ratio(c, bg) < 4.5 {
								t.Errorf("unreadable %s on %s", key, bg)
							}
						}
					}
				}
				for key, want := range map[string]string{"text": p.Foreground, "accent": p.Accent, "success": p.Success, "warning": p.Warning, "error": p.Error, "toolDiffAdded": p.Success, "toolDiffRemoved": p.Error, "toolOutput": p.Foreground} {
					if doc.Colors[key] != want {
						t.Errorf("%s lost its palette role", key)
					}
				}
			})
		}
	}
	for _, name := range []string{"", "session/blue"} {
		if _, err := PiTheme(name, Palette{}); err == nil {
			t.Errorf("accepted invalid name %q", name)
		}
	}
	if _, err := PiTheme("session", Palette{Background: "not-a-color"}); err == nil {
		t.Fatal("accepted invalid palette")
	}
}
