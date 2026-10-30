package mcp

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/url"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// The diagnose tool (ADR-101). When a view misbehaves, what happened lies
// between the page and the host, where a server cannot see it, and bb doctor
// cannot see the environment an MCP client starts the server with. So diagnose
// is two halves. The tool reports what the server can see, from inside the
// server. Its page, a second one of its own, measures what the client's host does
// with a view, and says so in the page, in the view's context and, where the host
// passes tool calls, to the next diagnose.
//
// What it reports gets pasted into issues, so it holds no credential, pull
// request or person, and not Bitbucket's address: only whether that is http or
// https.
const (
	// diagnoseURI is the page the tool's answer renders in. It is a page apart
	// from the views' because a host binds a page to a tool and a page declares
	// its network: this one declares Bitbucket's address, to measure what gets
	// through, and the views' declares none.
	diagnoseURI = "ui://bb/diagnose"

	// diagnosePayloadKey is where the tool puts what only the page reads, in the
	// result's _meta.
	diagnosePayloadKey = "io.github.vriesdemichael.bb/diagnose"

	// diagnoseFixtureTemplateURI is the one template the page reads.
	diagnoseFixtureTemplateURI = diagnoseURI + "/template/{name}"

	// diagnoseDefaultMetaKiB is how much filler an answer carries by default.
	// A view may carry more than this, and through Claude Code this is as far
	// as it was measured.
	diagnoseDefaultMetaKiB = 1024
	diagnoseMaxMetaKiB     = 32 << 10

	// diagnoseMaxReportBytes bounds what a page may leave with the server.
	diagnoseMaxReportBytes = 256 << 10

	// diagnoseBudget bounds how long the tool spends on the way to Bitbucket.
	diagnoseBudget = 20 * time.Second

	diagnoseAddress = "<bitbucket>"
)

// diagnoseScripts are the diagnose page's scripts: it shares the views' side of
// MCP Apps and the way they build elements, and nothing that draws Bitbucket's
// data.
var diagnoseScripts = []string{"bridge.js", "dom.js", "diagnose.js"}

//go:embed views/diagnose.css
var diagnoseStyle string

const diagnoseConfigPlaceholder = "/*BB_DIAGNOSE_CONFIG*/{}"

// A one-pixel PNG and a minimal PDF, the types a page reads besides text.
var (
	diagnoseFixturePNG = mustDecodeBase64("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==")
	diagnoseFixturePDF = []byte("%PDF-1.1\n1 0 obj<</Type/Catalog/Pages 2 0 R>>endobj\n2 0 obj<</Type/Pages/Kids[3 0 R]/Count 1>>endobj\n3 0 obj<</Type/Page/Parent 2 0 R/MediaBox[0 0 72 72]>>endobj\ntrailer<</Root 1 0 R>>\n")
)

func mustDecodeBase64(text string) []byte {
	decoded, err := base64.StdEncoding.DecodeString(text)
	if err != nil {
		panic(fmt.Sprintf("decoding a fixture: %v", err))
	}
	return decoded
}

// diagnoseFixtures are what the page reads to see which types of resource reach
// it, beside itself and the template. They are the check's own: reading them
// brings no Bitbucket data near the report.
var diagnoseFixtures = []mcp.ResourceContents{
	{URI: diagnoseURI + "/fixture.png", MIMEType: "image/png", Blob: diagnoseFixturePNG},
	{URI: diagnoseURI + "/fixture.txt", MIMEType: "text/plain", Text: "A file bb's diagnose page reads."},
	{URI: diagnoseURI + "/fixture.pdf", MIMEType: "application/pdf", Blob: diagnoseFixturePDF},
}

// diagnoseLog is what reached the server of a check, for the next diagnose to
// say: a server sees a page only by what the host passes on to it.
type diagnoseLog struct {
	mu         sync.Mutex
	viewReads  int
	viewReadAt time.Time
	pageReads  int
	pageReadAt time.Time
	checkReads int
	checkCalls int
	report     map[string]any
}

func (log *diagnoseLog) readView() {
	log.mu.Lock()
	defer log.mu.Unlock()
	log.viewReads++
	log.viewReadAt = time.Now()
}

func (log *diagnoseLog) readPage() {
	log.mu.Lock()
	defer log.mu.Unlock()
	log.pageReads++
	log.pageReadAt = time.Now()
}

