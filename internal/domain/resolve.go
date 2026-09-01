package domain

import (
	"fmt"
	"strings"
)

// ResolveTargets expands a list of `godev run <target>...`-style
// targets into the deduplicated set of services those targets name,
// preserving first-appearance order. Each target is matched as a
// group name first - against every group a service lists, not just
// its first, so a service in more than one group (`group: [core,
// test]`) is reachable by any of them - then as an exact service
// name; group match takes precedence so a bare word like "core"
// expands to every service in that group even if a single service
// happens to share the name. A service reachable through more than
// one requested target (its own name plus a group it belongs to, two
// overlapping groups, or two of its own groups both being requested)
// is only included once. Every target must resolve to at least one
// service, or the whole call fails listing everything that didn't
// match, so a typo doesn't silently resolve a smaller set than asked
// for. Shared by `godev run <target>...` (cmd/godev) and the TUI's
// own ad-hoc ":" run prompt, so both accept exactly the same syntax.
func ResolveTargets(services []Service, targets []string) ([]Service, error) {
	byName := make(map[string]Service, len(services))
	for _, s := range services {
		byName[s.Name] = s
	}

	seen := make(map[string]bool, len(services))
	var out []Service
	var unmatched []string

	for _, target := range targets {
		matched := false
		for _, s := range services {
			inGroup := false
			for _, g := range s.Group {
				if g == target {
					inGroup = true
					break
				}
			}
			if !inGroup {
				continue
			}
			matched = true
			if !seen[s.Name] {
				seen[s.Name] = true
				out = append(out, s)
			}
		}
		if !matched {
			if s, ok := byName[target]; ok {
				matched = true
				if !seen[s.Name] {
					seen[s.Name] = true
					out = append(out, s)
				}
			}
		}
		if !matched {
			unmatched = append(unmatched, target)
		}
	}

	if len(unmatched) > 0 {
		return nil, fmt.Errorf("no group or service named %s", strings.Join(unmatched, ", "))
	}
	return out, nil
}
