package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	commitservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/commit"
	diffservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/diff"
	pullrequestservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequest"
)

// The resources bb serves: Bitbucket content the person can attach in the
// client, and a model can read, named by bitbucket:// URIs.
//
// A resource URI is a name in bb's namespace, not a link. The client asks bb
// for it over the same connection, and bb fetches the content with its own
// credentials, so the client never contacts Bitbucket and never sees a token.
// The paths follow Bitbucket's own, without a host: one server serves one
// instance (ADR-039).
const (
	pullRequestResource        = "bitbucket://projects/{project}/repos/{repo}/pull-requests/{id}"
	pullRequestDiffResource    = "bitbucket://projects/{project}/repos/{repo}/pull-requests/{id}/diff"
	pullRequestThreadsResource = "bitbucket://projects/{project}/repos/{repo}/pull-requests/{id}/threads"
	fileResource               = "bitbucket://projects/{project}/repos/{repo}/files/{+path}{?at}"
	commitResource             = "bitbucket://projects/{project}/repos/{repo}/commits/{id}"
)

// The kinds of resource, which are also the templates' names.
const (
	pullRequestKind        = "pull_request"
	pullRequestDiffKind    = "pull_request_diff"
	pullRequestThreadsKind = "pull_request_threads"
	fileKind               = "file"
	commitKind             = "commit"
)

// Bounds on what a resource answers with, which ADR-094 asks of every answer.
const (
	// diffResourceBytes caps a pull request diff. A diff the person attaches
	// goes into the model's context whole, and a large one would crowd out
	// everything else in it.
	diffResourceBytes = 128 << 10

	// threadResourceLimit caps the open threads of a pull request.
	threadResourceLimit = 100

	// ListedPullRequests caps each half of resources/list: the caller's own
	// open pull requests, and those waiting on their review.
	ListedPullRequests = 25
)

// ResourceSpec is one resource template: how it is listed, and how a URI of
// its kind is read.
type ResourceSpec struct {
	Template *mcp.ResourceTemplate

	// Tool is the tool the resource answers like. The template is served only
	// while that tool is exposed, so --tools and --exclude decide it too: an
	// operator who leaves get_file_content out means files are not to be read.
	Tool string

	read func(ctx context.Context, c Clients, ref resourceRef) ([]*mcp.ResourceContents, error)
}

// resourceRef is what a resource URI names.
type resourceRef struct {
	kind    string
	uri     string
	project string
	repo    string
	id      string
	path    string
	at      string
}

// AllResourceSpecs returns every resource template bb has. A server serves
// those whose tool it exposes (servedResourceSpecs).
func AllResourceSpecs() []ResourceSpec {
	return resourceSpecs()
}

var resourceSpecs = sync.OnceValue(func() []ResourceSpec {
	return []ResourceSpec{
		{
			Template: &mcp.ResourceTemplate{
				Name:  pullRequestKind,
				Title: "Pull request",
				Description: "A pull request: title, state, branches, author, reviewers and their votes, the review summary " +
					"and the description, as get_pull_request answers with them.",
				URITemplate: pullRequestResource,
				MIMEType:    "application/json",
			},
			Tool: "get_pull_request",
			read: readPullRequestResource,
		},
		{
			Template: &mcp.ResourceTemplate{
				Name:  pullRequestDiffKind,
				Title: "Pull request diff",
				Description: fmt.Sprintf("A pull request's changes as a unified diff, up to %d KiB; a longer diff says where "+
					"it stops.", diffResourceBytes>>10),
				URITemplate: pullRequestDiffResource,
				MIMEType:    "text/x-diff",
			},
			Tool: "get_pr_diff",
			read: readPullRequestDiffResource,
		},
		{
			Template: &mcp.ResourceTemplate{
				Name:  pullRequestThreadsKind,
				Title: "Open review threads",
				Description: fmt.Sprintf("A pull request's unresolved review comments and tasks, up to %d threads, as "+
					"list_pr_comments answers with state=open.", threadResourceLimit),
				URITemplate: pullRequestThreadsResource,
				MIMEType:    "application/json",
			},
			Tool: "list_pr_comments",
			read: readPullRequestThreadsResource,
		},
		{
			Template: &mcp.ResourceTemplate{
				Name:  fileKind,
				Title: "File",
				Description: "A file at a branch, tag or commit (the default branch when at is left out), as get_file_content " +
					"answers with it: text as its first window of numbered lines, a document as its extracted text, an archive " +
					"as a listing, an image or small audio and video as themselves beside a description, and any other file " +
					"described.",
				URITemplate: fileResource,
			},
			Tool: "get_file_content",
			read: readFileResource,
		},
		{
			Template: &mcp.ResourceTemplate{
				Name:        commitKind,
				Title:       "Commit",
				Description: "A commit: its message, author, committer, dates and parents, as get_commit answers with them.",
				URITemplate: commitResource,
				MIMEType:    "application/json",
			},
			Tool: "get_commit",
			read: readCommitResource,
		},
	}
})

