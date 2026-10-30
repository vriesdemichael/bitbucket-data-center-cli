package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// connectAs is a client of a server with these options, declaring these
// capabilities.
func connectAs(t *testing.T, opts ServerOptions, client *mcp.ClientOptions) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := NewServer(opts).Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Wait() })
	session, err := mcp.NewClient(&mcp.Implementation{Name: "a-client", Version: "9.9"}, client).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func diagnoseServer(t *testing.T) ServerOptions {
	t.Helper()
	return ServerOptions{Name: "bb", Version: "1.2.3", Clients: testClients(t)}
}

// callDiagnose calls a tool and decodes its structured answer.
func callDiagnose[Out any](t *testing.T, session *mcp.ClientSession, tool string, arguments map[string]any) (*mcp.CallToolResult, Out) {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: arguments})
	if err != nil {
		t.Fatalf("%s: %v", tool, err)
	}
	if result.IsError {
		t.Fatalf("%s failed: %+v", tool, result.Content)
	}
	var out Out
	if err := remarshal(result.StructuredContent, &out); err != nil {
		t.Fatalf("decode %s: %v", tool, err)
	}
	return result, out
}

// diagnose reports what the server can see without a page: its build, what it
// exposes, and the client as it declared itself, in full.
func TestDiagnoseReportsTheServerAndTheClient(t *testing.T) {
	t.Parallel()

	// As a client declares them on the wire, deprecated ones included: the report
	// says what was declared.
	capabilities := &mcp.ClientCapabilities{}
	if err := json.Unmarshal([]byte(`{"sampling":{},"elicitation":{"form":{}}}`), capabilities); err != nil {
		t.Fatal(err)
	}
	capabilities.AddExtension(appsExtension, map[string]any{"mimeTypes": []string{viewMIMEType}})

	opts := diagnoseServer(t)
	opts.Exclude = []string{"get_commit"}
	opts.Allow = nil
	opts.ReadOnly = true
	opts.ReadOnlyByPolicy = true
	opts.ElicitationOptIn = true
	opts.Scope = Scope{ProjectKey: "SECRETPROJECT"}
	opts.Configuration = func() config.Diagnosis {
		return config.Diagnosis{
			Files:    []config.DiagnosedFile{{Tier: "stored", Path: "/home/alice/.config/bb/config.yaml", Read: true, Exists: true, Problem: "no YAML in /home/alice"}},
			Settings: []config.DiagnosedSetting{{Name: "bitbucket_url", Value: "https://bitbucket.corp.example.com", Configured: true, Source: config.SettingSource{Kind: "environment", Name: "BITBUCKET_URL"}}},
			Keyring:  config.KeyringDiagnosis{Required: true},
		}
	}
	_, out := callDiagnose[DiagnoseOutput](t, connectAs(t, opts, &mcp.ClientOptions{Capabilities: capabilities}), "diagnose", nil)

	if out.Build.Version != "1.2.3" || out.Build.Page != diagnoseBuildID() || out.Build.Go == "" || out.Build.OS == "" {
		t.Errorf("build = %+v", out.Build)
	}
	if out.Client.Name != "a-client" || out.Client.Version != "9.9" || out.Client.ProtocolVersion == "" || !out.Client.DisplaysViews {
		t.Errorf("client = %+v", out.Client)
	}
	for _, capability := range []string{"elicitation", "sampling", "extensions"} {
		if _, ok := out.Client.Capabilities[capability]; !ok {
			t.Errorf("the client's capabilities do not hold %s: %v", capability, out.Client.Capabilities)
		}
	}
	if _, declared := out.Client.Capabilities["roots"]; declared {
		t.Errorf("a client that declared no roots is reported with them: %v", out.Client.Capabilities)
	}
	// A client left to the SDK's defaults declares roots.
	_, defaults := callDiagnose[DiagnoseOutput](t, connectAs(t, diagnoseServer(t), nil), "diagnose", nil)
	if _, declared := defaults.Client.Capabilities["roots"]; !declared {
		t.Errorf("a client that declared roots is reported without them: %v", defaults.Client.Capabilities)
	}
	exposure := out.Exposure
	if !exposure.ReadOnly || !exposure.ReadOnlyByPolicy || exposure.Scope != "project" || !slices.Contains(exposure.Exclude, "get_commit") {
		t.Errorf("exposure = %+v", exposure)
	}
	if slices.Contains(exposure.Exposed, "create_pull_request") || !slices.Contains(exposure.Exposed, "show") || !slices.Contains(exposure.ViewKinds, showKindDiff) {
		t.Errorf("exposed = %v, view kinds = %v", exposure.Exposed, exposure.ViewKinds)
	}
	if !out.Confirmations.On || !out.Confirmations.ClientCanShow {
		t.Errorf("confirmations = %+v", out.Confirmations)
	}
	if out.Configuration == nil || out.Configuration.Keyring != "unreachable" ||
		len(out.Configuration.Files) != 1 || out.Configuration.Files[0].Valid ||
		len(out.Configuration.Settings) != 1 || out.Configuration.Settings[0].Source != "environment" {
		t.Errorf("configuration = %+v", out.Configuration)
	}
	if len(out.NotCapturable) != 2 {
		t.Errorf("not capturable = %v, want it said what nothing here can find out", out.NotCapturable)
	}
	findings := strings.Join(out.Findings, "\n")
	for _, want := range []string{"cannot be reached", "stored configuration file is invalid", "keyring"} {
		if !strings.Contains(findings, want) {
			t.Errorf("the findings do not say %q:\n%s", want, findings)
		}
	}
}

