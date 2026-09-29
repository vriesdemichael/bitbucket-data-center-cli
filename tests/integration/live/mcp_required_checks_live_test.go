//go:build live

package live_test

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	bbmcp "github.com/vriesdemichael/bitbucket-data-center-cli/internal/mcp"
	pullrequestservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequest"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// TestLiveRequiredChecksAgreeWithTheMergeVeto holds the builds bb says a pull
// request must pass against Bitbucket's own verdict: the merge veto of each
// pull request names exactly the required builds bb says have not passed.
//
// The conditions cover every matcher: a branch, by its full id and by its
// name; a pattern for the target, and the same pattern for a branch one level
// below it, which it does not match; the branching model's development branch
// and a category of it, both of the repository's own choosing; the default
// branch; an exemption for the source branch by category and by pattern; and
// a condition for the merge queue alone. The builds cover what satisfies a key
// and what does not: a build that names the key as its parent, passed, failed
// or running; a passing sibling beside a failed one; a build that has the key
// as its own key and names no parent; one posted through the deprecated
// build-status endpoint; and one posted for the target branch rather than the
// source.
func TestLiveRequiredChecksAgreeWithTheMergeVeto(t *testing.T) {
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
	repoPath := fmt.Sprintf("/rest/api/latest/projects/%s/repos/%s", seeded.Key, repo.Slug)

	// The sources and the targets change different files, so no pull request
	// conflicts: a conflict keeps it from merging whatever the builds say.
	for branch, file := range map[string]string{
		"feature/checked": "source.txt", "hotfix/checked": "source.txt",
		"release/1.0": "target.txt", "release/1/2": "target.txt", "develop": "target.txt", "stable/1.0": "target.txt",
	} {
		if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, file); err != nil {
			t.Fatalf("push %s failed: %v", branch, err)
		}
	}

	// A branching model of the repository's own, whose development branch is
	// not the default branch and whose releases go by a prefix of their own:
	// the model decides, not the names.
	modelPath := fmt.Sprintf("/rest/branch-utils/latest/projects/%s/repos/%s/branchmodel", seeded.Key, repo.Slug)
	if _, err := harness.liveJSON(ctx, http.MethodPut, modelPath+"/configuration", map[string]any{
		"development": map[string]any{"refId": "refs/heads/develop", "useDefault": false},
		"production":  nil,
		"types": []map[string]any{
			{"id": "BUGFIX", "enabled": true, "prefix": "bugfix/"},
			{"id": "FEATURE", "enabled": true, "prefix": "feature/"},
			{"id": "HOTFIX", "enabled": true, "prefix": "hotfix/"},
			{"id": "RELEASE", "enabled": true, "prefix": "stable/"},
		},
	}); err != nil {
		t.Fatalf("configure the branching model: %v", err)
	}
	model, err := harness.liveJSON(ctx, http.MethodGet, modelPath, nil)
	if err != nil {
		t.Fatalf("read the branching model back: %v", err)
	}
	development, _ := model["development"].(map[string]any)
	prefixes := map[string]string{}
	types, _ := model["types"].([]any)
	for _, entry := range types {
		category, _ := entry.(map[string]any)
		prefixes[asString(category["id"])] = asString(category["prefix"])
	}
	if development["id"] != "refs/heads/develop" || prefixes["RELEASE"] != "stable/" || prefixes["HOTFIX"] != "hotfix/" {
		t.Fatalf("the branching model reads development %v and categories %v, want develop, releases under stable/", development["id"], prefixes)
	}

	clients, err := bbmcp.ClientsFromConfig(harness.config)
	if err != nil {
		t.Fatalf("build the clients: %v", err)
	}
	open := func(from, to string) pullrequestservice.PullRequest {
		t.Helper()
		id, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, from, to)
		if err != nil {
			t.Fatalf("open %s into %s: %v", from, to, err)
		}
		pr, err := pullrequestservice.NewService(clients.HTTP).Get(ctx, pullrequestservice.RepositoryRef{ProjectKey: seeded.Key, Slug: repo.Slug}, id)
		if err != nil || pr.SourceCommit == "" {
			t.Fatalf("read pull request %s back: %v %+v", id, err, pr)
		}
		return pr
	}
	toMaster := open("feature/checked", "master")
	toRelease := open("feature/checked", "release/1.0")
	toNested := open("feature/checked", "release/1/2")
	toDevelop := open("feature/checked", "develop")
	toStable := open("feature/checked", "stable/1.0")
	hotfix := open("hotfix/checked", "master")

	// Keys of one length, so that none is part of another in the veto's text.
	prefix := testsupport.UniqueName("rq")
	key := func(code string) string { return prefix + "-" + code }
	conditions := []struct {
		code           string
		target         liveMatcher
		exempt         *liveMatcher
		mergeQueueOnly bool
	}{
		{code: "brn", target: liveMatcher{"BRANCH", "refs/heads/master"}},
		{code: "pat", target: liveMatcher{"PATTERN", "release/*"}},
		{code: "dev", target: liveMatcher{"MODEL_BRANCH", "development"}},
		{code: "cat", target: liveMatcher{"MODEL_CATEGORY", "RELEASE"}},
		{code: "dfl", target: liveMatcher{"DEFAULT_BRANCH", "#"}},
		{code: "exc", target: liveMatcher{"BRANCH", "master"}, exempt: &liveMatcher{"MODEL_CATEGORY", "HOTFIX"}},
		{code: "exp", target: liveMatcher{"ANY_REF", "ANY_REF_MATCHER_ID"}, exempt: &liveMatcher{"PATTERN", "feature/*"}},
		{code: "mqo", target: liveMatcher{"BRANCH", "refs/heads/master"}, mergeQueueOnly: true},
	}
	var keys []string
	wantStored := map[string]string{}
	for _, condition := range conditions {
		// A release before the scope applies every condition to pull requests,
		// and bb refuses one that asks otherwise (TestLiveRequiredBuildScope).
		if condition.mergeQueueOnly && harness.release(t).Before(requiredBuildScopeSince) {
			continue
		}
		body := map[string]any{"buildParentKeys": []string{key(condition.code)}, "refMatcher": condition.target.body()}
		description := condition.target.String()
		if condition.exempt != nil {
			body["exemptRefMatcher"] = condition.exempt.body()
			description += " exempt " + condition.exempt.String()
		}
		if condition.mergeQueueOnly {
			body["requiredForPullRequest"], body["requiredForMergeQueue"] = false, true
			description += " merge queue only"
		}
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("encode condition %s: %v", condition.code, err)
		}
		createRequiredBuildCheckWithRetry(t, string(encoded))
		keys = append(keys, key(condition.code))
		wantStored[key(condition.code)] = description
	}

	// Read back: each condition is stored as it was sent.
	listed, err := harness.liveJSON(ctx, http.MethodGet,
		fmt.Sprintf("/rest/required-builds/latest/projects/%s/repos/%s/conditions?limit=100", seeded.Key, repo.Slug), nil)
	if err != nil {
		t.Fatalf("read the conditions back: %v", err)
	}
	stored := map[string]string{}
	values, _ := listed["values"].([]any)
	for _, value := range values {
		condition, _ := value.(map[string]any)
		parents, _ := condition["buildParentKeys"].([]any)
		if len(parents) != 1 {
			continue
		}
		description := storedMatcher(condition["refMatcher"])
		if condition["exemptRefMatcher"] != nil {
			description += " exempt " + storedMatcher(condition["exemptRefMatcher"])
		}
		if condition["requiredForPullRequest"] == false {
			description += " merge queue only"
		}
		stored[asString(parents[0])] = description
	}
	if !maps.Equal(stored, wantStored) {
		t.Fatalf("the conditions are stored as\n%v\nwant\n%v", stored, wantStored)
	}

	// agree reads a pull request's required checks through bb and Bitbucket's
	// veto of it, and holds both to what the scenario wants: for each key, its
	// state, empty for a build that is missing.
	agree := func(t *testing.T, name string, pr pullrequestservice.PullRequest, want map[string]string) []liveRequiredCheck {
		t.Helper()

		checks, known := bbmcp.RequiredChecksForLiveSuite(ctx, clients, seeded.Key, repo.Slug, pr)
		if !known {
			t.Fatalf("%s: bb could not say what the pull request requires", name)
		}
		got := map[string]string{}
		var listed []liveRequiredCheck
		for _, check := range checks {
			got[check.Key] = check.State
			listed = append(listed, liveRequiredCheck{Key: check.Key, Name: check.Name, State: check.State, URL: check.URL, Unparented: check.Unparented})
		}
		wanted := map[string]string{}
		for code, state := range want {
			wanted[key(code)] = state
		}
		if !maps.Equal(got, wanted) {
			t.Errorf("%s: bb says the pull request requires %v, want %v", name, got, wanted)
		}

		named, canMerge := mergeVeto(ctx, t, harness, repoPath, pr.ID, keys)
		blocked := false
		for _, k := range keys {
			state, required := got[k]
			unpassed := required && state != "SUCCESSFUL"
			blocked = blocked || unpassed
			if named[k] != unpassed {
				t.Errorf("%s: Bitbucket's veto names %s: %v; bb says it is required %v with state %q", name, k, named[k], required, state)
			}
		}
		if canMerge == blocked {
			t.Errorf("%s: Bitbucket says the pull request can merge: %v; bb says a required build has not passed: %v", name, canMerge, blocked)
		}
		return listed
	}

	t.Run("no build has reported", func(t *testing.T) {
		agree(t, "into master", toMaster, map[string]string{"brn": "", "dfl": "", "exc": ""})
		agree(t, "into release/1.0", toRelease, map[string]string{"pat": ""})
		agree(t, "into release/1/2", toNested, map[string]string{})
		agree(t, "into develop", toDevelop, map[string]string{"dev": ""})
		agree(t, "into stable/1.0", toStable, map[string]string{"cat": ""})
		agree(t, "a hotfix into master", hotfix, map[string]string{"brn": "", "dfl": "", "exp": ""})
	})

	commit := toMaster.SourceCommit
	scoped := func(build map[string]any) {
		t.Helper()
		build["url"] = "https://ci.example.com/" + asString(build["key"])
		if _, err := harness.liveJSON(ctx, http.MethodPost, repoPath+"/commits/"+commit+"/builds", build); err != nil {
			t.Fatalf("post build %v: %v", build["key"], err)
		}
	}
	scoped(map[string]any{"key": key("brn") + "-unit", "parent": key("brn"), "state": "SUCCESSFUL", "name": "Unit tests"})
	scoped(map[string]any{"key": key("exc"), "state": "SUCCESSFUL"})
	scoped(map[string]any{"key": key("pat") + "-src", "parent": key("pat"), "state": "FAILED", "ref": "refs/heads/feature/checked"})
	scoped(map[string]any{"key": key("pat") + "-tgt", "parent": key("pat"), "state": "SUCCESSFUL", "ref": "refs/heads/release/1.0"})
	scoped(map[string]any{"key": key("cat") + "-run", "parent": key("cat"), "state": "INPROGRESS"})
	scoped(map[string]any{"key": key("dev") + "-bad", "parent": key("dev"), "state": "FAILED"})
	scoped(map[string]any{"key": key("dev") + "-ok", "parent": key("dev"), "state": "SUCCESSFUL"})
	if _, err := harness.liveJSON(ctx, http.MethodPost, "/rest/build-status/latest/commits/"+commit, map[string]any{
		"key": key("dfl"), "parent": key("dfl"), "state": "SUCCESSFUL", "url": "https://ci.example.com/legacy",
	}); err != nil {
		t.Fatalf("post the build through the deprecated endpoint: %v", err)
	}

	// Read back: every build is there as posted, the one without a parent is
	// stored without one, and the one for the target branch keeps its parent
	// and its ref.
	posted, err := harness.liveJSON(ctx, http.MethodGet, "/rest/build-status/latest/commits/"+commit+"?limit=100", nil)
	if err != nil {
		t.Fatalf("read the builds back: %v", err)
	}
	states := map[string]string{}
	builds, _ := posted["values"].([]any)
	for _, entry := range builds {
		build, _ := entry.(map[string]any)
		states[asString(build["key"])] = asString(build["state"])
	}
	for build, state := range map[string]string{
		key("brn") + "-unit": "SUCCESSFUL", key("exc"): "SUCCESSFUL", key("dfl"): "SUCCESSFUL",
		key("pat") + "-src": "FAILED", key("pat") + "-tgt": "SUCCESSFUL", key("cat") + "-run": "INPROGRESS",
		key("dev") + "-bad": "FAILED", key("dev") + "-ok": "SUCCESSFUL",
	} {
		if states[build] != state {
			t.Fatalf("build %s reads %q, want %q: %v", build, states[build], state, states)
		}
	}
	if own := liveScopedBuild(ctx, t, harness, repoPath, commit, key("exc")); own["parent"] != nil {
		t.Fatalf("the build posted without a parent is stored with one: %v", own)
	}
	if target := liveScopedBuild(ctx, t, harness, repoPath, commit, key("pat")+"-tgt"); target["parent"] != key("pat") || target["ref"] != "refs/heads/release/1.0" {
		t.Fatalf("the build for the target branch is stored as %v", target)
	}

	t.Run("builds have reported", func(t *testing.T) {
		checks := agree(t, "into master", toMaster, map[string]string{"brn": "SUCCESSFUL", "dfl": "", "exc": ""})
		want := []liveRequiredCheck{
			{Key: key("dfl"), Unparented: true},
			{Key: key("exc"), Unparented: true},
			{Key: key("brn"), Name: "Unit tests", State: "SUCCESSFUL", URL: "https://ci.example.com/" + key("brn") + "-unit"},
		}
		if !slices.Equal(checks, want) {
			t.Errorf("the checks into master read\n%+v\nwant the missing first, each said to have reported without a parent\n%+v", checks, want)
		}
		agree(t, "into release/1.0", toRelease, map[string]string{"pat": "FAILED"})
		agree(t, "into release/1/2", toNested, map[string]string{})
		agree(t, "into develop", toDevelop, map[string]string{"dev": "SUCCESSFUL"})
		agree(t, "into stable/1.0", toStable, map[string]string{"cat": "INPROGRESS"})
		agree(t, "a hotfix into master", hotfix, map[string]string{"brn": "", "dfl": "", "exp": ""})
	})

	// The card carries what the function says, missing first, and the model
	// is told what is still missing.
	t.Run("the card shows them", func(t *testing.T) {
		capabilities := &mcp.ClientCapabilities{}
		capabilities.AddExtension("io.modelcontextprotocol/ui", map[string]any{"mimeTypes": []string{"text/html;profile=mcp-app"}})
		executeLiveMCPServerAs(t, &mcp.ClientOptions{Capabilities: capabilities}, func(session *mcp.ClientSession) {
			result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "show",
				Arguments: map[string]any{"kind": "pull_request", "project": seeded.Key, "repo": repo.Slug, "id": fmt.Sprint(toMaster.ID)}})
			if err != nil || result.IsError {
				t.Fatalf("show the card: %v %s", err, mcpResultText(result))
			}
			payload, _ := result.Meta[viewPayloadKey].(map[string]any)
			card, _ := payload["pull_request"].(map[string]any)
			if card["required_known"] != true {
				t.Fatalf("the card says bb could not tell what is required: %v", card["required_known"])
			}
			var got []string
			listed, _ := card["required_checks"].([]any)
			for _, entry := range listed {
				check, _ := entry.(map[string]any)
				got = append(got, asString(check["key"])+"="+asString(check["state"]))
			}
			want := []string{key("dfl") + "=", key("exc") + "=", key("brn") + "=SUCCESSFUL"}
			if !slices.Equal(got, want) {
				t.Errorf("the card lists required builds %v, want %v", got, want)
			}
			if text := mcpResultText(result); !strings.Contains(text, "Required builds: 2 missing, 0 failed, of 3.") {
				t.Errorf("the model reads %q, want the required builds still missing", text)
			}
		}, "ai", "mcp", "serve")
	})

	scoped(map[string]any{"key": key("exc"), "parent": key("exc"), "state": "SUCCESSFUL"})
	scoped(map[string]any{"key": key("dfl"), "parent": key("dfl"), "state": "SUCCESSFUL"})

	t.Run("every required build has passed", func(t *testing.T) {
		agree(t, "into master", toMaster, map[string]string{"brn": "SUCCESSFUL", "dfl": "SUCCESSFUL", "exc": "SUCCESSFUL"})
	})
}