// servedResourceSpecs are the templates a server serves: those whose tool it
// exposes.
func servedResourceSpecs(exposed map[string]bool) []ResourceSpec {
	var served []ResourceSpec
	for _, spec := range AllResourceSpecs() {
		if exposed[spec.Tool] {
			served = append(served, spec)
		}
	}

	return served
}

// templateVariables names the variables of a URI template, as a client fills
// them in: {+path} and {?at} are path and at.
func templateVariables(template string) []string {
	var names []string
	for _, expression := range templateExpression.FindAllStringSubmatch(template, -1) {
		names = append(names, strings.Split(expression[1], ",")...)
	}

	return names
}

var templateExpression = regexp.MustCompile(`\{[+?]?([^}]+)\}`)

// resourcePrefix begins every resource URI.
const resourcePrefix = "bitbucket://projects/"

// resourceURI builds the URI of one resource.
//
// Every value is escaped as RFC 6570 expands a variable: unreserved characters
// stay, and every other byte of its UTF-8 is percent-encoded, so a URI bb
// builds is one a client filling in the template would build, and the SDK's
// router matches it. A path keeps its slashes between segments. Building
// cannot fail, whatever the bytes.
func resourceURI(ref resourceRef) string {
	base := resourcePrefix + escapeComponent(ref.project) + "/repos/" + escapeComponent(ref.repo)

	switch ref.kind {
	case pullRequestKind:
		return base + "/pull-requests/" + escapeComponent(ref.id)
	case pullRequestDiffKind:
		return base + "/pull-requests/" + escapeComponent(ref.id) + "/diff"
	case pullRequestThreadsKind:
		return base + "/pull-requests/" + escapeComponent(ref.id) + "/threads"
	case commitKind:
		return base + "/commits/" + escapeComponent(ref.id)
	case fileKind:
		var segments []string
		for _, segment := range strings.Split(ref.path, "/") {
			// As the browse service reads a path: empty and "." segments
			// name nothing.
			if trimmed := strings.TrimSpace(segment); trimmed != "" && trimmed != "." {
				segments = append(segments, escapeComponent(trimmed))
			}
		}
		uri := base + "/files/" + strings.Join(segments, "/")
		if at := strings.TrimSpace(ref.at); at != "" {
			uri += "?at=" + escapeComponent(at)
		}
		return uri
	}

	return ""
}

// escapeComponent percent-encodes every byte that is not an RFC 3986
// unreserved character.
func escapeComponent(value string) string {
	const hex = "0123456789ABCDEF"

	var escaped strings.Builder
	for index := 0; index < len(value); index++ {
		switch c := value[index]; {
		case 'a' <= c && c <= 'z', 'A' <= c && c <= 'Z', '0' <= c && c <= '9', c == '-', c == '.', c == '_', c == '~':
			escaped.WriteByte(c)
		default:
			escaped.WriteByte('%')
			escaped.WriteByte(hex[c>>4])
			escaped.WriteByte(hex[c&15])
		}
	}

	return escaped.String()
}

