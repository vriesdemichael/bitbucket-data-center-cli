package mcp

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Every template names the project and repository it reads from, which is
// what the scope is checked against. A template without them would read past
// --project and --repo with nothing to stop it, the way a tool without a scope
// rule would (TestEveryToolHasAScopeRule).
func TestEveryResourceTemplateNamesItsProjectAndRepository(t *testing.T) {
	t.Parallel()

	for _, spec := range AllResourceSpecs() {
		variables := templateVariables(spec.Template.URITemplate)
		if !slices.Contains(variables, "project") || !slices.Contains(variables, "repo") {
			t.Errorf("resource template %s (%s) does not name project and repo, so no scope can bound it",
				spec.Template.Name, spec.Template.URITemplate)
		}
		if !strings.HasPrefix(spec.Template.URITemplate, "bitbucket://projects/{project}/repos/{repo}/") {
			t.Errorf("resource template %s does not start with the repository it reads: %s", spec.Template.Name, spec.Template.URITemplate)
		}
	}
}

// Every prompt takes the project and repository it reads from, for the same
// reason.
func TestEveryPromptTakesItsProjectAndRepository(t *testing.T) {
	t.Parallel()

	for _, spec := range AllPromptSpecs() {
		var names []string
		for _, argument := range spec.Prompt.Arguments {
			names = append(names, argument.Name)
		}
		if !slices.Contains(names, "project") || !slices.Contains(names, "repo") {
			t.Errorf("prompt %s does not take project and repo, so no scope can bound it", spec.Prompt.Name)
		}
	}
}

// A URI built for a resource reads back as the values it was built from, and
// is escaped as RFC 6570 expands a variable: the UTF-8 bytes of anything but
// an unreserved character are percent-encoded, a path keeps its slashes, and a
// question mark or a hash in a path stays part of the path.
func TestAResourceURIReadsBackAsWhatItNames(t *testing.T) {
	t.Parallel()

	cases := []struct {
		ref  resourceRef
		want string
	}{
		{resourceRef{kind: pullRequestKind, project: "PROJ", repo: "app", id: "42"}, "bitbucket://projects/PROJ/repos/app/pull-requests/42"},
		{resourceRef{kind: pullRequestDiffKind, project: "PROJ", repo: "app", id: "42"}, "bitbucket://projects/PROJ/repos/app/pull-requests/42/diff"},
		{resourceRef{kind: pullRequestThreadsKind, project: "PROJ", repo: "app", id: "42"}, "bitbucket://projects/PROJ/repos/app/pull-requests/42/threads"},
		{resourceRef{kind: fileKind, project: "~alice", repo: "my.repo", path: "docs/a b.md", at: "refs/heads/main"},
			"bitbucket://projects/~alice/repos/my.repo/files/docs/a%20b.md?at=refs%2Fheads%2Fmain"},
		{resourceRef{kind: fileKind, project: "PROJ", repo: "app", path: "README.md"}, "bitbucket://projects/PROJ/repos/app/files/README.md"},
		{resourceRef{kind: fileKind, project: "PROJ", repo: "app", path: "docs/café.md", at: "féature"},
			"bitbucket://projects/PROJ/repos/app/files/docs/caf%C3%A9.md?at=f%C3%A9ature"},
		{resourceRef{kind: fileKind, project: "PROJ", repo: "app", path: "一/😀.txt"},
			"bitbucket://projects/PROJ/repos/app/files/%E4%B8%80/%F0%9F%98%80.txt"},
		{resourceRef{kind: fileKind, project: "PROJ", repo: "app", path: "x?at=y/C#.md", at: "a b&c=d"},
			"bitbucket://projects/PROJ/repos/app/files/x%3Fat%3Dy/C%23.md?at=a%20b%26c%3Dd"},
		{resourceRef{kind: fileKind, project: "PROJ", repo: "app", path: "�.txt"},
			"bitbucket://projects/PROJ/repos/app/files/%EF%BF%BD.txt"},
		{resourceRef{kind: commitKind, project: "PROJ", repo: "app", id: "0a1b2c3d"}, "bitbucket://projects/PROJ/repos/app/commits/0a1b2c3d"},
	}

	for _, tc := range cases {
		uri := resourceURI(tc.ref)
		if uri != tc.want {
			t.Errorf("%+v builds %s, want %s", tc.ref, uri, tc.want)
			continue
		}
		spec, ref, ok := matchResource(uri)
		if !ok || spec.Template.Name != tc.ref.kind {
			t.Errorf("%s matched %v as %q, want %s", uri, ok, spec.Template.Name, tc.ref.kind)
			continue
		}
		if ref.project != tc.ref.project || ref.repo != tc.ref.repo || ref.id != tc.ref.id || ref.path != tc.ref.path || ref.at != tc.ref.at {
			t.Errorf("%s reads back as %+v, want %+v", uri, ref, tc.ref)
		}
		if err := spec.validate(ref); err != nil {
			t.Errorf("%s does not validate: %v", uri, err)
		}
	}
}