func (log *diagnoseLog) readFixture() {
	log.mu.Lock()
	defer log.mu.Unlock()
	log.checkReads++
}

// called counts a check's call, and keeps a report that came with it.
func (log *diagnoseLog) called(report map[string]any) DiagnoseCheckOutput {
	log.mu.Lock()
	defer log.mu.Unlock()
	log.checkCalls++
	if report != nil {
		log.report = report
	}
	return DiagnoseCheckOutput{Reads: log.checkReads, Calls: log.checkCalls, Stored: report != nil}
}

func (log *diagnoseLog) snapshot() (diagnosePages, map[string]any) {
	log.mu.Lock()
	defer log.mu.Unlock()
	return diagnosePages{
		ViewPageReads:      log.viewReads,
		ViewPageReadAt:     stampOf(log.viewReadAt),
		DiagnosePageReads:  log.pageReads,
		DiagnosePageReadAt: stampOf(log.pageReadAt),
		CheckReads:         log.checkReads,
		CheckCalls:         log.checkCalls,
	}, log.report
}

func stampOf(at time.Time) string {
	if at.IsZero() {
		return ""
	}
	return at.UTC().Format(time.RFC3339)
}

// DiagnoseInput is the argument set for diagnose.
type DiagnoseInput struct {
	MetaKiB int `json:"meta_kib,omitempty" jsonschema:"How many KiB of filler the answer carries for the page, to find the largest result that reaches a view in this client. Defaults to 1024, at most 32768. Send more only when the person wants to find that limit"`
}

// DiagnoseOutput is what the server can see of itself and of the client.
type DiagnoseOutput struct {
	Build         diagnoseBuild          `json:"build" jsonschema:"This bb"`
	Configuration *diagnoseConfiguration `json:"configuration,omitempty" jsonschema:"The configuration the client started bb with, as bb doctor checks it, without values that name someone or something"`
	Connection    diagnoseConnection     `json:"connection" jsonschema:"The way to Bitbucket"`
	Exposure      diagnoseExposure       `json:"exposure" jsonschema:"What this server offers"`
	Confirmations diagnoseConfirmations  `json:"confirmations" jsonschema:"Whether tools ask the person, and whether this client can show it"`
	Client        diagnoseClient         `json:"client" jsonschema:"The client, as it declared itself to this server"`
	Pages         diagnosePages          `json:"pages" jsonschema:"When the host read the pages, and how many of the page's checks reached the server"`
	Findings      []string               `json:"findings" jsonschema:"What the above means, and what to fix"`
	PageReport    map[string]any         `json:"page_report,omitempty" jsonschema:"What the diagnose page last measured in this client, where the host passes a view's calls to the server"`
	NotCapturable []string               `json:"not_capturable" jsonschema:"What nothing here can find out"`
}

type diagnoseBuild struct {
	Version string `json:"version" jsonschema:"bb's version"`
	Page    string `json:"page" jsonschema:"Identifies the pages this bb serves. A page that names another was read from another bb"`
	Go      string `json:"go" jsonschema:"The Go version bb was built with"`
	OS      string `json:"os" jsonschema:"The operating system and architecture bb runs on"`
}

type diagnoseConfiguration struct {
	Files    []diagnoseFile    `json:"files" jsonschema:"The configuration files bb read, by where they come from"`
	Settings []diagnoseSetting `json:"settings" jsonschema:"Each setting, and where it came from"`
	Keyring  string            `json:"keyring" jsonschema:"not required, reachable or unreachable"`
}

type diagnoseFile struct {
	Tier   string `json:"tier" jsonschema:"stored, workspace or system"`
	Exists bool   `json:"exists" jsonschema:"Whether the file is there"`
	Valid  bool   `json:"valid" jsonschema:"False when the file is present and bb would refuse it"`
}

type diagnoseSetting struct {
	Name       string `json:"name" jsonschema:"The setting"`
	Configured bool   `json:"configured" jsonschema:"Whether it has a value"`
	Source     string `json:"source" jsonschema:"Where it came from: a flag, the environment, a file, the registry or the default"`
	Problem    bool   `json:"problem,omitempty" jsonschema:"True when a command loading it would refuse it"`
}

