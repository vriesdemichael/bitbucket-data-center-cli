//go:build live

package live_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
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
		// bb acts as the harness's user, who opened the pull request, so the
		// card offers them no review of their own.
		if me, _ := card["me"].(map[string]any); !strings.EqualFold(asString(me["username"]), harness.username()) || me["author"] != true {
			t.Errorf("the card sees bb's user as %v, want %s, the author", card["me"], harness.username())
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

// TestLiveMCPShowPutsCommentsWhereBitbucketDoes: a pull request's comments
// are where Bitbucket's web interface has them. In its diff, each is on the
// line Bitbucket's own diff with comments draws it on, a line far from any
// change and a comment moved by a later push included, and the diff has that
// line; in its overview, the activity lists them newest first, a comment on a
// line with the lines Bitbucket's activity shows it among, beside who opened
// and updated the pull request.
func TestLiveMCPShowPutsCommentsWhereBitbucketDoes(t *testing.T) {
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

	// ledger.txt has forty lines on master. The pull request changes line 20
	// and adds notes.txt.
	lines := make([]string, 40)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i+1)
	}
	if err := harness.pushFileOnBranch(seeded.Key, repo.Slug, "master", "ledger.txt", strings.Join(lines, "\n")+"\n"); err != nil {
		t.Fatalf("push ledger.txt to master failed: %v", err)
	}
	branch := testsupport.UniqueName("feature/comments-")
	changed := append([]string(nil), lines...)
	changed[19] = "line twenty"
	if err := harness.pushFilesOnBranch(seeded.Key, repo.Slug, branch, map[string][]byte{
		"ledger.txt": []byte(strings.Join(changed, "\n") + "\n"),
		"notes.txt":  []byte("one\ntwo\nthree\nfour\nfive\n"),
	}); err != nil {
		t.Fatalf("push failed: %v", err)
	}
	created := extractPRData(decodeJSONMap(t, mustLiveCLI(t, "pr", "create",
		"--from-ref", branch, "--to-ref", "refs/heads/master", "--title", testsupport.UniqueName("Comments "),
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
	anchor := func(path string, line int, lineType, fileType string) map[string]any {
		return map[string]any{"path": path, "line": line, "lineType": lineType, "fileType": fileType, "diffType": "EFFECTIVE"}
	}
	onAdded := post(map[string]any{"text": "on notes line four", "anchor": anchor("notes.txt", 4, "ADDED", "TO")})
	post(map[string]any{"text": "a reply", "parent": map[string]any{"id": onAdded["id"]}})
	post(map[string]any{"text": "on the removed line twenty", "anchor": anchor("ledger.txt", 20, "REMOVED", "FROM")})
	farAway := post(map[string]any{"text": "far from the change", "anchor": anchor("ledger.txt", 2, "CONTEXT", "TO")})
	moved := post(map[string]any{"text": "on the new line twenty", "anchor": anchor("ledger.txt", 20, "ADDED", "TO")})
	post(map[string]any{"text": "on the file", "anchor": map[string]any{"path": "notes.txt", "diffType": "EFFECTIVE"}})
	post(map[string]any{"text": "a task", "severity": "BLOCKER"})

	// A later push puts three lines above line twenty, which Bitbucket follows
	// the comment on it to.
	shifted := append(append(append([]string(nil), changed[:19]...), "new a", "new b", "new c"), changed[19:]...)
	if err := pushOnTop(t, harness, seeded.Key, repo.Slug, branch, "ledger.txt", strings.Join(shifted, "\n")+"\n"); err != nil {
		t.Fatalf("second push failed: %v", err)
	}
	// An unchanged line commented on as the file was, as the left side of
	// Bitbucket's side-by-side diff does: line 25 then is line 28 now, and its
	// anchor names 25.
	fromSide := post(map[string]any{"text": "on line 25 as it was", "anchor": anchor("ledger.txt", 25, "CONTEXT", "FROM")})

	// Where Bitbucket's own diff with comments draws each comment, read
	// straight from Bitbucket, by the comment's ID.
	bitbucketPlaces := func(path string) map[string]string {
		t.Helper()
		out, err := harness.liveJSON(ctx, http.MethodGet, fmt.Sprintf("/rest/api/latest/projects/%s/repos/%s/pull-requests/%s/diff/%s?withComments=true",
			seeded.Key, repo.Slug, id, path), nil)
		if err != nil {
			t.Fatalf("read Bitbucket's diff of %s failed: %v", path, err)
		}
		places := map[string]string{}
		diffs, _ := out["diffs"].([]any)
		for _, entry := range diffs {
			diff, _ := entry.(map[string]any)
			fileComments, _ := diff["fileComments"].([]any)
			for _, comment := range fileComments {
				places[fmt.Sprint(comment.(map[string]any)["id"])] = "file"
			}
			hunks, _ := diff["hunks"].([]any)
			for _, entry := range hunks {
				segments, _ := entry.(map[string]any)["segments"].([]any)
				for _, entry := range segments {
					segment, _ := entry.(map[string]any)
					rows, _ := segment["lines"].([]any)
					for _, entry := range rows {
						row, _ := entry.(map[string]any)
						ids, _ := row["commentIds"].([]any)
						for _, commentID := range ids {
							if segment["type"] == "REMOVED" {
								places[fmt.Sprint(commentID)] = fmt.Sprintf("old:%v", row["source"])
							} else {
								places[fmt.Sprintf("%v", commentID)] = fmt.Sprintf("new:%v", row["destination"])
							}
						}
					}
				}
			}
		}
		return places
	}
	// Bitbucket follows a comment through a push as it reads the diff; it is
	// waited for, so the check below sees where Bitbucket put it.
	movedID := fmt.Sprint(moved["id"])
	var want map[string]string
	deadline := time.Now().Add(time.Minute)
	for {
		want = bitbucketPlaces("ledger.txt")
		for commentID, place := range bitbucketPlaces("notes.txt") {
			want[commentID] = place
		}
		if want[movedID] == "new:23" || time.Now().After(deadline) {
			break
		}
		time.Sleep(2 * time.Second)
	}
	if want[movedID] != "new:23" {
		t.Fatalf("Bitbucket draws the comment on the new line twenty at %q after the push, want new:23; places: %v", want[movedID], want)
	}
	if fromID := fmt.Sprint(fromSide["id"]); want[fromID] != "new:28" {
		t.Fatalf("Bitbucket draws the comment on line 25 as it was at %q, want new:28, the line it is now; places: %v", want[fromID], want)
	}

	capabilities := &mcp.ClientCapabilities{}
	capabilities.AddExtension("io.modelcontextprotocol/ui", map[string]any{"mimeTypes": []string{"text/html;profile=mcp-app"}})
	executeLiveMCPServerAs(t, &mcp.ClientOptions{Capabilities: capabilities}, func(session *mcp.ClientSession) {
		show := func(kind string) (*mcp.CallToolResult, map[string]any) {
			t.Helper()
			result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "show",
				Arguments: map[string]any{"kind": kind, "project": seeded.Key, "repo": repo.Slug, "id": id}})
			if err != nil || result.IsError {
				t.Fatalf("show %s: %v %s", kind, err, mcpResultText(result))
			}
			payload, _ := result.Meta[viewPayloadKey].(map[string]any)
			return result, payload
		}

		// The diff: each thread where Bitbucket's diff draws it, and the line
		// it is on in the patch the view draws.
		_, diff := show("diff")
		carried, _ := diff["threads"].(map[string]any)
		threads, _ := carried["threads"].([]any)
		got := map[string]string{}
		for _, entry := range threads {
			thread, _ := entry.(map[string]any)
			got[fmt.Sprint(thread["id"])] = asString(thread["place"])
		}
		for commentID, place := range want {
			if got[commentID] != place {
				t.Errorf("comment %s is placed at %q, where Bitbucket's diff draws it at %q", commentID, got[commentID], place)
			}
		}
		// A comment on a line outside the diff's hunks is not in Bitbucket's
		// diff, and not in the view's: it is in the activity.
		farID := fmt.Sprint(farAway["id"])
		if _, drawnThere := want[farID]; drawnThere || got[farID] != "" {
			t.Errorf("the comment far from the change is drawn at %q by Bitbucket and %q by the view, want neither", want[farID], got[farID])
		}
		drawn := patchLineKeys(asString(diff["diff"].(map[string]any)["patch"]))
		for _, entry := range threads {
			thread, _ := entry.(map[string]any)
			place := asString(thread["place"])
			anchor, _ := thread["anchor"].(map[string]any)
			path := asString(anchor["path"])
			if strings.Contains(place, ":") && !drawn[path+"|"+place] {
				t.Errorf("comment %v is placed at %s of %s, a line the view's patch does not have", thread["id"], place, path)
			}
		}

		// The overview: the activity, newest first, from Bitbucket's timeline.
		result, card := show("pull_request")
		activity, _ := card["activity"].(map[string]any)
		items, _ := activity["items"].([]any)
		var actions []string
		var previous float64
		for i, entry := range items {
			item, _ := entry.(map[string]any)
			actions = append(actions, asString(item["action"]))
			date, _ := item["date"].(float64)
			if i > 0 && date > previous {
				t.Errorf("item %d is newer than the one before it: %v", i, items)
			}
			previous = date
		}
		counted := map[string]int{}
		for _, action := range actions {
			counted[action]++
		}
		if counted["COMMENTED"] != 7 || counted["RESCOPED"] != 1 || counted["OPENED"] != 1 || activity["total"] != float64(len(items)) {
			t.Errorf("the activity is %v of %v, want the seven threads, the push and the opening, all carried", actions, activity["total"])
		}

		// The comment on notes.txt's line four comes with the lines
		// Bitbucket's activity shows it among, that line marked, and its
		// reply.
		timeline, err := harness.liveJSON(ctx, http.MethodGet, fmt.Sprintf("/rest/api/latest/projects/%s/repos/%s/pull-requests/%s/activities?limit=100", seeded.Key, repo.Slug, id), nil)
		if err != nil {
			t.Fatalf("read the activities failed: %v", err)
		}
		var bitbucketLines []string
		values, _ := timeline["values"].([]any)
		for _, entry := range values {
			value, _ := entry.(map[string]any)
			comment, _ := value["comment"].(map[string]any)
			if value["action"] != "COMMENTED" || fmt.Sprint(comment["id"]) != fmt.Sprint(onAdded["id"]) {
				continue
			}
			hunks, _ := value["diff"].(map[string]any)["hunks"].([]any)
			for _, entry := range hunks {
				segments, _ := entry.(map[string]any)["segments"].([]any)
				for _, entry := range segments {
					rows, _ := entry.(map[string]any)["lines"].([]any)
					for _, row := range rows {
						bitbucketLines = append(bitbucketLines, asString(row.(map[string]any)["line"]))
					}
				}
			}
		}
		var onLine map[string]any
		for _, entry := range items {
			thread, _ := entry.(map[string]any)["thread"].(map[string]any)
			if fmt.Sprint(thread["id"]) == fmt.Sprint(onAdded["id"]) {
				onLine = thread
			}
		}
		if onLine == nil {
			t.Fatalf("the activity has no comment on notes.txt's line four: %v", items)
		}
		context, _ := onLine["context"].([]any)
		var ours, marked []string
		for _, entry := range context {
			line, _ := entry.(map[string]any)
			ours = append(ours, asString(line["text"]))
			if line["anchor"] == true {
				marked = append(marked, fmt.Sprintf("%v %v %v", line["type"], line["new"], line["text"]))
			}
		}
		if len(bitbucketLines) == 0 || strings.Join(ours, "|") != strings.Join(bitbucketLines, "|") {
			t.Errorf("the comment is shown among %q, where Bitbucket's activity shows %q", ours, bitbucketLines)
		}
		if strings.Join(marked, "|") != "add 4 four" {
			t.Errorf("the lines mark %q, want the added line four alone", marked)
		}
		if replies, _ := onLine["replies"].([]any); len(replies) != 1 || asString(replies[0].(map[string]any)["text"]) != "a reply" {
			t.Errorf("the comment carries replies %v, want the one", onLine["replies"])
		}

		avatars, _ := card["avatars"].(map[string]any)
		if !strings.HasPrefix(asString(avatars[harness.username()]), "data:image/") {
			t.Errorf("the overview has no avatar image for %s: %.40q", harness.username(), asString(avatars[harness.username()]))
		}
		if text := mcpResultText(result); !strings.Contains(text, "Comments: 7 unresolved (1 of them open tasks), 0 resolved.") {
			t.Errorf("the model reads %q, want the comments counted", text)
		}
	}, "ai", "mcp", "serve")
}