// A decoded variable can hold what its expansion could not, so a URI is
// validated after it matched, before anything reaches Bitbucket.
func TestAResourceURIThatCannotNameAnythingIsRefused(t *testing.T) {
	t.Parallel()

	for _, uri := range []string{
		"bitbucket://projects/PROJ/repos/app/pull-requests/42%2Fdiff",
		"bitbucket://projects/PROJ/repos/app/pull-requests/%2342",
		"bitbucket://projects/PROJ/repos/app/commits/main",
		"bitbucket://projects/PROJ%2Frepos%2Fother/repos/app/pull-requests/1",
		"bitbucket://projects/PROJ/repos/app/files/a.txt?at=main%00",
	} {
		spec, ref, ok := matchResource(uri)
		if !ok {
			t.Errorf("%s matched no template; want it matched, then refused", uri)
			continue
		}
		if err := spec.validate(ref); err == nil {
			t.Errorf("%s validated as %+v", uri, ref)
		}
	}

	for _, uri := range []string{
		"bitbucket://projects/PROJ/repos/app/files/a.txt?ref=main",
		"bitbucket://projects/PROJ/repos/app/files/a.txt?at=main&at=dev",
		"bitbucket://projects/PROJ/repos/app/pull-requests/1?at=main",
		"bitbucket://projects/PROJ/repos/app/files/a.txt#top",
		"bitbucket://projects/PROJ/repos/app/files/a//b.txt",
		"bitbucket://projects/PROJ/repos/app/files/a%2Fb.txt",
		"bitbucket://projects/PROJ/repos/app/files/%FF.txt",
		"bitbucket://projects/PR%FFOJ/repos/app/pull-requests/1",
		"https://bb.example.com/projects/PROJ/repos/app/browse/a.txt",
		"bitbucket://projects/PROJ/repos/app/branches/main",
	} {
		if _, ref, ok := matchResource(uri); ok {
			t.Errorf("%s matched as %+v; want no template to match", uri, ref)
		}
	}
}

// The template and prompt lists are the same for every connection, and every
// entry says what it is.
func TestTheServerListsEveryTemplateAndPrompt(t *testing.T) {
	t.Parallel()

	session := connectWith(t, ServerOptions{Name: "bb", Version: "test", Clients: testClients(t), ReadOnly: true})
	ctx := context.Background()

	templates, err := session.ListResourceTemplates(ctx, nil)
	if err != nil {
		t.Fatalf("resources/templates/list: %v", err)
	}
	if len(templates.ResourceTemplates) != len(AllResourceSpecs()) {
		t.Errorf("listed %d templates, want all %d, even on a read-only server", len(templates.ResourceTemplates), len(AllResourceSpecs()))
	}
	for _, template := range templates.ResourceTemplates {
		if template.Name == "" || template.Title == "" || template.Description == "" {
			t.Errorf("template %s lacks a name, title or description: %+v", template.URITemplate, template)
		}
	}

	prompts, err := session.ListPrompts(ctx, nil)
	if err != nil {
		t.Fatalf("prompts/list: %v", err)
	}
	if len(prompts.Prompts) != len(AllPromptSpecs()) {
		t.Errorf("listed %d prompts, want all %d", len(prompts.Prompts), len(AllPromptSpecs()))
	}
	for _, prompt := range prompts.Prompts {
		if prompt.Title == "" || prompt.Description == "" {
			t.Errorf("prompt %s lacks a title or description", prompt.Name)
		}
	}
}