type diagnoseConnection struct {
	Scheme           string `json:"scheme" jsonschema:"http or https"`
	Reachable        bool   `json:"reachable" jsonschema:"Whether Bitbucket answered"`
	BitbucketVersion string `json:"bitbucket_version,omitempty" jsonschema:"The version Bitbucket says it is"`
	Credential       string `json:"credential" jsonschema:"works, refused or unknown when Bitbucket could not be reached"`
	Problem          string `json:"problem,omitempty" jsonschema:"What went wrong, in words that name no address"`
}

type diagnoseExposure struct {
	ReadOnly         bool     `json:"read_only" jsonschema:"Whether the server offers only the tools that read"`
	ReadOnlyByPolicy bool     `json:"read_only_by_policy" jsonschema:"Whether administrative policy keeps it so"`
	Tools            []string `json:"tools,omitempty" jsonschema:"What --tools names"`
	Exclude          []string `json:"exclude,omitempty" jsonschema:"What --exclude names"`
	Scope            string   `json:"scope" jsonschema:"none, project or repository: what --project or --repo confines it to"`
	Exposed          []string `json:"exposed" jsonschema:"The tools this server offers"`
	ViewKinds        []string `json:"view_kinds,omitempty" jsonschema:"The kinds show offers"`
	Audit            bool     `json:"audit" jsonschema:"Whether calls are recorded in an audit trail"`
}

type diagnoseConfirmations struct {
	On            bool     `json:"on" jsonschema:"Whether the server was started with --elicitation-opt-in"`
	ClientCanShow bool     `json:"client_can_show" jsonschema:"Whether this client declared it can show a confirmation"`
	Tools         []string `json:"tools,omitempty" jsonschema:"The tools that ask, while confirmations are on"`
}

type diagnoseClient struct {
	Name            string         `json:"name,omitempty" jsonschema:"The client's name"`
	Version         string         `json:"version,omitempty" jsonschema:"The client's version"`
	ProtocolVersion string         `json:"protocol_version,omitempty" jsonschema:"The MCP version it speaks"`
	Capabilities    map[string]any `json:"capabilities,omitempty" jsonschema:"Its capabilities in full: roots, elicitation, sampling and extensions"`
	DisplaysViews   bool           `json:"displays_views" jsonschema:"Whether it says it displays MCP Apps views"`
}

type diagnosePages struct {
	ViewPageReads      int    `json:"view_page_reads" jsonschema:"How many times the host read the page views draw in"`
	ViewPageReadAt     string `json:"view_page_read_at,omitempty" jsonschema:"When it last did, as an RFC 3339 time"`
	DiagnosePageReads  int    `json:"diagnose_page_reads" jsonschema:"How many times the host read the diagnose page"`
	DiagnosePageReadAt string `json:"diagnose_page_read_at,omitempty" jsonschema:"When it last did, as an RFC 3339 time"`
	CheckReads         int    `json:"check_reads" jsonschema:"How many of the page's resource reads reached the server"`
	CheckCalls         int    `json:"check_calls" jsonschema:"How many of the page's tool calls reached the server"`
}

// diagnoseNotCapturable is said rather than left out, so that a report does not
// read as if nothing could be wrong there.
var diagnoseNotCapturable = []string{
	"Whether the model sees a result's _meta or structuredContent: only the model's own requests show that.",
	"Where a client stores a result on disk.",
}

// The two halves of diagnose's description, the second for a server with views.
const (
	diagnoseServerWords = "Check how this server is set up: the build, the configuration the client started it with, the way to Bitbucket and " +
		"whether the credential works, what is exposed, whether confirmations can show, and the client's name, version and capabilities. " +
		"Call it only when the person asks, such as why a tool does not work. It never returns a credential, a pull request, a person or " +
		"Bitbucket's address, so its answer can be pasted into an issue."
	diagnosePageWords = " In a client that displays MCP Apps views it also draws a page for the person, which measures what a view can do " +
		"there and says what that means for bb's views. Where the client passes a view's calls to the server, the next call includes what the page measured."
)