// pushOnTop commits content to a file on top of a branch as it is, as a
// second push to a pull request does.
func pushOnTop(t *testing.T, h *liveHarness, projectKey, repositorySlug, branch, fileName, content string) error {
	t.Helper()
	directory := t.TempDir()
	pushURL, err := repositoryPushURL(h.config, projectKey, repositorySlug)
	if err != nil {
		return err
	}
	for _, args := range [][]string{
		{"init"},
		{"config", "user.name", "bb-live-test"},
		{"config", "user.email", "bb-live-test@example.local"},
		{"config", "core.autocrlf", "false"},
		{"remote", "add", "origin", pushURL},
		{"fetch", "origin", branch},
		{"checkout", "-b", branch, "FETCH_HEAD"},
	} {
		if err := runGit(directory, args...); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(directory, fileName), []byte(content), 0o644); err != nil {
		return err
	}
	for _, args := range [][]string{{"add", fileName}, {"commit", "-m", "push on top of " + branch}, {"push", "origin", branch}} {
		if err := runGit(directory, args...); err != nil {
			return err
		}
	}
	return nil
}

// patchLineKeys is each line a patch draws, as path|old:N for a removed line
// and path|new:N for any other, as a thread's place names it.
func patchLineKeys(patch string) map[string]bool {
	keys := map[string]bool{}
	path, oldLine, newLine := "", 0, 0
	for _, line := range strings.Split(patch, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			if fields := strings.Fields(line); len(fields) == 4 {
				path = strings.TrimPrefix(strings.TrimPrefix(fields[3], "b/"), "dst://")
			}
			oldLine, newLine = 0, 0
		case strings.HasPrefix(line, "@@ "):
			fmt.Sscanf(line, "@@ -%d", &oldLine)
			if plus := strings.Index(line, " +"); plus >= 0 {
				fmt.Sscanf(line[plus+2:], "%d", &newLine)
			}
		case strings.HasPrefix(line, "+++ "), strings.HasPrefix(line, "--- "):
		case strings.HasPrefix(line, "+") && newLine > 0:
			keys[fmt.Sprintf("%s|new:%d", path, newLine)] = true
			newLine++
		case strings.HasPrefix(line, "-") && oldLine > 0:
			keys[fmt.Sprintf("%s|old:%d", path, oldLine)] = true
			oldLine++
		case strings.HasPrefix(line, " ") && newLine > 0:
			keys[fmt.Sprintf("%s|new:%d", path, newLine)] = true
			oldLine++
			newLine++
		}
	}
	return keys
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

		card := show(map[string]any{"kind": "pull_request", "project": seeded.Key, "repo": repo.Slug, "id": id})
		before := asString(card["fingerprint"])
		comments := fmt.Sprintf("/rest/api/latest/projects/%s/repos/%s/pull-requests/%s/comments", seeded.Key, repo.Slug, id)
		if _, err := harness.liveJSON(ctx, http.MethodPost, comments, map[string]any{"text": "a comment after the view was drawn"}); err != nil {
			t.Fatalf("post a comment failed: %v", err)
		}

		result, answer := refresh(card, before)
		payload, _ := result.Meta[viewPayloadKey].(map[string]any)
		if answer["changed"] != true || payload == nil {
			t.Fatalf("after a comment refresh_view answered changed=%v with data %v, want the data", answer["changed"], payload != nil)
		}
		if after := asString(payload["fingerprint"]); after == before || after != answer["fingerprint"] {
			t.Errorf("the new data has fingerprint %q, the answer %v, the view had %q; want a new one, the same in both", after, answer["fingerprint"], before)
		}
		activity, _ := payload["activity"].(map[string]any)
		items, _ := activity["items"].([]any)
		newest, _ := valueOf(items, 0).(map[string]any)
		thread, _ := newest["thread"].(map[string]any)
		if newest["action"] != "COMMENTED" || asString(thread["text"]) != "a comment after the view was drawn" {
			t.Errorf("the new data's activity starts %v, want the comment", newest)
		}
		if text := mcpResultText(result); !strings.Contains(text, "has changed") || !strings.Contains(text, "Comments: 1 unresolved") {
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
		everyKind := true
		for _, kind := range []string{"pull_request", "pull_requests", "diff", "pull_request_form"} {
			everyKind = everyKind && containsAny(kinds, kind)
		}
		if !everyKind || !containsAny(tools, "add_pr_comment") || !containsAny(tools, "submit_pr_review") {
			t.Errorf("a server with every tool offers its views %v, want every kind and the actions", offers)
		}
	}, "ai", "mcp", "serve")
}

