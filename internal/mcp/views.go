package mcp

import (
	"bytes"
	"context"
	"embed"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/safederef"
	diffservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/diff"
	pullrequestservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequest"
	qualityservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/quality"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/download"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/httpclient"
)

// The view surface (ADR-101). A client that renders MCP Apps shows the show
// tool's answer as an interactive view: one page, served as a ui:// resource,
// which renders the data the tool puts in its result's _meta. The data tools
// carry no view, because a client mounts a view for every call of a tool that
// has one, and an agent calls the data tools many times in a turn.
const (
	// appsExtension is the MCP Apps extension a client declares when it
	// renders views, and the server when it offers them.
	appsExtension = "io.modelcontextprotocol/ui"

	// viewMIMEType is the one type MCP Apps defines for a view.
	viewMIMEType = "text/html;profile=mcp-app"

	// viewURI is the page every view renders in. It holds no Bitbucket data,
	// so a client may cache it; the data arrives with each result.
	viewURI = "ui://bb/view"

	// viewPayloadKey is where the show tool puts what the view renders, in the
	// result's _meta: the part of a result meant for the view, not the model.
	viewPayloadKey = "io.github.vriesdemichael.bb/view"

	// viewPayloadVersion is the shape of that payload, so a page from one
	// release can tell a payload from another.
	viewPayloadVersion = 1
)

// The kinds show can put in front of the person, each with the data tool whose
// answer it shows. A kind is offered while its tool is exposed, so --tools and
// --exclude decide the views as they decide the resources.
const (
	showKindPullRequest  = "pull_request"
	showKindPullRequests = "pull_requests"
	showKindDiff         = "diff"
)

var showKindTools = map[string]string{
	showKindPullRequest:  "get_pull_request",
	showKindPullRequests: "list_pull_requests",
	showKindDiff:         "get_pr_diff",
}

// showKinds is the order the kinds are described in.
var showKinds = []string{showKindPullRequest, showKindPullRequests, showKindDiff}

// ShowInput is the argument set for show.
type ShowInput struct {
	Kind    string `json:"kind"`
	Project string `json:"project,omitempty" jsonschema:"Bitbucket project key"`
	Repo    string `json:"repo,omitempty" jsonschema:"Repository slug"`
	ID      string `json:"id,omitempty" jsonschema:"Pull request ID, for kind pull_request and diff"`
	// State's description is set in specShow, from the values the service
	// accepts, as list_pull_requests does.
	State string `json:"state,omitempty"`
	Role  string `json:"role,omitempty" jsonschema:"For kind pull_requests without repo: REVIEWER, AUTHOR or PARTICIPANT. Omit for all three"`
	Limit int    `json:"limit,omitempty" jsonschema:"For kind pull_requests: how many to show (default 25)"`
}

// ShowOutput says whether anything was shown, and what.
type ShowOutput struct {
	Shown  bool   `json:"shown" jsonschema:"True when the view was sent to a client that displays views; false when the client displays none, so nothing was shown"`
	Kind   string `json:"kind"`
	Target string `json:"target,omitempty" jsonschema:"What the view shows, such as PROJ/repo#42"`
	// LimitReached is for kind pull_requests, which stops at limit as
	// list_pull_requests does; it is false for the others.
	LimitReached bool `json:"limit_reached" jsonschema:"True when a list stopped at limit, so there may be more than the view shows"`
}

func specShow() Spec {
	tool := &mcp.Tool{
		Name: "show",
		Description: "Show the person a pull request, a list of pull requests, or a pull request's diff as an interactive view, " +
			"in clients that display MCP Apps views. Call it once, after you have what you need and before your answer, for what the " +
			"person should see; use the other tools to find it. kind pull_request and kind diff take project, repo and id; kind " +
			"pull_requests takes the filters list_pull_requests takes. In a client that displays no views, it shows nothing and says so.",
		Annotations: readOnly("Show a view"),
		InputSchema: showInputSchema(showKinds),
		Meta:        viewToolMeta(),
	}
	titled(tool)

	return Spec{
		Tool:  tool,
		Asks:  AsksNever,
		Needs: sortedValues(showKindTools),
		Register: func(server *mcp.Server, clients Clients) {
			mcp.AddTool(server, tool, showHandler(clients, showKinds))
		},
		RegisterExposed: func(server *mcp.Server, clients Clients, exposed map[string]bool) {
			kinds := offeredShowKinds(exposed)
			// The schema names only the kinds this server offers, so a model is
			// not told about a view it would be refused.
			offered := *tool
			offered.InputSchema = showInputSchema(kinds)
			mcp.AddTool(server, &offered, showHandler(clients, kinds))
		},
	}
}

