package mcp

import "testing"

// TestToolsThatAskAreTheOnesThatDecideAMerge pins which tools ask the person
// to confirm a call, so that changing it has to be deliberate.
//
// A tool asks when it merges, changes whether or when a pull request merges,
// or feeds a check that decides whether one may. create_tag asks as well,
// since release pipelines commonly act on a new tag. No tool approves or sets
// needs-work: APPROVED is the input a required-reviewer check consumes, and an
// approval made with the person's token is indistinguishable from theirs, so a
// review is prepared as a draft the person publishes (#796).
func TestToolsThatAskAreTheOnesThatDecideAMerge(t *testing.T) {
	t.Parallel()

	expected := map[string]struct {
		asks   Asking
		reason string
	}{
		"merge_pull_request":  {AsksAlways, "merges now, and a merge cannot be undone"},
		"enable_auto_merge":   {AsksAlways, "merges later, or at once when the checks already pass"},
		"disable_auto_merge":  {AsksAlways, "changes when a pull request merges"},
		"set_build_status":    {AsksAlways, "a successful required build can unblock a merge"},
		"create_tag":          {AsksAlways, "release pipelines commonly act on a new tag, and no tool here deletes one"},
		"update_pull_request": {AsksWhenSettingDraft, "a draft cannot merge, and making one a draft cancels its auto-merge"},
	}

	implemented := map[string]bool{}

	for _, spec := range AllSpecs() {
		name := spec.Tool.Name
		implemented[name] = true

		want, listed := expected[name]
		switch {
		case listed && spec.Asks != want.asks:
			t.Errorf("%s asks %q, want %q: %s", name, spec.Asks, want.asks, want.reason)
		case !listed && spec.Asks != AsksNever:
			t.Errorf("%s asks (%s) but is not listed here as deciding a merge; add it with a reason, or make it not ask", name, spec.Asks)
		}
	}

	for name := range expected {
		if !implemented[name] {
			t.Errorf("tool %q, expected to ask, no longer exists", name)
		}
	}
}