func specDiagnose() Spec {
	tool := &mcp.Tool{
		Name:        "diagnose",
		Description: diagnoseServerWords + diagnosePageWords,
		Annotations: readOnly("Diagnose"),
		Meta:        diagnoseToolMeta(),
	}
	titled(tool)

	return Spec{
		Tool: tool,
		Asks: AsksNever,
		Register: func(server *mcp.Server, clients Clients) {
			mcp.AddTool(server, tool, diagnoseHandler(ServerOptions{Clients: clients, checks: &diagnoseLog{}}, true))
		},
		RegisterExposed: func(server *mcp.Server, opts ServerOptions, exposed map[string]bool) {
			// --exclude show turns views off, and this page is one: the tool still
			// reports what the server can see.
			if !exposed["show"] {
				bare := *tool
				bare.Description = diagnoseServerWords
				bare.Meta = nil
				mcp.AddTool(server, &bare, diagnoseHandler(opts, false))
				return
			}
			mcp.AddTool(server, tool, diagnoseHandler(opts, true))
		},
	}
}

// diagnoseToolMeta names the page a client renders diagnose's answer in, in the
// two forms hosts read, as show's does.
func diagnoseToolMeta() mcp.Meta {
	return mcp.Meta{
		"ui":             map[string]any{"resourceUri": diagnoseURI},
		"ui/resourceUri": diagnoseURI,
	}
}

func diagnoseHandler(opts ServerOptions, page bool) mcp.ToolHandlerFor[DiagnoseInput, DiagnoseOutput] {
	return func(ctx context.Context, req *mcp.CallToolRequest, in DiagnoseInput) (*mcp.CallToolResult, DiagnoseOutput, error) {
		kib, err := diagnoseMetaKiB(in.MetaKiB)
		if err != nil {
			return nil, DiagnoseOutput{}, err
		}
		out := diagnoseReport(ctx, req, opts)
		// Nothing is carried for a client that draws no page.
		draws := page && clientDisplaysViews(req)
		result := &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: diagnoseText(out, draws)}}}
		if draws {
			result.Meta = mcp.Meta{diagnosePayloadKey: diagnosePayload(kib)}
		}
		return result, out, nil
	}
}

func diagnoseMetaKiB(kib int) (int, error) {
	switch {
	case kib == 0:
		return diagnoseDefaultMetaKiB, nil
	case kib < 0 || kib > diagnoseMaxMetaKiB:
		return 0, fmt.Errorf("meta_kib must be between 1 and %d", diagnoseMaxMetaKiB)
	}
	return kib, nil
}

// diagnosePayload is what only the page reads: a build to compare its own with,
// and filler of the size being tried. The page's arrival marks that _meta came.
func diagnosePayload(kib int) map[string]any {
	return map[string]any{"build": diagnoseBuildID(), "meta_kib": kib, "filler": strings.Repeat("x", kib<<10)}
}

// diagnoseReport is what the server can see from here. It does not depend on the
// page, which may never be drawn.
func diagnoseReport(ctx context.Context, req *mcp.CallToolRequest, opts ServerOptions) DiagnoseOutput {
	ctx, cancel := context.WithTimeout(ctx, diagnoseBudget)
	defer cancel()

	pages, pageReport := opts.checks.snapshot()
	out := DiagnoseOutput{
		Build: diagnoseBuild{
			Version: opts.Version,
			Page:    diagnoseBuildID(),
			Go:      runtime.Version(),
			OS:      runtime.GOOS + "/" + runtime.GOARCH,
		},
		Connection:    diagnoseConnectionOf(ctx, opts.Clients),
		Exposure:      diagnoseExposureOf(opts),
		Confirmations: diagnoseConfirmationsOf(req, opts),
		Client:        diagnoseClientOf(req),
		Pages:         pages,
		PageReport:    pageReport,
		NotCapturable: diagnoseNotCapturable,
	}
	if opts.Configuration != nil {
		configuration := diagnoseConfigurationOf(opts.Configuration())
		out.Configuration = &configuration
	}
	out.Findings = diagnoseFindings(out)
	return out
}

func diagnoseConfigurationOf(diagnosis config.Diagnosis) diagnoseConfiguration {
	out := diagnoseConfiguration{Keyring: "not required"}
	for _, file := range diagnosis.Files {
		out.Files = append(out.Files, diagnoseFile{Tier: file.Tier, Exists: file.Exists, Valid: file.Valid()})
	}
	for _, setting := range diagnosis.Settings {
		out.Settings = append(out.Settings, diagnoseSetting{
			Name: setting.Name, Configured: setting.Configured, Source: setting.Source.Kind, Problem: setting.Problem != "",
		})
	}
	if diagnosis.Keyring.Required {
		out.Keyring = "unreachable"
		if diagnosis.Keyring.Reachable {
			out.Keyring = "reachable"
		}
	}
	return out
}

