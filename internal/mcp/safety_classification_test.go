package mcp

import "testing"

// TestGatedToolsAreTheOnesThatMergeOrGate pins the safety classification to the
// two reasons the Spec doc names, so a reclassification has to be deliberate.
//
// submit_pr_review is here because it was originally safe, justified as "like
// commenting and can be dismissed". APPROVED is the input a required-reviewer
// check consumes, so an agent able to submit it participates in the control it
// is meant to be subject to — the same reason set_build_status is withheld.
func TestGatedToolsAreTheOnesThatMergeOrGate(t *testing.T) {
	expectedGated := map[string]string{
		"merge_pull_request": "irreversible: changes the target branch",
		"enable_auto_merge":  "irreversible: causes a merge once checks pass",
		"set_build_status":   "gating: a green status can unblock a required build check",
		"submit_pr_review":   "gating: APPROVED is consumed by required-reviewer checks",
	}

	implemented := map[string]bool{}

	for _, spec := range AllSpecs() {
		name := spec.Tool.Name
		implemented[name] = true

		reason, shouldGate := expectedGated[name]

		switch {
		case shouldGate && spec.Safe:
			t.Errorf("%s is exposed by default but should be gated — %s", name, reason)
		case !shouldGate && !spec.Safe:
			t.Errorf("%s is gated but is not listed as merging or gating; add it here with a reason, or make it safe", name)
		}
	}

	for name := range expectedGated {
		if !implemented[name] {
			t.Errorf("expected-gated tool %q no longer exists", name)
		}
	}
}
