//go:build live

package live_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
)

// The review surfaces a person and an agent actually read, against a pull
// request that is genuinely mid-review.
//
// These replace the mocked CLI tests in internal/cli/pr_review_visibility_test.go,
// which drove the same commands against an invented activity timeline. Every
// count they checked was a count their own fixture had put there, so the
// assertions could only fail if the formatter disagreed with the author -- never
// if bb disagreed with Bitbucket.
//
// The state is seeded once and read many ways: an unresolved comment with a
// reply, an unresolved task, and a reviewer who has asked for changes.
func TestLivePullRequestReviewSurfaces(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	reviewer, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create reviewer failed: %v", err)
	}
	if err := harness.grantRepoPermission(ctx, seeded.Key, repo.Slug, reviewer.Username, "REPO_WRITE"); err != nil {
		t.Fatalf("grant the reviewer write access failed: %v", err)
	}
	prReviewAssertRepoPermission(t, reviewer.Username, "REPO_WRITE")

	const branch = "feature/review-surfaces"
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "surfaces.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}

	prID := createLivePRForRegression(t, branch, "Review surfaces",
		"--reviewers", reviewer.Username, "--no-default-reviewers", "--no-codeowners")
	// Before the status below, which would add the reviewer as a participant
	// had the create dropped them.
	created := prReviewPullRequest(t, prID)
	if created["title"] != "Review surfaces" {
		t.Fatalf("title = %v, want Review surfaces", created["title"])
	}
	prReviewAssertSoleReviewer(t, created, reviewer.Username, "UNAPPROVED")

	commentsPath := fmt.Sprintf("/rest/api/latest/projects/%s/repos/%s/pull-requests/%s/comments",
		seeded.Key, repo.Slug, prID)

	comment, err := harness.liveJSON(ctx, http.MethodPost, commentsPath,
		map[string]any{"text": "this should handle nil"})
	if err != nil {
		t.Fatalf("create the comment failed: %v", err)
	}
	commentID := trimNumeric(comment["id"])

	if _, err := harness.liveJSON(ctx, http.MethodPost, commentsPath, map[string]any{
		"text":   "fixed in abc123",
		"parent": map[string]any{"id": comment["id"]},
	}); err != nil {
		t.Fatalf("create the reply failed: %v", err)
	}

	// A task on Bitbucket Data Center 8+ is a blocker comment.
	task, err := harness.liveJSON(ctx, http.MethodPost, commentsPath,
		map[string]any{"text": "add a regression test", "severity": "BLOCKER"})
	if err != nil {
		t.Fatalf("create the task failed: %v", err)
	}
	taskID := trimNumeric(task["id"])

	// An inline comment, so the path-scoped listing has something to return.
	inlineID, _ := numericOrStringID(prReviewCommentIn(t, mustLiveCLI(t, "pr", "comment", "add", prID, "--text", "this line needs a guard",
		"--path", "surfaces.txt", "--line", "1"))["id"])

	// A review status belongs to the participant who holds it, so this is set
	// as the reviewer rather than as the admin.
	if _, err := harness.liveJSONAs(ctx, reviewer, http.MethodPut,
		fmt.Sprintf("/rest/api/latest/projects/%s/repos/%s/pull-requests/%s/participants/%s",
			seeded.Key, repo.Slug, prID, reviewer.Username),
		map[string]any{"user": map[string]any{"name": reviewer.Username}, "status": "NEEDS_WORK"}); err != nil {
		t.Fatalf("set the reviewer status to NEEDS_WORK failed: %v", err)
	}
	prReviewAssertSoleReviewer(t, prReviewPullRequest(t, prID), reviewer.Username, "NEEDS_WORK")

	// Everything seeded, read back as stored before any subtest reads it some
	// other way: the three threads with their texts, the reply under the
	// comment it answers, and the inline comment's anchor.
	seededThreads, seededSummary := prReviewThreadList(t, mustLiveCLI(t, "pr", "comment", "list", prID, "--with-replies"))
	if ids := prReviewIDs(seededThreads); !slices.Equal(ids, prReviewSorted(commentID, taskID, inlineID)) {
		t.Fatalf("threads = %v, want exactly the comment %s, the task %s and the inline comment %s", ids, commentID, taskID, inlineID)
	}
	for _, thread := range seededThreads {
		switch id, _ := numericOrStringID(thread["id"]); id {
		case commentID:
			replies := prReviewTexts(t, thread, "replies")
			if thread["kind"] != "comment" || thread["text"] != "this should handle nil" || !slices.Equal(replies, []string{"fixed in abc123"}) {
				t.Errorf("comment thread = kind %v, text %v, replies %q; want the comment and its one reply", thread["kind"], thread["text"], replies)
			}
		case taskID:
			if thread["kind"] != "task" || thread["text"] != "add a regression test" {
				t.Errorf("task thread = kind %v, text %v; want the task as posted", thread["kind"], thread["text"])
			}
		case inlineID:
			anchor, _ := thread["anchor"].(map[string]any)
			if thread["kind"] != "comment" || thread["text"] != "this line needs a guard" ||
				anchor["path"] != "surfaces.txt" || anchor["line"] != float64(1) || anchor["lineType"] != "ADDED" {
				t.Errorf("inline thread = kind %v, text %v, anchor %v; want the comment on surfaces.txt line 1", thread["kind"], thread["text"], thread["anchor"])
			}
		}
	}
	if seededSummary["unresolved"] != float64(3) || seededSummary["openTasks"] != float64(1) {
		t.Errorf("summary = %v, want 3 unresolved threads and 1 open task", seededSummary)
	}
	pathThreads, _ := prReviewThreadList(t, mustLiveCLI(t, "pr", "comment", "list", prID, "--path", "surfaces.txt"))
	if ids := prReviewIDs(pathThreads); !slices.Equal(ids, []string{inlineID}) {
		t.Fatalf("threads on surfaces.txt = %v, want only the inline comment %s", ids, inlineID)
	}

	t.Run("pr get names the reviewer who asked for changes", func(t *testing.T) {
		// Whether Bitbucket reports a NEEDS_WORK participant, and under which
		// field, is the part a fixture cannot establish.
		summary := liveReviewSummary(t, mustLiveCLI(t, "pr", "get", prID))

		if summary["countsSource"] != "activities" {
			t.Errorf("countsSource = %#v, want activities", summary["countsSource"])
		}
		if summary["actionRequired"] != true {
			t.Errorf("expected actionRequired on a pull request with open feedback: %#v", summary)
		}

		needsWork, _ := summary["needsWork"].([]any)
		if len(needsWork) != 1 || needsWork[0] != reviewer.Username {
			t.Errorf("needsWork = %#v, want [%s]", summary["needsWork"], reviewer.Username)
		}

		human := mustLiveHumanCLI(t, "pr", "get", prID)
		if !strings.Contains(human, "Needs work: "+reviewer.Username) {
			t.Errorf("expected the needs-work line for a person:\n%s", human)
		}
		if !strings.Contains(human, "Open items:") {
			t.Errorf("expected an open-items line:\n%s", human)
		}
	})

	t.Run("--no-review-summary skips the timeline", func(t *testing.T) {
		// The flag exists because walking the timeline costs a request per
		// page. Skipping it must change where the counts come from, not remove
		// them -- and on 10.x there is nothing on the pull request payload to
		// fall back to, so the task tally is the whole of the cheap path.
		summary := liveReviewSummary(t, mustLiveCLI(t, "pr", "get", prID, "--no-review-summary"))

		if summary["countsSource"] != "blocker_comments" {
			t.Fatalf("countsSource = %#v, want blocker_comments", summary["countsSource"])
		}
		if summary["openTasks"] != float64(1) {
			t.Errorf("expected the exact open task count without the walk, got: %#v", summary)
		}
		if summary["actionRequired"] != true {
			t.Errorf("expected an open task to still require action: %#v", summary)
		}
	})

	t.Run("the comment listing marks what is outstanding", func(t *testing.T) {
		output := mustLiveHumanCLI(t, "pr", "comment", "list", prID)

		// The comment, the inline comment and the task; the reply belongs to the
		// comment's thread rather than making one of its own.
		if !strings.Contains(output, "3 unresolved") || !strings.Contains(output, "1 open task") {
			t.Errorf("expected the counts header, got:\n%s", output)
		}
		// "!" is how an unresolved thread is picked out of a long listing.
		if !strings.Contains(output, "! ["+commentID+"]") {
			t.Errorf("expected the unresolved comment to be marked, got:\n%s", output)
		}
		if !strings.Contains(output, "! ["+taskID+"]") || !strings.Contains(output, "(task") {
			t.Errorf("expected the task to be marked and labelled, got:\n%s", output)
		}
		if !strings.Contains(output, "this should handle nil") {
			t.Errorf("expected the comment body, got:\n%s", output)
		}
	})

	t.Run("--with-replies renders the reply", func(t *testing.T) {
		output := mustLiveHumanCLI(t, "pr", "comment", "list", prID, "--with-replies")
		if !strings.Contains(output, "fixed in abc123") {
			t.Errorf("expected the reply body, got:\n%s", output)
		}
	})

	t.Run("--full keeps the raw comment rendering", func(t *testing.T) {
		output := mustLiveHumanCLI(t, "pr", "comment", "list", prID, "--full")
		if !strings.Contains(output, "this should handle nil") {
			t.Errorf("expected the raw comment rendering, got:\n%s", output)
		}
	})

	t.Run("a filter matching nothing still reports the counts", func(t *testing.T) {
		// Nothing here is a pending review comment, so the filter is genuinely
		// empty rather than emptied for the test.
		output := mustLiveHumanCLI(t, "pr", "comment", "list", prID, "--state", "pending")

		if !strings.Contains(output, "unresolved") {
			t.Errorf("expected the pull request counts even when the filter matched nothing, got:\n%s", output)
		}
		if !strings.Contains(output, "No comments match the current filter") {
			t.Errorf("expected an empty-filter message, got:\n%s", output)
		}
	})

	t.Run("--blocker lists tasks through the blocker endpoint", func(t *testing.T) {
		data := decodeJSONMap(t, mustLiveCLI(t, "pr", "comment", "list", prID, "--blocker"))

		if data["source"] != "blocker_comments" {
			t.Fatalf("source = %#v, want blocker_comments", data["source"])
		}
		threads, _ := data["threads"].([]any)
		if len(threads) != 1 {
			t.Fatalf("expected one task thread, got: %#v", data["threads"])
		}
		first, _ := threads[0].(map[string]any)
		if first["kind"] != "task" {
			t.Fatalf("expected the blocker comment to map to a task, got: %#v", first)
		}
		if id, _ := numericOrStringID(first["id"]); id != taskID || first["text"] != "add a regression test" {
			t.Errorf("the task thread is %s reading %v, want %s reading the task as posted", id, first["text"], taskID)
		}
	})

	t.Run("conflicting state filters are rejected", func(t *testing.T) {
		if output, err := executeLiveCLI(t, "pr", "comment", "list", prID, "--unresolved", "--state", "resolved"); err == nil {
			t.Errorf("expected --unresolved and --state resolved to conflict, got:\n%s", output)
		}
		if output, err := executeLiveCLI(t, "pr", "comment", "list", prID, "--state", "nonsense"); err == nil {
			t.Errorf("expected an unknown state to be rejected, got:\n%s", output)
		}
	})

	t.Run("the listing carries the free counters and the resolved ones", func(t *testing.T) {
		// A declined pull request beside the open one. --state all has to bring
		// it in, which the default of open would not, so the listing shows the
		// state was sent.
		const declinedBranch = "feature/review-surfaces-declined"
		if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, declinedBranch, "declined.txt"); err != nil {
			t.Fatalf("push commit on branch failed: %v", err)
		}
		declinedID := createLivePRForRegression(t, declinedBranch, "Declined beside review surfaces", "--no-default-reviewers", "--no-codeowners")
		mustLiveCLI(t, "pr", "decline", declinedID)

		listed := prReviewEntries(t, decodeJSONMap(t, mustLiveCLI(t, "pr", "list", "--state", "all")), "pullRequests")
		if ids := prReviewIDs(listed); !slices.Equal(ids, prReviewSorted(prID, declinedID)) {
			t.Fatalf("pull requests = %v, want the open %s and the declined %s", ids, prID, declinedID)
		}
		for _, pullRequest := range listed {
			switch id, _ := numericOrStringID(pullRequest["id"]); id {
			case declinedID:
				if pullRequest["state"] != "DECLINED" || pullRequest["title"] != "Declined beside review surfaces" {
					t.Errorf("declined pull request = state %v, title %v", pullRequest["state"], pullRequest["title"])
				}
			case prID:
				// Bitbucket counts the reply as a comment and the task only as
				// a task: the comment, its reply and the inline comment make 3.
				if pullRequest["openTaskCount"] != float64(1) || pullRequest["commentCount"] != float64(3) {
					t.Errorf("counters = openTaskCount %v, commentCount %v; want 1 and 3", pullRequest["openTaskCount"], pullRequest["commentCount"])
				}
			}
		}

		// The counters in the plain listing ride along on the pull request
		// payload; --with-review-status pays for the timeline walk instead.
		output := mustLiveHumanCLI(t, "pr", "list", "--state", "all")
		if !strings.Contains(output, "tasks:1") || !strings.Contains(output, "comments:") {
			t.Errorf("expected the property counters in the listing, got:\n%s", output)
		}

		withStatus := mustLiveHumanCLI(t, "pr", "list", "--state", "all", "--with-review-status")
		if !strings.Contains(withStatus, "unresolved:3") || !strings.Contains(withStatus, "tasks:1") {
			t.Errorf("expected the resolved thread counts, got:\n%s", withStatus)
		}
	})
}