// offeredShowKinds are the kinds whose data tools are exposed.
func offeredShowKinds(exposed map[string]bool) []string {
	kinds := make([]string, 0, len(showKinds))
	for _, kind := range showKinds {
		if exposed[showKindTools[kind]] {
			kinds = append(kinds, kind)
		}
	}
	return kinds
}

func showInputSchema(kinds []string) *jsonschema.Schema {
	schema, err := jsonschema.For[ShowInput](nil)
	if err != nil {
		panic(fmt.Sprintf("deriving input schema for ShowInput: %v", err))
	}
	schema.Properties["kind"].Enum = make([]any, len(kinds))
	for i, kind := range kinds {
		schema.Properties["kind"].Enum[i] = kind
	}
	schema.Properties["kind"].Description = "What to show: " + strings.Join(kinds, ", ")
	schema.Properties["state"].Description = "For kind pull_requests, in any case: " +
		strings.Join(openapi.PullRequestStateFilters, ", ") + ". Defaults to open."
	return schema
}

// viewToolMeta names the page a client renders the tool's answer in. The flat
// ui/resourceUri key is the form the specification deprecates; the reference
// SDK still writes both, and hosts read either.
func viewToolMeta() mcp.Meta {
	return mcp.Meta{
		"ui":             map[string]any{"resourceUri": viewURI},
		"ui/resourceUri": viewURI,
	}
}

// viewResourceMeta is the page's own metadata. It declares no domains, so the
// view keeps the default policy: no network at all. Every image it shows,
// avatars included, arrives inside the result as data.
func viewResourceMeta() mcp.Meta {
	return mcp.Meta{
		"ui": map[string]any{
			"csp":           map[string]any{},
			"prefersBorder": true,
		},
	}
}

func viewResource() *mcp.Resource {
	return &mcp.Resource{
		URI:         viewURI,
		Name:        "view",
		Title:       "Bitbucket view",
		Description: "The page bb's views render in. The show tool names it; it holds no Bitbucket data.",
		MIMEType:    viewMIMEType,
		Meta:        viewResourceMeta(),
	}
}

func readViewResource(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{
		URI:      req.Params.URI,
		MIMEType: viewMIMEType,
		Text:     viewPage(),
		Meta:     viewResourceMeta(),
	}}}, nil
}

// clientDisplaysViews reports whether the calling client renders MCP Apps
// views. A client that declares the extension names the types it renders;
// one that declares it without naming any is taken to render the only type
// there is.
func clientDisplaysViews(req *mcp.CallToolRequest) bool {
	if req == nil {
		return false
	}
	capabilities := req.ClientCapabilities()
	if capabilities == nil {
		return false
	}
	settings, ok := capabilities.Extensions[appsExtension]
	if !ok {
		return false
	}
	declared, _ := settings.(map[string]any)
	switch types := declared["mimeTypes"].(type) {
	case nil:
		return true
	case []string:
		for _, value := range types {
			if value == viewMIMEType {
				return true
			}
		}
	case []any:
		for _, value := range types {
			if text, _ := value.(string); text == viewMIMEType {
				return true
			}
		}
	}
	return false
}

