//go:build live

package live_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// viewPayloadKey is where show puts what its view draws (ADR-101).
const viewPayloadKey = "io.github.vriesdemichael.bb/view"

// TestLiveMCPShowCarriesWhatItsViewDraws: in a client that renders views, show
// answers with what its view draws, read from Bitbucket through bb: the pull
// request and its reviewers' decisions, the builds on its source commit, the
// avatars as images, the diff, and the list with each pull request's build
// counts. A client that renders none is told nothing was shown.
func TestLiveMCPShowCarriesWhatItsViewDraws(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	repoRef := seeded.Key + "/" + repo.Slug
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	reviewer, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create a reviewer failed: %v", err)
	}
	if err := harness.grantRepoPermission(ctx, seeded.Key, repo.Slug, reviewer.Username,
		openapigenerated.SetPermissionForUserParamsPermissionREPOREAD); err != nil {
		t.Fatalf("grant the reviewer read access failed: %v", err)
	}

	branch := testsupport.UniqueName("feature/shown-")
	if err := harness.pushFileOnBranch(seeded.Key, repo.Slug, branch, "shown.txt", "shown in a view\n"); err != nil {
		t.Fatalf("push failed: %v", err)
	}
	title := testsupport.UniqueName("Shown in a view ")
	created := extractPRData(decodeJSONMap(t, mustLiveCLI(t, "pr", "create",
		"--from-ref", branch, "--to-ref", "refs/heads/master", "--title", title,
		"--reviewers", reviewer.Username, "--no-default-reviewers", "--no-codeowners")))
	id := fmt.Sprint(created["id"])

	path := fmt.Sprintf("/rest/api/latest/projects/%s/repos/%s/pull-requests/%s", seeded.Key, repo.Slug, id)
	if _, err := harness.liveJSONAs(ctx, reviewer, http.MethodPut, path+"/participants/"+reviewer.Username,
		map[string]any{"user": map[string]any{"name": reviewer.Username}, "status": "APPROVED"}); err != nil {
		t.Fatalf("the reviewer's approval failed: %v", err)
	}
	commit := asString(mcpLivePullRequest(t, repoRef, id)["sourceCommit"])
	buildKey := testsupport.UniqueName("shown-build-")
	if _, err := harness.liveJSON(ctx, http.MethodPost, "/rest/build-status/latest/commits/"+commit, map[string]any{
		"state": "FAILED", "key": buildKey, "name": "Shown build", "url": "https://ci.example.com/shown",
	}); err != nil {
		t.Fatalf("post a build status failed: %v", err)
	}

	capabilities := &mcp.ClientCapabilities{}
	capabilities.AddExtension("io.modelcontextprotocol/ui", map[string]any{"mimeTypes": []string{"text/html;profile=mcp-app"}})
	executeLiveMCPServerAs(t, &mcp.ClientOptions{Capabilities: capabilities}, func(session *mcp.ClientSession) {
		show := func(arguments map[string]any) map[string]any {
			t.Helper()
			result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "show", Arguments: arguments})
			if err != nil || result.IsError {
				t.Fatalf("show %v: %v %s", arguments, err, mcpResultText(result))
			}
			payload, _ := result.Meta[viewPayloadKey].(map[string]any)
			if payload == nil {
				t.Fatalf("show %v carries no view payload: %v", arguments, result.Meta)
			}
			return payload
		}

		card := show(map[string]any{"kind": "pull_request", "project": seeded.Key, "repo": repo.Slug, "id": id})
		pr, _ := card["pull_request"].(map[string]any)
		if pr["title"] != title {
			t.Errorf("the card shows %q, want %q", pr["title"], title)
		}
		reviewers, _ := pr["reviewers"].([]any)
		if len(reviewers) != 1 || asString(reviewers[0].(map[string]any)["status"]) != "APPROVED" {
			t.Errorf("the card shows reviewers %v, want %s approved", reviewers, reviewer.Username)
		}
		checks, _ := pr["checks"].([]any)
		if len(checks) != 1 || asString(checks[0].(map[string]any)["key"]) != buildKey || asString(checks[0].(map[string]any)["state"]) != "FAILED" {
			t.Errorf("the card shows builds %v, want %s failed", checks, buildKey)
		}
		avatars, _ := card["avatars"].(map[string]any)
		for _, person := range []string{asString(pr["author_username"]), reviewer.Username} {
			if !strings.HasPrefix(asString(avatars[person]), "data:image/") {
				t.Errorf("the card has no avatar image for %s: %.40q", person, asString(avatars[person]))
			}
		}

		diff := show(map[string]any{"kind": "diff", "project": seeded.Key, "repo": repo.Slug, "id": id})
		patch := asString(diff["diff"].(map[string]any)["patch"])
		if !strings.Contains(patch, "shown.txt") || !strings.Contains(patch, "+shown in a view") {
			t.Errorf("the diff does not carry the change:\n%s", patch)
		}

		list := show(map[string]any{"kind": "pull_requests", "project": seeded.Key, "repo": repo.Slug})
		listed, _ := list["pull_requests"].([]any)
		var found map[string]any
		for _, item := range listed {
			if candidate, _ := item.(map[string]any); fmt.Sprint(candidate["id"]) == id {
				found = candidate
			}
		}
		if found == nil {
			t.Fatalf("the list does not show #%s: %v", id, listed)
		}
		if counts, _ := found["check_counts"].(map[string]any); counts["failed"] != float64(1) {
			t.Errorf("the list counts builds %v for #%s, want one failed", found["check_counts"], id)
		}
	}, "ai", "mcp", "serve")

	executeLiveMCPServer(t, func(session *mcp.ClientSession) {
		result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "show",
			Arguments: map[string]any{"kind": "pull_request", "project": seeded.Key, "repo": repo.Slug, "id": id}})
		if err != nil || result.IsError {
			t.Fatalf("show in a client without views: %v %s", err, mcpResultText(result))
		}
		if !strings.Contains(mcpResultText(result), "displays no views") || result.Meta[viewPayloadKey] != nil {
			t.Errorf("a client without views got %q with payload %v", mcpResultText(result), result.Meta[viewPayloadKey] != nil)
		}
	}, "ai", "mcp", "serve")
}