// liveRequiredCheck is a required check as bb reports it, in a type the live
// suite can name.
type liveRequiredCheck struct {
	Key, Name, State, URL string
	Unparented            bool
}

// mergeVeto is Bitbucket's own verdict on a pull request: which of the keys
// its merge vetoes name, and whether it can merge.
func mergeVeto(ctx context.Context, t *testing.T, harness *liveHarness, repoPath string, id int64, keys []string) (map[string]bool, bool) {
	t.Helper()

	merge, err := harness.liveJSON(ctx, http.MethodGet, fmt.Sprintf("%s/pull-requests/%d/merge", repoPath, id), nil)
	if err != nil {
		t.Fatalf("read the merge veto of #%d: %v", id, err)
	}
	named := map[string]bool{}
	vetoes, _ := merge["vetoes"].([]any)
	for _, entry := range vetoes {
		veto, _ := entry.(map[string]any)
		detail := asString(veto["detailedMessage"])
		for _, key := range keys {
			if strings.Contains(detail, key) {
				named[key] = true
			}
		}
	}
	canMerge, ok := merge["canMerge"].(bool)
	if !ok {
		t.Fatalf("the merge veto of #%d says nothing about merging: %v", id, merge)
	}
	return named, canMerge
}

// liveScopedBuild reads one build of a commit back through the repository's
// builds endpoint, which carries its parent and its ref.
func liveScopedBuild(ctx context.Context, t *testing.T, harness *liveHarness, repoPath, commit, key string) map[string]any {
	t.Helper()

	build, err := harness.liveJSON(ctx, http.MethodGet, repoPath+"/commits/"+commit+"/builds?key="+key, nil)
	if err != nil {
		t.Fatalf("read build %s back: %v", key, err)
	}
	return build
}

// liveMatcher is a required-build condition's matcher: its type and its id.
type liveMatcher struct {
	kind, id string
}

func (matcher liveMatcher) body() map[string]any {
	return map[string]any{"id": matcher.id, "type": map[string]any{"id": matcher.kind}}
}

func (matcher liveMatcher) String() string {
	return matcher.kind + " " + matcher.id
}

// storedMatcher describes a matcher as Bitbucket stored it, as liveMatcher
// describes one that was sent.
func storedMatcher(value any) string {
	matcher, _ := value.(map[string]any)
	kind, _ := matcher["type"].(map[string]any)
	return liveMatcher{kind: asString(kind["id"]), id: asString(matcher["id"])}.String()
}