// A URI that names nothing is -32602, as the 2026-07-28 revision requires for
// a resource that does not exist, and a malformed one says why. Neither
// reaches Bitbucket, which the refused port would turn into -32603.
func TestReadingWhatDoesNotExistIsInvalidParams(t *testing.T) {
	t.Parallel()

	session := connectWith(t, ServerOptions{Name: "bb", Version: "test", Clients: testClients(t)})
	for uri, why := range map[string]string{
		"bitbucket://projects/PROJ/repos/app/branches/main":             "Resource not found",
		"bitbucket://projects/PROJ/repos/app/pull-requests/42%2Fdiff":   "is not a pull request ID",
		"bitbucket://projects/PROJ/repos/app/files/../../etc?at=main":   `path must not contain ".." segments`,
		"bitbucket://projects/PROJ%3Fx/repos/app/pull-requests/42/diff": "is not a project key or repository slug",
	} {
		_, err := session.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: uri})
		wire := protocolError(t, err)
		if wire.Code != jsonrpc.CodeInvalidParams || !strings.Contains(wire.Message, why) {
			t.Errorf("%s: got %d %q, want -32602 saying %q", uri, wire.Code, wire.Message, why)
		}
	}
}

// Under a scope, a resource outside it is refused before any request, and the
// refusal is audited as denied. One inside it reaches Bitbucket.
func TestAScopeBoundsResourceReads(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "audit.jsonl")
	audit, err := NewAuditLogger(path)
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}
	session := connectWith(t, ServerOptions{
		Name: "bb", Version: "test", Clients: testClients(t),
		Scope: Scope{ProjectKey: "PROJ", RepoSlug: "app"}, Audit: audit,
	})
	ctx := context.Background()

	_, err = session.ReadResource(ctx, &mcp.ReadResourceParams{URI: "bitbucket://projects/PROJ/repos/other/pull-requests/1"})
	if wire := protocolError(t, err); wire.Code != jsonrpc.CodeInvalidParams || !strings.Contains(wire.Message, "outside the scope") {
		t.Errorf("an out-of-scope read got %d %q, want -32602 saying it is outside the scope", wire.Code, wire.Message)
	}

	_, err = session.ReadResource(ctx, &mcp.ReadResourceParams{URI: "bitbucket://projects/proj/repos/APP/pull-requests/1"})
	if wire := protocolError(t, err); wire.Code != jsonrpc.CodeInternalError {
		t.Errorf("an in-scope read got %d %q, want it to reach Bitbucket and fail at the refused port (-32603)", wire.Code, wire.Message)
	}
	_ = audit.Close()

	records := readAuditRecords(t, path)
	if len(records) != 2 {
		t.Fatalf("want a record for each read, got %d: %+v", len(records), records)
	}
	denied, failed := records[0], records[1]
	if denied.Event != auditEventResourceRead || denied.Status != auditStatusDenied || denied.Resource != "bitbucket://projects/PROJ/repos/other/pull-requests/1" || denied.Repo != "other" {
		t.Errorf("the refused read is recorded as %+v", denied)
	}
	if failed.Event != auditEventResourceRead || failed.Status != auditStatusError || failed.Tool != "" {
		t.Errorf("the failed read is recorded as %+v", failed)
	}
}

// A prompt's project and repository are bound to the scope as a tool call's
// are: filled in when left out, refused when they name something else. The
// resource list is recorded too.
func TestAScopeBoundsPromptsAndTheListIsAudited(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "audit.jsonl")
	audit, err := NewAuditLogger(path)
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}
	session := connectWith(t, ServerOptions{
		Name: "bb", Version: "test", Clients: testClients(t),
		Scope: Scope{ProjectKey: "PROJ", RepoSlug: "app"}, Audit: audit,
	})
	ctx := context.Background()

	_, err = session.GetPrompt(ctx, &mcp.GetPromptParams{Name: "review_pull_request", Arguments: map[string]string{"project": "OTHER", "repo": "app", "id": "1"}})
	if wire := protocolError(t, err); wire.Code != jsonrpc.CodeInvalidParams || !strings.Contains(wire.Message, "outside the scope") {
		t.Errorf("an out-of-scope prompt got %d %q", wire.Code, wire.Message)
	}

	// Left out, the project and repository come from the scope, and the
	// prompt then fails reading at the refused port.
	if _, err = session.GetPrompt(ctx, &mcp.GetPromptParams{Name: "explain_pull_request", Arguments: map[string]string{"id": "1"}}); err == nil {
		t.Error("a prompt read from a refused port succeeded")
	}

	if _, err = session.ListResources(ctx, nil); err == nil {
		t.Error("the resource list succeeded against a refused port")
	}
	_ = audit.Close()

	records := readAuditRecords(t, path)
	if len(records) != 3 {
		t.Fatalf("want three records, got %d: %+v", len(records), records)
	}
	if records[0].Event != auditEventPromptGet || records[0].Status != auditStatusDenied || records[0].Prompt != "review_pull_request" {
		t.Errorf("the refused prompt is recorded as %+v", records[0])
	}
	if records[1].Event != auditEventPromptGet || records[1].Project != "PROJ" || records[1].Repo != "app" {
		t.Errorf("the prompt left unscoped is recorded as %+v, want the scope filled in", records[1])
	}
	if records[2].Event != auditEventResourceList || records[2].Status != auditStatusError {
		t.Errorf("the resource list is recorded as %+v", records[2])
	}
}