// parseResource reads what a resource URI names. ok is false for a URI that
// names nothing this server serves: another scheme or shape, a query on a
// resource that takes none, a fragment, an empty path segment, or an escape
// that does not decode to UTF-8.
//
// It mirrors the templates, whose project, repo and id cannot hold a slash, so
// the governance middleware and the SDK's router read a URI the same way.
func parseResource(uri string) (resourceRef, bool) {
	rest, found := strings.CutPrefix(uri, resourcePrefix)
	if !found || strings.Contains(rest, "#") {
		return resourceRef{}, false
	}
	path, rawQuery, hasQuery := strings.Cut(rest, "?")

	segments := strings.Split(path, "/")
	if len(segments) < 5 || segments[1] != "repos" {
		return resourceRef{}, false
	}
	project, projectOK := unescapeComponent(segments[0])
	repo, repoOK := unescapeComponent(segments[2])
	if !projectOK || !repoOK {
		return resourceRef{}, false
	}
	ref := resourceRef{uri: uri, project: project, repo: repo}

	switch segments[3] {
	case "pull-requests":
		switch {
		case len(segments) == 5:
			ref.kind = pullRequestKind
		case len(segments) == 6 && segments[5] == "diff":
			ref.kind = pullRequestDiffKind
		case len(segments) == 6 && segments[5] == "threads":
			ref.kind = pullRequestThreadsKind
		default:
			return resourceRef{}, false
		}
		id, ok := unescapeComponent(segments[4])
		if !ok {
			return resourceRef{}, false
		}
		ref.id = id
	case "commits":
		id, ok := unescapeComponent(segments[4])
		if len(segments) != 5 || !ok {
			return resourceRef{}, false
		}
		ref.kind, ref.id = commitKind, id
	case "files":
		decoded := make([]string, 0, len(segments)-4)
		for _, segment := range segments[4:] {
			value, ok := unescapeComponent(segment)
			if !ok || value == "" || strings.Contains(value, "/") {
				return resourceRef{}, false
			}
			decoded = append(decoded, value)
		}
		ref.kind, ref.path = fileKind, strings.Join(decoded, "/")
	default:
		return resourceRef{}, false
	}

	if hasQuery {
		if ref.kind != fileKind {
			return resourceRef{}, false
		}
		query, err := url.ParseQuery(rawQuery)
		if err != nil || len(query) != 1 || len(query["at"]) != 1 || !utf8.ValidString(query.Get("at")) {
			return resourceRef{}, false
		}
		ref.at = query.Get("at")
	}

	return ref, true
}

// unescapeComponent decodes one escaped component, refusing one that does not
// decode to UTF-8.
func unescapeComponent(value string) (string, bool) {
	decoded, err := url.PathUnescape(value)
	if err != nil || !utf8.ValidString(decoded) {
		return "", false
	}

	return decoded, true
}

// matchResource finds the template a URI is of and reads what it names. ok is
// false for a URI that names nothing bb has a template for.
func matchResource(uri string) (ResourceSpec, resourceRef, bool) {
	ref, ok := parseResource(uri)
	if !ok {
		return ResourceSpec{}, resourceRef{}, false
	}
	for _, spec := range resourceSpecs() {
		if spec.Template.Name == ref.kind {
			return spec, ref, true
		}
	}

	return ResourceSpec{}, resourceRef{}, false
}

// pullRequestID and commitID are what the id of each kind of resource may be.
var (
	pullRequestID = regexp.MustCompile(`^[0-9]{1,18}$`)
	commitID      = regexp.MustCompile(`^[0-9a-fA-F]{4,64}$`)
)

// validate refuses a URI whose parts could not name what its template says,
// before anything is asked of Bitbucket. A decoded variable can hold what its
// expansion could not: %2F reads as a slash.
func (spec ResourceSpec) validate(ref resourceRef) error {
	if err := openapi.ValidateRepository(ref.project, ref.repo); err != nil {
		return err
	}

	switch ref.kind {
	case pullRequestKind, pullRequestDiffKind, pullRequestThreadsKind:
		if !pullRequestID.MatchString(ref.id) {
			return fmt.Errorf("%q is not a pull request ID", ref.id)
		}
	case commitKind:
		if !commitID.MatchString(ref.id) {
			return fmt.Errorf("%q is not a commit ID", ref.id)
		}
	case fileKind:
		if strings.TrimSpace(ref.path) == "" {
			return fmt.Errorf("the file resource needs a path")
		}
		if strings.IndexFunc(ref.path+ref.at, unicode.IsControl) >= 0 {
			return fmt.Errorf("a path or ref cannot hold control characters")
		}
	}

	return nil
}

