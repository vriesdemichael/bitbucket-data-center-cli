package mcp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/mcp/highlight"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
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
	showKindThreads      = "threads"
	// showKindPullRequestForm is a pull request for the person to finish and
	// submit, offered while the tool that creates one is.
	showKindPullRequestForm = "pull_request_form"
	showKindFile            = "file"
)

var showKindTools = map[string]string{
	showKindPullRequest:     "get_pull_request",
	showKindPullRequests:    "list_pull_requests",
	showKindDiff:            "get_pr_diff",
	showKindThreads:         "list_pr_comments",
	showKindPullRequestForm: "create_pull_request",
	showKindFile:            "get_file_content",
}

// showKinds is the order the kinds are described in.
var showKinds = []string{showKindPullRequest, showKindPullRequests, showKindDiff, showKindThreads, showKindPullRequestForm, showKindFile}

// viewActionTools are the model's tools a view calls for the person: what a
// click in a view does goes through them, so the scope, the audit trail and
// the confirmations of the tools that ask apply to it as to the model's call.
var viewActionTools = []string{"add_pr_comment", "submit_pr_review", "create_pull_request", "update_pull_request"}

// viewOffers is what a server lets its views do beyond drawing: the kinds a
// view can open in place, and the tools it can call for the person. A view
// offers only what works here, so a server that leaves a tool out has views
// without its button.
type viewOffers struct {
	Kinds []string `json:"kinds"`
	Tools []string `json:"tools,omitempty"`
	// templates highlights the code inside templates (--highlight-templates).
	// It decides what the view is sent, not what it may do, so it is not sent.
	templates bool
}

// offersFor is what views may do on a server with these options that exposes
// these tools.
func offersFor(opts ServerOptions, exposed map[string]bool) viewOffers {
	offers := viewOffers{Kinds: offeredShowKinds(exposed), templates: opts.HighlightTemplates}
	for _, tool := range append(slices.Clone(viewActionTools), viewHelperTools...) {
		if exposed[tool] {
			offers.Tools = append(offers.Tools, tool)
		}
	}
	return offers
}

// allOffers is what views may do on a server that exposes every tool.
func allOffers() viewOffers {
	exposed := map[string]bool{}
	for _, spec := range AllSpecs() {
		exposed[spec.Tool.Name] = true
	}
	return offersFor(ServerOptions{}, exposed)
}

// kind reports whether a view may open the kind.
func (offers viewOffers) kind(kind string) bool {
	return slices.Contains(offers.Kinds, kind)
}