// diagnoseConnectionOf goes the way a tool goes to Bitbucket: its version, which
// needs no credential, and then who the credential says it is, which the report
// does not repeat.
func diagnoseConnectionOf(ctx context.Context, c Clients) diagnoseConnection {
	out := diagnoseConnection{Scheme: schemeOf(c.BaseURL), Credential: "unknown"}
	if c.HTTP == nil {
		out.Problem = "this server has no way to Bitbucket"
		return out
	}
	var properties struct {
		Version string `json:"version"`
	}
	if err := c.HTTP.GetJSON(ctx, "/rest/api/latest/application-properties", nil, &properties); err != nil {
		out.Problem = connectionProblem(err)
		// Bitbucket refuses a wrong credential on every request, even one that
		// needs none: that is an answer, not a server out of reach.
		if refusesCredential(err) {
			out.Reachable = true
			out.Credential = "refused"
		}
		return out
	}
	out.Reachable = true
	out.BitbucketVersion = properties.Version
	if _, err := c.HTTP.CurrentUserName(ctx); err != nil {
		out.Credential = "refused"
		out.Problem = connectionProblem(err)
		return out
	}
	out.Credential = "works"
	return out
}

// connectionProblem says what went wrong without the error's own words, which
// name the address, and in a dial error its IP.
func connectionProblem(err error) string {
	text := err.Error()
	for _, known := range []struct{ needle, says string }{
		{"x509:", "bb does not trust Bitbucket's certificate; set BB_CA_FILE"},
		{"tls:", "the TLS handshake with Bitbucket failed"},
		{"no such host", "Bitbucket's address does not resolve"},
		{"connection refused", "Bitbucket's address refused the connection"},
		{"deadline exceeded", "Bitbucket did not answer in time"},
		{"Client.Timeout", "Bitbucket did not answer in time"},
	} {
		if strings.Contains(text, known.needle) {
			return known.says
		}
	}
	if refusesCredential(err) {
		return "Bitbucket refused the credential"
	}
	return "the request to Bitbucket failed (" + string(apperrors.KindOf(err)) + ")"
}

func refusesCredential(err error) bool {
	return apperrors.IsKind(err, apperrors.KindAuthentication) || apperrors.IsKind(err, apperrors.KindAuthorization)
}

func diagnoseExposureOf(opts ServerOptions) diagnoseExposure {
	exposed := map[string]bool{}
	out := diagnoseExposure{
		ReadOnly:         opts.ReadOnly,
		ReadOnlyByPolicy: opts.ReadOnlyByPolicy,
		Tools:            opts.Allow,
		Exclude:          opts.Exclude,
		Scope:            "none",
		Audit:            opts.Audit != nil,
	}
	for _, spec := range exposedSpecs(opts) {
		exposed[spec.Tool.Name] = true
		out.Exposed = append(out.Exposed, spec.Tool.Name)
	}
	switch {
	case opts.Scope.RepoSlug != "":
		out.Scope = "repository"
	case opts.Scope.ProjectKey != "":
		out.Scope = "project"
	}
	if exposed["show"] {
		out.ViewKinds = offeredShowKinds(exposed)
	}
	return out
}

func diagnoseConfirmationsOf(req *mcp.CallToolRequest, opts ServerOptions) diagnoseConfirmations {
	out := diagnoseConfirmations{On: opts.ElicitationOptIn, ClientCanShow: req != nil && canConfirm(req)}
	if !opts.ElicitationOptIn {
		return out
	}
	for _, spec := range exposedSpecs(opts) {
		if spec.Asks != AsksNever {
			out.Tools = append(out.Tools, spec.Tool.Name)
		}
	}
	return out
}

