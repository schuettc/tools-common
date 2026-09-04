package tools

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
)

// GroupedUsage prints "usage: <name> <command> [args]" then the commands,
// grouped under the headings in groups (display order) with a trailing "Other"
// bucket for commands whose Group is "" or matches no key. When groups is
// empty, the commands are printed as one flat list with no headings.
func GroupedUsage(w io.Writer, name string, groups []Group, cmds []Command) {
	fmt.Fprintf(w, "usage: %s <command> [args]\n", name)
	byName := append([]Command(nil), cmds...)
	sort.Slice(byName, func(i, j int) bool { return byName[i].Name < byName[j].Name })

	if len(groups) == 0 {
		fmt.Fprintln(w, "\ncommands:")
		writeRows(w, byName)
		return
	}

	// Bucket by group key.
	known := map[string]bool{}
	for _, g := range groups {
		known[g.Key] = true
	}
	for _, g := range groups {
		var rows []Command
		for _, c := range byName {
			if c.Group == g.Key {
				rows = append(rows, c)
			}
		}
		if len(rows) == 0 {
			continue
		}
		fmt.Fprintf(w, "\n%s\n", g.Heading)
		writeRows(w, rows)
	}
	var other []Command
	for _, c := range byName {
		if !known[c.Group] {
			other = append(other, c)
		}
	}
	if len(other) > 0 {
		fmt.Fprintf(w, "\nOther\n")
		writeRows(w, other)
	}
}

func writeRows(w io.Writer, cmds []Command) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, c := range cmds {
		fmt.Fprintf(tw, "  %s\t%s\n", c.Name, c.Summary)
	}
	tw.Flush()
}

// HelpFor prints one command's help: the usage line, its long Help, and its
// flags (from NewFlags). Synopsis falls back to the bare name.
func HelpFor(w io.Writer, name string, c Command) {
	syn := c.Synopsis
	if syn == "" {
		syn = c.Name
	}
	fmt.Fprintf(w, "Usage: %s %s\n", name, syn)
	if c.Help != "" {
		fmt.Fprintf(w, "\n%s\n", c.Help)
	}
	if c.NewFlags != nil {
		infos := FlagsOf(c.NewFlags())
		if len(infos) > 0 {
			fmt.Fprintln(w, "\nflags:")
			tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
			for _, fi := range infos {
				label := "  -" + fi.Name
				if fi.Type != "" {
					label += " " + fi.Type
				}
				usage := fi.Usage
				if fi.Default != "" && fi.Default != "false" {
					usage += fmt.Sprintf(" (default %s)", fi.Default)
				}
				fmt.Fprintf(tw, "%s\t%s\n", label, usage)
			}
			tw.Flush()
		}
	}
}

// roffEscape escapes the characters roff treats specially in body text.
func roffEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "-", `\-`)
	return s
}

// ManPage renders the registry as a roff man page (section 1).
func ManPage(name, domain string, groups []Group, cmds []Command) string {
	var b strings.Builder
	up := strings.ToUpper(name)
	fmt.Fprintf(&b, ".TH %s 1\n", up)
	fmt.Fprintf(&b, ".SH NAME\n%s \\- %s command-line interface\n", name, name)
	fmt.Fprintf(&b, ".SH SYNOPSIS\n.B %s\n<command> [args]\n", name)
	fmt.Fprintf(&b, ".SH DESCRIPTION\nCommands for %s (%s).\n", name, domain)

	sorted := append([]Command(nil), cmds...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	fmt.Fprintf(&b, ".SH COMMANDS\n")
	for _, c := range sorted {
		syn := c.Synopsis
		if syn == "" {
			syn = c.Name
		}
		fmt.Fprintf(&b, ".TP\n.B %s\n%s\n", roffEscape(syn), roffEscape(c.Summary))
		var flags []FlagInfo
		if c.NewFlags != nil {
			flags = FlagsOf(c.NewFlags())
		}
		if c.Help != "" || len(flags) > 0 {
			fmt.Fprintf(&b, ".RS\n")
			if c.Help != "" {
				fmt.Fprintf(&b, "%s\n", roffEscape(c.Help))
			}
			for _, fi := range flags {
				fmt.Fprintf(&b, ".br\n\\-%s\t%s\n", roffEscape(fi.Name), roffEscape(fi.Usage))
			}
			fmt.Fprintf(&b, ".RE\n")
		}
	}
	return b.String()
}

// CommandsJSON serializes the registry as the agent-facing command index.
func CommandsJSON(name string, cmds []Command) ([]byte, error) {
	type flagJSON struct {
		Name    string `json:"name"`
		Type    string `json:"type"`
		Default string `json:"default"`
		Usage   string `json:"usage"`
	}
	type cmdJSON struct {
		Name       string     `json:"name"`
		Synopsis   string     `json:"synopsis"`
		Summary    string     `json:"summary"`
		Group      string     `json:"group"`
		Help       string     `json:"help"`
		SelfRouted bool       `json:"selfRouted"`
		Flags      []flagJSON `json:"flags"`
	}
	sorted := append([]Command(nil), cmds...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	out := make([]cmdJSON, 0, len(sorted))
	for _, c := range sorted {
		cj := cmdJSON{Name: c.Name, Synopsis: c.Synopsis, Summary: c.Summary, Group: c.Group, Help: c.Help, SelfRouted: c.Run == nil, Flags: []flagJSON{}}
		if c.NewFlags != nil {
			for _, fi := range FlagsOf(c.NewFlags()) {
				cj.Flags = append(cj.Flags, flagJSON{fi.Name, fi.Type, fi.Default, fi.Usage})
			}
		}
		out = append(out, cj)
	}
	return json.MarshalIndent(out, "", "  ")
}