// What goes into an issue holds no credential, person, path or address, and not
// the project a scope names: a report is read by whoever the issue is open to.
func TestDiagnoseSaysNothingItShouldNot(t *testing.T) {
	t.Parallel()

	opts := diagnoseServer(t)
	clients, err := ClientsFromConfig(config.AppConfig{BitbucketURL: "http://bitbucket.corp.example.com:7990/context", BitbucketPassword: "hunter2", BitbucketUsername: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	opts.Clients = clients
	opts.Scope = Scope{ProjectKey: "SECRETPROJECT"}
	opts.Configuration = func() config.Diagnosis {
		return config.Diagnosis{
			Files:    []config.DiagnosedFile{{Tier: "stored", Path: "/home/alice/.config/bb/config.yaml", Read: true, Exists: true, Problem: "no YAML in /home/alice"}},
			Settings: []config.DiagnosedSetting{{Name: "bitbucket_username", Value: "alice", Configured: true, Source: config.SettingSource{Kind: "environment", Path: "/home/alice/.env"}}},
		}
	}
	session := connectAs(t, opts, viewClient())
	result, out := callDiagnose[DiagnoseOutput](t, session, "diagnose", nil)

	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"hunter2", "alice", "corp.example.com", "7990", "SECRETPROJECT"} {
		if strings.Contains(string(encoded), name) {
			t.Errorf("the answer holds %q:\n%.2000s", name, encoded)
		}
	}
	if out.Connection.Scheme != "http" || out.Connection.Reachable || out.Connection.Credential != "unknown" || out.Connection.Problem == "" {
		t.Errorf("connection = %+v, want http and not reached", out.Connection)
	}
}

// A report a page left is scrubbed of Bitbucket's address, however the page wrote
// it, is bounded, and comes back with the next diagnose.
func TestDiagnoseReadsBackWhatThePageMeasured(t *testing.T) {
	t.Parallel()

	opts := diagnoseServer(t)
	clients, err := ClientsFromConfig(config.AppConfig{BitbucketURL: "https://bitbucket.corp.example.com:8443"})
	if err != nil {
		t.Fatal(err)
	}
	opts.Clients = clients
	session := connectAs(t, opts, viewClient())

	_, first := callDiagnose[DiagnoseOutput](t, session, "diagnose", nil)
	if first.PageReport != nil {
		t.Errorf("a first diagnose reports a page report: %v", first.PageReport)
	}

	_, check := callDiagnose[DiagnoseCheckOutput](t, session, "diagnose_check", map[string]any{"report": map[string]any{
		"host":  "a-host",
		"csp":   []string{"https://bitbucket.corp.example.com:8443", "bitbucket.corp.example.com"},
		"error": "fetch of https://bitbucket.corp.example.com:8443/status failed",
	}})
	if !check.Stored || check.Calls != 1 {
		t.Errorf("the check answered %+v", check)
	}

	result, second := callDiagnose[DiagnoseOutput](t, session, "diagnose", nil)
	if second.PageReport["host"] != "a-host" {
		t.Errorf("the next diagnose does not hold the page's report: %v", second.PageReport)
	}
	encoded, _ := json.Marshal(second.PageReport)
	if strings.Contains(string(encoded), "corp.example.com") || second.PageReport["error"] != "fetch of "+diagnoseAddress+"/status failed" {
		t.Errorf("the page's report still names Bitbucket: %s", encoded)
	}
	if second.Pages.CheckCalls != 1 {
		t.Errorf("the next diagnose counts %d calls, want 1", second.Pages.CheckCalls)
	}
	if text := result.Content[0].(*mcp.TextContent).Text; !strings.Contains(text, "page_report") {
		t.Errorf("the text does not say where the page's report is:\n%s", text)
	}

	oversized := map[string]any{"filler": strings.Repeat("x", diagnoseMaxReportBytes)}
	answer, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "diagnose_check", Arguments: map[string]any{"report": oversized}})
	if err != nil {
		t.Fatal(err)
	}
	if !answer.IsError {
		t.Error("the server kept a report past its bound")
	}
}