// resourceHandler reads one template's resources. The SDK routes a URI to it
// by the template; it reads the URI again, and refuses one that is not of its
// kind.
func resourceHandler(spec ResourceSpec, clients Clients) mcp.ResourceHandler {
	return func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		uri := req.Params.URI
		ref, ok := parseResource(uri)
		if !ok || ref.kind != spec.Template.Name {
			return nil, mcp.ResourceNotFoundError(uri)
		}
		if err := spec.validate(ref); err != nil {
			return nil, invalidResource(uri, err)
		}

		contents, err := spec.read(ctx, clients, ref)
		if err != nil {
			return nil, resourceError(uri, err)
		}

		return &mcp.ReadResourceResult{Contents: contents}, nil
	}
}

// invalidResource is the error for a URI that cannot name anything: -32602,
// as for a resource that does not exist, with the reason.
func invalidResource(uri string, err error) error {
	return &jsonrpc.Error{
		Code:    jsonrpc.CodeInvalidParams,
		Message: fmt.Sprintf("%s cannot be read: %v", uri, err),
		Data:    json.RawMessage(fmt.Sprintf(`{"uri":%q}`, uri)),
	}
}

// resourceError maps what reading a resource ran into onto the codes the
// 2026-07-28 revision gives: -32602 for a resource that does not exist, or
// that the credentials may not read, and -32603 for a failure of bb's or of
// Bitbucket's.
func resourceError(uri string, err error) error {
	switch {
	case apperrors.IsKind(err, apperrors.KindNotFound):
		return mcp.ResourceNotFoundError(uri)
	case apperrors.IsKind(err, apperrors.KindValidation),
		apperrors.IsKind(err, apperrors.KindAuthorization):
		return invalidResource(uri, err)
	default:
		return &jsonrpc.Error{
			Code:    jsonrpc.CodeInternalError,
			Message: fmt.Sprintf("reading %s failed: %v", uri, err),
		}
	}
}

func readPullRequestResource(ctx context.Context, c Clients, ref resourceRef) ([]*mcp.ResourceContents, error) {
	out, err := pullRequestWithReviewSummary(ctx, c, GetPullRequestInput{Project: ref.project, Repo: ref.repo, ID: ref.id})
	if err != nil {
		return nil, err
	}

	return jsonContents(ref.uri, out)
}

func readPullRequestDiffResource(ctx context.Context, c Clients, ref resourceRef) ([]*mcp.ResourceContents, error) {
	result, err := diffservice.NewService(c.OpenAPI).DiffPR(ctx, diffservice.DiffPRInput{
		Repository:    diffservice.RepositoryRef{ProjectKey: ref.project, Slug: ref.repo},
		PullRequestID: ref.id,
		Output:        diffservice.OutputKindRaw,
	})
	if err != nil {
		return nil, err
	}

	return []*mcp.ResourceContents{{URI: ref.uri, MIMEType: "text/x-diff", Text: capDiff(result.Patch, diffResourceBytes)}}, nil
}

// capDiff cuts a diff to at most limit bytes, at the end of a line, and says
// where it stopped. A line boundary is also a character boundary.
func capDiff(diff string, limit int) string {
	if len(diff) <= limit {
		return diff
	}

	cut := strings.LastIndexByte(diff[:limit], '\n') + 1

	return diff[:cut] + fmt.Sprintf("\n[The diff continues: this is the first %d KiB of %d KiB. get_pr_diff returns all of it.]\n",
		cut>>10, len(diff)>>10)
}

func readPullRequestThreadsResource(ctx context.Context, c Clients, ref resourceRef) ([]*mcp.ResourceContents, error) {
	out, err := pullRequestThreads(ctx, c, ListPRCommentsInput{
		Project: ref.project,
		Repo:    ref.repo,
		PRID:    ref.id,
		State:   "open",
		Limit:   threadResourceLimit,
	})
	if err != nil {
		return nil, err
	}

	return jsonContents(ref.uri, out)
}

