//go:build views

package mcp

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/mcp/viewhost"
)

// The diagnose page, drawn by a real browser in the stand-in host, with each thing
// a host may do less of taken away, holding what the report says.

// diagnoseHostAddress is a Bitbucket nothing listens on, so a network check fails
// for want of a server, and is named nowhere in what the page reports.
const diagnoseHostAddress = "http://127.0.0.1:9"

// diagnoseResult is the tool's result as a client receives it, in the parts asked
// for: a host may drop any of them on the way to the page.
func diagnoseResult(t *testing.T, content, structured, meta bool, build string) json.RawMessage {
	t.Helper()
	result := map[string]any{}
	if content {
		result["content"] = []map[string]any{{"type": "text", "text": "bb 1.2.3"}}
	}
	if structured {
		result["structuredContent"] = map[string]any{"build": map[string]any{"page": build}}
	}
	if meta {
		result["_meta"] = map[string]any{diagnosePayloadKey: map[string]any{"build": build, "meta_kib": 1024, "filler": "x"}}
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

// diagnoseAnswers answer the page's own calls as a server does, each with a
// _meta that arrives.
func diagnoseAnswers(t *testing.T) map[string][]json.RawMessage {
	t.Helper()
	encoded, err := json.Marshal(&mcp.CallToolResult{
		Content:           []mcp.Content{&mcp.TextContent{Text: "1 reads and 1 calls"}},
		StructuredContent: DiagnoseCheckOutput{Reads: 1, Calls: 1},
		Meta:              mcp.Meta{diagnosePayloadKey: map[string]any{"build": diagnoseBuildID(), "meta_kib": 1, "filler": "x"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return map[string][]json.RawMessage{"diagnose_check": {encoded}}
}

func diagnoseResources(t *testing.T) map[string]json.RawMessage {
	t.Helper()
	read := func(mimeType string, field string) json.RawMessage {
		encoded, err := json.Marshal(map[string]any{"contents": []map[string]any{{"mimeType": mimeType, field: "x"}}})
		if err != nil {
			t.Fatal(err)
		}
		return encoded
	}
	return map[string]json.RawMessage{
		diagnoseCopyURI:               read(viewMIMEType, "text"),
		diagnoseURI + "/fixture.png":  read("image/png", "blob"),
		diagnoseURI + "/fixture.txt":  read("text/plain", "text"),
		diagnoseURI + "/fixture.pdf":  read("application/pdf", "blob"),
		diagnoseURI + "/template/any": read("text/plain", "text"),
	}
}

// openDiagnose draws the page in these hosts, and waits until each has run the
// checks that need no press.
func openDiagnose(t *testing.T, frames []viewhost.Frame) context.Context {
	t.Helper()
	page, err := viewhost.Page(diagnosePage(diagnoseHostAddress), frames, viewhost.Options{SameOrigin: true})
	if err != nil {
		t.Fatalf("render the host page: %v", err)
	}
	return openHostPage(t, page, nil, diagnoseReady(len(frames)))
}

// diagnoseReady holds once every frame's page has finished the checks that need
// no press.
func diagnoseReady(count int) string {
	return `window.bbHost && window.bbHost.frames.length === ` + strconv.Itoa(count) + ` && window.bbHost.frames.every((f) => {
		const d = f.iframe.contentDocument;
		return d && d.querySelector(".diagnose") && !d.querySelector('[role="status"]');
	})`
}

// pageReport is the report a frame shows as text to select.
func pageReport(t *testing.T, ctx context.Context, frame int) map[string]any {
	t.Helper()
	text := pageReportText(t, ctx, frame)
	var report map[string]any
	if err := json.Unmarshal([]byte(text), &report); err != nil {
		t.Fatalf("frame %d: the report is not JSON: %v\n%s", frame, err, text)
	}
	return report
}

func pageReportText(t *testing.T, ctx context.Context, frame int) string {
	t.Helper()
	var text string
	inFrame(t, ctx, frame, `return d.querySelector(".report-text").value;`, &text)
	return text
}

func effectsOf(t *testing.T, ctx context.Context, frame int) string {
	t.Helper()
	report := pageReport(t, ctx, frame)
	effects, _ := report["effects"].([]any)
	parts := make([]string, 0, len(effects))
	for _, effect := range effects {
		parts = append(parts, effect.(string))
	}
	return strings.Join(parts, "\n")
}

func fieldOf(report map[string]any, path ...string) any {
	var at any = report
	for _, key := range path {
		next, ok := at.(map[string]any)
		if !ok {
			return nil
		}
		at = next[key]
	}
	return at
}

// A host that does all of it: the page reports each answer, runs nothing that
// needs a press, sends the report where it can go, and says what cannot be found out.
func TestDiagnoseReportsAHostThatDoesEverything(t *testing.T) {
	ctx := openDiagnose(t, []viewhost.Frame{{
		Title: "all of it", Mode: "inline", Fullscreen: true,
		Result:          diagnoseResult(t, true, true, true, diagnoseBuildID()),
		ToolResults:     diagnoseAnswers(t),
		ResourceResults: diagnoseResources(t),
		Sandbox:         json.RawMessage(`{"csp":{"connectDomains":["http://127.0.0.1:9"]}}`),
	}})
	report := pageReport(t, ctx, 0)

	for _, check := range []struct {
		name string
		got  any
		want any
	}{
		{"host", fieldOf(report, "initialize", "host_info", "name"), "bb-view-host"},
		{"protocol", fieldOf(report, "initialize", "protocol_version"), "2026-01-26"},
		{"content", fieldOf(report, "tool_result", "content"), true},
		{"structured content", fieldOf(report, "tool_result", "structured_content"), true},
		{"meta", fieldOf(report, "tool_result", "meta"), true},
		{"older", fieldOf(report, "page", "older_than_server"), false},
		{"model call", fieldOf(report, "meta_size", "model_call_kib"), float64(1024)},
		{"largest of a view's calls", fieldOf(report, "meta_size", "view_call_largest_kib"), float64(16384)},
		{"page read", fieldOf(report, "resources_read", "page", "has_text"), true},
		{"image read", fieldOf(report, "resources_read", "image", "type"), "image/png"},
		{"pdf read", fieldOf(report, "resources_read", "pdf", "type"), "application/pdf"},
		{"text read", fieldOf(report, "resources_read", "text", "type"), "text/plain"},
		{"template read", fieldOf(report, "resources_read", "template", "answered"), true},
		{"tool call", fieldOf(report, "tools_call", "reached_server"), true},
		{"data image", fieldOf(report, "data_image"), true},
		{"context", fieldOf(report, "model_context", "answered"), true},
	} {
		if check.got != check.want {
			t.Errorf("%s = %v, want %v", check.name, check.got, check.want)
		}
	}
	if keys, _ := fieldOf(report, "initialize", "host_context_keys").([]any); len(keys) == 0 {
		t.Error("the report does not name the host context's keys")
	}
	if modes, _ := fieldOf(report, "initialize", "available_display_modes").([]any); len(modes) != 2 {
		t.Errorf("display modes = %v", modes)
	}
	if fieldOf(report, "frame", "origin") == nil {
		t.Error("the report does not say the frame's origin")
	}
	if fieldOf(report, "size", "measured") != true || fieldOf(report, "size", "follows_growth") != true || fieldOf(report, "size", "follows_shrinking") != true {
		t.Errorf("size = %v", fieldOf(report, "size"))
	}
	if declared := fieldOf(report, "declared_only", "logging"); declared != false {
		t.Errorf("logging is reported as %v, want what was declared", declared)
	}
	// With the views' style beside it, which hides an inline view's scrollbar, this
	// page still has to see whether one stays.
	var scrollbar string
	inFrame(t, ctx, 0, `return w.getComputedStyle(d.documentElement).scrollbarWidth;`, &scrollbar)
	if scrollbar != "auto" {
		t.Errorf("the page's scrollbar-width is %q, so it cannot see one stay", scrollbar)
	}
	if reports := hostMessages(t, ctx, 0, "ui/notifications/size-changed"); len(reports) < 3 {
		t.Errorf("the page reported its size %d times, want it to keep the host told", len(reports))
	}
	if notes, _ := report["not_capturable"].([]any); len(notes) != 2 {
		t.Errorf("not capturable = %v", notes)
	}

	// Nothing that needs a press ran without one.
	for _, method := range []string{"ui/open-link", "ui/request-display-mode", "ui/message"} {
		if sent := hostMessages(t, ctx, 0, method); len(sent) != 0 {
			t.Errorf("the page sent %s with no press: %v", method, sent)
		}
	}

	// The report leaves by every route: the context holds it, whole, and the next
	// diagnose can read it where the host passes the call.
	waitInFrame(t, ctx, 0, `window.parent.bbHost.frames[0].messages.some((m) => m.method === "tools/call" && m.params.arguments && m.params.arguments.report)`, "the report did not go to the server")
	told := hostMessages(t, ctx, 0, "ui/update-model-context")
	if len(told) < 2 || !strings.Contains(messageText(told[len(told)-1]), `"initialize"`) {
		t.Errorf("the model context holds %v, want the whole report", told)
	}
	if text := frameText(t, ctx, 0); !strings.Contains(text, "What this cannot find out") || !strings.Contains(text, "only the model's own requests show that") {
		t.Errorf("the page does not say what it cannot find out:\n%s", text)
	}
}

// The report states the effect on bb, and each capability taken away shows.
func TestDiagnoseSaysWhatEachLimitMeansForViews(t *testing.T) {
	everything := diagnoseResult(t, true, true, true, diagnoseBuildID())
	const (
		noCalls = iota
		noResources
		noContext
		noMessage
		noLinks
		refusesCalls
		droppedMeta
		droppedNoCalls
		droppedOwnCallsToo
		onlyStructured
		olderPage
		capsMeta
		saysNoViews
		listsOnlyInline
	)
	dropped := diagnoseResult(t, true, true, false, diagnoseBuildID())
	checkWithoutMeta, err := json.Marshal(&mcp.CallToolResult{
		Content:           []mcp.Content{&mcp.TextContent{Text: "1 reads and 1 calls"}},
		StructuredContent: DiagnoseCheckOutput{Reads: 1, Calls: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := openDiagnose(t, []viewhost.Frame{
		noCalls:        {Title: "no serverTools", Mode: "inline", Result: everything, ResourceResults: diagnoseResources(t)},
		noResources:    {Title: "no serverResources", Mode: "inline", Result: everything, ToolResults: diagnoseAnswers(t)},
		noContext:      {Title: "no updateModelContext", Mode: "inline", Result: everything, ToolResults: diagnoseAnswers(t), Without: []string{"updateModelContext"}},
		noMessage:      {Title: "no message", Mode: "inline", Result: everything, ToolResults: diagnoseAnswers(t), Without: []string{"message"}},
		noLinks:        {Title: "no openLinks", Mode: "inline", Result: everything, ToolResults: diagnoseAnswers(t), OpensNoLinks: true},
		refusesCalls:   {Title: "declares serverTools, refuses them", Mode: "inline", Result: everything, ToolResults: diagnoseAnswers(t), Refuses: []string{"tools/call"}},
		droppedMeta:    {Title: "_meta dropped", Mode: "inline", Result: dropped, ToolResults: diagnoseAnswers(t)},
		droppedNoCalls: {Title: "_meta dropped, no serverTools", Mode: "inline", Result: dropped, ResourceResults: diagnoseResources(t)},
		droppedOwnCallsToo: {Title: "_meta dropped from a view's own calls too", Mode: "inline", Result: dropped,
			ToolResults: map[string][]json.RawMessage{"diagnose_check": {checkWithoutMeta}}},
		onlyStructured: {Title: "text dropped", Mode: "inline", Result: diagnoseResult(t, false, true, false, diagnoseBuildID()),
			ToolResults: diagnoseAnswers(t)},
		olderPage: {Title: "a page older than bb", Mode: "inline", Result: diagnoseResult(t, true, true, true, "a-later-build"), ToolResults: diagnoseAnswers(t)},
		capsMeta: {Title: "caps _meta below what was sent", Mode: "inline", ToolResults: diagnoseAnswers(t),
			Result: json.RawMessage(`{"content":[{"type":"text","text":"bb"}],"structuredContent":{"meta_kib_sent":1024,"client":{"displays_views":true}}}`)},
		saysNoViews: {Title: "draws the page, told the server it displays no views", Mode: "inline", ToolResults: diagnoseAnswers(t),
			Result: json.RawMessage(`{"content":[{"type":"text","text":"bb"}],"structuredContent":{"meta_kib_sent":0,"client":{"displays_views":false}}}`)},
		listsOnlyInline: {Title: "lists inline alone", Mode: "inline", Result: everything, ToolResults: diagnoseAnswers(t)},
	})

	for frame, want := range map[int]string{
		noCalls:            "views do not stay current, their tabs and actions open Bitbucket instead, and a pull request form cannot submit",
		noContext:          "views offer no Add to chat context",
		noMessage:          "views offer no Ask about this",
		noLinks:            "a link in a view is shown to copy",
		refusesCalls:       "the check's call failed",
		droppedMeta:        "views cannot draw from show's answer alone here. A view reads it with a call of its own, which kept its _meta here up to 16384 KiB.",
		droppedNoCalls:     "views cannot draw from show's answer alone here. A view cannot read it with a call of its own here",
		droppedOwnCallsToo: "views cannot draw from show's answer alone here. A view's own call lost a _meta of 64 KiB too",
		olderPage:          "Start a new conversation",
		capsMeta:           "A result's _meta of 1024 KiB did not arrive",
		saysNoViews:        "told the server it displays no views",
	} {
		effects := effectsOf(t, ctx, frame)
		if !strings.Contains(effects, want) {
			t.Errorf("frame %d does not say %q:\n%s", frame, want, effects)
		}
		if text := frameText(t, ctx, frame); !strings.Contains(text, want) {
			t.Errorf("frame %d's page does not show %q:\n%s", frame, want, text)
		}
	}
	// A host that declares it takes a context is told that much, and no more.
	if effects := effectsOf(t, ctx, noMessage); !strings.Contains(effects, "Whether the model reads it is not known") {
		t.Errorf("a host that takes a context reads:\n%s", effects)
	}

	// A host that lists inline alone answers inline to a press, and refuses nothing.
	click(t, ctx, listsOnlyInline, `[data-press="fullscreen"]`)
	waitInFrame(t, ctx, listsOnlyInline, `JSON.parse(d.querySelector(".report-text").value).presses.fullscreen`, "the press did not report")
	if effects := effectsOf(t, ctx, listsOnlyInline); strings.Contains(effects, "refused it") {
		t.Errorf("a host that lists inline alone is told it refused fullscreen:\n%s", effects)
	}

	// Each answer is the answer it got, and the page goes on without it.
	report := pageReport(t, ctx, noCalls)
	if fieldOf(report, "tools_call", "answered") != false {
		t.Errorf("no serverTools: tools_call = %v", fieldOf(report, "tools_call"))
	}
	if hostCalls := hostMessages(t, ctx, noCalls, "tools/call"); len(hostCalls) != 1 {
		t.Errorf("no serverTools: the page called %d times, want one try and no report: %v", len(hostCalls), hostCalls)
	}
	report = pageReport(t, ctx, noResources)
	if fieldOf(report, "resources_read", "image", "answered") != false || fieldOf(report, "tools_call", "reached_server") != true {
		t.Errorf("no serverResources: %v, %v", fieldOf(report, "resources_read"), fieldOf(report, "tools_call"))
	}
	report = pageReport(t, ctx, droppedMeta)
	if fieldOf(report, "tool_result", "meta") != false || fieldOf(report, "page", "older_than_server") != false {
		t.Errorf("_meta dropped: %v, %v; want it missing, and the page's age taken from the rest", fieldOf(report, "tool_result"), fieldOf(report, "page"))
	}
	report = pageReport(t, ctx, onlyStructured)
	if fieldOf(report, "tool_result", "content") != false || fieldOf(report, "tool_result", "structured_content") != true {
		t.Errorf("text dropped: %v", fieldOf(report, "tool_result"))
	}
	report = pageReport(t, ctx, refusesCalls)
	if fieldOf(report, "tools_call", "answered") != false || fieldOf(report, "meta_size", "view_call_largest_kib") != nil {
		t.Errorf("refused calls: %v, %v", fieldOf(report, "tools_call"), fieldOf(report, "meta_size"))
	}
}

// A result that never comes does not stop the checks, and says so.
func TestDiagnoseGoesOnWithoutAResult(t *testing.T) {
	waitless := heldWaits(8000)
	frames := []viewhost.Frame{{Title: "no result", Mode: "inline", ToolResults: diagnoseAnswers(t)}}
	page, err := viewhost.Page(diagnosePage(diagnoseHostAddress), frames, viewhost.Options{SameOrigin: true})
	if err != nil {
		t.Fatal(err)
	}
	ctx := openHostPage(t, page, []chromedp.Action[chromedp.Void]{waitless}, `window.bbHost && window.bbHost.frames.length === 1 && window.bbHost.frames[0].iframe.contentDocument.querySelector(".diagnose") !== null`)
	if err := chromedp.Do(ctx, chromedp.Poll[chromedp.Void](`window.bbHost.frames[0].iframe.contentWindow.bbWaits && window.bbHost.frames[0].iframe.contentWindow.bbWaits[8000]`, chromedp.WithPollingTimeout(30*time.Second))); err != nil {
		t.Fatalf("the page did not wait for its result: %v", err)
	}
	if err := chromedp.Do(ctx, chromedp.Evaluate[chromedp.Void](`window.bbHost.frames[0].iframe.contentWindow.bbWaits[8000]()`)); err != nil {
		t.Fatal(err)
	}
	if _, err := chromedp.Run(ctx, chromedp.Poll[chromedp.Void](diagnoseReady(1), chromedp.WithPollingTimeout(30*time.Second))); err != nil {
		t.Fatalf("the checks did not run without a result: %v", err)
	}
	report := pageReport(t, ctx, 0)
	if fieldOf(report, "tool_result", "arrived") != false || fieldOf(report, "tools_call", "reached_server") != true {
		t.Errorf("no result: %v, %v", fieldOf(report, "tool_result"), fieldOf(report, "tools_call"))
	}
}

// The network checks say what got through and what a policy stopped, of an address
// the page declared and one it did not, and say neither address in the report.
func TestDiagnoseSeesWhatAPolicyStops(t *testing.T) {
	everything := diagnoseResult(t, true, true, true, diagnoseBuildID())
	policy := "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data: http://127.0.0.1:9; connect-src http://127.0.0.1:9"
	ctx := openDiagnose(t, []viewhost.Frame{
		{Title: "enforces the declared policy", Mode: "inline", Result: everything, ToolResults: diagnoseAnswers(t), Policy: policy,
			Sandbox: json.RawMessage(`{"csp":{"connectDomains":["http://127.0.0.1:9"]}}`)},
		{Title: "enforces no policy", Mode: "inline", Result: everything, ToolResults: diagnoseAnswers(t)},
	})

	report := pageReport(t, ctx, 0)
	if violation := fieldOf(report, "network", "unlisted_fetch", "policy_violation"); violation != "connect-src" {
		t.Errorf("an unlisted fetch under a policy reports %v, want connect-src", violation)
	}
	if violation := fieldOf(report, "network", "unlisted_image", "policy_violation"); violation != "img-src" {
		t.Errorf("an unlisted image under a policy reports %v, want img-src", violation)
	}
	if fieldOf(report, "network", "bitbucket_fetch", "policy_violation") != nil {
		t.Errorf("the declared address was stopped by policy: %v", fieldOf(report, "network", "bitbucket_fetch"))
	}
	blocked, _ := fieldOf(report, "sandbox_csp", "blocked_by_policy").([]any)
	if len(blocked) != 2 {
		t.Errorf("blocked by policy = %v, want both unlisted checks", blocked)
	}
	if fieldOf(report, "sandbox_csp", "host_says_approved") == nil {
		t.Errorf("sandbox_csp = %v, want what the host says it approved", fieldOf(report, "sandbox_csp"))
	}

	open := pageReport(t, ctx, 1)
	if fieldOf(open, "network", "unlisted_fetch", "policy_violation") != nil || fieldOf(open, "network", "unlisted_fetch", "got_through") != false {
		t.Errorf("an address that cannot resolve, with no policy: %v", fieldOf(open, "network", "unlisted_fetch"))
	}

	// A host's own words about the address, as a browser or a host would put it
	// in order, are scrubbed in the report and on the page.
	sandboxed := openDiagnose(t, []viewhost.Frame{{Title: "echoes the address", Mode: "inline", Result: everything, ToolResults: diagnoseAnswers(t),
		Sandbox: json.RawMessage(`{"csp":{"connectDomains":["HTTP://127.0.0.1:9","127.0.0.1"]}}`)}})
	for name, text := range map[string]string{"report": pageReportText(t, sandboxed, 0), "page": frameText(t, sandboxed, 0)} {
		if strings.Contains(text, "127.0.0.1") {
			t.Errorf("the %s names the address:\n%s", name, text)
		}
	}

	// The address goes nowhere it could be pasted from.
	for frame := range 2 {
		for _, name := range []string{"127.0.0.1", "bb-view-host:"} {
			if text := pageReportText(t, ctx, frame); strings.Contains(text, name) {
				t.Errorf("frame %d's report names %q:\n%s", frame, name, text)
			}
		}
		for _, sent := range hostMessages(t, ctx, frame, "ui/update-model-context") {
			if strings.Contains(messageText(sent), "127.0.0.1") {
				t.Errorf("frame %d told the model the address: %s", frame, messageText(sent))
			}
		}
	}
}

// One press each, and each answer goes into the report; where only the person can
// tell, the page asks them.
func TestDiagnoseOnePressEach(t *testing.T) {
	everything := diagnoseResult(t, true, true, true, diagnoseBuildID())
	ctx := openDiagnose(t, []viewhost.Frame{
		{Title: "grants", Mode: "inline", Fullscreen: true, Result: everything, ToolResults: diagnoseAnswers(t)},
		{Title: "lists fullscreen and refuses", Mode: "inline", RefusesFullscreen: true, Result: everything, ToolResults: diagnoseAnswers(t)},
		{Title: "refuses links and messages", Mode: "inline", RefuseLinks: true, Refuses: []string{"ui/message"}, Result: everything, ToolResults: diagnoseAnswers(t)},
	})
	pressNamed := func(frame int, name string) {
		t.Helper()
		click(t, ctx, frame, `[data-press="`+name+`"]`)
		waitInFrame(t, ctx, frame, `d.querySelector('[data-press="`+name+`"]') && !d.querySelector('[data-press="`+name+`"]').disabled && JSON.parse(d.querySelector(".report-text").value).presses.`+name, name+" did not report")
	}

	pressNamed(0, "link_own_scheme")
	pressNamed(0, "link_other_scheme")
	opened := hostMessages(t, ctx, 0, "ui/open-link")
	if len(opened) != 2 || messageText(opened[0]) != "http://example.com/?bb-diagnose" || messageText(opened[1]) != "https://example.com/?bb-diagnose" {
		t.Errorf("the links opened = %v; want http first, as this Bitbucket is, then https", opened)
	}
	pressNamed(0, "message")
	sent := hostMessages(t, ctx, 0, "ui/message")
	if len(sent) != 1 || !strings.Contains(messageText(sent[0]), "This is a test from bb's diagnose page") {
		t.Errorf("the message = %v, want it labelled as a test", sent)
	}
	click(t, ctx, 0, `[data-said="message-yes"]`)
	waitInFrame(t, ctx, 0, `JSON.parse(d.querySelector(".report-text").value).presses.message.person_says === true`, "what the person said did not go into the report")
	pressNamed(0, "clipboard")
	if answer := fieldOf(pageReport(t, ctx, 0), "presses", "clipboard"); answer == nil {
		t.Error("the clipboard press reports nothing")
	}
	pressNamed(0, "window_open")
	click(t, ctx, 0, `[data-said="window_open-no"]`)
	waitInFrame(t, ctx, 0, `JSON.parse(d.querySelector(".report-text").value).presses.window_open.person_says === false`, "what the person said about window.open did not go into the report")

	// A refused fullscreen says what that means; a granted one, and the host's own
	// changes, are logged as they happen.
	pressNamed(1, "fullscreen")
	if effects := effectsOf(t, ctx, 1); !strings.Contains(effects, "lists fullscreen and refused it") {
		t.Errorf("a refused fullscreen reads:\n%s", effects)
	}
	if mode := fieldOf(pageReport(t, ctx, 1), "presses", "fullscreen", "mode"); mode != "inline" {
		t.Errorf("the refused fullscreen's answer = %v", mode)
	}
	pressNamed(0, "fullscreen")
	if mode := fieldOf(pageReport(t, ctx, 0), "presses", "fullscreen", "mode"); mode != "fullscreen" {
		t.Errorf("the granted fullscreen's answer = %v", mode)
	}
	pressNamed(0, "inline")
	if err := chromedp.Do(ctx, chromedp.Evaluate[chromedp.Void](`window.bbHost.frames[1].setMode("fullscreen", { containerDimensions: { width: 900, height: 700 } })`)); err != nil {
		t.Fatal(err)
	}
	waitInFrame(t, ctx, 1, `JSON.parse(d.querySelector(".report-text").value).display_mode_changes.some((c) => c.display_mode === "fullscreen" && c.container_dimensions.width === 900)`, "the host's own fullscreen was not logged")

	// What a host refuses is reported, not lost.
	pressNamed(2, "link_own_scheme")
	pressNamed(2, "message")
	report := pageReport(t, ctx, 2)
	if fieldOf(report, "presses", "link_own_scheme", "answered") != false || fieldOf(report, "presses", "message", "answered") != false {
		t.Errorf("refused presses read %v", fieldOf(report, "presses"))
	}

	// The report still leaves by text where the clipboard is refused.
	click(t, ctx, 2, `[data-copy="report"]`)
	waitInFrame(t, ctx, 2, `d.body.innerText.includes("Selected: press Ctrl+C.") || d.body.innerText.includes("Copied.")`, "Copy said nothing")
	var left map[string]any
	inFrame(t, ctx, 2, `const box = d.querySelector(".report-text"); return { selected: d.body.innerText.includes("Selected: press Ctrl+C."), length: box.selectionEnd - box.selectionStart, whole: box.value.length };`, &left)
	if left["selected"] == true && left["length"] != left["whole"] {
		t.Errorf("Copy said it selected the report and left %v of it selected", left)
	}
}