// The page sees a result's _meta only if it arrives, so the answer carries a
// marker in it, and filler of the size being tried. It goes in _meta alone: the
// model is never given it.
func TestDiagnoseCarriesItsFillerOnlyInMeta(t *testing.T) {
	t.Parallel()

	session := connectAs(t, diagnoseServer(t), viewClient())
	meta := func(result *mcp.CallToolResult) map[string]any {
		t.Helper()
		var payload map[string]any
		if err := remarshal(result.Meta[diagnosePayloadKey], &payload); err != nil {
			t.Fatal(err)
		}
		return payload
	}

	result, _ := callDiagnose[DiagnoseOutput](t, session, "diagnose", nil)
	payload := meta(result)
	if payload["build"] != diagnoseBuildID() || payload["meta_kib"] != float64(diagnoseDefaultMetaKiB) {
		t.Errorf("the payload is %v", payload["meta_kib"])
	}
	if filler, _ := payload["filler"].(string); len(filler) != diagnoseDefaultMetaKiB<<10 {
		t.Errorf("the filler is %d bytes, want %d", len(filler), diagnoseDefaultMetaKiB<<10)
	}
	encoded, _ := json.Marshal(result.StructuredContent)
	text := result.Content[0].(*mcp.TextContent).Text
	if len(encoded)+len(text) > 32<<10 || strings.Contains(string(encoded), "xxxxxxxx") || strings.Contains(text, "xxxxxxxx") {
		t.Errorf("what the model reads holds the filler: %d bytes", len(encoded)+len(text))
	}

	larger, _ := callDiagnose[DiagnoseOutput](t, session, "diagnose", map[string]any{"meta_kib": 4})
	if filler, _ := meta(larger)["filler"].(string); len(filler) != 4<<10 {
		t.Errorf("meta_kib 4 carried %d bytes", len(filler))
	}
	for tool, arguments := range map[string]map[string]any{
		"diagnose":       {"meta_kib": diagnoseMaxMetaKiB + 1},
		"diagnose_check": {"meta_kib": -1},
	} {
		answer, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: arguments})
		if err != nil {
			t.Fatal(err)
		}
		if !answer.IsError {
			t.Errorf("%s accepted %v", tool, arguments)
		}
	}

	// A view's own call of the size it asks for, and none where it asks for none.
	sized, _ := callDiagnose[DiagnoseCheckOutput](t, session, "diagnose_check", map[string]any{"meta_kib": 2})
	if filler, _ := meta(sized)["filler"].(string); len(filler) != 2<<10 {
		t.Errorf("the check's meta_kib 2 carried %d bytes", len(filler))
	}
	plain, _ := callDiagnose[DiagnoseCheckOutput](t, session, "diagnose_check", nil)
	if _, carried := plain.Meta[diagnosePayloadKey]; carried {
		t.Error("a check that asks for no size carried filler")
	}

	// A client that draws no page is given none.
	bare := connectAs(t, diagnoseServer(t), nil)
	result, out := callDiagnose[DiagnoseOutput](t, bare, "diagnose", nil)
	if _, carried := result.Meta[diagnosePayloadKey]; carried {
		t.Error("a client that displays no views was sent the page's payload")
	}
	if !slices.ContainsFunc(out.Findings, func(finding string) bool { return strings.Contains(finding, "displays no views") }) {
		t.Errorf("the findings do not say the client displays no views: %v", out.Findings)
	}
}