func valueOf(values []any, index int) any {
	if index < len(values) {
		return values[index]
	}
	return nil
}

func containsAny(values []any, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// TestLiveMCPShowsAPullRequestFormAndCreatesWhatItSends: the pull request
// form starts from what the model drafted and the repository's default
// branch, with the reviewers Bitbucket's create page fills in -- the default
// reviewers and the code owners for the branches, less the author -- named
// and pictured as Bitbucket has them. It suggests the repository's branches
// and the people who can read it, names whom Bitbucket names for a pair of
// branches, and the calls the form makes create and save the pull request as
// the person finished it.
func TestLiveMCPShowsAPullRequestFormAndCreatesWhatItSends(t *testing.T) {
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

	reviewer, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create a reviewer failed: %v", err)
	}
	if err := harness.grantRepoPermission(ctx, seeded.Key, repo.Slug, reviewer.Username,
		openapigenerated.SetPermissionForUserParamsPermissionREPOREAD); err != nil {
		t.Fatalf("grant the reviewer read access failed: %v", err)
	}
	// The reviewer is the repository's default reviewer for every branch:
	// create_pull_request adds only the reviewers it is given, so the form
	// fills them in, as Bitbucket's create page does.
	reviewerID, err := harness.userID(ctx, reviewer.Username)
	if err != nil {
		t.Fatalf("look up the reviewer's id failed: %v", err)
	}
	mustLiveCLI(t, "reviewer", "condition", "create", fmt.Sprintf(
		`{"sourceMatcher":{"id":"ANY_REF","type":{"id":"ANY_REF"}},"targetMatcher":{"id":"ANY_REF","type":{"id":"ANY_REF"}},"reviewers":[{"id":%d}],"requiredApprovals":0}`,
		reviewerID), "--repo", seeded.Key+"/"+repo.Slug)
	// The owner owns form.txt beside bb's own user, who opens the pull
	// request and so is left out, as the create page leaves them out:
	// Bitbucket refuses an author as a reviewer.
	owner := liveCodeOwner(t, ctx, harness, seeded.Key, repo.Slug)
	if err := harness.pushFileOnBranch(seeded.Key, repo.Slug, "master", ".bitbucket/CODEOWNERS",
		"form.txt @"+owner.Username+" @"+harness.username()+"\n"); err != nil {
		t.Fatalf("push CODEOWNERS failed: %v", err)
	}
	branch := testsupport.UniqueName("feature/form-")
	if err := harness.pushFileOnBranch(seeded.Key, repo.Slug, branch, "form.txt", "made with the form\n"); err != nil {
		t.Fatalf("push failed: %v", err)
	}
	// The names the form shows, read from Bitbucket on their own.
	displayName := func(username string) string {
		t.Helper()
		user, err := harness.liveJSON(ctx, http.MethodGet, "/rest/api/latest/users/"+username, nil)
		if err != nil || asString(user["displayName"]) == "" {
			t.Fatalf("read %s's display name: %v %v", username, err, user)
		}
		return asString(user["displayName"])
	}
	reviewerName, ownerName := displayName(reviewer.Username), displayName(owner.Username)

	capabilities := &mcp.ClientCapabilities{}
	capabilities.AddExtension("io.modelcontextprotocol/ui", map[string]any{"mimeTypes": []string{"text/html;profile=mcp-app"}})
	executeLiveMCPServerAs(t, &mcp.ClientOptions{Capabilities: capabilities}, func(session *mcp.ClientSession) {
		call := func(name string, arguments map[string]any) *mcp.CallToolResult {
			t.Helper()
			result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: arguments})
			if err != nil || result.IsError {
				t.Fatalf("%s %v: %v %s", name, arguments, err, mcpResultText(result))
			}
			return result
		}
		structured := func(result *mcp.CallToolResult) map[string]any {
			t.Helper()
			encoded, _ := json.Marshal(result.StructuredContent)
			var decoded map[string]any
			_ = json.Unmarshal(encoded, &decoded)
			return decoded
		}

		shown := call("show", map[string]any{"kind": "pull_request_form", "project": seeded.Key, "repo": repo.Slug,
			"from_ref": branch, "title": "Made with the form", "description": "Drafted by the model."})
		payload, _ := shown.Meta[viewPayloadKey].(map[string]any)
		form, _ := payload["form"].(map[string]any)
		for key, want := range map[string]any{"mode": "create", "from_ref": branch, "to_ref": "master", "default_branch": "master", "title": "Made with the form"} {
			if form[key] != want {
				t.Errorf("the form starts with %s = %v, want %v (all: %v)", key, form[key], want, form)
			}
		}
		if !strings.HasSuffix(asString(form["repository_url"]), "/projects/"+seeded.Key+"/repos/"+repo.Slug) {
			t.Errorf("the form links under %v, want the repository's page", form["repository_url"])
		}
		// The create page fills in the default reviewer and the code owner,
		// and leaves out bb's own user, who owns the file too.
		for key, want := range map[string][]any{
			"reviewers":         {reviewer.Username, owner.Username},
			"default_reviewers": {reviewer.Username},
			"code_owners":       {owner.Username},
		} {
			if got, _ := form[key].([]any); !slices.Equal(got, want) {
				t.Errorf("the form starts with %s %v, want %v", key, form[key], want)
			}
		}
		people, _ := form["people"].(map[string]any)
		avatars, _ := payload["avatars"].(map[string]any)
		for username, name := range map[string]string{reviewer.Username: reviewerName, owner.Username: ownerName} {
			if person, _ := people[username].(map[string]any); asString(person["display_name"]) != name {
				t.Errorf("the form names %s %v, want %q as Bitbucket does", username, people[username], name)
			}
			if !strings.HasPrefix(asString(avatars[username]), "data:image/") {
				t.Errorf("the form has no avatar image for %s: %.40q", username, asString(avatars[username]))
			}
		}
		text := mcpResultText(shown)
		if !strings.Contains(text, "Nothing is created until they submit it.") || !strings.Contains(text, "with reviewers "+reviewer.Username+" and "+owner.Username) {
			t.Errorf("the model reads %q, want it told whom the form starts with and that nothing is created yet", text)
		}

		branches, _ := structured(call("suggest_form_values", map[string]any{"project": seeded.Key, "repo": repo.Slug, "field": "branch", "text": "form-"}))["values"].([]any)
		if len(branches) != 1 || asString(branches[0].(map[string]any)["value"]) != branch {
			t.Errorf("the form suggests branches %v for what was typed, want %s", branches, branch)
		}
		// A person the form offers comes with the name and the picture
		// Bitbucket has for them.
		offered := func(values []any, username, name string) bool {
			for _, value := range values {
				entry, _ := value.(map[string]any)
				if entry["value"] == username && entry["label"] == name && strings.HasPrefix(asString(entry["avatar"]), "data:image/") {
					return true
				}
			}
			return false
		}
		found, _ := structured(call("suggest_form_values", map[string]any{"project": seeded.Key, "repo": repo.Slug, "field": "reviewer", "text": reviewer.Username}))["values"].([]any)
		if len(found) != 1 || !offered(found, reviewer.Username, reviewerName) {
			t.Errorf("the form suggests reviewers %v, want %s, who can read the repository, named and pictured", found, reviewer.Username)
		}
		everyone, _ := structured(call("suggest_form_values", map[string]any{"project": seeded.Key, "repo": repo.Slug, "field": "reviewer"}))["values"].([]any)
		for _, person := range everyone {
			if strings.EqualFold(asString(person.(map[string]any)["value"]), harness.username()) {
				t.Errorf("the form suggests bb's own user %s as a reviewer of their pull request", harness.username())
			}
		}
		// Whom Bitbucket names for the branches the person picks, as the form
		// asks when they pick others.
		named := map[string]any{"project": seeded.Key, "repo": repo.Slug, "from_ref": branch, "to_ref": "master"}
		named["field"] = "default_reviewers"
		defaults, _ := structured(call("suggest_form_values", named))["values"].([]any)
		if len(defaults) != 1 || !offered(defaults, reviewer.Username, reviewerName) {
			t.Errorf("the form names default reviewers %v, want %s, named and pictured", defaults, reviewer.Username)
		}
		named["field"] = "code_owners"
		owners, _ := structured(call("suggest_form_values", named))["values"].([]any)
		if len(owners) != 1 || !offered(owners, owner.Username, ownerName) {
			t.Errorf("the form names code owners %v, want %s alone, named and pictured, without bb's own user", owners, owner.Username)
		}

		// The person finishes it and submits: the form calls create_pull_request
		// with what they wrote, as the view does.
		created := structured(call("create_pull_request", map[string]any{"project": seeded.Key, "repo": repo.Slug,
			"from_ref": branch, "to_ref": "master", "title": "Made with the form, finished", "description": "Drafted by the model.",
			"reviewers": reviewer.Username + "," + owner.Username, "draft": false}))
		pr, _ := created["pull_request"].(map[string]any)
		id := fmt.Sprint(pr["id"])
		read := structured(call("get_pull_request", map[string]any{"project": seeded.Key, "repo": repo.Slug, "id": id}))
		readPR, _ := read["pull_request"].(map[string]any)
		reviewers, _ := readPR["reviewers"].([]any)
		var held []string
		for _, entry := range reviewers {
			held = append(held, asString(entry.(map[string]any)["name"]))
		}
		slices.Sort(held)
		want := []string{reviewer.Username, owner.Username}
		slices.Sort(want)
		if readPR["title"] != "Made with the form, finished" || !slices.Equal(held, want) {
			t.Errorf("Bitbucket holds %v with reviewers %v, want the title and the reviewers the person finished it with, %v", readPR["title"], held, want)
		}

		// Shown again to edit, the form starts from the pull request as it is.
		edit := call("show", map[string]any{"kind": "pull_request_form", "project": seeded.Key, "repo": repo.Slug, "id": id})
		editPayload, _ := edit.Meta[viewPayloadKey].(map[string]any)
		editForm, _ := editPayload["form"].(map[string]any)
		if editForm["mode"] != "edit" || editForm["title"] != "Made with the form, finished" || editForm["version"] != readPR["version"] {
			t.Errorf("the edit form starts with %v, want the pull request's title and version %v", editForm, readPR["version"])
		}
	}, "ai", "mcp", "serve")
}