func readFileResource(ctx context.Context, c Clients, ref resourceRef) ([]*mcp.ResourceContents, error) {
	_, view, err := readFileView(ctx, c, GetFileContentInput{Project: ref.project, Repo: ref.repo, Path: ref.path, At: ref.at})
	if err != nil {
		return nil, err
	}

	// The text first, as in get_file_content: it says what the file is, and
	// whether an image was scaled, before the bytes.
	contents := []*mcp.ResourceContents{{URI: ref.uri, MIMEType: "text/plain", Text: view.Text}}
	if image := view.Image; image != nil {
		contents = append(contents, &mcp.ResourceContents{URI: ref.uri, MIMEType: image.MIMEType, Blob: image.Data})
	}
	if media := view.Media; media != nil {
		contents = append(contents, &mcp.ResourceContents{URI: ref.uri, MIMEType: media.MIMEType, Blob: media.Data})
	}

	return contents, nil
}

func readCommitResource(ctx context.Context, c Clients, ref resourceRef) ([]*mcp.ResourceContents, error) {
	commit, err := commitservice.NewService(c.OpenAPI).Get(ctx, commitservice.RepositoryRef{ProjectKey: ref.project, Slug: ref.repo}, ref.id)
	if err != nil {
		return nil, err
	}

	return jsonContents(ref.uri, GetCommitOutput{Commit: commit})
}

// jsonContents is a resource answering with the JSON its tool answers with, so
// a resource and its tool never disagree.
func jsonContents(uri string, value any) ([]*mcp.ResourceContents, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encoding %s: %w", uri, err)
	}

	return []*mcp.ResourceContents{{URI: uri, MIMEType: "application/json", Text: string(encoded)}}, nil
}

// resourceName is how one resource is named in a link: an identifier, and a
// title a client can show.
func resourceName(ref resourceRef) (name, title string) {
	repository := ref.project + "/" + ref.repo
	switch ref.kind {
	case pullRequestKind:
		name = repository + "#" + ref.id
		return name, "Pull request " + name
	case pullRequestDiffKind:
		name = repository + "#" + ref.id
		return name + " diff", "Diff of pull request " + name
	case pullRequestThreadsKind:
		name = repository + "#" + ref.id
		return name + " threads", "Open review threads of pull request " + name
	case commitKind:
		return repository + "@" + shortCommit(ref.id), "Commit " + shortCommit(ref.id) + " in " + repository
	case fileKind:
		if ref.at != "" {
			return repository + ":" + ref.path + "@" + ref.at, ref.path + " at " + ref.at + " in " + repository
		}
		return repository + ":" + ref.path, ref.path + " in " + repository
	}

	return ref.uri, ref.uri
}

// listedResources is the bounded list resources/list answers with: the
// caller's open pull requests, and those waiting on their review, narrowed to
// the scope.
//
// It varies with the person the credentials belong to and with time, which the
// 2026-07-28 revision allows, and never with the connection asking.
func listedResources(ctx context.Context, c Clients, scope Scope) ([]*mcp.Resource, error) {
	svc := pullrequestservice.NewService(c.HTTP)

	// The dashboard narrows inside its walk, so the cap counts pull requests
	// in the scope rather than dashboard rows. It holds nothing but the
	// caller's own work, so narrowing its answer bounds it (see
	// scopeOptionalProjectRepo).
	mine, err := svc.ListDashboard(ctx, pullrequestservice.DashboardListOptions{
		State: "OPEN", Role: "AUTHOR", ProjectKey: scope.ProjectKey, RepoSlug: scope.RepoSlug, MaxResults: ListedPullRequests,
	})
	if err != nil {
		return nil, fmt.Errorf("listing your pull requests: %w", err)
	}
	waiting, err := svc.ListDashboard(ctx, pullrequestservice.DashboardListOptions{
		State: "OPEN", Role: "REVIEWER", ParticipantStatus: "UNAPPROVED",
		ProjectKey: scope.ProjectKey, RepoSlug: scope.RepoSlug, MaxResults: ListedPullRequests,
	})
	if err != nil {
		return nil, fmt.Errorf("listing the pull requests waiting on your review: %w", err)
	}

	resources := make([]*mcp.Resource, 0, len(mine)+len(waiting))
	add := func(pr pullrequestservice.PullRequest, description string) {
		if pr.Repository == nil {
			return
		}
		ref := resourceRef{kind: pullRequestKind, project: pr.Repository.ProjectKey, repo: pr.Repository.Slug, id: fmt.Sprintf("%d", pr.ID)}
		name, _ := resourceName(ref)
		resources = append(resources, &mcp.Resource{
			URI:         resourceURI(ref),
			Name:        name,
			Title:       pr.Title,
			Description: description,
			MIMEType:    "application/json",
		})
	}
	for _, pr := range mine {
		draft := ""
		if pr.Draft {
			draft = " (draft)"
		}
		add(pr, fmt.Sprintf("Your pull request%s: %s into %s", draft, pr.SourceBranch, pr.TargetBranch))
	}
	for _, pr := range waiting {
		add(pr, fmt.Sprintf("Waiting on your review, by %s: %s into %s", pr.Author, pr.SourceBranch, pr.TargetBranch))
	}

	return resources, nil
}

