//go:build views

package mcp

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/mcp/viewhost"
)

// creating is what a server that lets views create pull requests offers them.
var creating = &viewOffers{
	Kinds: []string{showKindPullRequest, showKindPullRequestForm},
	Tools: []string{"create_pull_request", "update_pull_request", "suggest_form_values"},
}

func formPayload(form viewForm, offers *viewOffers) viewPayload {
	show := &ShowInput{Kind: showKindPullRequestForm, Project: "PAY", Repo: "ledger", FromRef: form.FromRef}
	payload := viewPayload{Kind: showKindPullRequestForm, GeneratedAt: time.Now().UTC().Format(time.RFC3339), Form: &form, Show: show, Fingerprint: "form", Offers: offers}
	if form.Mode == "edit" {
		show.ID = "42"
		pr := viewPullRequest{PullRequest: fixturePullRequest(), URL: "https://bitbucket.example.com/projects/PAY/repos/ledger/pull-requests/42/overview"}
		payload.PullRequest = &pr
	}
	return payload
}

func newForm() viewForm {
	return viewForm{
		Mode: "create", FromRef: "feature/retries", ToRef: "master", Title: "Retry payments", Description: "Retries **twice**.",
		Reviewers: []string{"bob"}, DefaultBranch: "master", RepositoryURL: "https://bitbucket.example.com/projects/PAY/repos/ledger",
	}
}

// structuredAnswer is a tool's result with structured content, as the host
// passes it back.
func structuredAnswer(t *testing.T, text string, content any) json.RawMessage {
	t.Helper()
	encoded, err := json.Marshal(&mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}, StructuredContent: content})
	if err != nil {
		t.Fatalf("encode the answer: %v", err)
	}
	return encoded
}

// The person finishes what the model drafted and creates it: the form sends
// what they wrote through create_pull_request, and the view then shows the
// pull request, and tells the model the person made it.
func TestAFormCreatesThePullRequestThePersonFinished(t *testing.T) {
	created := refreshCardPayload("Retry payments with a cap", "OPEN", time.Now(), "card")
	created.Offers = creating
	ctx := browser(t, []viewhost.Frame{
		{Title: "form", Mode: "inline", Fullscreen: true, Result: fixtureResult(t, formPayload(newForm(), creating)),
			ToolResults: map[string][]json.RawMessage{
				"suggest_form_values": {structuredAnswer(t, "2 suggestions", map[string]any{"values": []any{map[string]any{"value": "carol", "label": "Carol Diaz"}}})},
				"create_pull_request": {toolAnswer(t, "Only one pull request may be open for a given source and target branch.", true),
					structuredAnswer(t, "created", map[string]any{"pull_request": map[string]any{"id": 43}})},
				"refresh_view": {refreshAnswer(t, &created, "The person opened pull request PAY/ledger#43 in a view: open.")},
			}},
	})

	typeInto(t, ctx, 0, `[data-draft="form.title"]`, "Retry payments with a cap")
	typeInto(t, ctx, 0, `[data-draft="form.reviewer"]`, "carol")
	var done bool
	inFrame(t, ctx, 0, `const i = d.querySelector('[data-draft="form.reviewer"]'); i.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true })); return true;`, &done)
	waitInFrame(t, ctx, 0, `[...d.querySelectorAll(".reviewer-chip")].map((c) => c.firstChild.textContent).join(",") === "bob,carol"`, "the reviewer typed in was not added")

	// The first answer refuses: the reason shows, and what the person wrote stays.
	clickButton(t, ctx, 0, "Create pull request")
	waitInFrame(t, ctx, 0, `d.querySelector(".pr-form .form-error") && d.querySelector(".pr-form .form-error").textContent.includes("Only one pull request")`, "a refused pull request does not say why")
	var kept string
	inFrame(t, ctx, 0, `return d.querySelector('[data-draft="form.title"]').value;`, &kept)
	if kept != "Retry payments with a cap" {
		t.Fatalf("after a refusal the title reads %q, want what the person wrote", kept)
	}

	clickButton(t, ctx, 0, "Create pull request")
	waitInFrame(t, ctx, 0, `d.querySelector(".pr-card") && !d.querySelector(".back-button")`, "the view does not show the pull request it made")
	calls := toolCalls(t, ctx, 0, "create_pull_request")
	if len(calls) != 2 {
		t.Fatalf("the form called create_pull_request %d times, want twice: %v", len(calls), calls)
	}
	for key, want := range map[string]any{
		"project": "PAY", "repo": "ledger", "from_ref": "feature/retries", "to_ref": "master",
		"title": "Retry payments with a cap", "description": "Retries **twice**.", "reviewers": "bob,carol", "draft": false,
	} {
		if calls[1][key] != want {
			t.Errorf("the form sent %s = %v, want %v (all: %v)", key, calls[1][key], want, calls[1])
		}
	}
	if opened := refreshCalls(t, ctx, 0); len(opened) != 1 || opened[0]["kind"] != "pull_request" || opened[0]["id"] != "43" {
		t.Errorf("after creating it the view opened %v, want PAY/ledger#43", opened)
	}
	told := hostMessages(t, ctx, 0, "ui/update-model-context")
	if len(told) != 1 || !strings.HasPrefix(messageText(told[0]), "The person created pull request PAY/ledger#43 with the form you showed them.") {
		t.Errorf("the model was told %v, want that the person created it", told)
	}
}