// ShowInput is the argument set for show.
type ShowInput struct {
	Kind    string `json:"kind"`
	Project string `json:"project,omitempty" jsonschema:"Bitbucket project key"`
	Repo    string `json:"repo,omitempty" jsonschema:"Repository slug"`
	ID      string `json:"id,omitempty" jsonschema:"Pull request ID, for kind pull_request, diff and threads"`
	// State's description is set in specShow, from the values the service
	// accepts, as list_pull_requests does.
	State string `json:"state,omitempty"`
	Role  string `json:"role,omitempty" jsonschema:"For kind pull_requests without repo: REVIEWER, AUTHOR or PARTICIPANT. Omit for all three"`
	Limit int    `json:"limit,omitempty" jsonschema:"For kind pull_requests: how many to show (default 25)"`
	// What the model drafted for kind pull_request_form, named as
	// create_pull_request names them.
	FromRef     string `json:"from_ref,omitempty" jsonschema:"For kind pull_request_form: the source branch"`
	ToRef       string `json:"to_ref,omitempty" jsonschema:"For kind pull_request_form: the target branch; omit for the repository's default branch"`
	Title       string `json:"title,omitempty" jsonschema:"For kind pull_request_form: the title you drafted"`
	Description string `json:"description,omitempty" jsonschema:"For kind pull_request_form: the description you drafted, in Markdown"`
	Reviewers   string `json:"reviewers,omitempty" jsonschema:"For kind pull_request_form: comma-separated reviewer usernames"`
	Draft       bool   `json:"draft,omitempty" jsonschema:"For kind pull_request_form: open it as a draft"`
	// The file for kind file, named as get_file_content names it.
	Path      string `json:"path,omitempty" jsonschema:"For kind file: the file's path in the repository"`
	At        string `json:"at,omitempty" jsonschema:"For kind file: the branch, tag or commit to read it at; omit for the default branch"`
	StartLine int    `json:"start_line,omitempty" jsonschema:"For kind file: the first line to show (default 1)"`
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
		Description: "Show the person a pull request, a list of pull requests, a pull request's diff or its comment threads as an " +
			"interactive view, in clients that display MCP Apps views, or a pull request form for them to finish and submit. Call it once, " +
			"after you have what you need and before your answer, for what the person should see; use the other tools to find it. kinds " +
			"pull_request, diff and threads take project, repo and id; kind pull_requests takes the filters list_pull_requests takes. " +
			"Kind pull_request_form takes project, repo, from_ref and what you drafted (title, description, to_ref, reviewers, draft), " +
			"or an id to edit that pull request; nothing is created or changed until the person submits it. Kind file takes project, " +
			"repo, path and at, and shows code, a picture, audio, a video or an archive's listing. A diff or a file is for what the " +
			"person cannot open in their own editor, such as another repository's. In a client that displays no views, it shows " +
			"nothing and says so.",
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
			mcp.AddTool(server, tool, showHandler(clients, allOffers()))
		},
		RegisterExposed: func(server *mcp.Server, opts ServerOptions, exposed map[string]bool) {
			offers := offersFor(opts, exposed)
			// The schema names only the kinds this server offers, so a model is
			// not told about a view it would be refused.
			offered := *tool
			offered.InputSchema = showInputSchema(offers.Kinds)
			mcp.AddTool(server, &offered, showHandler(opts.Clients, offers))
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

func showHandler(c Clients, offers viewOffers) mcp.ToolHandlerFor[ShowInput, ShowOutput] {
	return func(ctx context.Context, req *mcp.CallToolRequest, in ShowInput) (*mcp.CallToolResult, ShowOutput, error) {
		if !offers.kind(in.Kind) {
			return nil, ShowOutput{}, fmt.Errorf("show cannot show %q here; it shows %s", in.Kind, strings.Join(offers.Kinds, ", "))
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

		payload, people, summary, err := buildView(ctx, c, in, offers)
		if err != nil {
			return nil, ShowOutput{}, fmt.Errorf("show failed: %w", err)
		}
		payload.Avatars = fetchAvatars(ctx, c, people)
		payload.Offers = &offers
		withHighlights(&payload, offers)
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: summary.shown()}},
			Meta:    mcp.Meta{viewPayloadKey: payload},
		}, ShowOutput{Shown: true, Kind: in.Kind, Target: target, LimitReached: payload.LimitReached}, nil
	}
}