func diagnoseClientOf(req *mcp.CallToolRequest) diagnoseClient {
	if req == nil {
		return diagnoseClient{}
	}
	out := diagnoseClient{ProtocolVersion: req.ProtocolVersion(), DisplaysViews: clientDisplaysViews(req)}
	if info := req.ClientInfo(); info != nil {
		out.Name, out.Version = info.Name, info.Version
	}
	capabilities := req.ClientCapabilities()
	if capabilities == nil {
		return out
	}
	// Through JSON, so a capability the SDK adds later is reported too. The SDK
	// writes roots whether or not the client declared them, so an empty one says
	// nothing, and goes.
	if encoded, err := json.Marshal(capabilities); err == nil {
		_ = json.Unmarshal(encoded, &out.Capabilities)
	}
	if roots, ok := out.Capabilities["roots"].(map[string]any); ok && len(roots) == 0 {
		delete(out.Capabilities, "roots")
	}
	return out
}

// diagnoseFindings says what the report means, and what to fix.
func diagnoseFindings(out DiagnoseOutput) []string {
	findings := []string{}
	if !out.Connection.Reachable {
		findings = append(findings, "Bitbucket cannot be reached: "+out.Connection.Problem+".")
	} else if out.Connection.Credential != "works" {
		findings = append(findings, "Bitbucket answers, and "+out.Connection.Problem+": check the credential the client gives bb in its env block.")
	}
	if out.Confirmations.On && !out.Confirmations.ClientCanShow {
		findings = append(findings, "Confirmations are on and this client cannot show them, so the tools that ask refuse their calls: "+strings.Join(out.Confirmations.Tools, ", ")+".")
	}
	if !out.Client.DisplaysViews {
		findings = append(findings, "This client says it displays no views, so show shows nothing, and the agent answers in words.")
	}
	if out.Configuration != nil {
		for _, file := range out.Configuration.Files {
			if file.Exists && !file.Valid {
				findings = append(findings, "The "+file.Tier+" configuration file is invalid: run bb doctor to see why.")
			}
		}
		if out.Configuration.Keyring == "unreachable" {
			findings = append(findings, "Policy requires the OS keyring, and it does not answer.")
		}
	}
	return findings
}

// diagnoseText is the report in words, for a client that gives the model the
// text. draws says the page is being drawn for the person.
func diagnoseText(out DiagnoseOutput, draws bool) string {
	var text strings.Builder
	fmt.Fprintf(&text, "bb %s, on %s.\n", out.Build.Version, out.Build.OS)
	switch {
	case !out.Connection.Reachable:
		fmt.Fprintf(&text, "Bitbucket, over %s: not reached: %s.\n", out.Connection.Scheme, out.Connection.Problem)
	default:
		fmt.Fprintf(&text, "Bitbucket %s, over %s: reached; the credential %s.\n", out.Connection.BitbucketVersion, out.Connection.Scheme, out.Connection.Credential)
	}
	access := "reads and writes"
	if out.Exposure.ReadOnly {
		access = "read-only"
	}
	fmt.Fprintf(&text, "This server: %s, %d tools, scope %s.\n", access, len(out.Exposure.Exposed), out.Exposure.Scope)
	fmt.Fprintf(&text, "The client: %s %s, MCP %s, displays views: %t.\n", out.Client.Name, out.Client.Version, out.Client.ProtocolVersion, out.Client.DisplaysViews)
	if len(out.Findings) > 0 {
		text.WriteString("\nWhat to fix or know:\n- " + strings.Join(out.Findings, "\n- ") + "\n")
	}
	switch {
	case out.PageReport != nil:
		text.WriteString("\nThe page's last report is under page_report.")
	case draws:
		text.WriteString("\nThe page shows the person what a view can do in this client, with a report to copy, and offers it as the view's context. Where the client passes a view's calls to the server, the next diagnose includes it.")
	}
	return text.String()
}

// DiagnoseCheckInput is what the diagnose page sends: a read-only call, a size
// to try, or its report.
type DiagnoseCheckInput struct {
	MetaKiB int            `json:"meta_kib,omitempty" jsonschema:"How many KiB of filler to answer with, to find the largest result that reaches the page"`
	Report  map[string]any `json:"report,omitempty" jsonschema:"What the page measured, for the next diagnose to include"`
}

// DiagnoseCheckOutput says what has reached the server of the page's checks.
type DiagnoseCheckOutput struct {
	Reads  int  `json:"reads" jsonschema:"How many of the page's resource reads have reached the server"`
	Calls  int  `json:"calls" jsonschema:"How many of the page's tool calls have reached the server"`
	Stored bool `json:"stored" jsonschema:"Whether the report was kept for the next diagnose"`
}