// TestLiveMCPShowCountsEveryBuild: a pull request whose commit has more
// builds than a card lists. The card counts them all, as Bitbucket totals
// them, and the builds it lists keep the ones that failed, though they are
// the oldest, which Bitbucket's default newest-first order would leave out.
// The model is told the same counts.
func TestLiveMCPShowCountsEveryBuild(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	repoRef := seeded.Key + "/" + repo.Slug
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	branch := testsupport.UniqueName("feature/many-builds-")
	if err := harness.pushFileOnBranch(seeded.Key, repo.Slug, branch, "built.txt", "built many times\n"); err != nil {
		t.Fatalf("push failed: %v", err)
	}
	created := extractPRData(decodeJSONMap(t, mustLiveCLI(t, "pr", "create",
		"--from-ref", branch, "--to-ref", "refs/heads/master", "--title", testsupport.UniqueName("Built many times "),
		"--no-default-reviewers", "--no-codeowners")))
	id := fmt.Sprint(created["id"])
	commit := asString(mcpLivePullRequest(t, repoRef, id)["sourceCommit"])

	// 120 builds, posted oldest first: four that failed, then one each that
	// runs, was canceled and has no known state, then the passes.
	states := []string{"FAILED", "FAILED", "FAILED", "FAILED", "INPROGRESS", "CANCELLED", "UNKNOWN"}
	for len(states) < 120 {
		states = append(states, "SUCCESSFUL")
	}
	failed := map[string]bool{}
	for i, state := range states {
		key := fmt.Sprintf("build-%03d", i)
		if state == "FAILED" {
			failed[key] = true
		}
		if _, err := harness.liveJSON(ctx, http.MethodPost, "/rest/build-status/latest/commits/"+commit, map[string]any{
			"state": state, "key": key, "name": "Build " + key, "url": "https://ci.example.com/" + key,
		}); err != nil {
			t.Fatalf("post build %s failed: %v", key, err)
		}
	}
	stats, err := harness.liveJSON(ctx, http.MethodGet, "/rest/build-status/latest/commits/stats/"+commit, nil)
	if err != nil {
		t.Fatalf("read the build totals back failed: %v", err)
	}
	if stats["failed"] != float64(4) || stats["successful"] != float64(113) || stats["unknown"] != float64(1) {
		t.Fatalf("Bitbucket holds %v for the commit, want the 120 builds posted", stats)
	}

	capabilities := &mcp.ClientCapabilities{}
	capabilities.AddExtension("io.modelcontextprotocol/ui", map[string]any{"mimeTypes": []string{"text/html;profile=mcp-app"}})
	executeLiveMCPServerAs(t, &mcp.ClientOptions{Capabilities: capabilities}, func(session *mcp.ClientSession) {
		result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "show",
			Arguments: map[string]any{"kind": "pull_request", "project": seeded.Key, "repo": repo.Slug, "id": id}})
		if err != nil || result.IsError {
			t.Fatalf("show: %v %s", err, mcpResultText(result))
		}
		payload, _ := result.Meta[viewPayloadKey].(map[string]any)
		pr, _ := payload["pull_request"].(map[string]any)

		counts, _ := pr["check_counts"].(map[string]any)
		want := map[string]float64{"failed": 4, "in_progress": 1, "cancelled": 1, "unknown": 1, "successful": 113}
		for field, count := range want {
			if counts[field] != count {
				t.Errorf("the card counts %v builds %s, want %v: %v", counts[field], field, count, counts)
			}
		}

		checks, _ := pr["checks"].([]any)
		listedFailed := 0
		for _, check := range checks {
			if failed[asString(check.(map[string]any)["key"])] {
				listedFailed++
			}
		}
		if len(checks) != 100 || listedFailed != 4 || pr["checks_limit_reached"] != true {
			t.Errorf("the card lists %d builds, %d of the 4 that failed, limit reached %v; want 100, all 4, and the limit said",
				len(checks), listedFailed, pr["checks_limit_reached"])
		}

		if text := mcpResultText(result); !strings.Contains(text, "Builds: 4 failed, 1 in progress, 1 canceled, 1 unknown, 113 passed.") {
			t.Errorf("the model is told %q, want every build counted", text)
		}
	}, "ai", "mcp", "serve")
}