// TestLivePullRequestOutputMatchesDeclaredSchema validates what the pull
// request commands actually emit against the schema they publish through
// --describe.
//
// The mocked version of this validated the schema against a payload built from
// the same assumptions as the schema, which can only catch a formatter that
// contradicts itself. The drift worth catching is between the published
// contract and what a real server's data turns into, and that needs the real
// server.
func TestLivePullRequestOutputMatchesDeclaredSchema(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	const branch = "feature/declared-schema"
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "schema.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}
	prID := createLivePRForRegression(t, branch, "Declared schema", "--no-default-reviewers", "--no-codeowners")
	if title := prReviewPullRequest(t, prID)["title"]; title != "Declared schema" {
		t.Fatalf("title = %v, want Declared schema", title)
	}

	mustLiveCLI(t, "pr", "comment", "add", prID, "--text", "a plain comment")
	mustLiveCLI(t, "pr", "comment", "add", prID, "--text", "an inline comment",
		"--path", "schema.txt", "--line", "1")

	commentsPath := fmt.Sprintf("/rest/api/latest/projects/%s/repos/%s/pull-requests/%s/comments",
		seeded.Key, repo.Slug, prID)
	if _, err := harness.liveJSON(ctx, http.MethodPost, commentsPath,
		map[string]any{"text": "a task", "severity": "BLOCKER"}); err != nil {
		t.Fatalf("create the task failed: %v", err)
	}

	// A schema is satisfied by an empty list, so what the listings below are
	// validated against is read back first: three comments, each as posted.
	published := prReviewPublished(t, prID)
	if len(published) != 3 {
		t.Fatalf("published comments = %v, want the three posted", published)
	}
	for _, sent := range []struct {
		text, severity string
		anchored       bool
	}{
		{"a plain comment", "NORMAL", false},
		{"an inline comment", "NORMAL", true},
		{"a task", "BLOCKER", false},
	} {
		matching := prReviewWithText(published, sent.text)
		if len(matching) != 1 || matching[0]["severity"] != sent.severity {
			t.Fatalf("want one %s comment reading %q, got %v", sent.severity, sent.text, matching)
		}
		if sent.anchored {
			prReviewAssertAnchor(t, matching[0], "schema.txt", 1, "ADDED")
		} else if matching[0]["anchor"] != nil {
			t.Errorf("%q was posted without an anchor and stored one: %v", sent.text, matching[0]["anchor"])
		}
	}

	t.Run("pr get", func(t *testing.T) {
		validateAgainstDeclaredSchema(t, "pr get", mustLiveCLI(t, "pr", "get", prID))
	})

	// Skipping the timeline omits every thread count, which is a different
	// shape rather than a smaller one.
	t.Run("pr get without the review summary", func(t *testing.T) {
		validateAgainstDeclaredSchema(t, "pr get",
			mustLiveCLI(t, "pr", "get", prID, "--no-review-summary"))
	})

	for _, testCase := range []struct {
		name string
		args []string
		// only names the one thread a narrowed listing has to hold, so an
		// empty listing cannot pass for a valid one.
		only string
	}{
		{name: "default thread view"},
		{name: "unresolved filter", args: []string{"--unresolved"}},
		{name: "with replies", args: []string{"--with-replies"}},
		{name: "tasks only", args: []string{"--tasks-only"}, only: "a task"},
		{name: "path scoped", args: []string{"--path", "schema.txt"}, only: "an inline comment"},
		{name: "full comment list", args: []string{"--full"}},
		{name: "blocker comments", args: []string{"--blocker"}, only: "a task"},
	} {
		t.Run("pr comment list: "+testCase.name, func(t *testing.T) {
			args := append([]string{"pr", "comment", "list", prID}, testCase.args...)
			output := mustLiveCLI(t, args...)
			validateAgainstDeclaredSchema(t, "pr comment list", output)

			if testCase.only == "" {
				return
			}
			threads, _ := prReviewThreadList(t, output)
			if len(threads) != 1 || threads[0]["text"] != testCase.only {
				t.Fatalf("threads = %v, want only %q", threads, testCase.only)
			}
		})
	}

	// --full adds the ungrouped comment list; it does not swap one payload for
	// another. The previous contract made the two mutually exclusive, so one
	// command had two shapes and a consumer needed to know which flag had been
	// passed to know what it was reading.
	t.Run("--full adds comments without removing threads", func(t *testing.T) {
		data := decodeJSONMap(t, mustLiveCLI(t, "pr", "comment", "list", prID, "--full"))

		comments, _ := data["comments"].([]any)
		if len(comments) == 0 {
			t.Fatalf("expected --full to carry the ungrouped comments, got: %#v", data)
		}
		threads, _ := data["threads"].([]any)
		if len(threads) == 0 {
			t.Fatalf("expected --full to keep the threads, got: %#v", data)
		}

		commentTexts := prReviewTexts(t, data, "comments")
		threadTexts := prReviewTexts(t, data, "threads")
		slices.Sort(commentTexts)
		slices.Sort(threadTexts)
		want := []string{"a plain comment", "a task", "an inline comment"}
		if !slices.Equal(commentTexts, want) || !slices.Equal(threadTexts, want) {
			t.Errorf("comments = %q, threads = %q; want both to be %q", commentTexts, threadTexts, want)
		}
	})
}