// A completion under a scope suggests the scope's own project and repository,
// and nothing else, whatever Bitbucket would have listed.
func TestAScopeBoundsCompletions(t *testing.T) {
	t.Parallel()

	session := connectWith(t, ServerOptions{
		Name: "bb", Version: "test", Clients: testClients(t),
		Scope: Scope{ProjectKey: "PROJ", RepoSlug: "app"},
	})
	ctx := context.Background()
	template := &mcp.CompleteReference{Type: "ref/resource", URI: pullRequestResource}

	for _, tc := range []struct {
		argument, typed string
		want            []string
	}{
		{"project", "", []string{"PROJ"}},
		{"project", "pr", []string{"PROJ"}},
		{"project", "OTHER", []string{}},
		{"repo", "a", []string{"app"}},
		{"repo", "x", []string{}},
	} {
		result, err := session.Complete(ctx, &mcp.CompleteParams{
			Ref:      template,
			Argument: mcp.CompleteParamsArgument{Name: tc.argument, Value: tc.typed},
			Context:  &mcp.CompleteContext{Arguments: map[string]string{"project": "OTHER"}},
		})
		if err != nil {
			t.Fatalf("complete %s %q: %v", tc.argument, tc.typed, err)
		}
		if !slices.Equal(result.Completion.Values, tc.want) {
			t.Errorf("complete %s %q = %v, want %v", tc.argument, tc.typed, result.Completion.Values, tc.want)
		}
	}
}

// A completion that names nothing this server has is -32602; one whose
// listing fails is an empty answer, as in the shell.
func TestCompletionRefusesWhatItDoesNotKnowAndStaysQuietOnFailure(t *testing.T) {
	t.Parallel()

	session := connectWith(t, ServerOptions{Name: "bb", Version: "test", Clients: testClients(t)})
	ctx := context.Background()

	for _, params := range []*mcp.CompleteParams{
		{Ref: &mcp.CompleteReference{Type: "ref/resource", URI: "bitbucket://projects/{project}/repos/{repo}/branches/{branch}"}, Argument: mcp.CompleteParamsArgument{Name: "project"}},
		{Ref: &mcp.CompleteReference{Type: "ref/prompt", Name: "deploy"}, Argument: mcp.CompleteParamsArgument{Name: "project"}},
		{Ref: &mcp.CompleteReference{Type: "ref/resource", URI: pullRequestResource}, Argument: mcp.CompleteParamsArgument{Name: "path"}},
	} {
		_, err := session.Complete(ctx, params)
		if wire := protocolError(t, err); wire.Code != jsonrpc.CodeInvalidParams {
			t.Errorf("completing %+v got %d %q, want -32602", params.Ref, wire.Code, wire.Message)
		}
	}

	result, err := session.Complete(ctx, &mcp.CompleteParams{
		Ref:      &mcp.CompleteReference{Type: "ref/prompt", Name: "review_pull_request"},
		Argument: mcp.CompleteParamsArgument{Name: "project", Value: "P"},
	})
	if err != nil || len(result.Completion.Values) != 0 {
		t.Errorf("a completion against a refused port got %v, %v; want an empty answer", result, err)
	}
}