// TestLiveMCPShowsADiffHighlighted: code in a diff comes highlighted, keyed by
// the file's place in the patch: ledger.go, and not the picture beside it.
func TestLiveMCPShowsADiffHighlighted(t *testing.T) {
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

	// A PNG of one pixel, as a picture in a repository is stored.
	png, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==")
	branch := testsupport.UniqueName("feature/files-")
	if err := harness.pushFilesOnBranch(seeded.Key, repo.Slug, branch, map[string][]byte{
		"ledger.go": []byte("package ledger\n\n// Refund reverses an entry.\nfunc Refund() {}\n"),
		"logo.png":  png,
	}); err != nil {
		t.Fatalf("push failed: %v", err)
	}
	created := extractPRData(decodeJSONMap(t, mustLiveCLI(t, "pr", "create",
		"--from-ref", branch, "--to-ref", "refs/heads/master", "--title", testsupport.UniqueName("Files "),
		"--no-default-reviewers", "--no-codeowners")))

	capabilities := &mcp.ClientCapabilities{}
	capabilities.AddExtension("io.modelcontextprotocol/ui", map[string]any{"mimeTypes": []string{"text/html;profile=mcp-app"}})
	executeLiveMCPServerAs(t, &mcp.ClientOptions{Capabilities: capabilities}, func(session *mcp.ClientSession) {
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "show",
			Arguments: map[string]any{"kind": "diff", "project": seeded.Key, "repo": repo.Slug, "id": fmt.Sprint(created["id"])}})
		if err != nil || result.IsError {
			t.Fatalf("show diff: %v %s", err, mcpResultText(result))
		}
		payload, _ := result.Meta[viewPayloadKey].(map[string]any)
		diff, _ := payload["diff"].(map[string]any)
		highlighted, _ := diff["highlight"].(map[string]any)
		files, _ := diff["files"].([]any)
		index := -1
		for i, entry := range files {
			if asString(entry.(map[string]any)["path"]) == "ledger.go" {
				index = i
			}
		}
		spans, _ := highlighted[fmt.Sprint(index)].([]any)
		if index < 0 || len(spans) != 4 || !strings.HasPrefix(asString(spans[0]), "k7") || len(highlighted) != 1 {
			t.Errorf("the diff is highlighted as %v, want ledger.go's four added lines alone, package a keyword", highlighted)
		}
	}, "ai", "mcp", "serve")
}
