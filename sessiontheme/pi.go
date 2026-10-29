package sessiontheme

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// PiTheme renders a pi 1.4 custom theme without touching settings or files.
// Tokens are explicitly mapped to shared roles; the pinned upstream schema
// checks completeness without becoming a second palette definition.
func PiTheme(name string, p Palette) ([]byte, error) {
	if name == "" || strings.Contains(name, "/") {
		return nil, fmt.Errorf("invalid pi theme name %q: must be nonempty and contain no slash", name)
	}
	colors := map[string]string{
		"accent":             p.Accent,
		"border":             p.Border,
		"borderAccent":       p.Accent,
		"borderMuted":        p.Muted,
		"success":            p.Success,
		"error":              p.Error,
		"warning":            p.Warning,
		"muted":              p.Muted,
		"dim":                p.Muted,
		"text":               p.Foreground,
		"thinkingText":       p.Muted,
		"scrollbarTrack":     p.Muted,
		"scrollbarThumb":     p.Foreground,
		"selectedBg":         p.SelectedBackground,
		"searchMatchBg":      p.SelectedBackground,
		"searchMatchText":    p.Foreground,
		"userMessageBg":      p.Panel,
		"userMessageText":    p.Foreground,
		"customMessageBg":    p.Panel,
		"customMessageText":  p.Foreground,
		"customMessageLabel": p.Accent,
		"toolPendingBg":      p.Panel,
		"toolSuccessBg":      p.Panel,
		"toolErrorBg":        p.Panel,
		"toolTitle":          p.Accent,
		"toolOutput":         p.Foreground,
		"mdHeading":          p.Accent,
		"mdLink":             p.Accent,
		"mdLinkUrl":          p.Muted,
		"mdCode":             p.Warning,
		"mdCodeBlock":        p.Foreground,
		"mdCodeBlockBorder":  p.Border,
		"mdQuote":            p.Muted,
		"mdQuoteBorder":      p.Border,
		"mdHr":               p.Border,
		"mdListBullet":       p.Accent,
		"toolDiffAdded":      p.Success,
		"toolDiffRemoved":    p.Error,
		"toolDiffContext":    p.Muted,
		"syntaxComment":      p.Muted,
		"syntaxKeyword":      p.Accent,
		"syntaxFunction":     p.Accent,
		"syntaxVariable":     p.Foreground,
		"syntaxString":       p.Success,
		"syntaxNumber":       p.Warning,
		"syntaxType":         p.Accent,
		"syntaxOperator":     p.Foreground,
		"syntaxPunctuation":  p.Muted,
		"thinkingOff":        p.Muted,
		"thinkingMinimal":    p.Border,
		"thinkingLow":        p.Accent,
		"thinkingMedium":     p.Accent,
		"thinkingHigh":       p.Accent,
		"thinkingXhigh":      p.Accent,
		"thinkingMax":        p.Accent,
		"bashMode":           p.Warning,
	}
	for token, color := range colors {
		if len(color) != 7 || color[0] != '#' {
			return nil, fmt.Errorf("invalid color for pi token %s: %q", token, color)
		}
		if _, err := strconv.ParseUint(color[1:], 16, 24); err != nil {
			return nil, fmt.Errorf("invalid color for pi token %s: %w", token, err)
		}
	}
	doc := struct {
		Name       string            `json:"name"`
		Appearance string            `json:"appearance"`
		Colors     map[string]string `json:"colors"`
		Export     struct {
			PageBg string `json:"pageBg"`
			CardBg string `json:"cardBg"`
			InfoBg string `json:"infoBg"`
		} `json:"export"`
	}{Name: name, Appearance: "dark", Colors: colors}
	doc.Export.PageBg = p.Background
	doc.Export.CardBg = p.Panel
	doc.Export.InfoBg = p.Panel
	return json.MarshalIndent(doc, "", "  ")
}
