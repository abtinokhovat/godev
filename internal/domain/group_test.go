package domain

import "testing"

func TestPrimaryGroupsPicksSmallestGroupByMemberCount(t *testing.T) {
	services := []Service{
		{Name: "api", Group: []string{"core", "test"}},    // core has 2, test has 3 -> core
		{Name: "worker", Group: []string{"core"}},         // only core -> core
		{Name: "e2e", Group: []string{"test"}},            // only test -> test
		{Name: "smoke", Group: []string{"test"}},          // only test -> test
		{Name: "solo", Group: []string{"test", "lonely"}}, // lonely has 1 member -> lonely
		{Name: "ungrouped"},
	}

	got := PrimaryGroups(services)

	want := map[string]string{
		"api":    "core", // core:{api,worker}=2 < test:{api,e2e,smoke,solo}=4
		"worker": "core",
		"e2e":    "test",
		"smoke":  "test",
		"solo":   "lonely", // lonely:{solo}=1 < test=4
	}
	for name, wantGroup := range want {
		if got[name] != wantGroup {
			t.Errorf("PrimaryGroups()[%q] = %q, want %q", name, got[name], wantGroup)
		}
	}
	if _, ok := got["ungrouped"]; ok {
		t.Errorf("ungrouped service should be absent from the result, got %q", got["ungrouped"])
	}
}

func TestPrimaryGroupsTiesKeepFirstListed(t *testing.T) {
	services := []Service{
		{Name: "a", Group: []string{"x", "y"}},
		{Name: "b", Group: []string{"x"}},
		{Name: "c", Group: []string{"y"}},
	}
	// x:{a,b}=2, y:{a,c}=2 - tied, "a" lists x first.
	got := PrimaryGroups(services)
	if got["a"] != "x" {
		t.Errorf("PrimaryGroups()[a] = %q, want %q (first-listed on a tie)", got["a"], "x")
	}
}

func TestPrimaryGroupsSingleGroupUnaffected(t *testing.T) {
	services := []Service{{Name: "api", Group: []string{"core"}}}
	got := PrimaryGroups(services)
	if got["api"] != "core" {
		t.Errorf("PrimaryGroups()[api] = %q, want core", got["api"])
	}
}
