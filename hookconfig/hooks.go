// Package hookconfig reconciles explicitly owned command hooks in Claude settings.
package hookconfig

import "slices"

// Removal identifies exact command spellings owned by one event and matcher.
// An absent matcher and an empty matcher both represent the no-matcher case.
// No shell parsing, path expansion or matcher normalization is performed.
type Removal struct {
	Event, Matcher string
	Commands       []string
}

// Remove deletes matching nested command hooks in place and reports a change.
// Unrelated hooks, group fields and matcher distinctions are preserved. A group
// is pruned only when removing its matching hooks leaves its hooks array empty.
// Empty event arrays and other document fields are retained.
func Remove(document map[string]any, rules []Removal) bool {
	return walk(document, rules, true) > 0
}

func walk(document map[string]any, rules []Removal, remove bool) int {
	events, _ := document["hooks"].(map[string]any)
	total := 0
	for event, value := range events {
		groups, ok := value.([]any)
		if !ok {
			continue
		}
		var keptGroups []any
		eventCount := 0
		for _, value := range groups {
			group, _ := value.(map[string]any)
			hooks, _ := group["hooks"].([]any)
			var keptHooks []any
			count := 0
			for _, hook := range hooks {
				if matches(event, group, hook, rules) {
					count++
				} else if remove {
					keptHooks = append(keptHooks, hook)
				}
			}
			eventCount += count
			if remove {
				if count > 0 {
					if len(keptHooks) == 0 {
						continue
					}
					group["hooks"] = keptHooks
				}
				keptGroups = append(keptGroups, value)
			}
		}
		if remove && eventCount > 0 {
			if keptGroups == nil {
				keptGroups = []any{}
			}
			events[event] = keptGroups
		}
		total += eventCount
	}
	return total
}

func matches(event string, group map[string]any, value any, rules []Removal) bool {
	matcher, ok := group["matcher"].(string)
	if _, present := group["matcher"]; present && !ok {
		return false
	}
	hook, _ := value.(map[string]any)
	if hook["type"] != "command" {
		return false
	}
	command, ok := hook["command"].(string)
	if !ok {
		return false
	}
	for _, rule := range rules {
		if event == rule.Event && matcher == rule.Matcher && slices.Contains(rule.Commands, command) {
			return true
		}
	}
	return false
}

// Count returns the number of nested command hooks matching rule, without mutation.
// Each matching entry counts once, even if Commands repeats a spelling. Callers
// supply one intended registration's known aliases; distinct hooks are not
// duplicates merely because they share an event. Counts do not prove delivery.
func Count(document map[string]any, rule Removal) int {
	return walk(document, []Removal{rule}, false)
}