// The page is one of its own, bound to the tool. Alone among bb's it declares a
// domain, Bitbucket's, and the clipboard, and it holds no other Bitbucket data, so
// it stays out of what a person attaches and out of any shared cache.
func TestDiagnoseHasAPageOfItsOwn(t *testing.T) {
	t.Parallel()

	opts := diagnoseServer(t)
	clients, err := ClientsFromConfig(config.AppConfig{BitbucketURL: "https://bitbucket.corp.example.com:8443/context"})
	if err != nil {
		t.Fatal(err)
	}
	opts.Clients = clients
	session := connectAs(t, opts, viewClient())

	tool := listedTool(t, session, "diagnose")
	if tool == nil {
		t.Fatal("diagnose is not listed")
	}
	ui, _ := tool.Meta["ui"].(map[string]any)
	if ui["resourceUri"] != diagnoseURI || tool.Meta["ui/resourceUri"] != diagnoseURI || tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
		t.Errorf("diagnose's _meta = %v, annotations %+v", tool.Meta, tool.Annotations)
	}
	check := listedTool(t, session, "diagnose_check")
	if check == nil {
		t.Fatal("diagnose_check is not listed")
	}
	checkUI, _ := check.Meta["ui"].(map[string]any)
	if visibility, _ := checkUI["visibility"].([]any); len(visibility) != 1 || visibility[0] != "app" {
		t.Errorf("diagnose_check's _meta = %v, want it offered to views alone", check.Meta)
	}

	read, err := session.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: diagnoseURI})
	if err != nil {
		t.Fatalf("read the page: %v", err)
	}
	page := read.Contents[0]
	if page.MIMEType != viewMIMEType || !strings.Contains(page.Text, "<title>bb diagnose</title>") || strings.Contains(page.Text, diagnoseConfigPlaceholder) {
		t.Errorf("the page is %s, %.200s", page.MIMEType, page.Text)
	}
	if !strings.Contains(page.Text, "https://bitbucket.corp.example.com:8443/context") {
		t.Error("the page does not know where to look")
	}
	if strings.Contains(page.Text, "renderPullRequest") {
		t.Error("the page carries what draws Bitbucket's data")
	}
	var meta struct {
		UI struct {
			CSP struct {
				ConnectDomains  []string `json:"connectDomains"`
				ResourceDomains []string `json:"resourceDomains"`
			} `json:"csp"`
			Permissions map[string]any `json:"permissions"`
		} `json:"ui"`
	}
	if err := remarshal(page.Meta, &meta); err != nil {
		t.Fatal(err)
	}
	want := []string{"https://bitbucket.corp.example.com:8443"}
	if !slices.Equal(meta.UI.CSP.ConnectDomains, want) || !slices.Equal(meta.UI.CSP.ResourceDomains, want) {
		t.Errorf("the page declares %+v, want its Bitbucket's origin", meta.UI.CSP)
	}
	if _, ok := meta.UI.Permissions["clipboardWrite"]; !ok {
		t.Errorf("the page declares %v, want the clipboard", meta.UI.Permissions)
	}

	// The fixtures, each of its type, and a template; each read is counted.
	for uri, mimeType := range map[string]string{
		diagnoseURI + "/fixture.png":  "image/png",
		diagnoseURI + "/fixture.txt":  "text/plain",
		diagnoseURI + "/fixture.pdf":  "application/pdf",
		diagnoseURI + "/template/any": "text/plain",
	} {
		fixture, err := session.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: uri})
		if err != nil {
			t.Errorf("read %s: %v", uri, err)
			continue
		}
		if got := fixture.Contents[0]; got.MIMEType != mimeType || (got.Text == "" && len(got.Blob) == 0) {
			t.Errorf("%s reads as %+v", uri, got)
		}
	}
	_, out := callDiagnose[DiagnoseOutput](t, session, "diagnose", nil)
	if out.Pages.CheckReads != 4 || out.Pages.DiagnosePageReads != 1 || out.Pages.DiagnosePageReadAt == "" {
		t.Errorf("pages = %+v, want four reads of fixtures and one of the page", out.Pages)
	}

	// None of it is for a person to attach. The list of pull requests, which
	// reads Bitbucket, goes with its tool.
	opts.Exclude = []string{"list_pull_requests"}
	listing := connectAs(t, opts, viewClient())
	resources, err := listing.ListResources(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, resource := range resources.Resources {
		if strings.HasPrefix(resource.URI, "ui://") {
			t.Errorf("resources/list offers %s", resource.URI)
		}
	}
	templates, err := session.ListResourceTemplates(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(templates.ResourceTemplates) == 0 {
		t.Fatal("resources/templates/list is empty, so the check below proves nothing")
	}
	for _, template := range templates.ResourceTemplates {
		if strings.HasPrefix(template.URITemplate, "ui://") {
			t.Errorf("resources/templates/list offers %s", template.URITemplate)
		}
	}

	hints := &mcp.Cacheable{}
	cacheHints(context.Background(), &mcp.ReadResourceRequest{Params: &mcp.ReadResourceParams{URI: diagnoseURI}}, hints)
	if hints.CacheScope != "private" {
		t.Errorf("the page, which names this server's Bitbucket, caches as %+v", hints)
	}
}

// The host reads the page views draw in, which diagnose reports, since a host may
// keep what it read.
func TestDiagnoseSaysWhenTheHostReadThePage(t *testing.T) {
	t.Parallel()

	session := connectAs(t, diagnoseServer(t), viewClient())
	if _, err := session.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: viewURI}); err != nil {
		t.Fatal(err)
	}
	_, out := callDiagnose[DiagnoseOutput](t, session, "diagnose", nil)
	if out.Pages.ViewPageReads != 1 || out.Pages.ViewPageReadAt == "" {
		t.Errorf("pages = %+v, want the view page's read", out.Pages)
	}
}