func showHandler(c Clients, kinds []string) mcp.ToolHandlerFor[ShowInput, ShowOutput] {
	offered := toSet(kinds)
	return func(ctx context.Context, req *mcp.CallToolRequest, in ShowInput) (*mcp.CallToolResult, ShowOutput, error) {
		if !offered[in.Kind] {
			return nil, ShowOutput{}, fmt.Errorf("show cannot show %q here; it shows %s", in.Kind, strings.Join(kinds, ", "))
		}
		if err := checkShowInput(in); err != nil {
			return nil, ShowOutput{}, err
		}
		target := showTarget(in)

		// Nothing is fetched for a client that shows nothing: the answer
		// tells the model to use its own words instead.
		if !clientDisplaysViews(req) {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{
				Text: "This client displays no views, so nothing was shown. Give the person what they asked for in your answer instead.",
			}}}, ShowOutput{Shown: false, Kind: in.Kind, Target: target}, nil
		}

		payload, summary, err := buildView(ctx, c, in)
		if err != nil {
			return nil, ShowOutput{}, fmt.Errorf("show failed: %w", err)
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: summary}},
			Meta:    mcp.Meta{viewPayloadKey: payload},
		}, ShowOutput{Shown: true, Kind: in.Kind, Target: target, LimitReached: payload.LimitReached}, nil
	}
}

func checkShowInput(in ShowInput) error {
	switch in.Kind {
	case showKindPullRequest, showKindDiff:
		if in.Project == "" || in.Repo == "" || in.ID == "" {
			return fmt.Errorf("kind %s needs project, repo and id", in.Kind)
		}
	case showKindPullRequests:
		if in.ID != "" {
			return fmt.Errorf("kind pull_requests takes no id; use kind pull_request to show one")
		}
	}
	return nil
}

func showTarget(in ShowInput) string {
	switch {
	case in.ID != "":
		return fmt.Sprintf("%s/%s#%s", in.Project, in.Repo, in.ID)
	case in.Repo != "":
		return in.Project + "/" + in.Repo
	default:
		return in.Project
	}
}

// viewPayload is what the view renders. It travels in the result, so a view
// re-rendered from a stored conversation needs no server.
type viewPayload struct {
	Version      int               `json:"version"`
	Kind         string            `json:"kind"`
	GeneratedAt  string            `json:"generated_at"`
	PullRequest  *viewPullRequest  `json:"pull_request,omitempty"`
	PullRequests []viewPullRequest `json:"pull_requests,omitempty"`
	LimitReached bool              `json:"limit_reached,omitempty"`
	Diff         *viewDiff         `json:"diff,omitempty"`
	// Avatars maps a username to its avatar as a data: URI. A user missing
	// from it is drawn with initials.
	Avatars map[string]string `json:"avatars,omitempty"`
}

type viewPullRequest struct {
	pullrequestservice.PullRequest
	URL           string                            `json:"url"`
	ReviewSummary *pullrequestservice.ReviewSummary `json:"review_summary,omitempty"`
	Checks        []viewCheck                       `json:"checks,omitempty"`
	// CheckCounts is the build count per state on the source commit, for a
	// list, where each pull request's checks are counted rather than listed.
	CheckCounts *viewCheckCounts              `json:"check_counts,omitempty"`
	AutoMerge   *pullrequestservice.AutoMerge `json:"auto_merge,omitempty"`
}

type viewCheck struct {
	Name  string `json:"name,omitempty"`
	Key   string `json:"key,omitempty"`
	State string `json:"state"`
	URL   string `json:"url,omitempty"`
}

type viewCheckCounts struct {
	Successful int `json:"successful"`
	Failed     int `json:"failed"`
	InProgress int `json:"in_progress"`
	Cancelled  int `json:"cancelled"`
	Unknown    int `json:"unknown"`
}

type viewDiff struct {
	Patch string `json:"patch"`
	// Truncated says the patch stops before the end of the diff, at a file
	// boundary, because the whole of it is more than a view carries.
	Truncated bool `json:"truncated,omitempty"`
}

// maxViewPatchBytes bounds the diff a view carries. The payload lives in the
// conversation, so a diff larger than this is cut at a file boundary and the
// view links to the rest in Bitbucket.
const maxViewPatchBytes = 256 << 10