// A tool result gets a link to the resource it came from, beside its content;
// a failed call, or one whose arguments name no resource, gets none.
func TestAToolResultLinksTheResourceItCameFrom(t *testing.T) {
	t.Parallel()

	arguments := func(values map[string]any) json.RawMessage {
		encoded, err := json.Marshal(values)
		if err != nil {
			t.Fatal(err)
		}
		return encoded
	}
	text := &mcp.TextContent{Text: "{}"}

	for tool, tc := range map[string]struct {
		arguments map[string]any
		want      string
	}{
		"get_pull_request": {map[string]any{"project": "PROJ", "repo": "app", "id": "42"}, "bitbucket://projects/PROJ/repos/app/pull-requests/42"},
		"get_pr_diff":      {map[string]any{"project": "PROJ", "repo": "app", "pr_id": "42"}, "bitbucket://projects/PROJ/repos/app/pull-requests/42/diff"},
		"get_file_content": {map[string]any{"project": "PROJ", "repo": "app", "path": "src/caf\u00e9 \ufffd.go", "at": "main"}, "bitbucket://projects/PROJ/repos/app/files/src/caf%C3%A9%20%EF%BF%BD.go?at=main"},
		"get_commit":       {map[string]any{"project": "PROJ", "repo": "app", "commit_id": "0a1b2c3d4e5f"}, "bitbucket://projects/PROJ/repos/app/commits/0a1b2c3d4e5f"},
	} {
		result := &mcp.CallToolResult{Content: []mcp.Content{text}}
		linkResource(tool, arguments(tc.arguments), result)
		if len(result.Content) != 2 || result.Content[0] != text {
			t.Errorf("%s: want the content kept first and one link beside it, got %d items", tool, len(result.Content))
			continue
		}
		if link, ok := result.Content[1].(*mcp.ResourceLink); !ok || link.URI != tc.want || !strings.HasPrefix(link.Name, "PROJ/app") || link.Title == "" {
			t.Errorf("%s: linked %+v, want %s", tool, result.Content[1], tc.want)
		}
	}

	failed := &mcp.CallToolResult{IsError: true, Content: []mcp.Content{text}}
	linkResource("get_pull_request", arguments(map[string]any{"project": "PROJ", "repo": "app", "id": "42"}), failed)
	unnamed := &mcp.CallToolResult{Content: []mcp.Content{text}}
	linkResource("get_pull_request", arguments(map[string]any{"project": "PROJ", "repo": "app", "id": "#42"}), unnamed)
	other := &mcp.CallToolResult{Content: []mcp.Content{text}}
	linkResource("list_pr_comments", arguments(map[string]any{"project": "PROJ", "repo": "app", "pr_id": "42", "state": "all"}), other)
	for name, result := range map[string]*mcp.CallToolResult{"a failed call": failed, "an unnamed pull request": unnamed, "a tool with no resource": other} {
		if len(result.Content) != 1 {
			t.Errorf("%s got a link: %+v", name, result.Content)
		}
	}
}

// A long diff is cut at the end of a line and says where it stopped.
func TestADiffResourceIsBounded(t *testing.T) {
	t.Parallel()

	short := "diff --git a/x b/x\n+one\n"
	if got := capDiff(short, 1<<10); got != short {
		t.Errorf("a short diff changed: %q", got)
	}

	long := strings.Repeat("+a line of the diff\n", 200)
	got := capDiff(long, 1000)
	body, note, found := strings.Cut(got, "\n[")
	if !found || !strings.HasPrefix(note, "The diff continues") || !strings.Contains(note, "get_pr_diff") {
		t.Fatalf("a cut diff does not say where it stopped: %q", got[len(got)-120:])
	}
	if len(body) > 1000 || !strings.HasSuffix(body, "diff\n") || !strings.HasPrefix(long, body) {
		t.Errorf("the diff was not cut at the end of a line within the limit: %q", body[len(body)-40:])
	}
}

// A completion answer holds at most completionValues values, and says how
// many there were.
func TestACompletionAnswerIsBounded(t *testing.T) {
	t.Parallel()

	values := make([]string, completionValues+20)
	for index := range values {
		values[index] = "value"
	}
	result := completionResult(values)
	if len(result.Completion.Values) != completionValues || !result.Completion.HasMore || result.Completion.Total != completionValues+20 {
		t.Errorf("got %d values, hasMore %v, total %d", len(result.Completion.Values), result.Completion.HasMore, result.Completion.Total)
	}
	if empty := completionResult(nil); empty.Completion.Values == nil {
		t.Error("an empty answer marshals values as null")
	}
	if got := prefixFirst([]string{"PS", "PLAT", "", "PLAT", "API"}, "pl"); !slices.Equal(got, []string{"PLAT", "PS", "API"}) {
		t.Errorf("prefixFirst = %v", got)
	}
}

