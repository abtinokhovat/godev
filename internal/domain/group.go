package domain

// PrimaryGroups computes, for every service that has at least one
// group, the "primary" group used for grouped display (the TUI
// sidebar, `godev list`): the smallest of the service's own listed
// groups, measured by how many services list that group name across
// the whole project.
//
// A service can be tagged into more than one group - `group: [core,
// test]` - but that's for `godev run <target>...` convenience (start
// everything in "core" or everything in "test" with one word), not
// for display: it still has one natural, most-specific home. The
// smallest group is a reasonable proxy for "most specific" without
// requiring the user to say which of their groups is the real one -
// a small, focused group like "core" is more informative to file a
// service under than a broad, cross-cutting one it's also tagged
// into for convenience. Ties keep the service's first-listed group,
// for determinism. Services with no groups are simply absent from
// the result.
func PrimaryGroups(services []Service) map[string]string {
	size := make(map[string]int)
	for _, svc := range services {
		for _, g := range svc.Group {
			size[g]++
		}
	}

	primary := make(map[string]string, len(services))
	for _, svc := range services {
		if len(svc.Group) == 0 {
			continue
		}
		best := svc.Group[0]
		for _, g := range svc.Group[1:] {
			if size[g] < size[best] {
				best = g
			}
		}
		primary[svc.Name] = best
	}
	return primary
}
