//go:build live

package live_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	commentservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/comment"
	pullrequestservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequest"
	pullrequestactivityservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequestactivity"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/httpclient"
)

// TestLivePullRequestReviewVisibility seeds a pull request with a normal comment
// and a task, resolves neither, and checks that both surface as unresolved
// threads. It pins the three Bitbucket behaviours the review summary is built
// on, none of which are in the published OpenAPI spec:
//
//   - a task (a blocker comment) reaches the activity timeline, which is what
//     lets one listing return comments and tasks together;
//   - the pull request *listing* carries the "properties" counters that
//     `bb pr list` renders for free — note the single pull request endpoint
//     does not, which is why `pr get` has a separate fallback; and
//   - the blocker-comment count endpoint agrees with the timeline, so that
//     fallback reports the same number.
//
// If Bitbucket ever stops doing any of these, this fails loudly instead of the
// CLI quietly reporting nothing outstanding.
func TestLivePullRequestReviewVisibility(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	branch := fmt.Sprintf("lt-review-vis-%d", time.Now().UnixNano()%100000)
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "review-visibility.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}

	pullRequestID, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, branch, "master")
	if err != nil {
		t.Fatalf("create pull request failed: %v", err)
	}

	repoRef := pullrequestservice.RepositoryRef{ProjectKey: seeded.Key, Slug: repo.Slug}
	commentSvc := commentservice.NewService(harness.client)
	commentTarget := commentservice.Target{
		Repository:    commentservice.RepositoryRef{ProjectKey: seeded.Key, Slug: repo.Slug},
		PullRequestID: pullRequestID,
	}

	if _, err := commentSvc.Create(ctx, commentTarget, "please handle the nil case"); err != nil {
		t.Fatalf("create pull request comment failed: %v", err)
	}

	prSvc := pullrequestservice.NewService(httpclient.NewFromConfig(harness.config))

	// An inline comment is the case that matters most and the one that used to
	// break: Bitbucket serialises its anchor path as a string on the activity
	// timeline, which the generated model could not decode, so a pull request
	// with any inline review comment returned nothing at all.
	inline, err := prSvc.AddInlineComment(ctx, repoRef, pullRequestID, "this line needs a guard", pullrequestservice.InlineCommentAnchor{
		Path:     "review-visibility.txt",
		Line:     1,
		LineType: "ADDED",
	})
	if err != nil {
		t.Fatalf("create inline comment failed: %v", err)
	}

	// A task on Bitbucket Data Center 8+ is a blocker comment. The legacy
	// pull-request-scoped /tasks endpoint is gone on 10.x, which is exactly why
	// reading tasks off the activity timeline is worth having.
	blockerTarget := commentTarget
	blockerTarget.Blocker = true
	task, err := commentSvc.Create(ctx, blockerTarget, "add a regression test")
	if err != nil {
		t.Fatalf("create pull request task failed: %v", err)
	}
	if task.Id == nil {
		t.Fatal("created task is missing an id")
	}
	taskID := *task.Id

	// 1. The activity timeline must carry the comments and the task.
	activitySvc := pullrequestactivityservice.NewService(harness.client)
	activities, err := activitySvc.List(ctx, pullrequestactivityservice.RepositoryRef{ProjectKey: seeded.Key, Slug: repo.Slug}, pullRequestID, pullrequestactivityservice.ListOptions{PageSize: 100})
	if err != nil {
		t.Fatalf("list pull request activities failed: %v", err)
	}

	threads, summary := pullrequestactivityservice.ExtractThreads(activities, pullrequestactivityservice.ThreadOptions{State: "open"})
	if summary.Unresolved < 3 {
		t.Fatalf("expected both comments and the task to be unresolved, got summary %#v (threads %#v)", summary, threads)
	}
	if summary.OpenTasks < 1 {
		t.Fatalf("expected the task to reach the activity timeline as a blocker comment, got summary %#v", summary)
	}
	if summary.UnresolvedInline < 1 {
		t.Fatalf("expected the inline comment to keep its anchor, got summary %#v", summary)
	}

	foundTask := false
	foundInline := false
	for _, thread := range threads {
		if thread.ID == taskID && thread.Kind == pullrequestactivityservice.ThreadKindTask {
			foundTask = true
		}
		if inline.ID != 0 && thread.ID == inline.ID {
			if thread.Anchor == nil || thread.Anchor.Path != "review-visibility.txt" || thread.Anchor.Line != 1 {
				t.Fatalf("expected the inline anchor to survive decoding, got %#v", thread.Anchor)
			}
			foundInline = true
		}
	}
	if !foundTask {
		t.Fatalf("expected task %d to appear as a task thread, got %#v", taskID, threads)
	}
	if !foundInline {
		t.Fatalf("expected inline comment %d in the thread list, got %#v", inline.ID, threads)
	}

	// 2. The listing payload must carry the undocumented property counters that
	//    `bb pr list` renders for free. Bitbucket 10.x sends them here but not
	//    on the single pull request endpoint, which is why `pr get` falls back
	//    to the blocker-comment tally instead of these.
	listed, err := prSvc.List(ctx, repoRef, pullrequestservice.ListOptions{State: "open", MaxResults: 25})
	if err != nil {
		t.Fatalf("list pull requests failed: %v", err)
	}

	var listedPullRequest *pullrequestservice.PullRequest
	for index := range listed {
		if fmt.Sprintf("%d", listed[index].ID) == pullRequestID {
			listedPullRequest = &listed[index]
		}
	}
	if listedPullRequest == nil {
		t.Fatalf("expected pull request %s in the listing", pullRequestID)
	}
	if listedPullRequest.OpenTaskCount == nil {
		t.Fatalf("expected Bitbucket to report properties.openTaskCount on the pull request listing")
	}
	if *listedPullRequest.OpenTaskCount != summary.OpenTasks {
		t.Fatalf("property open task count %d disagrees with the activity-derived count %d", *listedPullRequest.OpenTaskCount, summary.OpenTasks)
	}
	if listedPullRequest.CommentCount == nil || *listedPullRequest.CommentCount < 1 {
		t.Fatalf("expected properties.commentCount to be reported, got %#v", listedPullRequest.CommentCount)
	}

	// 3. The blocker-comment count endpoint backs the `pr get` fallback, so its
	//    tally has to agree with the timeline too.
	taskCounts, err := commentSvc.CountTasks(ctx, commentservice.RepositoryRef{ProjectKey: seeded.Key, Slug: repo.Slug}, pullRequestID)
	if err != nil {
		t.Fatalf("count pull request tasks failed: %v", err)
	}
	if taskCounts.Open != summary.OpenTasks {
		t.Fatalf("blocker comment task count %d disagrees with the activity-derived count %d", taskCounts.Open, summary.OpenTasks)
	}

	// 4. The CLI must surface all of it.
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	getOutput, err := executeLiveCLI(t, "--json", "pr", "get", pullRequestID)
	if err != nil {
		t.Fatalf("pr get failed: %v\noutput: %s", err, getOutput)
	}
	reviewSummary, ok := decodeJSONMap(t, getOutput)["reviewSummary"].(map[string]any)
	if !ok {
		t.Fatalf("expected review_summary in pr get output: %s", getOutput)
	}
	if reviewSummary["actionRequired"] != true {
		t.Fatalf("expected action_required on a pull request with open feedback: %#v", reviewSummary)
	}
	if reviewSummary["countsSource"] != "activities" {
		t.Fatalf("expected activity-derived counts, got %#v", reviewSummary["countsSource"])
	}

	listOutput, err := executeLiveCLI(t, "--json", "pr", "comment", "list", pullRequestID, "--unresolved")
	if err != nil {
		t.Fatalf("pr comment list failed: %v\noutput: %s", err, listOutput)
	}
	if strings.Contains(listOutput, "\"pullRequest\"") {
		t.Fatalf("expected the nested pull request payload to be dropped from thread output: %s", listOutput)
	}
	listedThreads, ok := decodeJSONMap(t, listOutput)["threads"].([]any)
	if !ok || len(listedThreads) < 2 {
		t.Fatalf("expected at least the comment and the task in unresolved output: %s", listOutput)
	}

	// 5. Resolving the task must move it out of the open set.
	if err := resolveBlockerComment(ctx, harness, seeded.Key, repo.Slug, pullRequestID, taskID); err != nil {
		t.Fatalf("resolve task failed: %v", err)
	}

	afterActivities, err := activitySvc.List(ctx, pullrequestactivityservice.RepositoryRef{ProjectKey: seeded.Key, Slug: repo.Slug}, pullRequestID, pullrequestactivityservice.ListOptions{PageSize: 100})
	if err != nil {
		t.Fatalf("list pull request activities after resolve failed: %v", err)
	}
	_, afterSummary := pullrequestactivityservice.ExtractThreads(afterActivities, pullrequestactivityservice.ThreadOptions{})
	if afterSummary.OpenTasks != summary.OpenTasks-1 {
		t.Fatalf("expected the resolved task to leave the open set, before %#v after %#v", summary, afterSummary)
	}
	if afterSummary.ResolvedTasks < 1 {
		t.Fatalf("expected a resolved task to be counted, got %#v", afterSummary)
	}
}