func specDiagnoseCheck() Spec {
	tool := &mcp.Tool{
		Name: "diagnose_check",
		Description: "Called by bb's diagnose page, not by the model: answers a read-only call, with filler of the size asked for, so the " +
			"page can tell what reaches it, and keeps the page's report for the next diagnose.",
		Annotations: readOnly("Diagnose check"),
		Meta:        map[string]any{"ui": map[string]any{"resourceUri": diagnoseURI, "visibility": []string{"app"}}},
	}
	titled(tool)

	return Spec{
		Tool: tool,
		Asks: AsksNever,
		// It goes with diagnose's page, whatever --tools names: it is part of
		// the page, not of what the model is offered.
		NeedsAll: []string{"diagnose", "show"},
		Register: func(server *mcp.Server, clients Clients) {
			mcp.AddTool(server, tool, diagnoseCheckHandler(ServerOptions{Clients: clients, checks: &diagnoseLog{}}))
		},
		RegisterExposed: func(server *mcp.Server, opts ServerOptions, _ map[string]bool) {
			mcp.AddTool(server, tool, diagnoseCheckHandler(opts))
		},
	}
}

func diagnoseCheckHandler(opts ServerOptions) mcp.ToolHandlerFor[DiagnoseCheckInput, DiagnoseCheckOutput] {
	return func(_ context.Context, _ *mcp.CallToolRequest, in DiagnoseCheckInput) (*mcp.CallToolResult, DiagnoseCheckOutput, error) {
		if in.MetaKiB < 0 || in.MetaKiB > diagnoseMaxMetaKiB {
			return nil, DiagnoseCheckOutput{}, fmt.Errorf("meta_kib must be between 1 and %d", diagnoseMaxMetaKiB)
		}
		report, err := diagnoseScrubbedReport(in.Report, opts.Clients.BaseURL)
		if err != nil {
			return nil, DiagnoseCheckOutput{}, err
		}
		out := opts.checks.called(report)
		result := &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("%d reads and %d calls of the check reached the server.", out.Reads, out.Calls)}}}
		if in.MetaKiB > 0 {
			result.Meta = mcp.Meta{diagnosePayloadKey: diagnosePayload(in.MetaKiB)}
		}
		return result, out, nil
	}
}

// diagnoseScrubbedReport bounds what a page leaves with the server, and takes
// Bitbucket's address out of it again: whatever a page sends, the next diagnose
// is pasted into an issue.
func diagnoseScrubbedReport(report map[string]any, baseURL string) (map[string]any, error) {
	if report == nil {
		return nil, nil
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		return nil, fmt.Errorf("the report cannot be read: %w", err)
	}
	if len(encoded) > diagnoseMaxReportBytes {
		return nil, fmt.Errorf("the report is %d bytes, more than the %d a server keeps", len(encoded), diagnoseMaxReportBytes)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(scrubAddress(string(encoded), baseURL)), &out); err != nil {
		return nil, fmt.Errorf("the report cannot be read: %w", err)
	}
	return out, nil
}

// scrubAddress takes Bitbucket's address out of JSON. It matches whole names, so a
// host called "git" leaves "digit" alone. It stops at a character that could go on
// in a name, and takes a second pass for an address that follows another with only
// one character between them.
func scrubAddress(text, baseURL string) string {
	for _, form := range diagnoseAddressForms(baseURL) {
		whole := regexp.MustCompile(`(^|[^A-Za-z0-9.-])` + regexp.QuoteMeta(form) + `($|[^A-Za-z0-9.-])`)
		for range 2 {
			text = whole.ReplaceAllString(text, "${1}"+diagnoseAddress+"${2}")
		}
	}
	return text
}

// diagnoseAddressForms are the ways Bitbucket's address is written, longest first.
func diagnoseAddressForms(baseURL string) []string {
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" {
		return nil
	}
	forms := []string{strings.TrimRight(baseURL, "/"), parsed.Scheme + "://" + parsed.Host, parsed.Host}
	if name := parsed.Hostname(); name != parsed.Host {
		forms = append(forms, name)
	}
	return forms
}

func schemeOf(baseURL string) string {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return ""
	}
	return parsed.Scheme
}