// The form suggests branches and reviewers as the person types, through
// suggest_form_values, and checks what it needs before it sends anything.
func TestAFormSuggestsAndChecksBeforeSending(t *testing.T) {
	form := newForm()
	form.Title = ""
	ctx := browser(t, []viewhost.Frame{
		{Title: "form", Mode: "inline", Fullscreen: true, Result: fixtureResult(t, formPayload(form, creating)),
			ToolResults: map[string][]json.RawMessage{
				"suggest_form_values": {structuredAnswer(t, "suggestions", map[string]any{"values": []any{map[string]any{"value": "feature/retries-v2"}}})},
			}},
	})

	typeInto(t, ctx, 0, `[data-draft="form.from"]`, "feature/ret")
	waitInFrame(t, ctx, 0, `[...d.querySelectorAll("#form-branches option")].some((o) => o.value === "feature/retries-v2")`, "the branch typed is not suggested")
	calls := toolCalls(t, ctx, 0, "suggest_form_values")
	if len(calls) == 0 || calls[len(calls)-1]["field"] != "branch" || calls[len(calls)-1]["text"] != "feature/ret" || calls[len(calls)-1]["repo"] != "ledger" {
		t.Errorf("the form asked for suggestions with %v, want the branch typed in PAY/ledger", calls)
	}

	clickButton(t, ctx, 0, "Create pull request")
	waitInFrame(t, ctx, 0, `d.querySelector(".pr-form .form-error") && d.querySelector(".pr-form .form-error").textContent.includes("needs a title")`, "a form without a title does not say so")
	if calls := toolCalls(t, ctx, 0, "create_pull_request"); len(calls) != 0 {
		t.Errorf("a form without a title was sent: %v", calls)
	}
}

// A pull request that exists is saved through update_pull_request, with the
// version the form read, and a draft flag only when the person changed it.
func TestAFormSavesAnExistingPullRequest(t *testing.T) {
	form := viewForm{Mode: "edit", FromRef: "fix/rounding", ToRef: "master", Title: "Fix rounding", Description: "Old.", Version: 7,
		Reviewers: []string{"bob", "carol"}, RepositoryURL: "https://bitbucket.example.com/projects/PAY/repos/ledger"}
	saved := refreshCardPayload("Fix rounding in refunds", "OPEN", time.Now(), "card")
	saved.Offers = creating
	ctx := browser(t, []viewhost.Frame{
		{Title: "edit form", Mode: "inline", Fullscreen: true, Result: fixtureResult(t, formPayload(form, creating)),
			ToolResults: map[string][]json.RawMessage{
				"update_pull_request": {structuredAnswer(t, "saved", map[string]any{"pull_request": map[string]any{"id": 42}})},
				"refresh_view":        {refreshAnswer(t, &saved, "opened")},
			}},
	})

	var hasBranchInputs bool
	inFrame(t, ctx, 0, `return Boolean(d.querySelector('[data-draft="form.from"]'));`, &hasBranchInputs)
	if hasBranchInputs {
		t.Error("an existing pull request's branches are offered for editing")
	}
	typeInto(t, ctx, 0, `[data-draft="form.title"]`, "Fix rounding in refunds")
	clickButton(t, ctx, 0, "Save")
	waitInFrame(t, ctx, 0, `d.querySelector(".pr-card")`, "the view does not show the pull request once saved")
	calls := toolCalls(t, ctx, 0, "update_pull_request")
	if len(calls) != 1 {
		t.Fatalf("the form called update_pull_request %d times, want once: %v", len(calls), calls)
	}
	for key, want := range map[string]any{"project": "PAY", "repo": "ledger", "pr_id": "42", "version": float64(7), "title": "Fix rounding in refunds", "description": "Old."} {
		if calls[0][key] != want {
			t.Errorf("the save sent %s = %v, want %v (all: %v)", key, calls[0][key], want, calls[0])
		}
	}
	if _, sent := calls[0]["draft"]; sent {
		t.Errorf("the save sent the draft flag the person did not change: %v", calls[0])
	}
}

// Where the host passes no tool calls the form says it cannot submit, and
// offers nothing that would not work.
func TestAFormInAHostWithoutToolCallsSaysSo(t *testing.T) {
	ctx := browser(t, []viewhost.Frame{
		{Title: "form", Mode: "inline", Fullscreen: true, Result: fixtureResult(t, formPayload(newForm(), creating))},
	})
	if err := chromedp.Run(ctx, chromedp.Sleep(150*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	text := frameText(t, ctx, 0)
	var title string
	inFrame(t, ctx, 0, `const i = d.querySelector('[data-draft="form.title"]'); return i ? i.value : "";`, &title)
	if title != "Retry payments" || !strings.Contains(text, "cannot submit") {
		t.Fatalf("the form has title %q and reads %q, want the draft and that it cannot be submitted here", title, text)
	}
	var button bool
	inFrame(t, ctx, 0, `return [...d.querySelectorAll("button")].some((b) => b.textContent.includes("Create pull request"));`, &button)
	if button {
		t.Error("a host that passes no tool calls is offered a button that cannot work")
	}
}