// TestLiveMCPShowThreadsCarriesEveryThreadWhereItIs: show's threads view
// carries a pull request's threads as Bitbucket has them: a comment on a
// changed line with the lines of the diff that lead to it and its reply, a
// task, and a thread resolved as Bitbucket's UI resolves one, counted as a
// whole, with the avatars of the people who wrote them.
func TestLiveMCPShowThreadsCarriesEveryThreadWhereItIs(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	branch := testsupport.UniqueName("feature/threads-")
	if err := harness.pushFileOnBranch(seeded.Key, repo.Slug, branch, "threads.txt", "one\ntwo\nthree\nfour\nfive\n"); err != nil {
		t.Fatalf("push failed: %v", err)
	}
	created := extractPRData(decodeJSONMap(t, mustLiveCLI(t, "pr", "create",
		"--from-ref", branch, "--to-ref", "refs/heads/master", "--title", testsupport.UniqueName("Threads "),
		"--no-default-reviewers", "--no-codeowners")))
	id := fmt.Sprint(created["id"])
	comments := fmt.Sprintf("/rest/api/latest/projects/%s/repos/%s/pull-requests/%s/comments", seeded.Key, repo.Slug, id)
	post := func(body map[string]any) map[string]any {
		t.Helper()
		out, err := harness.liveJSON(ctx, http.MethodPost, comments, body)
		if err != nil {
			t.Fatalf("post comment %v failed: %v", body["text"], err)
		}
		return out
	}

	inline := post(map[string]any{"text": "on line four",
		"anchor": map[string]any{"path": "threads.txt", "line": 4, "lineType": "ADDED", "fileType": "TO", "diffType": "EFFECTIVE"}})
	post(map[string]any{"text": "a reply", "parent": map[string]any{"id": inline["id"]}})
	post(map[string]any{"text": "a task", "severity": "BLOCKER"})
	resolved := post(map[string]any{"text": "resolved"})
	if _, err := harness.liveJSON(ctx, http.MethodPut, fmt.Sprintf("%s/%v", comments, resolved["id"]),
		map[string]any{"version": resolved["version"], "threadResolved": true}); err != nil {
		t.Fatalf("resolve the thread failed: %v", err)
	}

	capabilities := &mcp.ClientCapabilities{}
	capabilities.AddExtension("io.modelcontextprotocol/ui", map[string]any{"mimeTypes": []string{"text/html;profile=mcp-app"}})
	executeLiveMCPServerAs(t, &mcp.ClientOptions{Capabilities: capabilities}, func(session *mcp.ClientSession) {
		result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "show",
			Arguments: map[string]any{"kind": "threads", "project": seeded.Key, "repo": repo.Slug, "id": id}})
		if err != nil || result.IsError {
			t.Fatalf("show threads: %v %s", err, mcpResultText(result))
		}
		payload, _ := result.Meta[viewPayloadKey].(map[string]any)
		threads, _ := payload["threads"].(map[string]any)
		if threads == nil {
			t.Fatalf("show threads carries no threads: %v", result.Meta)
		}

		summary, _ := threads["summary"].(map[string]any)
		for field, want := range map[string]float64{"total_threads": 3, "unresolved": 2, "resolved": 1, "open_tasks": 1} {
			if summary[field] != want {
				t.Errorf("the summary counts %s %v, want %v: %v", field, summary[field], want, summary)
			}
		}
		if text := mcpResultText(result); !strings.Contains(text, "2 unresolved (1 of them open tasks), 1 resolved") {
			t.Errorf("the model reads %q, want the counts", text)
		}

		byText := map[string]map[string]any{}
		listed, _ := threads["threads"].([]any)
		for _, item := range listed {
			thread, _ := item.(map[string]any)
			byText[asString(thread["text"])] = thread
		}
		if len(byText) != 3 {
			t.Fatalf("the view carries threads %v, want the three", listed)
		}

		onLine := byText["on line four"]
		anchor, _ := onLine["anchor"].(map[string]any)
		if anchor["path"] != "threads.txt" || anchor["line"] != float64(4) {
			t.Errorf("the comment on line four is anchored at %v", anchor)
		}
		context, _ := onLine["context"].([]any)
		if len(context) == 0 {
			t.Fatalf("the comment on line four carries no diff: %v", onLine)
		}
		last, _ := context[len(context)-1].(map[string]any)
		if last["text"] != "four" || last["type"] != "add" || last["new"] != float64(4) || last["anchor"] != true {
			t.Errorf("the diff leading to line four ends %v, want the added line four, marked", last)
		}
		replies, _ := onLine["replies"].([]any)
		if len(replies) != 1 || asString(replies[0].(map[string]any)["text"]) != "a reply" {
			t.Errorf("the comment on line four carries replies %v, want the one", replies)
		}
		if onLine["author_username"] != harness.username() {
			t.Errorf("the comment on line four is by %v, want %s", onLine["author_username"], harness.username())
		}
		if task := byText["a task"]; task["task"] != true || task["resolved"] == true {
			t.Errorf("the task reads %v, want an open task", task)
		}
		if byText["resolved"]["resolved"] != true {
			t.Errorf("the resolved thread reads %v, want it resolved", byText["resolved"])
		}

		avatars, _ := payload["avatars"].(map[string]any)
		if !strings.HasPrefix(asString(avatars[harness.username()]), "data:image/") {
			t.Errorf("the threads view has no avatar image for %s: %.40q", harness.username(), asString(avatars[harness.username()]))
		}
	}, "ai", "mcp", "serve")
}

