package tools

import (
	"fmt"
	"io"
	"sort"
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