// Reads and the list carry the person's credentials, so they are private to
// them; a read is stale at once and the list keeps for a minute.
func TestResourceResultsArePrivate(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		request mcp.Request
		ttl     int
	}{
		"a read":  {&mcp.ReadResourceRequest{}, 0},
		"a list":  {&mcp.ListResourcesRequest{}, 60_000},
		"a tools": {&mcp.ListToolsRequest{}, -1},
	} {
		hints := mcp.Cacheable{TTLMs: -1}
		cacheHints(context.Background(), tc.request, &hints)
		if tc.ttl < 0 {
			if hints.CacheScope != "" || hints.TTLMs != -1 {
				t.Errorf("%s list got hints %+v; it is not the person's", name, hints)
			}
			continue
		}
		if hints.CacheScope != "private" || hints.TTLMs != tc.ttl {
			t.Errorf("%s got %+v, want private with a ttl of %d", name, hints, tc.ttl)
		}
	}
}

// No argument a client or a model sends crashes the server: the replacement
// character, bytes that are not UTF-8, and a path that is not one are each
// refused as invalid, or linked as themselves.
func TestNoArgumentCrashesTheServer(t *testing.T) {
	t.Parallel()

	session := connectWith(t, ServerOptions{Name: "bb", Version: "test", Clients: testClients(t)})
	ctx := context.Background()

	for _, id := range []string{"\ufffd", "4\xff2", "1/diff"} {
		_, err := session.GetPrompt(ctx, &mcp.GetPromptParams{Name: "review_pull_request", Arguments: map[string]string{"project": "PROJ", "repo": "app", "id": id}})
		if wire := protocolError(t, err); wire.Code != jsonrpc.CodeInvalidParams {
			t.Errorf("a prompt for pull request %q got %d %q, want -32602", id, wire.Code, wire.Message)
		}
	}
	for _, uri := range []string{"bitbucket://projects/PROJ/repos/app/files/%FF", "bitbucket://projects/PROJ/repos/app/pull-requests/%EF%BF%BD"} {
		_, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: uri})
		if wire := protocolError(t, err); wire.Code != jsonrpc.CodeInvalidParams {
			t.Errorf("reading %s got %d %q, want -32602", uri, wire.Code, wire.Message)
		}
	}

	for tool, arguments := range map[string]map[string]any{
		"get_file_content": {"project": "PROJ", "repo": "app", "path": "\ufffd", "at": "\ufffd"},
		"get_commit":       {"project": "PROJ", "repo": "app", "commit_id": "refs/heads/\ufffd"},
	} {
		encoded, err := json.Marshal(arguments)
		if err != nil {
			t.Fatal(err)
		}
		linkResource(tool, encoded, &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "{}"}}})
	}
	if _, err := session.ListTools(ctx, nil); err != nil {
		t.Errorf("the server stopped answering: %v", err)
	}
}