// --exclude show turns views off, and diagnose's page is one: the tool still
// reports what the server can see, and asks the client to draw nothing.
func TestDiagnoseWithoutViewsDrawsNoPage(t *testing.T) {
	t.Parallel()

	opts := diagnoseServer(t)
	opts.Exclude = []string{"show"}
	session := connectAs(t, opts, viewClient())

	tool := listedTool(t, session, "diagnose")
	if tool == nil {
		t.Fatal("diagnose went with show")
	}
	if len(tool.Meta) != 0 || strings.Contains(tool.Description, "page") {
		t.Errorf("diagnose names a page without views: %v, %s", tool.Meta, tool.Description)
	}
	if listedTool(t, session, "diagnose_check") != nil {
		t.Error("diagnose_check is listed with no page to call it")
	}
	if _, err := session.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: diagnoseURI}); err == nil {
		t.Error("the page is served with views off")
	}
	result, _ := callDiagnose[DiagnoseOutput](t, session, "diagnose", nil)
	if _, carried := result.Meta[diagnosePayloadKey]; carried {
		t.Error("diagnose carried the page's payload with views off")
	}
	if text := result.Content[0].(*mcp.TextContent).Text; strings.Contains(text, "The page shows") {
		t.Errorf("diagnose speaks of a page with views off:\n%s", text)
	}

	withoutIt := diagnoseServer(t)
	withoutIt.Exclude = []string{"diagnose"}
	excluded := connectAs(t, withoutIt, viewClient())
	if listedTool(t, excluded, "diagnose") != nil || listedTool(t, excluded, "diagnose_check") != nil {
		t.Error("--exclude diagnose leaves the tool or its check")
	}
}