func checkShowInput(in ShowInput) error {
	switch in.Kind {
	case showKindFile:
		if in.Project == "" || in.Repo == "" || strings.TrimSpace(in.Path) == "" {
			return fmt.Errorf("kind file needs project, repo and path")
		}
	case showKindPullRequestForm:
		if in.Project == "" || in.Repo == "" {
			return fmt.Errorf("kind pull_request_form needs project and repo")
		}
		if in.ID == "" && strings.TrimSpace(in.FromRef) == "" {
			return fmt.Errorf("kind pull_request_form needs from_ref for a new pull request, or id to edit one")
		}
	case showKindPullRequest, showKindDiff, showKindThreads:
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
	Threads      *viewThreads      `json:"threads,omitempty"`
	// Avatars maps a username to its avatar as a data: URI. A user missing
	// from it is drawn with initials.
	Avatars map[string]string `json:"avatars,omitempty"`
	// Show is the call the payload answers, which the view repeats to keep
	// itself current, and Fingerprint identifies what the payload shows: a
	// refresh sends the data again only when it changes.
	Show        *ShowInput `json:"show,omitempty"`
	Fingerprint string     `json:"fingerprint,omitempty"`
	// Offers is what the view may do here beyond drawing. It is the same for
	// every read, so it is set after the fingerprint is taken.
	Offers *viewOffers `json:"offers,omitempty"`
	// Me is the person bb acts for, as the pull request sees them, so a view
	// offers a review only to someone who can give one.
	Me *viewMe `json:"me,omitempty"`
	// Form is a pull request for the person to finish, for kind
	// pull_request_form.
	Form *viewForm `json:"form,omitempty"`
	// File is a file, for kind file.
	File *viewFile `json:"file,omitempty"`
}

// viewMe is the person bb acts for, on one pull request: whether they wrote
// it, and how they reviewed it, in Bitbucket's words (APPROVED, NEEDS_WORK,
// UNAPPROVED), empty when they are not among its reviewers.
type viewMe struct {
	Username string `json:"username"`
	Author   bool   `json:"author,omitempty"`
	Status   string `json:"status,omitempty"`
}

// meFor is username on the pull request.
func meFor(username string, pr pullrequestservice.PullRequest) *viewMe {
	if username == "" {
		return nil
	}
	me := &viewMe{Username: username, Author: strings.EqualFold(pr.AuthorUsername, username)}
	for _, reviewer := range pr.Reviewers {
		if !strings.EqualFold(reviewer.Name, username) {
			continue
		}
		me.Status = reviewer.Status
		if reviewer.Approved {
			me.Status = "APPROVED"
		}
	}
	return me
}

// currentUsers caches who bb acts for on each Bitbucket, for the life of the
// server: the credentials do not change while it runs.
var currentUsers sync.Map

// currentUsername is who bb acts for, as Bitbucket says on any answer, or
// empty when it does not say.
func currentUsername(ctx context.Context, c Clients) string {
	if cached, ok := currentUsers.Load(c.BaseURL); ok {
		if username, isString := cached.(string); isString {
			return username
		}
	}
	if c.HTTP == nil {
		return ""
	}
	username, err := c.HTTP.CurrentUserSlug(ctx)
	if err != nil {
		return ""
	}
	currentUsers.Store(c.BaseURL, username)
	return username
}

type viewPullRequest struct {
	pullrequestservice.PullRequest
	URL           string                            `json:"url"`
	ReviewSummary *pullrequestservice.ReviewSummary `json:"review_summary,omitempty"`
	// Checks are the builds a card lists: at most maxViewChecks of them, the
	// ones that need attention first.
	Checks []viewCheck `json:"checks,omitempty"`
	// CheckCounts is the build count per state on the source commit, as
	// Bitbucket totals it: every build, however many the card lists. A view
	// counts from it, never from Checks.
	CheckCounts *viewCheckCounts              `json:"check_counts,omitempty"`
	AutoMerge   *pullrequestservice.AutoMerge `json:"auto_merge,omitempty"`
	// ChecksLimitReached says the card lists fewer builds than the commit has.
	ChecksLimitReached bool `json:"checks_limit_reached,omitempty"`
	// RequiredChecks are the builds the target branch requires before the
	// pull request merges, and where each stands; RequiredKnown says bb could
	// tell as Bitbucket's merge check would. Unknown, the card says nothing
	// about requirements.
	RequiredChecks []viewRequiredCheck `json:"required_checks,omitempty"`
	RequiredKnown  bool                `json:"required_known,omitempty"`
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

func (counts viewCheckCounts) total() int {
	return counts.Successful + counts.Failed + counts.InProgress + counts.Cancelled + counts.Unknown
}

// checkCountsOf is Bitbucket's build totals for a commit, as a view counts
// them.
func checkCountsOf(stats openapigenerated.RestBuildStats) *viewCheckCounts {
	return &viewCheckCounts{
		Successful: intValue(stats.Successful),
		Failed:     intValue(stats.Failed),
		InProgress: intValue(stats.InProgress),
		Cancelled:  intValue(stats.Cancelled),
		Unknown:    intValue(stats.Unknown),
	}
}

// countChecks counts listed builds by state, for when Bitbucket's totals are
// not to be had.
func countChecks(checks []viewCheck) viewCheckCounts {
	var counts viewCheckCounts
	for _, check := range checks {
		switch strings.ToUpper(check.State) {
		case "SUCCESSFUL":
			counts.Successful++
		case "FAILED":
			counts.Failed++
		case "INPROGRESS":
			counts.InProgress++
		case "CANCELLED":
			counts.Cancelled++
		default:
			counts.Unknown++
		}
	}
	return counts
}

// viewDiff is the diff a view draws: every file, with its change and counts,
// and the patch of as many of them as fit.
type viewDiff struct {
	Files []viewDiffFile `json:"files"`
	Patch string         `json:"patch"`
	// Truncated says some files' changes are not in the patch.
	Truncated bool `json:"truncated,omitempty"`
	// Highlight are the spans of each code line of each file in the patch,
	// keyed by the file's place among the patch's files, for the languages
	// the highlighter knows and as far as it got in its time.
	Highlight map[int][]string `json:"highlight,omitempty"`
}

// highlightBudget is how long a view's code is highlighted for. Code not
// reached by then is drawn plain.
const highlightBudget = 2 * time.Second

// highlighting is how far code is highlighted from now on, for views.
func (offers viewOffers) highlighting() highlight.Options {
	return highlight.Options{Deadline: time.Now().Add(highlightBudget), Templates: offers.templates}
}

// withHighlights colours the diff a payload carries, once it is to be sent:
// an answer that finds nothing changed sends none, and pays for none.
func withHighlights(payload *viewPayload, offers viewOffers) {
	if payload.Diff == nil || payload.Diff.Patch == "" {
		return
	}
	if spans := highlight.PatchWith(payload.Diff.Patch, offers.highlighting()); len(spans) > 0 {
		payload.Diff.Highlight = spans
	}
}

// viewDiffFile is one file of a diff, as the view lists it.
type viewDiffFile struct {
	Path      string `json:"path"`
	OldPath   string `json:"old_path,omitempty"`
	Status    string `json:"status"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Binary    bool   `json:"binary,omitempty"`
	// Omitted says the file's changes are not in the patch: they are more than
	// a view carries for one file, or the patch was full.
	Omitted bool `json:"omitted,omitempty"`
}

// The payload lives in the conversation, so a view carries at most this much
// of a diff, and at most maxViewFileBytes of one file. A file that does not
// fit is still listed, with its counts, and the view links to it in Bitbucket.
const (
	maxViewPatchBytes = 256 << 10
	maxViewFileBytes  = 64 << 10
)

// maxViewChecks is how many builds a card lists. Its counts come from
// Bitbucket's totals, so they hold for any number of builds.
const maxViewChecks = 100

// maxViewPeople is how many avatars a view carries.
const maxViewPeople = 60

// buildView reads what a view of the kind shows, and what the model reads
// beside it. It names the people whose avatars the view draws and leaves
// fetching them to the caller, which needs them only for data it sends.
// offers says what else the server shows, which a view may carry some of.
func buildView(ctx context.Context, c Clients, in ShowInput, offers viewOffers) (viewPayload, map[string]string, viewSummary, error) {
	payload := viewPayload{
		Version:     viewPayloadVersion,
		Kind:        in.Kind,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
	}
	var (
		people  map[string]string
		summary viewSummary
	)

	switch in.Kind {
	case showKindPullRequest:
		pr, err := pullRequestForView(ctx, c, in)
		if err != nil {
			return viewPayload{}, nil, viewSummary{}, err
		}
		payload.PullRequest = &pr
		payload.Me = meFor(currentUsername(ctx, c), pr.PullRequest)
		people = peopleOf(pr.PullRequest, maxViewPeople)
		summary = summarizePullRequest(pr)
	case showKindPullRequests:
		list, err := listPullRequests(ctx, c, ListPullRequestsInput{
			Project: in.Project, Repo: in.Repo, State: in.State, Role: in.Role, Limit: in.Limit,
		})
		if err != nil {
			return viewPayload{}, nil, viewSummary{}, err
		}
		payload.PullRequests = pullRequestsForView(ctx, c, list.PullRequests)
		payload.LimitReached = list.LimitReached
		// A row draws its author and its first three reviewers, so those are
		// the avatars the list carries.
		people = map[string]string{}
		for _, pr := range list.PullRequests {
			shown := pr
			shown.Reviewers = sortedReviewers(pr.Reviewers)
			if len(shown.Reviewers) > 3 {
				shown.Reviewers = shown.Reviewers[:3]
			}
			for username, slug := range peopleOf(shown, maxViewPeople) {
				if len(people) < maxViewPeople {
					people[username] = slug
				}
			}
		}
		summary = summarizePullRequests(payload.PullRequests, list.LimitReached)
	case showKindDiff:
		pr, err := pullrequestservice.NewService(c.HTTP).Get(ctx, pullrequestservice.RepositoryRef{ProjectKey: in.Project, Slug: in.Repo}, in.ID)
		if err != nil {
			return viewPayload{}, nil, viewSummary{}, err
		}
		result, err := diffservice.NewService(c.OpenAPI).DiffPR(ctx, diffservice.DiffPRInput{
			Repository:    diffservice.RepositoryRef{ProjectKey: in.Project, Slug: in.Repo},
			PullRequestID: in.ID,
			Output:        diffservice.OutputKindRaw,
		})
		if err != nil {
			return viewPayload{}, nil, viewSummary{}, err
		}
		files, patch, truncated := splitPatch(result.Patch, maxViewFileBytes, maxViewPatchBytes)
		payload.PullRequest = &viewPullRequest{PullRequest: pr, URL: pullRequestURL(c.BaseURL, in.Project, in.Repo, in.ID)}
		payload.Diff = &viewDiff{Files: files, Patch: patch, Truncated: truncated}
		payload.Me = meFor(currentUsername(ctx, c), pr)
		people = map[string]string{pr.AuthorUsername: pr.AuthorSlug}
		// The diff draws each comment on its line, as Bitbucket's diff does,
		// where the server shows the threads at all. A Bitbucket that cannot
		// answer for them leaves the diff without them.
		if offers.kind(showKindThreads) {
			if threads, threadPeople, err := threadsForView(ctx, c, in, false); err == nil {
				payload.Threads = &threads
				for username, slug := range threadPeople {
					if len(people) < maxViewPeople {
						people[username] = slug
					}
				}
			}
		}
		summary = summarizeDiff(in, pr, result.Patch)
	case showKindThreads:
		pr, err := pullrequestservice.NewService(c.HTTP).Get(ctx, pullrequestservice.RepositoryRef{ProjectKey: in.Project, Slug: in.Repo}, in.ID)
		if err != nil {
			return viewPayload{}, nil, viewSummary{}, err
		}
		threads, threadPeople, err := threadsForView(ctx, c, in, true)
		if err != nil {
			return viewPayload{}, nil, viewSummary{}, err
		}
		payload.PullRequest = &viewPullRequest{PullRequest: pr, URL: pullRequestURL(c.BaseURL, in.Project, in.Repo, in.ID)}
		payload.Threads = &threads
		people = threadPeople
		summary = summarizeThreads(in, pr.Title, threads)
	case showKindPullRequestForm:
		form, pr, formSummary, err := formForView(ctx, c, in)
		if err != nil {
			return viewPayload{}, nil, viewSummary{}, err
		}
		payload.Form = &form
		payload.PullRequest = pr
		summary = formSummary
	case showKindFile:
		file, fileSummary, err := fileForView(ctx, c, in, offers)
		if err != nil {
			return viewPayload{}, nil, viewSummary{}, err
		}
		payload.File = &file
		summary = fileSummary
	default:
		return viewPayload{}, nil, viewSummary{}, fmt.Errorf("unknown kind %q", in.Kind)
	}

	show := in
	payload.Show = &show
	payload.Fingerprint = fingerprintOf(payload)
	return payload, people, summary, nil
}

// fingerprintOf identifies what a payload shows. When it was read, the
// avatars and the highlighting are left out: they differ between two reads of
// the same state, and the avatars go with the people, who are in the payload
// already.
func fingerprintOf(payload viewPayload) string {
	payload.GeneratedAt = ""
	payload.Avatars = nil
	payload.Fingerprint = ""
	// Highlighting stops at a deadline, so two reads of the same code can
	// colour it differently.
	if payload.Diff != nil {
		diff := *payload.Diff
		diff.Highlight = nil
		payload.Diff = &diff
	}
	if payload.File != nil {
		file := *payload.File
		file.Highlight = nil
		payload.File = &file
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		// Unreachable for a payload of plain data. An empty fingerprint
		// matches nothing a view holds, so the view is sent the data.
		return ""
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:16])
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
		view.CheckCounts, view.Checks, view.ChecksLimitReached = buildsForView(ctx, c, commit)
	}
	view.RequiredChecks, view.RequiredKnown = requiredChecksForView(ctx, c, in.Project, in.Repo, out.PullRequest)

	ref := pullrequestservice.RepositoryRef{ProjectKey: in.Project, Slug: in.Repo}
	if autoMerge, err := pullrequestservice.NewService(c.HTTP).GetAutoMerge(ctx, ref, in.ID); err == nil && autoMerge.Enabled {
		view.AutoMerge = &autoMerge
	}

	return view, nil
}

// buildsForView reads a commit's builds as a card shows them: Bitbucket's
// count per state, whole however many builds there are, and the builds
// themselves, at most maxViewChecks of them, the ones that need attention
// first. Either part is left out when Bitbucket cannot answer for it, and the
// last result says the card lists fewer builds than the commit has.
func buildsForView(ctx context.Context, c Clients, commit string) (*viewCheckCounts, []viewCheck, bool) {
	quality := qualityservice.NewService(c.OpenAPI)
	var counts *viewCheckCounts
	if stats, err := quality.GetBuildStatusStats(ctx, commit, false); err == nil {
		counts = checkCountsOf(stats)
	}

	// Ordered by state, the canceled, failed and running builds come before
	// the passes, so a list cut at maxViewChecks keeps them. Bitbucket orders
	// the states alphabetically, which puts UNKNOWN last, and its published
	// specification calls the order STATUS, which it refuses. A release that
	// refuses STATE as well still lists the builds, newest first.
	statuses, err := quality.GetBuildStatuses(ctx, commit, maxViewChecks, "STATE")
	if err != nil {
		statuses, err = quality.GetBuildStatuses(ctx, commit, maxViewChecks, "")
	}
	if err != nil {
		return counts, nil, false
	}
	checks := make([]viewCheck, 0, len(statuses))
	for _, status := range statuses {
		check := viewCheck{
			Name: safederef.String(status.Name),
			Key:  safederef.String(status.Key),
			URL:  safederef.String(status.Url),
		}
		if status.State != nil {
			check.State = string(*status.State)
		}
		checks = append(checks, check)
	}
	if counts != nil {
		return counts, checks, counts.total() > len(checks)
	}
	return nil, checks, len(checks) >= maxViewChecks
}

// pullRequestsForView adds each pull request's link and its build counts,
// which one request fetches for every source commit at once.
func pullRequestsForView(ctx context.Context, c Clients, prs []pullrequestservice.PullRequest) []viewPullRequest {
	views := make([]viewPullRequest, 0, len(prs))
	commits := make([]string, 0, len(prs))
	for _, pr := range prs {
		// A list draws no description, and twenty-five of them would weigh
		// more than the rest of the payload.
		pr.Description = ""
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
		if counts, ok := stats[views[i].SourceCommit]; ok {
			views[i].CheckCounts = checkCountsOf(counts)
		}
	}
	return views
}

func pullRequestURL(baseURL, project, repo, id string) string {
	return fmt.Sprintf("%s/projects/%s/repos/%s/pull-requests/%s/overview",
		strings.TrimRight(baseURL, "/"), url.PathEscape(project), url.PathEscape(repo), url.PathEscape(id))
}

// peopleOf maps the username of the author and the reviewers of a pull
// request, at most limit of them, to the slug their avatar is addressed by.
// The reviewers go in the order the view draws them.
func peopleOf(pr pullrequestservice.PullRequest, limit int) map[string]string {
	people := map[string]string{}
	if pr.AuthorUsername != "" && pr.AuthorSlug != "" {
		people[pr.AuthorUsername] = pr.AuthorSlug
	}
	for _, reviewer := range sortedReviewers(pr.Reviewers) {
		if len(people) >= limit {
			break
		}
		if reviewer.Name != "" && reviewer.Slug != "" {
			people[reviewer.Name] = reviewer.Slug
		}
	}
	return people
}

// sortedReviewers orders reviewers as Bitbucket does, and as the view draws
// them: approvals first, then requests for changes, then the rest, each by
// name.
func sortedReviewers(reviewers []pullrequestservice.Reviewer) []pullrequestservice.Reviewer {
	rank := func(reviewer pullrequestservice.Reviewer) int {
		switch {
		case reviewer.Approved || reviewer.Status == "APPROVED":
			return 1
		case reviewer.Status == "NEEDS_WORK":
			return 2
		default:
			return 3
		}
	}
	name := func(reviewer pullrequestservice.Reviewer) string {
		if reviewer.DisplayName != "" {
			return reviewer.DisplayName
		}
		return reviewer.Name
	}
	sorted := slices.Clone(reviewers)
	sort.SliceStable(sorted, func(i, j int) bool {
		if rank(sorted[i]) != rank(sorted[j]) {
			return rank(sorted[i]) < rank(sorted[j])
		}
		return name(sorted[i]) < name(sorted[j])
	})
	return sorted
}

// splitPatch reads a unified diff into its files, each with its change and
// counts, and keeps the patch of as many whole files as fit: none larger than
// perFile, and together no more than total. A file that does not fit is still
// listed, marked omitted, so the view names every file and draws what it can.
func splitPatch(patch string, perFile, total int) ([]viewDiffFile, string, bool) {
	files := []viewDiffFile{}
	var kept strings.Builder
	truncated := false
	for _, chunk := range patchFiles(patch) {
		file := describePatchFile(chunk)
		if len(chunk) > perFile || kept.Len()+len(chunk) > total {
			file.Omitted = true
			truncated = true
		} else {
			kept.WriteString(chunk)
		}
		files = append(files, file)
	}
	return files, kept.String(), truncated
}

// patchFiles splits a unified diff at each file's header.
func patchFiles(patch string) []string {
	var chunks []string
	start := -1
	for offset := 0; offset < len(patch); {
		next := len(patch)
		if end := strings.IndexByte(patch[offset:], '\n'); end >= 0 {
			next = offset + end + 1
		}
		if strings.HasPrefix(patch[offset:], "diff --git ") {
			if start >= 0 {
				chunks = append(chunks, patch[start:offset])
			}
			start = offset
		}
		offset = next
	}
	if start >= 0 {
		chunks = append(chunks, patch[start:])
	}
	return chunks
}

// patchHeader reads a file's paths from its header, as git writes them and as
// Bitbucket does, with src:// and dst:// for a/ and b/.
var patchHeader = regexp.MustCompile(`^diff --git (?:a/|src://)(.*) (?:b/|dst://)(.*)$`)

// describePatchFile reads one file of a diff: its paths, how it changed, and
// how many lines it adds and removes.
func describePatchFile(chunk string) viewDiffFile {
	file := viewDiffFile{Status: "modified"}
	inHunks := false
	for _, line := range strings.Split(chunk, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			if paths := patchHeader.FindStringSubmatch(line); paths != nil {
				file.OldPath, file.Path = paths[1], paths[2]
			}
		case inHunks && strings.HasPrefix(line, "+"):
			file.Additions++
		case inHunks && strings.HasPrefix(line, "-"):
			file.Deletions++
		case strings.HasPrefix(line, "@@"):
			inHunks = true
		case inHunks:
		case strings.HasPrefix(line, "new file mode"), line == "--- /dev/null":
			file.Status = "added"
		case strings.HasPrefix(line, "deleted file mode"), line == "+++ /dev/null":
			file.Status = "deleted"
		case strings.HasPrefix(line, "rename from "):
			file.OldPath, file.Status = strings.TrimPrefix(line, "rename from "), "renamed"
		case strings.HasPrefix(line, "rename to "):
			file.Path, file.Status = strings.TrimPrefix(line, "rename to "), "renamed"
		case strings.HasPrefix(line, "copy from "):
			file.OldPath, file.Status = strings.TrimPrefix(line, "copy from "), "copied"
		case strings.HasPrefix(line, "copy to "):
			file.Path, file.Status = strings.TrimPrefix(line, "copy to "), "copied"
		case strings.HasPrefix(line, "Binary files "), strings.HasPrefix(line, "GIT binary patch"):
			file.Binary = true
		}
	}
	if file.Status != "renamed" && file.Status != "copied" {
		file.OldPath = ""
	}
	return file
}

// The summaries are the text a model reads beside the view: enough to answer
// with if it called show without looking first, and short, because the person
// has the view.

// viewSummary is what a view shows, as what, and the state it shows, so the
// model can be told of the view once it is shown and again when it changes.
type viewSummary struct {
	subject string
	form    string
	state   string
}

// shown is the text of show's answer.
func (s viewSummary) shown() string {
	return "Showed the person " + s.subject + " as " + s.form + ":" + s.stateText()
}

// changed is what the model is told when a view the person has open changes.
func (s viewSummary) changed() string {
	return "The view of " + s.subject + " you showed the person has changed:" + s.stateText()
}

// opened is what the model is told when the person opens something else in a
// view, such as a pull request's diff from its card.
func (s viewSummary) opened() string {
	return "The person opened " + s.subject + " in a view:" + s.stateText()
}

// stateText is the state after a colon: on the same line, or on lines of its
// own when it is a list.
func (s viewSummary) stateText() string {
	if strings.HasPrefix(s.state, "\n") {
		return s.state
	}
	return " " + s.state
}

func summarizePullRequest(pr viewPullRequest) viewSummary {
	var lines []string
	lines = append(lines, fmt.Sprintf("%q, %s%s, by %s, %s into %s.",
		pr.Title, strings.ToLower(pr.State), draftNote(pr.PullRequest), pr.Author, pr.SourceBranch, pr.TargetBranch))
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
	switch {
	case pr.CheckCounts != nil && pr.CheckCounts.total() > 0:
		lines = append(lines, "Builds: "+buildCountsText(*pr.CheckCounts)+".")
	case len(pr.Checks) > 0 && pr.ChecksLimitReached:
		lines = append(lines, fmt.Sprintf("Builds, of the first %d: %s.", len(pr.Checks), buildCountsText(countChecks(pr.Checks))))
	case len(pr.Checks) > 0:
		lines = append(lines, "Builds: "+buildCountsText(countChecks(pr.Checks))+".")
	}
	if missing, failed := requiredStanding(pr); missing+failed > 0 {
		lines = append(lines, fmt.Sprintf("Required builds: %d missing, %d failed, of %d.", missing, failed, len(pr.RequiredChecks)))
	}
	if pr.AutoMerge != nil {
		lines = append(lines, "Auto-merge is on.")
	}
	return viewSummary{
		subject: fmt.Sprintf("pull request %s/%s#%d", repositoryKey(pr.PullRequest), repositorySlug(pr.PullRequest), pr.ID),
		form:    "an interactive card",
		state:   strings.Join(lines, " "),
	}
}

// requiredStanding counts the required builds that have not reported and
// those that failed, when bb could tell.
func requiredStanding(pr viewPullRequest) (missing, failed int) {
	if !pr.RequiredKnown {
		return 0, 0
	}
	for _, check := range pr.RequiredChecks {
		switch strings.ToUpper(check.State) {
		case "":
			missing++
		case "FAILED":
			failed++
		}
	}
	return missing, failed
}

func summarizePullRequests(prs []viewPullRequest, limitReached bool) viewSummary {
	form := "an interactive list"
	if limitReached {
		form += " (there may be more)"
	}
	var b strings.Builder
	for _, pr := range prs {
		fmt.Fprintf(&b, "\n- %s/%s#%d %q, %s%s, by %s", repositoryKey(pr.PullRequest), repositorySlug(pr.PullRequest), pr.ID, pr.Title,
			strings.ToLower(pr.State), draftNote(pr.PullRequest), pr.Author)
	}
	return viewSummary{subject: fmt.Sprintf("%d pull requests", len(prs)), form: form, state: b.String()}
}

func summarizeDiff(in ShowInput, pr pullrequestservice.PullRequest, patch string) viewSummary {
	files, additions, deletions := diffCounts(patch)
	return viewSummary{
		subject: fmt.Sprintf("the diff of %s/%s#%s %q", in.Project, in.Repo, in.ID, pr.Title),
		form:    "an interactive view",
		state:   fmt.Sprintf("%d files, +%d -%d.", files, additions, deletions),
	}
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

// buildCountsText is build counts in the words Bitbucket's UI uses, in the
// order the view draws them: what needs attention first, the passes last.
func buildCountsText(counts viewCheckCounts) string {
	var parts []string
	for _, state := range []struct {
		count int
		word  string
	}{
		{counts.Failed, "failed"},
		{counts.InProgress, "in progress"},
		{counts.Cancelled, "canceled"},
		{counts.Unknown, "unknown"},
		{counts.Successful, "passed"},
	} {
		if state.count > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", state.count, state.word))
		}
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
	"markdown.js",
	"pull_request.js",
	"pull_requests.js",
	"diff.js",
	"threads.js",
	"refresh.js",
	"open.js",
	"actions.js",
	"form.js",
	"file.js",
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