func buildView(ctx context.Context, c Clients, in ShowInput) (viewPayload, string, error) {
	payload := viewPayload{
		Version:     viewPayloadVersion,
		Kind:        in.Kind,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
	}

	switch in.Kind {
	case showKindPullRequest:
		pr, err := pullRequestForView(ctx, c, in)
		if err != nil {
			return viewPayload{}, "", err
		}
		payload.PullRequest = &pr
		payload.Avatars = fetchAvatars(ctx, c, peopleOf(pr.PullRequest))
		return payload, summarizePullRequest(pr), nil
	case showKindPullRequests:
		list, err := listPullRequests(ctx, c, ListPullRequestsInput{
			Project: in.Project, Repo: in.Repo, State: in.State, Role: in.Role, Limit: in.Limit,
		})
		if err != nil {
			return viewPayload{}, "", err
		}
		payload.PullRequests = pullRequestsForView(ctx, c, list.PullRequests)
		payload.LimitReached = list.LimitReached
		people := map[string]string{}
		for _, pr := range list.PullRequests {
			for username, slug := range peopleOf(pr) {
				people[username] = slug
			}
		}
		payload.Avatars = fetchAvatars(ctx, c, people)
		return payload, summarizePullRequests(payload.PullRequests, list.LimitReached), nil
	case showKindDiff:
		pr, err := pullrequestservice.NewService(c.HTTP).Get(ctx, pullrequestservice.RepositoryRef{ProjectKey: in.Project, Slug: in.Repo}, in.ID)
		if err != nil {
			return viewPayload{}, "", err
		}
		result, err := diffservice.NewService(c.OpenAPI).DiffPR(ctx, diffservice.DiffPRInput{
			Repository:    diffservice.RepositoryRef{ProjectKey: in.Project, Slug: in.Repo},
			PullRequestID: in.ID,
			Output:        diffservice.OutputKindRaw,
		})
		if err != nil {
			return viewPayload{}, "", err
		}
		patch, truncated := truncatePatch(result.Patch, maxViewPatchBytes)
		payload.PullRequest = &viewPullRequest{PullRequest: pr, URL: pullRequestURL(c.BaseURL, in.Project, in.Repo, in.ID)}
		payload.Diff = &viewDiff{Patch: patch, Truncated: truncated}
		payload.Avatars = fetchAvatars(ctx, c, map[string]string{pr.AuthorUsername: pr.AuthorSlug})
		return payload, summarizeDiff(in, pr, result.Patch), nil
	}

	return viewPayload{}, "", fmt.Errorf("unknown kind %q", in.Kind)
}

// pullRequestForView reads a pull request with everything its card shows:
// the review summary, the checks on its source commit and auto-merge. The
// checks and auto-merge are extras: a Bitbucket that cannot answer for them
// leaves them out rather than failing the view.
func pullRequestForView(ctx context.Context, c Clients, in ShowInput) (viewPullRequest, error) {
	out, err := pullRequestWithReviewSummary(ctx, c, GetPullRequestInput{Project: in.Project, Repo: in.Repo, ID: in.ID})
	if err != nil {
		return viewPullRequest{}, err
	}

	view := viewPullRequest{
		PullRequest:   out.PullRequest,
		URL:           pullRequestURL(c.BaseURL, in.Project, in.Repo, in.ID),
		ReviewSummary: &out.ReviewSummary,
	}

	// The checks come from the pull request's own source commit, which is in
	// the scope the call was bound to. That is why a scoped server shows them
	// here while it withholds get_build_status, which takes any commit.
	if commit := out.PullRequest.SourceCommit; commit != "" {
		if statuses, err := qualityservice.NewService(c.OpenAPI).GetBuildStatuses(ctx, commit, 100, ""); err == nil {
			view.Checks = make([]viewCheck, 0, len(statuses))
			for _, status := range statuses {
				check := viewCheck{
					Name: safederef.String(status.Name),
					Key:  safederef.String(status.Key),
					URL:  safederef.String(status.Url),
				}
				if status.State != nil {
					check.State = string(*status.State)
				}
				view.Checks = append(view.Checks, check)
			}
		}
	}

	ref := pullrequestservice.RepositoryRef{ProjectKey: in.Project, Slug: in.Repo}
	if autoMerge, err := pullrequestservice.NewService(c.HTTP).GetAutoMerge(ctx, ref, in.ID); err == nil && autoMerge.Enabled {
		view.AutoMerge = &autoMerge
	}

	return view, nil
}