// originOf is the address's scheme and host, which a page's policy names.
func originOf(baseURL string) string {
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
}

// The page.

// diagnoseBuildID identifies the pages this bb serves. A host may keep a page for
// a whole session, so a page's build, against the one in a fresh answer, says
// whether what is drawing is this bb's.
var diagnoseBuildID = sync.OnceValue(func() string {
	sum := sha256.New()
	err := fs.WalkDir(viewAssets, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := viewAssets.ReadFile(path)
		if err != nil {
			return err
		}
		_, _ = sum.Write(data)
		return nil
	})
	if err != nil {
		panic(fmt.Sprintf("reading the view assets: %v", err))
	}
	return hex.EncodeToString(sum.Sum(nil))[:16]
})

// diagnosePage is the page for a server on this address, which it needs for its
// network checks. It carries no other Bitbucket data, and nothing it measures
// says the address.
func diagnosePage(baseURL string) string {
	settings, err := json.Marshal(map[string]any{
		"build":     diagnoseBuildID(),
		"bitbucket": strings.TrimRight(baseURL, "/"),
		"scheme":    schemeOf(baseURL),
		"forms":     diagnoseAddressForms(baseURL),
	})
	if err != nil {
		panic(fmt.Sprintf("encoding the diagnose page's settings: %v", err))
	}
	page := assemblePage(diagnoseScripts, mustReadViewAsset("view.css")+"\n"+diagnoseStyle)
	page = strings.Replace(page, "<title>Bitbucket view</title>", "<title>bb diagnose</title>", 1)
	if !strings.Contains(page, diagnoseConfigPlaceholder) {
		panic("views/diagnose.js has lost its settings placeholder")
	}
	return strings.Replace(page, diagnoseConfigPlaceholder, string(settings), 1)
}

// diagnoseResourceMeta is the page's own metadata. Alone among bb's pages it
// declares a domain, Bitbucket's, so a host that enforces its policy shows what it
// lets through; the page reads nothing from there. It declares the clipboard for
// the same reason.
func diagnoseResourceMeta(baseURL string) mcp.Meta {
	csp := map[string]any{}
	if origin := originOf(baseURL); origin != "" {
		csp["connectDomains"] = []string{origin}
		csp["resourceDomains"] = []string{origin}
	}
	return mcp.Meta{
		"ui": map[string]any{
			"csp":           csp,
			"permissions":   map[string]any{"clipboardWrite": map[string]any{}},
			"prefersBorder": true,
		},
	}
}

// addDiagnoseResources serves the page and the fixtures it reads.
func addDiagnoseResources(server *mcp.Server, opts ServerOptions) {
	meta := diagnoseResourceMeta(opts.Clients.BaseURL)
	server.AddResource(&mcp.Resource{
		URI:         diagnoseURI,
		Name:        "diagnose",
		Title:       "bb diagnose",
		Description: "The page the diagnose tool renders in. It measures what a view can do in this client.",
		MIMEType:    viewMIMEType,
		Meta:        meta,
	}, func(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		opts.checks.readPage()
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{
			URI:      req.Params.URI,
			MIMEType: viewMIMEType,
			Text:     diagnosePage(opts.Clients.BaseURL),
			Meta:     meta,
		}}}, nil
	})
	for _, fixture := range diagnoseFixtures {
		server.AddResource(&mcp.Resource{
			URI:      fixture.URI,
			Name:     "diagnose-fixture-" + fixture.URI[strings.LastIndex(fixture.URI, ".")+1:],
			MIMEType: fixture.MIMEType,
		}, func(context.Context, *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			opts.checks.readFixture()
			contents := fixture
			return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{&contents}}, nil
		})
	}
	server.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: diagnoseFixtureTemplateURI,
		Name:        "diagnose-fixture-template",
		MIMEType:    "text/plain",
	}, func(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		opts.checks.readFixture()
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{
			URI: req.Params.URI, MIMEType: "text/plain", Text: "A template bb's diagnose page reads.",
		}}}, nil
	})
}

// isDiagnoseResource reports whether a resource belongs to the diagnose page,
// which names the address of this server's Bitbucket.
func isDiagnoseResource(uri string) bool {
	return uri == diagnoseURI || strings.HasPrefix(uri, diagnoseURI+"/")
}