// A template, a prompt and the resource list are served only while the tools
// whose answers they give are exposed, so --tools and --exclude decide them
// too. The hardening guide's allowlist leaves files and commits out, and a
// file resource must not read what get_file_content was left out to stop.
func TestTheToolFiltersDecideResourcesAndPrompts(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	templatesOf := func(t *testing.T, session *mcp.ClientSession) []string {
		t.Helper()
		listed, err := session.ListResourceTemplates(ctx, nil)
		if err != nil {
			t.Fatalf("resources/templates/list: %v", err)
		}
		var names []string
		for _, template := range listed.ResourceTemplates {
			names = append(names, template.Name)
		}
		return names
	}
	promptsOf := func(t *testing.T, session *mcp.ClientSession) []string {
		t.Helper()
		listed, err := session.ListPrompts(ctx, nil)
		if err != nil {
			t.Fatalf("prompts/list: %v", err)
		}
		var names []string
		for _, prompt := range listed.Prompts {
			names = append(names, prompt.Name)
		}
		return names
	}

	allowed := connectWith(t, ServerOptions{Name: "bb", Version: "test", Clients: testClients(t),
		Allow: []string{"get_pull_request", "list_pull_requests", "get_pr_diff", "list_pr_comments", "add_pr_comment"}})
	if got, want := templatesOf(t, allowed), []string{"pull_request", "pull_request_diff", "pull_request_threads"}; !slices.Equal(got, want) {
		t.Errorf("the allowlist serves templates %v, want %v", got, want)
	}
	if got := promptsOf(t, allowed); len(got) != len(AllPromptSpecs()) {
		t.Errorf("the allowlist serves prompts %v, want all of them", got)
	}
	_, err := allowed.ReadResource(ctx, &mcp.ReadResourceParams{URI: "bitbucket://projects/PROJ/repos/app/files/secrets.env"})
	if wire := protocolError(t, err); wire.Code != jsonrpc.CodeInvalidParams || !strings.Contains(wire.Message, "not found") {
		t.Errorf("a file read under the allowlist got %d %q, want -32602: the template is not served", wire.Code, wire.Message)
	}
	_, err = allowed.Complete(ctx, &mcp.CompleteParams{Ref: &mcp.CompleteReference{Type: "ref/resource", URI: fileResource}, Argument: mcp.CompleteParamsArgument{Name: "path"}})
	if wire := protocolError(t, err); wire.Code != jsonrpc.CodeInvalidParams {
		t.Errorf("completing a template that is not served got %d %q, want -32602", wire.Code, wire.Message)
	}

	excluded := connectWith(t, ServerOptions{Name: "bb", Version: "test", Clients: testClients(t), Exclude: []string{"get_pr_diff", "list_pull_requests"}})
	if got := promptsOf(t, excluded); len(got) != 0 {
		t.Errorf("with get_pr_diff left out the server serves prompts %v, which embed its answer", got)
	}
	// With list_pull_requests left out the list asks Bitbucket for nothing,
	// so against the refused port it answers rather than fails.
	if list, err := excluded.ListResources(ctx, nil); err != nil || len(list.Resources) != 0 {
		t.Errorf("with list_pull_requests left out the list is %v, %v; want it empty", list, err)
	}

	bare := connectWith(t, ServerOptions{Name: "bb", Version: "test", Clients: testClients(t), Allow: []string{"list_branches"}})
	result := bare.InitializeResult()
	if result.Capabilities.Resources != nil || result.Capabilities.Prompts != nil || result.Capabilities.Completions != nil {
		t.Errorf("a server with nothing to serve advertises %+v", result.Capabilities)
	}
}

// Under a scope, the arguments a completion has already chosen are pinned to
// the scope before anything is listed, whatever the client sent, and a project
// or repository is answered without asking Bitbucket at all.
func TestAScopePinsWhatACompletionListsFrom(t *testing.T) {
	t.Parallel()

	var seen []map[string]string
	next := func(_ context.Context, _ string, req mcp.Request) (mcp.Result, error) {
		complete := req.(*mcp.CompleteRequest)
		seen = append(seen, complete.Params.Context.Arguments)
		return &mcp.CompleteResult{Completion: mcp.CompletionResultDetails{Values: []string{}}}, nil
	}
	governance := governanceMiddleware(Scope{ProjectKey: "PROJ", RepoSlug: "app"}, nil, AuditFailureDeny, nil)(next)

	ask := func(argument string, given map[string]string) {
		t.Helper()
		_, err := governance(context.Background(), "completion/complete", &mcp.CompleteRequest{Params: &mcp.CompleteParams{
			Ref:      &mcp.CompleteReference{Type: "ref/resource", URI: fileResource},
			Argument: mcp.CompleteParamsArgument{Name: argument},
			Context:  &mcp.CompleteContext{Arguments: given},
		}})
		if err != nil {
			t.Fatalf("complete %s: %v", argument, err)
		}
	}
	ask("path", map[string]string{"project": "OTHER", "repo": "elsewhere", "at": "main"})
	ask("at", nil)
	ask("project", map[string]string{"project": "OTHER"})
	ask("repo", nil)

	if len(seen) != 2 {
		t.Fatalf("Bitbucket was asked %d times, want only for the path and the ref: %v", len(seen), seen)
	}
	for _, arguments := range seen {
		if arguments["project"] != "PROJ" || arguments["repo"] != "app" {
			t.Errorf("a completion listed from %v, want the scope's PROJ/app", arguments)
		}
	}
	if seen[0]["at"] != "main" {
		t.Errorf("the ref the client chose was lost: %v", seen[0])
	}
}

// A client on a revision before resource links gets tool results without
// them.
func TestResourceLinksGoOnlyToClientsThatHaveThem(t *testing.T) {
	t.Parallel()

	for version, want := range map[string]bool{"2024-11-05": false, "2025-03-26": false, "2025-06-18": true, "2025-11-25": true, "2026-07-28": true, "": false} {
		if got := supportsResourceLinks(version); got != want {
			t.Errorf("supportsResourceLinks(%q) = %v, want %v", version, got, want)
		}
	}
}