// pullRequestsForView adds each pull request's link and its build counts,
// which one request fetches for every source commit at once.
func pullRequestsForView(ctx context.Context, c Clients, prs []pullrequestservice.PullRequest) []viewPullRequest {
	views := make([]viewPullRequest, 0, len(prs))
	commits := make([]string, 0, len(prs))
	for _, pr := range prs {
		view := viewPullRequest{PullRequest: pr}
		if pr.Repository != nil {
			view.URL = pullRequestURL(c.BaseURL, pr.Repository.ProjectKey, pr.Repository.Slug, strconv.FormatInt(pr.ID, 10))
		}
		views = append(views, view)
		if pr.SourceCommit != "" {
			commits = append(commits, pr.SourceCommit)
		}
	}
	if len(commits) == 0 {
		return views
	}

	stats, err := qualityservice.NewService(c.OpenAPI).GetMultipleBuildStatusStats(ctx, commits)
	if err != nil {
		return views
	}
	for i := range views {
		// Bitbucket leaves a commit with no builds out of the answer.
		counts, ok := stats[views[i].SourceCommit]
		if !ok {
			continue
		}
		views[i].CheckCounts = &viewCheckCounts{
			Successful: intValue(counts.Successful),
			Failed:     intValue(counts.Failed),
			InProgress: intValue(counts.InProgress),
			Cancelled:  intValue(counts.Cancelled),
			Unknown:    intValue(counts.Unknown),
		}
	}
	return views
}

func pullRequestURL(baseURL, project, repo, id string) string {
	return fmt.Sprintf("%s/projects/%s/repos/%s/pull-requests/%s/overview",
		strings.TrimRight(baseURL, "/"), url.PathEscape(project), url.PathEscape(repo), url.PathEscape(id))
}

// peopleOf maps the username of everyone a pull request's card draws to the
// slug their avatar is addressed by.
func peopleOf(pr pullrequestservice.PullRequest) map[string]string {
	people := map[string]string{}
	if pr.AuthorUsername != "" && pr.AuthorSlug != "" {
		people[pr.AuthorUsername] = pr.AuthorSlug
	}
	for _, reviewer := range pr.Reviewers {
		if reviewer.Name != "" && reviewer.Slug != "" {
			people[reviewer.Name] = reviewer.Slug
		}
	}
	return people
}

// truncatePatch cuts a patch to at most limit bytes, at the start of a file,
// so the view never draws half a file.
func truncatePatch(patch string, limit int) (string, bool) {
	if len(patch) <= limit {
		return patch, false
	}
	const header = "\ndiff --git "
	// The last file that starts within the limit. Its header may run past
	// the limit, so the search does too.
	search := patch[:min(len(patch), limit+len(header))]
	for {
		cut := strings.LastIndex(search, header)
		if cut < 0 {
			return "", true
		}
		if cut+1 <= limit {
			return patch[:cut+1], true
		}
		search = search[:cut]
	}
}

// The summaries are the text a model reads beside the view: enough to answer
// with if it called show without looking first, and short, because the person
// has the view.

func summarizePullRequest(pr viewPullRequest) string {
	var lines []string
	lines = append(lines, fmt.Sprintf("Showed the person pull request %s/%s#%d as an interactive card: %q, %s%s, by %s, %s into %s.",
		repositoryKey(pr.PullRequest), repositorySlug(pr.PullRequest), pr.ID, pr.Title, strings.ToLower(pr.State), draftNote(pr.PullRequest),
		pr.Author, pr.SourceBranch, pr.TargetBranch))
	approved, requested := 0, 0
	for _, reviewer := range pr.Reviewers {
		switch {
		case reviewer.Approved:
			approved++
		case reviewer.Status == "NEEDS_WORK":
			requested++
		}
	}
	reviewers := fmt.Sprintf("Reviewers: %d approved", approved)
	if requested > 0 {
		reviewers += fmt.Sprintf(", %d changes requested", requested)
	}
	lines = append(lines, reviewers+fmt.Sprintf(", of %d.", len(pr.Reviewers)))
	if len(pr.Checks) > 0 {
		counts := map[string]int{}
		for _, check := range pr.Checks {
			counts[buildStateWord(check.State)]++
		}
		lines = append(lines, "Builds: "+formatCounts(counts)+".")
	}
	if pr.AutoMerge != nil {
		lines = append(lines, "Auto-merge is on.")
	}
	return strings.Join(lines, " ")
}