// --tools names what the model is offered: diagnose comes when it is named, and
// its check with it.
func TestDiagnoseFollowsTools(t *testing.T) {
	t.Parallel()

	opts := diagnoseServer(t)
	opts.Allow = []string{"get_pull_request", "show"}
	session := connectAs(t, opts, viewClient())
	if listedTool(t, session, "diagnose") != nil || listedTool(t, session, "diagnose_check") != nil {
		t.Error("diagnose is listed where --tools does not name it")
	}

	opts.Allow = []string{"get_pull_request", "show", "diagnose"}
	session = connectAs(t, opts, viewClient())
	if listedTool(t, session, "diagnose") == nil || listedTool(t, session, "diagnose_check") == nil {
		t.Error("diagnose and its check are not listed where --tools names it")
	}
}

// A scope has nothing to bind on diagnose: it reads no repository, takes no
// project, and a scoped server needs it as much as any other.
func TestDiagnoseIsOfferedUnderAScope(t *testing.T) {
	t.Parallel()

	opts := diagnoseServer(t)
	opts.Scope = Scope{ProjectKey: "PROJ", RepoSlug: "app"}
	session := connectAs(t, opts, viewClient())
	_, out := callDiagnose[DiagnoseOutput](t, session, "diagnose", nil)
	if out.Exposure.Scope != "repository" {
		t.Errorf("scope = %q, want repository", out.Exposure.Scope)
	}
	callDiagnose[DiagnoseCheckOutput](t, session, "diagnose_check", nil)
}

func TestConnectionProblemNamesNoAddress(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		err  error
		want string
	}{
		{"certificate", errors.New(`Get "https://bb.corp.example.com/": x509: certificate signed by unknown authority`), "certificate"},
		{"tls", errors.New("remote error: tls: handshake failure at 10.1.2.3"), "TLS"},
		{"resolve", errors.New("dial tcp: lookup bb.corp.example.com: no such host"), "does not resolve"},
		{"refused", errors.New("dial tcp 10.1.2.3:443: connect: connection refused"), "refused the connection"},
		{"deadline", errors.New("Get bb.corp.example.com: context deadline exceeded"), "in time"},
		{"credential", apperrors.New(apperrors.KindAuthentication, "the token for bb.corp.example.com was refused", nil), "refused the credential"},
		{"permission", apperrors.New(apperrors.KindAuthorization, "bb.corp.example.com says no", nil), "refused the credential"},
		{"other", apperrors.New(apperrors.KindPermanent, "bb.corp.example.com broke", nil), "the request to Bitbucket failed"},
	} {
		got := connectionProblem(test.err)
		if !strings.Contains(got, test.want) {
			t.Errorf("%s: %q, want it to say %q", test.name, got, test.want)
		}
		if strings.Contains(got, "corp.example.com") || strings.Contains(got, "10.1.2.3") {
			t.Errorf("%s: %q names the address", test.name, got)
		}
	}
}