// TestLiveMCPRefreshViewSendsTheDataOnlyWhenItChanged: a view that asks again
// about the same state is told nothing changed, for every kind, and one that
// asks after a comment is sent the data with it, although Bitbucket leaves the
// pull request's version as it was.
func TestLiveMCPRefreshViewSendsTheDataOnlyWhenItChanged(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	branch := testsupport.UniqueName("feature/refresh-")
	if err := harness.pushFileOnBranch(seeded.Key, repo.Slug, branch, "refresh.txt", "one\ntwo\n"); err != nil {
		t.Fatalf("push failed: %v", err)
	}
	created := extractPRData(decodeJSONMap(t, mustLiveCLI(t, "pr", "create",
		"--from-ref", branch, "--to-ref", "refs/heads/master", "--title", testsupport.UniqueName("Refresh "),
		"--no-default-reviewers", "--no-codeowners")))
	id := fmt.Sprint(created["id"])

	capabilities := &mcp.ClientCapabilities{}
	capabilities.AddExtension("io.modelcontextprotocol/ui", map[string]any{"mimeTypes": []string{"text/html;profile=mcp-app"}})
	executeLiveMCPServerAs(t, &mcp.ClientOptions{Capabilities: capabilities}, func(session *mcp.ClientSession) {
		show := func(arguments map[string]any) map[string]any {
			t.Helper()
			result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "show", Arguments: arguments})
			if err != nil || result.IsError {
				t.Fatalf("show %v: %v %s", arguments, err, mcpResultText(result))
			}
			payload, _ := result.Meta[viewPayloadKey].(map[string]any)
			if payload == nil || asString(payload["fingerprint"]) == "" || payload["show"] == nil {
				t.Fatalf("show %v carries no fingerprint or call to refresh with: %v", arguments, payload)
			}
			return payload
		}
		refresh := func(payload map[string]any, since string) (*mcp.CallToolResult, map[string]any) {
			t.Helper()
			arguments := map[string]any{"since": since}
			call, _ := payload["show"].(map[string]any)
			for key, value := range call {
				arguments[key] = value
			}
			result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "refresh_view", Arguments: arguments})
			if err != nil || result.IsError {
				t.Fatalf("refresh_view %v: %v %s", arguments, err, mcpResultText(result))
			}
			answer, _ := result.StructuredContent.(map[string]any)
			return result, answer
		}

		for _, arguments := range []map[string]any{
			{"kind": "pull_request", "project": seeded.Key, "repo": repo.Slug, "id": id},
			{"kind": "pull_requests", "project": seeded.Key, "repo": repo.Slug},
			{"kind": "diff", "project": seeded.Key, "repo": repo.Slug, "id": id},
			{"kind": "threads", "project": seeded.Key, "repo": repo.Slug, "id": id},
		} {
			payload := show(arguments)
			fingerprint := asString(payload["fingerprint"])
			result, answer := refresh(payload, fingerprint)
			if answer["changed"] != false || answer["fingerprint"] != fingerprint || result.Meta[viewPayloadKey] != nil {
				t.Errorf("%s: asked again about the same state, refresh_view answered changed=%v, fingerprint %v (the view has %s), data sent: %v",
					arguments["kind"], answer["changed"], answer["fingerprint"], fingerprint, result.Meta[viewPayloadKey] != nil)
			}
			if asString(answer["generated_at"]) == "" {
				t.Errorf("%s: refresh_view does not say when it read the data: %v", arguments["kind"], answer)
			}
		}

		threads := show(map[string]any{"kind": "threads", "project": seeded.Key, "repo": repo.Slug, "id": id})
		before := asString(threads["fingerprint"])
		comments := fmt.Sprintf("/rest/api/latest/projects/%s/repos/%s/pull-requests/%s/comments", seeded.Key, repo.Slug, id)
		if _, err := harness.liveJSON(ctx, http.MethodPost, comments, map[string]any{"text": "a comment after the view was drawn"}); err != nil {
			t.Fatalf("post a comment failed: %v", err)
		}

		result, answer := refresh(threads, before)
		payload, _ := result.Meta[viewPayloadKey].(map[string]any)
		if answer["changed"] != true || payload == nil {
			t.Fatalf("after a comment refresh_view answered changed=%v with data %v, want the data", answer["changed"], payload != nil)
		}
		if after := asString(payload["fingerprint"]); after == before || after != answer["fingerprint"] {
			t.Errorf("the new data has fingerprint %q, the answer %v, the view had %q; want a new one, the same in both", after, answer["fingerprint"], before)
		}
		carried, _ := payload["threads"].(map[string]any)
		listed, _ := carried["threads"].([]any)
		if len(listed) != 1 || asString(listed[0].(map[string]any)["text"]) != "a comment after the view was drawn" {
			t.Errorf("the new data carries threads %v, want the comment", listed)
		}
		if text := mcpResultText(result); !strings.Contains(text, "has changed") || !strings.Contains(text, "1 unresolved") {
			t.Errorf("the model would be told %q, want the change and the count", text)
		}

		if _, again := refresh(payload, asString(payload["fingerprint"])); again["changed"] != false {
			t.Errorf("asked again with the new fingerprint, refresh_view answered changed=%v", again["changed"])
		}

		// A view opening something it does not hold yet, such as a card
		// opening its diff, is sent the data, with what the server lets the
		// view do there; the model is told what the person opened.
		opened, answer := refresh(map[string]any{"show": map[string]any{"kind": "diff", "project": seeded.Key, "repo": repo.Slug, "id": id}}, "")
		diff, _ := opened.Meta[viewPayloadKey].(map[string]any)
		if answer["changed"] != true || diff == nil || diff["kind"] != "diff" {
			t.Fatalf("opening the diff answered changed=%v with %v, want the diff", answer["changed"], diff)
		}
		if text := mcpResultText(opened); !strings.HasPrefix(text, "The person opened the diff of ") {
			t.Errorf("the model would be told %q, want what the person opened", text)
		}
		offers, _ := diff["offers"].(map[string]any)
		kinds, _ := offers["kinds"].([]any)
		tools, _ := offers["tools"].([]any)
		if len(kinds) != 4 || !containsAny(tools, "add_pr_comment") || !containsAny(tools, "submit_pr_review") {
			t.Errorf("a server with every tool offers its views %v, want every kind and the actions", offers)
		}
	}, "ai", "mcp", "serve")
}

func containsAny(values []any, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