// resourceLink says how a tool's arguments name the resource its result came
// from: the kind, and for each part the argument it is read from.
type resourceLink struct {
	kind                        string
	project, repo, id, path, at string
}

// toolResourceLinks are the tools whose result is one resource. The link comes
// beside the result's content, never in place of it (ADR-094), so a client can
// offer the resource to attach or read it again. list_pr_comments is not one:
// its result depends on filters the open threads resource does not take.
var toolResourceLinks = map[string]resourceLink{
	"get_pull_request": {kind: pullRequestKind, project: "project", repo: "repo", id: "id"},
	"get_pr_diff":      {kind: pullRequestDiffKind, project: "project", repo: "repo", id: "pr_id"},
	"get_file_content": {kind: fileKind, project: "project", repo: "repo", path: "path", at: "at"},
	"get_commit":       {kind: commitKind, project: "project", repo: "repo", id: "commit_id"},
}

// ResourceLinkedTools names the tools whose results link the resource they
// came from, in order.
func ResourceLinkedTools() []string {
	tools := make([]string, 0, len(toolResourceLinks))
	for tool := range toolResourceLinks {
		tools = append(tools, tool)
	}
	sort.Strings(tools)

	return tools
}

// resourceLinkRevision is the first protocol revision with resource_link
// content. A client on an earlier one gets the result without the link, which
// it may not know how to read.
const resourceLinkRevision = "2025-06-18"

func supportsResourceLinks(protocolVersion string) bool {
	return protocolVersion >= resourceLinkRevision
}

// linkResource appends the resource_link a tool result names, when the tool
// has a matching resource and the call succeeded.
func linkResource(tool string, arguments json.RawMessage, result *mcp.CallToolResult) {
	link, ok := toolResourceLinks[tool]
	if !ok || result == nil || result.IsError || result.InputRequests != nil {
		return
	}

	decoded := map[string]any{}
	if err := json.Unmarshal(arguments, &decoded); err != nil {
		return
	}
	argument := func(name string) string {
		if name == "" {
			return ""
		}
		value, _ := decoded[name].(string)
		return strings.TrimSpace(value)
	}

	uri := resourceURI(resourceRef{
		kind:    link.kind,
		project: argument(link.project),
		repo:    argument(link.repo),
		id:      argument(link.id),
		path:    argument(link.path),
		at:      argument(link.at),
	})
	spec, ref, ok := matchResource(uri)
	if !ok || spec.validate(ref) != nil {
		// A call that succeeded with arguments no resource can name, such
		// as a ref given for a commit. Better no link than one that does not
		// read.
		return
	}

	name, title := resourceName(ref)
	result.Content = append(result.Content, &mcp.ResourceLink{
		URI:      uri,
		Name:     name,
		Title:    title,
		MIMEType: spec.Template.MIMEType,
	})
}

// cacheHints are the caching hints on the results that carry them.
//
// A resource and the list answer with what the person's credentials may read,
// so they are private: a cache must not hand them to anyone else. A read is
// stale at once, since a pull request or a branch can change the next moment;
// the list may be kept for a minute, since it is a picker's starting point
// rather than a record.
func cacheHints(_ context.Context, req mcp.Request, c *mcp.Cacheable) {
	switch req.(type) {
	case *mcp.ReadResourceRequest:
		c.CacheScope = "private"
		c.TTLMs = 0
	case *mcp.ListResourcesRequest:
		c.CacheScope = "private"
		c.TTLMs = 60_000
	}
}