// Whole names only: a host called "git" leaves "digit" alone, and a report keeps
// its words.
func TestScrubAddressMatchesWholeNames(t *testing.T) {
	t.Parallel()

	for _, test := range []struct{ base, in, want string }{
		{"https://git:8443", `"digit git:8443,https://git:8443/x git legit"`, `"digit <bitbucket>,<bitbucket>/x <bitbucket> legit"`},
		{"https://bb.example.com", `["bb.example.com","bb.example.com.evil.org"]`, `["<bitbucket>","bb.example.com.evil.org"]`},
		{"https://bb.example.com", `bb.example.com,bb.example.com`, `<bitbucket>,<bitbucket>`},
	} {
		if got := scrubAddress(test.in, test.base); got != test.want {
			t.Errorf("scrubAddress(%q, %q) = %q, want %q", test.in, test.base, got, test.want)
		}
	}
}

func TestDiagnoseAddressForms(t *testing.T) {
	t.Parallel()

	got := diagnoseAddressForms("https://bb.example.com:8443/context/")
	want := []string{"https://bb.example.com:8443/context", "https://bb.example.com:8443", "bb.example.com:8443", "bb.example.com"}
	if !slices.Equal(got, want) {
		t.Errorf("forms = %v, want %v", got, want)
	}
	if forms := diagnoseAddressForms("not a url"); forms != nil {
		t.Errorf("forms of nothing = %v", forms)
	}
	if origin := originOf(testsupport.RefusedURL); origin == "" {
		t.Error("no origin for a real address")
	}
	if origin := originOf("nonsense"); origin != "" {
		t.Errorf("origin of nonsense = %q", origin)
	}
	if meta := diagnoseResourceMeta("nonsense"); strings.Contains(strings.ToLower(jsonOf(t, meta)), "connectdomains") {
		t.Errorf("a server with no address declares domains: %v", meta)
	}
}

func jsonOf(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

// The checks' words for a configuration file that bb reads and could not use, and
// a client that cannot confirm what the server asks.
func TestDiagnoseFindingsSayWhatToFix(t *testing.T) {
	t.Parallel()

	findings := diagnoseFindings(DiagnoseOutput{
		Connection:    diagnoseConnection{Reachable: true, Credential: "refused", Problem: "Bitbucket refused the credential"},
		Confirmations: diagnoseConfirmations{On: true, Tools: []string{"merge_pull_request"}},
		Client:        diagnoseClient{DisplaysViews: true},
	})
	joined := strings.Join(findings, "\n")
	for _, want := range []string{"check the credential", "cannot show them", "merge_pull_request"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the findings do not say %q:\n%s", want, joined)
		}
	}
	text := diagnoseText(DiagnoseOutput{Connection: diagnoseConnection{Reachable: true, BitbucketVersion: "10.5.1", Scheme: "https", Credential: "works"}, Exposure: diagnoseExposure{ReadOnly: true}}, true)
	for _, want := range []string{"Bitbucket 10.5.1, over https: reached; the credential works", "read-only", "The page shows"} {
		if !strings.Contains(text, want) {
			t.Errorf("the text does not say %q:\n%s", want, text)
		}
	}
	if none := diagnoseConnectionOf(context.Background(), Clients{BaseURL: "https://x"}); none.Problem == "" || none.Reachable {
		t.Errorf("a server with no client = %+v", none)
	}
	if client := diagnoseClientOf(nil); client.DisplaysViews {
		t.Errorf("no request = %+v", client)
	}
	configuration := diagnoseConfigurationOf(config.Diagnosis{Keyring: config.KeyringDiagnosis{Required: true, Reachable: true}})
	if configuration.Keyring != "reachable" {
		t.Errorf("keyring = %q", configuration.Keyring)
	}
	if plain := diagnoseConfigurationOf(config.Diagnosis{}); plain.Keyring != "not required" {
		t.Errorf("keyring = %q", plain.Keyring)
	}
}