func summarizePullRequests(prs []viewPullRequest, limitReached bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Showed the person %d pull requests as an interactive list", len(prs))
	if limitReached {
		b.WriteString(" (there may be more)")
	}
	b.WriteString(":")
	for _, pr := range prs {
		fmt.Fprintf(&b, "\n- %s/%s#%d %q, %s%s, by %s", repositoryKey(pr.PullRequest), repositorySlug(pr.PullRequest), pr.ID, pr.Title,
			strings.ToLower(pr.State), draftNote(pr.PullRequest), pr.Author)
	}
	return b.String()
}

func summarizeDiff(in ShowInput, pr pullrequestservice.PullRequest, patch string) string {
	files, additions, deletions := diffCounts(patch)
	return fmt.Sprintf("Showed the person the diff of %s/%s#%s %q as an interactive view: %d files, +%d -%d.",
		in.Project, in.Repo, in.ID, pr.Title, files, additions, deletions)
}

// diffCounts counts a unified diff's files and changed lines.
func diffCounts(patch string) (files, additions, deletions int) {
	for _, line := range strings.Split(patch, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			files++
		case strings.HasPrefix(line, "+++ "), strings.HasPrefix(line, "--- "):
		case strings.HasPrefix(line, "+"):
			additions++
		case strings.HasPrefix(line, "-"):
			deletions++
		}
	}
	return files, additions, deletions
}

// buildStateWord is a build state in the words Bitbucket's UI uses for it.
func buildStateWord(state string) string {
	switch strings.ToUpper(state) {
	case "SUCCESSFUL":
		return "passed"
	case "FAILED":
		return "failed"
	case "INPROGRESS":
		return "in progress"
	case "CANCELLED":
		return "canceled"
	default:
		return "unknown"
	}
}

func formatCounts(counts map[string]int) string {
	states := make([]string, 0, len(counts))
	for state := range counts {
		states = append(states, state)
	}
	sort.Strings(states)
	parts := make([]string, 0, len(states))
	for _, state := range states {
		parts = append(parts, fmt.Sprintf("%d %s", counts[state], state))
	}
	return strings.Join(parts, ", ")
}

func draftNote(pr pullrequestservice.PullRequest) string {
	if pr.Draft {
		return " (draft)"
	}
	return ""
}

func repositoryKey(pr pullrequestservice.PullRequest) string {
	if pr.Repository == nil {
		return ""
	}
	return pr.Repository.ProjectKey
}

func repositorySlug(pr pullrequestservice.PullRequest) string {
	if pr.Repository == nil {
		return ""
	}
	return pr.Repository.Slug
}

func intValue(value *int32) int {
	return int(safederef.Int32(value))
}

func sortedValues(m map[string]string) []string {
	values := make([]string, 0, len(m))
	for _, value := range m {
		values = append(values, value)
	}
	sort.Strings(values)
	return values
}

// Avatars.
//
// bb fetches them from the configured Bitbucket with its own credentials and
// hands them to the view as data, so the view needs no network: it inherits
// bb's CA bundle, client certificate, proxy and allowed_hosts policy, and a
// view re-rendered from a stored conversation still has them. They come from
// Bitbucket's own /users/{slug}/avatar.png, never from the avatarUrl the REST
// API returns, which is usually Gravatar: a third party, over plain http, with
// a hash of the user's email in its URL.

const (
	avatarSize = 64
	// maxAvatarBytes bounds one avatar. Bitbucket's are a few kilobytes at
	// this size.
	maxAvatarBytes = 256 << 10
	// avatarBudget bounds how long a view waits for avatars. One that is not
	// in by then is drawn with initials.
	avatarBudget = 5 * time.Second
	// maxAvatarFetches bounds the concurrent avatar requests of one view.
	maxAvatarFetches = 4
)

// avatarImageTypes are the image types a view may show as an avatar, by
// content. SVG is not among them: a raster is all an avatar needs.
var avatarImageTypes = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/gif":  true,
	"image/webp": true,
}

