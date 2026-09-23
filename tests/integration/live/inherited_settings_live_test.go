//go:build live

package live_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// TestLiveRepositoryCommandsLeaveWhatTheProjectDefinesAlone is #657.
//
// A repository's listing of branch restrictions, default reviewer conditions
// and default tasks holds its project's as well, and Bitbucket's repository
// routes take their ids: a restriction or a condition deleted through them is
// deleted from the project, and so from every repository in it, and a task
// answers 204 and stays. The repository's commands mark the project's entries
// as inherited and refuse to change them, naming the project's command.
//
// Each part seeds an entry on the project straight through the API, has every
// repository command that would change it refuse, and then reads the entry
// back from the project to show it is still there as it was.
func TestLiveRepositoryCommandsLeaveWhatTheProjectDefinesAlone(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	key := seeded.Key
	selector := key + "/" + seeded.Repos[0].Slug

	seed := func(t *testing.T, what, path string, payload any) string {
		t.Helper()

		created, err := harness.liveJSON(ctx, http.MethodPost, path, payload)
		if err != nil {
			t.Fatalf("create %s failed: %v", what, err)
		}
		id, ok := numericOrStringID(created["id"])
		if !ok {
			t.Fatalf("the created %s has no id: %v", what, created)
		}

		return id
	}

	// entry finds the listed object with id in a command's JSON output, under
	// the named list.
	entry := func(t *testing.T, output, list, id string) map[string]any {
		t.Helper()

		var data map[string]any
		if err := decodeJSONEnvelopeData(output, &data); err != nil {
			t.Fatalf("decode %s: %v\n%s", list, err, output)
		}
		entries, _ := data[list].([]any)
		for _, candidate := range entries {
			object, _ := candidate.(map[string]any)
			if got, ok := numericOrStringID(object["id"]); ok && got == id {
				return object
			}
		}
		t.Fatalf("%s holds no entry %s: %s", list, id, output)

		return nil
	}

	// refused asserts a command refused with the validation failure that names
	// the project's command, rather than doing anything.
	refused := func(t *testing.T, output string, err error, command string) {
		t.Helper()

		if !apperrors.IsKind(err, apperrors.KindValidation) {
			t.Fatalf("expected a validation refusal naming %q, got %v\n%s", command, err, output)
		}
		if !strings.Contains(err.Error(), command) {
			t.Errorf("the refusal does not name %q: %v", command, err)
		}
	}

	// inheritedRow asserts a text listing marks the entry id as inherited.
	inheritedRow := func(t *testing.T, listing, id string) {
		t.Helper()

		for _, line := range strings.Split(listing, "\n") {
			fields := strings.Fields(line)
			if len(fields) > 0 && fields[0] == id {
				if !strings.Contains(line, "inherited from "+key) {
					t.Errorf("entry %s is not marked as inherited: %q", id, line)
				}
				return
			}
		}
		t.Errorf("the listing has no row for %s:\n%s", id, listing)
	}

	t.Run("a branch restriction", func(t *testing.T) {
		const matcher = "refs/heads/inherited-restriction"
		id := seed(t, "project restriction", fmt.Sprintf("/rest/branch-permissions/latest/projects/%s/restrictions", key),
			map[string]any{
				"type":    "no-deletes",
				"matcher": map[string]any{"id": matcher, "type": map[string]any{"id": "BRANCH"}},
				"users":   []string{},
				"groups":  []string{},
			})

		listed := entry(t, mustLiveCLIUnscoped(t, "branch", "restriction", "list", "--repo", selector), "restrictions", id)
		if listed["scope"] != "PROJECT" {
			t.Errorf("the repository's listing does not say restriction %s is the project's: %v", id, listed)
		}
		text, err := executeLiveCLIUnscoped(t, "branch", "restriction", "list", "--repo", selector)
		if err != nil {
			t.Fatalf("branch restriction list failed: %v\n%s", err, text)
		}
		inheritedRow(t, text, id)

		output, err := executeLiveCLIUnscoped(t, "--json", "branch", "restriction", "delete", id, "--repo", selector, "--yes")
		refused(t, output, err, "bb project branch-restriction delete "+key+" "+id)
		output, err = executeLiveCLIUnscoped(t, "--json", "--dry-run", "branch", "restriction", "delete", id, "--repo", selector, "--yes")
		refused(t, output, err, "bb project branch-restriction delete "+key+" "+id)
		// An update creates the new restriction and deletes the one it
		// replaces through the same route, so it would have removed the
		// project's restriction as well.
		output, err = executeLiveCLIUnscoped(t, "--json", "branch", "restriction", "update", id, "--repo", selector,
			"--type", "read-only", "--matcher-type", "BRANCH", "--matcher-id", matcher)
		refused(t, output, err, "bb project branch-restriction update "+key+" "+id)

		kept, err := harness.liveJSON(ctx, http.MethodGet,
			fmt.Sprintf("/rest/branch-permissions/latest/projects/%s/restrictions/%s", key, id), nil)
		if err != nil {
			t.Fatalf("the project's restriction %s is gone: %v", id, err)
		}
		if kept["type"] != "no-deletes" {
			t.Errorf("the project's restriction %s changed: %v", id, kept)
		}
		for _, candidate := range entriesOf(t, mustLiveCLIUnscoped(t, "branch", "restriction", "list", "--repo", selector), "restrictions") {
			if candidate["scope"] == "REPOSITORY" {
				t.Errorf("the refused update left a restriction on the repository: %v", candidate)
			}
		}
	})

	t.Run("a default reviewer condition", func(t *testing.T) {
		reviewerID, err := harness.userID(ctx, harness.username())
		if err != nil {
			t.Fatalf("look up the reviewer id: %v", err)
		}
		const target = "refs/heads/inherited-condition"
		id := seed(t, "project reviewer condition", fmt.Sprintf("/rest/default-reviewers/latest/projects/%s/condition", key),
			map[string]any{
				"sourceMatcher":     anyRefMatcherPayload,
				"targetMatcher":     map[string]any{"id": target, "type": map[string]any{"id": "BRANCH"}},
				"reviewers":         []map[string]any{{"id": reviewerID}},
				"requiredApprovals": 2,
			})

		listed := entry(t, mustLiveCLIUnscoped(t, "reviewer", "condition", "list", "--repo", selector), "conditions", id)
		if listed["scope"] != "PROJECT" {
			t.Errorf("the repository's listing does not say condition %s is the project's: %v", id, listed)
		}
		text, err := executeLiveCLIUnscoped(t, "reviewer", "condition", "list", "--repo", selector)
		if err != nil {
			t.Fatalf("reviewer condition list failed: %v\n%s", err, text)
		}
		inheritedRow(t, text, id)

		output, err := executeLiveCLIUnscoped(t, "--json", "reviewer", "condition", "delete", id, "--repo", selector, "--yes")
		refused(t, output, err, "bb reviewer condition delete "+id+" --project "+key)
		output, err = executeLiveCLIUnscoped(t, "--json", "--dry-run", "reviewer", "condition", "delete", id, "--repo", selector, "--yes")
		refused(t, output, err, "bb reviewer condition delete "+id+" --project "+key)
		update := fmt.Sprintf(`{"sourceMatcher": {"id": "ANY_REF", "type": {"id": "ANY_REF"}},
			"targetMatcher": {"id": %q, "type": {"id": "BRANCH"}}, "reviewers": [{"id": %d}], "requiredApprovals": 1}`,
			target, reviewerID)
		output, err = executeLiveCLIUnscoped(t, "--json", "reviewer", "condition", "update", id, update, "--repo", selector)
		refused(t, output, err, "bb reviewer condition update "+id+" --project "+key)

		kept := entry(t, mustLiveCLIUnscoped(t, "reviewer", "condition", "list", "--project", key), "conditions", id)
		if approvals, _ := kept["requiredApprovals"].(float64); approvals != 2 {
			t.Errorf("the project's condition %s changed: %v", id, kept)
		}
	})

	t.Run("a default task", func(t *testing.T) {
		description := testsupport.UniqueName("Inherited task ")
		id := seed(t, "project default task", fmt.Sprintf("/rest/default-tasks/latest/projects/%s/tasks", key),
			map[string]any{
				"description":   description,
				"sourceMatcher": anyRefMatcherPayload,
				"targetMatcher": anyRefMatcherPayload,
			})

		listed := entry(t, mustLiveCLIUnscoped(t, "repo", "default-task", "list", "--repo", selector), "tasks", id)
		if listed["scope"] != "PROJECT" {
			t.Errorf("the repository's listing does not say task %s is the project's: %v", id, listed)
		}
		text, err := executeLiveCLIUnscoped(t, "repo", "default-task", "list", "--repo", selector)
		if err != nil {
			t.Fatalf("repo default-task list failed: %v\n%s", err, text)
		}
		inheritedRow(t, text, id)

		output, err := executeLiveCLIUnscoped(t, "--json", "repo", "default-task", "delete", id, "--repo", selector, "--yes")
		refused(t, output, err, "bb project default-task delete "+key+" "+id)
		output, err = executeLiveCLIUnscoped(t, "--json", "--dry-run", "repo", "default-task", "delete", id, "--repo", selector, "--yes")
		refused(t, output, err, "bb project default-task delete "+key+" "+id)
		output, err = executeLiveCLIUnscoped(t, "--json", "repo", "default-task", "update", id, "--repo", selector,
			"--description", "changed through the repository")
		refused(t, output, err, "bb project default-task update "+key+" "+id)

		page, err := harness.liveJSON(ctx, http.MethodGet, fmt.Sprintf("/rest/default-tasks/latest/projects/%s/tasks", key), nil)
		if err != nil {
			t.Fatalf("read the project's tasks back: %v", err)
		}
		found := false
		values, _ := page["values"].([]any)
		for _, value := range values {
			task, _ := value.(map[string]any)
			if got, ok := numericOrStringID(task["id"]); ok && got == id {
				found = true
				if task["description"] != description {
					t.Errorf("the project's task %s changed: %v", id, task)
				}
			}
		}
		if !found {
			t.Errorf("the project's task %s is gone: %v", id, page)
		}

		// The repository's route answers 204 for a project's task and
		// deletes nothing (#657), so its answer is no evidence of a delete: a
		// task that is not there is reported as such, from the listing.
		output, err = executeLiveCLIUnscoped(t, "--json", "repo", "default-task", "delete", "2147483000", "--repo", selector, "--yes")
		if !apperrors.IsKind(err, apperrors.KindNotFound) {
			t.Errorf("deleting a task that is not there should report not_found, got %v\n%s", err, output)
		}
	})
}

// entriesOf is every object in the named list of a command's JSON output.
func entriesOf(t *testing.T, output, list string) []map[string]any {
	t.Helper()

	var data map[string]any
	if err := decodeJSONEnvelopeData(output, &data); err != nil {
		t.Fatalf("decode %s: %v\n%s", list, err, output)
	}

	entries := []map[string]any{}
	listed, _ := data[list].([]any)
	for _, candidate := range listed {
		if object, ok := candidate.(map[string]any); ok {
			entries = append(entries, object)
		}
	}

	return entries
}
