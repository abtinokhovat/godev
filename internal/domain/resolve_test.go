package domain

import "testing"

// Exhaustive behavior coverage (group precedence, dedup, ties, ...)
// lives in cmd/godev/setup_test.go, which already exercised this
// exact logic before it moved here - this is just a sanity check that
// the move didn't break anything at the new home.
func TestResolveTargetsGroupAndIndividualMatch(t *testing.T) {
	services := []Service{
		{Name: "api", Group: []string{"core"}},
		{Name: "worker", Group: []string{"core"}},
		{Name: "web", Group: []string{"frontend"}},
	}

	got, err := ResolveTargets(services, []string{"core", "web"})
	if err != nil {
		t.Fatalf("ResolveTargets: %v", err)
	}
	var names []string
	for _, s := range got {
		names = append(names, s.Name)
	}
	want := []string{"api", "worker", "web"}
	if len(names) != len(want) {
		t.Fatalf("ResolveTargets(core, web) = %v, want %v", names, want)
	}
	for i, n := range want {
		if names[i] != n {
			t.Errorf("names[%d] = %q, want %q (full: %v)", i, names[i], n, names)
		}
	}

	if _, err := ResolveTargets(services, []string{"nonexistent"}); err == nil {
		t.Fatal("expected an error for an unmatched target")
	}
}