// avatars caches each fetched avatar for the life of the server, keyed by the
// Bitbucket it came from and the user's slug.
var avatars sync.Map

// fetchAvatars returns the avatars of the given people, as data: URIs keyed
// by username. A user whose avatar cannot be fetched in time is left out.
func fetchAvatars(ctx context.Context, c Clients, people map[string]string) map[string]string {
	if len(people) == 0 || c.HTTP == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, avatarBudget)
	defer cancel()

	var (
		mu     sync.Mutex
		wg     sync.WaitGroup
		result = map[string]string{}
		slots  = make(chan struct{}, maxAvatarFetches)
	)
	for username, slug := range people {
		key := c.BaseURL + "\x00" + slug
		if cached, ok := avatars.Load(key); ok {
			if uri, isURI := cached.(string); isURI {
				result[username] = uri
				continue
			}
		}
		wg.Add(1)
		go func(username, slug, key string) {
			defer wg.Done()
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
			case <-ctx.Done():
				return
			}
			uri, err := fetchAvatar(ctx, c.HTTP, slug)
			if err != nil {
				return
			}
			avatars.Store(key, uri)
			mu.Lock()
			result[username] = uri
			mu.Unlock()
		}(username, slug, key)
	}
	wg.Wait()

	if len(result) == 0 {
		return nil
	}
	return result
}

func fetchAvatar(ctx context.Context, client *httpclient.Client, slug string) (string, error) {
	var body bytes.Buffer
	_, err := client.Download(ctx, httpclient.RequestOptions{
		Path:  "/users/" + url.PathEscape(slug) + "/avatar.png",
		Query: url.Values{"s": {strconv.Itoa(avatarSize)}},
	}, download.To(&body), maxAvatarBytes)
	if err != nil {
		return "", err
	}
	return avatarDataURI(body.Bytes())
}

// avatarDataURI turns an avatar's bytes into a data: URI, refusing anything
// that is not one of the raster types a view shows. The type comes from the
// content, not from what the server said it was.
func avatarDataURI(data []byte) (string, error) {
	if len(data) == 0 {
		return "", fmt.Errorf("the avatar is empty")
	}
	contentType := http.DetectContentType(data)
	if !avatarImageTypes[contentType] {
		return "", fmt.Errorf("the avatar is %s, not an image a view shows", contentType)
	}
	return "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

// The page.
//
// Plain JavaScript and CSS, embedded in the binary and assembled into one
// self-contained document the first time a client reads it: no build step, no
// package manager, and a `go build` that always includes the views.

//go:embed views/page.html views/view.css views/*.js
var viewAssets embed.FS

// viewScripts are the page's scripts, in the order they are concatenated into
// one. main.js starts the page, so it comes last.
var viewScripts = []string{
	"bridge.js",
	"dom.js",
	"format.js",
	"pull_request.js",
	"pull_requests.js",
	"diff.js",
	"main.js",
}

const (
	viewStylePlaceholder  = "/*BB_VIEW_STYLE*/"
	viewScriptPlaceholder = "/*BB_VIEW_SCRIPT*/"
)

var viewPage = sync.OnceValue(func() string {
	page := mustReadViewAsset("page.html")
	style := mustReadViewAsset("view.css")

	var script strings.Builder
	// One scope for every script, so nothing the page defines is global.
	script.WriteString("(() => {\n\"use strict\";\n")
	for _, name := range viewScripts {
		script.WriteString("\n// ---- " + name + " ----\n")
		script.WriteString(mustReadViewAsset(name))
	}
	script.WriteString("\n})();\n")

	if !strings.Contains(page, viewStylePlaceholder) || !strings.Contains(page, viewScriptPlaceholder) {
		panic("views/page.html has lost a placeholder")
	}
	page = strings.Replace(page, viewStylePlaceholder, style, 1)
	return strings.Replace(page, viewScriptPlaceholder, script.String(), 1)
})

func mustReadViewAsset(name string) string {
	data, err := viewAssets.ReadFile("views/" + name)
	if err != nil {
		panic(fmt.Sprintf("reading view asset %s: %v", name, err))
	}
	return string(data)
}