// validateAgainstDeclaredSchema compiles the schema a command declares -- the
// same one --describe publishes -- and validates a real invocation's payload
// against it.
func validateAgainstDeclaredSchema(t *testing.T, commandPath, output string) {
	t.Helper()

	declared, ok := result.SchemaFor(commandPath)
	if !ok {
		t.Fatalf("no schema is declared for %q", commandPath)
	}

	encoded, err := json.Marshal(declared)
	if err != nil {
		t.Fatalf("encode declared schema: %v", err)
	}
	var schemaMap map[string]any
	if err := json.Unmarshal(encoded, &schemaMap); err != nil {
		t.Fatalf("decode declared schema: %v", err)
	}

	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource(commandPath, schemaMap); err != nil {
		t.Fatalf("add schema resource: %v", err)
	}
	schema, err := compiler.Compile(commandPath)
	if err != nil {
		t.Fatalf("compile schema: %v", err)
	}

	var envelope struct {
		Data any `json:"data"`
	}
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		t.Fatalf("decode command output: %v\noutput: %s", err, output)
	}

	if err := schema.Validate(envelope.Data); err != nil {
		t.Fatalf("%s output does not match its declared schema: %v\noutput: %s", commandPath, err, output)
	}
}

// liveReviewSummary pulls the review summary out of a `pr get` payload.
func liveReviewSummary(t *testing.T, output string) map[string]any {
	t.Helper()

	summary, ok := decodeJSONMap(t, output)["reviewSummary"].(map[string]any)
	if !ok {
		t.Fatalf("no reviewSummary in the pull request payload:\n%s", output)
	}

	return summary
}