// resolveBlockerComment marks a task (blocker comment) resolved.
//
// This went straight at the REST API on the belief that the comment service
// could only update text. It resolves comments perfectly well -- see
// TestLiveCommentStateAndPending -- and going through it means the setup for
// this test exercises the code rather than stepping around it.
func resolveBlockerComment(ctx context.Context, harness *liveHarness, projectKey, slug, pullRequestID string, commentID int64) error {
	target := commentservice.Target{
		Repository:    commentservice.RepositoryRef{ProjectKey: projectKey, Slug: slug},
		PullRequestID: pullRequestID,
	}

	_, err := commentservice.NewService(harness.client).SetState(
		ctx, target, strconv.FormatInt(commentID, 10), commentservice.CommentStateResolved, nil)

	return err
}

// TestLiveRouteMissingClassification pins the discriminator that tells a removed
// endpoint apart from a missing resource. Both are 404s, and the whole
// degrade-vs-report decision rests on separating them correctly.
//
// This is also the regression guard that was absent when Atlassian retired the
// pull request task endpoint: nothing exercised a removed route, so the CLI
// reported a bare "not found" for a call that could never succeed. If a future
// Bitbucket changes either 404 format, this fails here rather than silently
// reclassifying every removed endpoint as a missing resource.
func TestLiveRouteMissingClassification(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	repo := seeded.Repos[0]

	client := httpclient.NewFromConfig(harness.config)
	prBase := fmt.Sprintf("/rest/api/latest/projects/%s/repos/%s/pull-requests", seeded.Key, repo.Slug)

	cases := []struct {
		name             string
		path             string
		wantRouteMissing bool
	}{
		{
			// Removed in Bitbucket 8.0; tasks are blocker comments since then.
			name:             "retired pull request task endpoint",
			path:             prBase + "/1/tasks",
			wantRouteMissing: true,
		},
		{
			// The legacy top-level task API, also retired in 8.0.
			name:             "retired top-level task endpoint",
			path:             "/rest/api/latest/tasks",
			wantRouteMissing: true,
		},
		{
			name:             "unknown route",
			path:             "/rest/api/latest/definitely-not-a-route",
			wantRouteMissing: true,
		},
		{
			// A live endpoint asked for a resource that does not exist must not
			// be mistaken for a server lacking the feature.
			name:             "missing pull request on a live endpoint",
			path:             prBase + "/999999/blocker-comments",
			wantRouteMissing: false,
		},
		{
			name:             "missing repository on a live endpoint",
			path:             fmt.Sprintf("/rest/api/latest/projects/%s/repos/does-not-exist/pull-requests/1/blocker-comments", seeded.Key),
			wantRouteMissing: false,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var discard map[string]any
			err := client.GetJSON(ctx, testCase.path, nil, &discard)
			if err == nil {
				t.Fatalf("expected %s to fail", testCase.path)
			}
			if !apperrors.IsKind(err, apperrors.KindNotFound) {
				t.Fatalf("expected a not_found error for %s, got: %v", testCase.path, err)
			}
			if got := openapi.IsRouteMissing(err); got != testCase.wantRouteMissing {
				t.Fatalf("IsRouteMissing(%s) = %v, want %v (error: %v)", testCase.path, got, testCase.wantRouteMissing, err)
			}
		})
	}

	// The container status document is content-negotiated. GetJSON above sends
	// Accept: application/json and receives the JSON form; a client that does
	// not gets the XML equivalent. Both must classify the same way, which is why
	// the detection keys on the missing "errors" array rather than the content
	// type.
	t.Run("xml form of the container status document", func(t *testing.T) {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, harness.config.BitbucketURL+prBase+"/1/tasks", nil)
		if err != nil {
			t.Fatalf("build request: %v", err)
		}
		request.SetBasicAuth(harness.config.BitbucketUsername, harness.config.BitbucketPassword)

		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatalf("request retired endpoint: %v", err)
		}
		defer response.Body.Close()

		body, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if response.StatusCode != http.StatusNotFound {
			t.Fatalf("expected 404 from the retired endpoint, got %d", response.StatusCode)
		}
		if contentType := response.Header.Get("Content-Type"); !strings.Contains(contentType, "xml") {
			t.Fatalf("expected the un-negotiated response to be xml, got %q", contentType)
		}
		if !openapi.IsRouteMissing(openapi.MapStatusError(response.StatusCode, body)) {
			t.Fatalf("expected the xml status document to classify as a missing route, got body: %s", body)
		}
	})
}
