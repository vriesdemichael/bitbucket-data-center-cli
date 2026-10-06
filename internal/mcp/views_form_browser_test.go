//go:build views

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
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
	payload := viewPayload{Kind: showKindPullRequestForm, GeneratedAt: time.Now().UTC().Format(time.RFC3339), Form: &form, Show: show, Fingerprint: "form", Offers: offers,
		Avatars: map[string]string{"bob": onePixelPNG, "carol": onePixelPNG}}
	if form.Mode == "edit" {
		show.ID = "42"
		pr := viewPullRequest{PullRequest: fixturePullRequest(), URL: "https://bitbucket.example.com/projects/PAY/repos/ledger/pull-requests/42/overview"}
		payload.PullRequest = &pr
	}
	return payload
}

// newForm is a new pull request as show drafts it: the model named bob, and
// Bitbucket names carol as the default reviewer and erin as the code owner
// for these branches, whom the create page fills in too.
func newForm() viewForm {
	return viewForm{
		Mode: "create", FromRef: "feature/retries", ToRef: "master", Title: "Retry payments", Description: "Retries **twice**.",
		Reviewers: []string{"bob", "carol", "erin"}, DefaultReviewers: []string{"carol"}, CodeOwners: []string{"erin"},
		People: map[string]formPerson{
			"bob": {DisplayName: "Bob Chen"}, "carol": {DisplayName: "Carol Diaz"}, "erin": {DisplayName: "Erin Walsh"},
		},
		DefaultBranch: "master", RepositoryURL: "https://bitbucket.example.com/projects/PAY/repos/ledger",
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

// suggestions is suggest_form_values' answer with these values.
func suggestions(t *testing.T, values ...formValue) json.RawMessage {
	t.Helper()
	if values == nil {
		values = []formValue{}
	}
	return structuredAnswer(t, fmt.Sprintf("%d suggestions", len(values)), SuggestFormValuesOutput{Values: values})
}

// press sends a key to the element the selector finds, as typing does.
func press(t *testing.T, ctx context.Context, frame int, selector, key string) {
	t.Helper()
	var sent bool
	inFrame(t, ctx, frame, fmt.Sprintf(`const n = d.querySelector(%q); if (!n) return false; n.dispatchEvent(new KeyboardEvent("keydown", { key: %q, bubbles: true, cancelable: true })); return true;`, selector, key), &sent)
	if !sent {
		t.Fatalf("frame %d has no %s to press %s in", frame, selector, key)
	}
}

// click clicks the element the selector finds.
func click(t *testing.T, ctx context.Context, frame int, selector string) {
	t.Helper()
	var clicked bool
	inFrame(t, ctx, frame, fmt.Sprintf(`const n = d.querySelector(%q); if (n) n.click(); return Boolean(n);`, selector), &clicked)
	if !clicked {
		t.Fatalf("frame %d has no %s to click", frame, selector)
	}
}

// chipNames are the reviewers the form holds, by username, in order.
func chipNames(t *testing.T, ctx context.Context, frame int) string {
	t.Helper()
	var names string
	inFrame(t, ctx, frame, `return [...d.querySelectorAll(".reviewer-chip")].map((c) => c.dataset.reviewer).join(",");`, &names)
	return names
}

// The person finishes what the model drafted and creates it: the form sends
// what they wrote through create_pull_request, with the reviewers the page
// filled in and the one they found by name, and the view then shows the pull
// request, and tells the model the person made it.
func TestAFormCreatesThePullRequestThePersonFinished(t *testing.T) {
	created := refreshCardPayload("Retry payments with a cap", "OPEN", time.Now(), "card")
	created.Offers = creating
	ctx := browser(t, []viewhost.Frame{
		{Title: "form", Mode: "inline", Fullscreen: true, Result: fixtureResult(t, formPayload(newForm(), creating)),
			ToolResults: map[string][]json.RawMessage{
				"suggest_form_values": {suggestions(t, formValue{Value: "dave", Label: "Dave Okafor", Avatar: onePixelPNG})},
				"create_pull_request": {toolAnswer(t, "Only one pull request may be open for a given source and target branch.", true),
					structuredAnswer(t, "created", map[string]any{"pull_request": map[string]any{"id": 43}})},
				"refresh_view": {refreshAnswer(t, &created, "The person opened pull request PAY/ledger#43 in a view: open.")},
			}},
	})

	// The reviewers the page fills in are chips, each with its avatar or
	// initials and the name Bitbucket shows.
	var chips string
	inFrame(t, ctx, 0, `return [...d.querySelectorAll(".reviewer-chip")].map((c) => c.querySelector(".reviewer-name").textContent + ":" + Boolean(c.querySelector(".avatar img, .avatar .initials"))).join(",");`, &chips)
	if chips != "Bob Chen:true,Carol Diaz:true,Erin Walsh:true" {
		t.Errorf("the form starts with the reviewer chips %q, want each name with an avatar", chips)
	}

	typeInto(t, ctx, 0, `[data-draft="form.title"]`, "Retry payments with a cap")
	typeInto(t, ctx, 0, `[data-draft="form.search"]`, "dav")
	waitInFrame(t, ctx, 0, `d.querySelector("#form-reviewer-list [role=option].active img")`, "the person found is not offered with their avatar")
	press(t, ctx, 0, `[data-draft="form.search"]`, "Enter")
	waitInFrame(t, ctx, 0, `[...d.querySelectorAll(".reviewer-chip")].map((c) => c.dataset.reviewer).join(",") === "bob,carol,erin,dave"`, "the reviewer found was not added")
	search := toolCalls(t, ctx, 0, "suggest_form_values")
	if len(search) != 1 || search[0]["field"] != "reviewer" || search[0]["text"] != "dav" || search[0]["repo"] != "ledger" {
		t.Errorf("the form searched with %v, want the reviewer typed in PAY/ledger", search)
	}

	// The first answer refuses: the reason shows, and what the person wrote stays.
	clickButton(t, ctx, 0, "Create")
	waitInFrame(t, ctx, 0, `d.querySelector(".pr-form .form-error") && d.querySelector(".pr-form .form-error").textContent.includes("Only one pull request")`, "a refused pull request does not say why")
	var kept string
	inFrame(t, ctx, 0, `return d.querySelector('[data-draft="form.title"]').value + "|" + [...d.querySelectorAll(".reviewer-chip")].map((c) => c.dataset.reviewer).join(",");`, &kept)
	if kept != "Retry payments with a cap|bob,carol,erin,dave" {
		t.Fatalf("after a refusal the form holds %q, want what the person made of it", kept)
	}

	clickButton(t, ctx, 0, "Create")
	waitInFrame(t, ctx, 0, `d.querySelector(".pr-card") && !d.querySelector(".back-button")`, "the view does not show the pull request it made")
	calls := toolCalls(t, ctx, 0, "create_pull_request")
	if len(calls) != 2 {
		t.Fatalf("the form called create_pull_request %d times, want twice: %v", len(calls), calls)
	}
	for key, want := range map[string]any{
		"project": "PAY", "repo": "ledger", "from_ref": "feature/retries", "to_ref": "master",
		"title": "Retry payments with a cap", "description": "Retries **twice**.", "reviewers": "bob,carol,erin,dave", "draft": false,
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

// Create as draft opens it as a draft, as the create page's button does; the
// form has no draft box to tick.
func TestAFormCreatesADraftFromItsOwnButton(t *testing.T) {
	ctx := browser(t, []viewhost.Frame{
		{Title: "form", Mode: "inline", Fullscreen: true, Result: fixtureResult(t, formPayload(newForm(), creating)),
			ToolResults: map[string][]json.RawMessage{
				"create_pull_request": {structuredAnswer(t, "created", map[string]any{"pull_request": map[string]any{"id": 44}})},
				"refresh_view":        {toolAnswer(t, "not now", true)},
			}},
	})

	var boxes int
	inFrame(t, ctx, 0, `return d.querySelectorAll('.pr-form input[type="checkbox"]').length;`, &boxes)
	if boxes != 0 {
		t.Errorf("a new pull request's form has %d checkboxes, want the create page's two buttons instead", boxes)
	}
	clickButton(t, ctx, 0, "Create as draft")
	waitInFrame(t, ctx, 0, `window.parent.bbHost.frames[0].messages.some((m) => m.method === "tools/call" && m.params.name === "create_pull_request")`, "Create as draft sent nothing")
	calls := toolCalls(t, ctx, 0, "create_pull_request")
	if len(calls) != 1 || calls[0]["draft"] != true {
		t.Errorf("Create as draft sent %v, want draft = true", calls)
	}
}

// The branches are picked from the repository's, as the create page's
// pickers pick them: a list to search and move through with the keyboard,
// and nothing that is not in it. Picking one asks for its default reviewers
// and code owners, and adds them, as continuing with other branches does on
// the page.
func TestABranchIsPickedFromTheRepositorysBranches(t *testing.T) {
	ctx := browser(t, []viewhost.Frame{
		{Title: "form", Mode: "inline", Fullscreen: true, Result: fixtureResult(t, formPayload(newForm(), creating)),
			ToolResults: map[string][]json.RawMessage{
				"suggest_form_values": {
					suggestions(t, formValue{Value: "master"}, formValue{Value: "feature/retries"}, formValue{Value: "feature/idempotency-key"}),
					suggestions(t, formValue{Value: "bob", Label: "Bob Chen"}, formValue{Value: "frank", Label: "Frank Ito"}),
					suggestions(t, formValue{Value: "dave", Label: "Dave Okafor", Avatar: onePixelPNG}),
					suggestions(t),
				},
			}},
	})

	var trigger map[string]any
	inFrame(t, ctx, 0, `const b = d.querySelector('[data-picker="from"] .picker-button'); return { role: b.getAttribute("role"), popup: b.getAttribute("aria-haspopup"), expanded: b.getAttribute("aria-expanded"), label: b.getAttribute("aria-label"), text: b.textContent, inputs: d.querySelectorAll('input[list], datalist').length };`, &trigger)
	if trigger["role"] != "combobox" || trigger["popup"] != "listbox" || trigger["expanded"] != "false" || trigger["label"] != "Source branch" ||
		trigger["text"] != "feature/retries" || trigger["inputs"] != float64(0) {
		t.Fatalf("the source branch is %v, want a closed picker labelled as the create page's, showing the branch, and no free-text input", trigger)
	}

	press(t, ctx, 0, `[data-picker="from"] .picker-button`, "ArrowDown")
	waitInFrame(t, ctx, 0, `d.querySelectorAll("#form-branch-list [role=option]").length === 3`, "the picker does not list the repository's branches")
	var list map[string]any
	inFrame(t, ctx, 0, `const s = d.querySelector(".picker-search"); const current = d.querySelector("#form-branch-list .current"); return { focused: d.activeElement === s, placeholder: s.placeholder, active: s.getAttribute("aria-activedescendant"), expanded: d.querySelector('[data-picker="from"] .picker-button').getAttribute("aria-expanded"), current: current && current.textContent };`, &list)
	if list["focused"] != true || list["placeholder"] != "Enter a branch name" || list["active"] != "form-branch-list-0" || list["expanded"] != "true" || list["current"] != "feature/retries" {
		t.Errorf("the open picker is %v, want its search focused with Bitbucket's placeholder, the first branch active and the current one marked", list)
	}
	calls := toolCalls(t, ctx, 0, "suggest_form_values")
	if len(calls) != 1 || calls[0]["field"] != "branch" || calls[0]["text"] != "" {
		t.Errorf("opening the picker asked %v, want the repository's branches", calls)
	}

	// Enter picks the branch the arrows are on.
	press(t, ctx, 0, ".picker-search", "ArrowDown")
	press(t, ctx, 0, ".picker-search", "ArrowDown")
	waitInFrame(t, ctx, 0, `d.querySelector(".picker-search").getAttribute("aria-activedescendant") === "form-branch-list-2"`, "the arrows do not move through the branches")
	press(t, ctx, 0, ".picker-search", "Enter")
	waitInFrame(t, ctx, 0, `!d.querySelector(".picker-popup") && d.querySelector('[data-picker="from"] .picker-button').textContent === "feature/idempotency-key"`, "Enter did not pick the branch")

	// The branches' default reviewers and code owners join the reviewers.
	waitInFrame(t, ctx, 0, `[...d.querySelectorAll(".reviewer-chip")].map((c) => c.dataset.reviewer).join(",") === "bob,carol,erin,frank,dave"`, "the new branches' default reviewers and code owners were not added")
	calls = toolCalls(t, ctx, 0, "suggest_form_values")
	if len(calls) != 3 || calls[1]["field"] != "default_reviewers" || calls[2]["field"] != "code_owners" ||
		calls[1]["from_ref"] != "feature/idempotency-key" || calls[1]["to_ref"] != "master" || calls[2]["from_ref"] != "feature/idempotency-key" {
		t.Errorf("picking a branch asked %v, want the default reviewers and code owners from it into master", calls)
	}
	var focused bool
	inFrame(t, ctx, 0, `return d.activeElement === d.querySelector('[data-picker="from"] .picker-button');`, &focused)
	if !focused {
		t.Error("after a pick the caret is not back on the picker")
	}

	// Only a branch in the list is picked: what was typed is not one.
	click(t, ctx, 0, `[data-picker="to"] .picker-button`)
	typeInto(t, ctx, 0, ".picker-search", "no-such-branch")
	waitInFrame(t, ctx, 0, `d.querySelector("#form-branch-list .picker-note") && d.querySelector("#form-branch-list .picker-note").textContent === "No branches found"`, "a search that matches nothing does not say so")
	press(t, ctx, 0, ".picker-search", "Enter")
	var target string
	inFrame(t, ctx, 0, `return d.querySelector('[data-picker="to"] .picker-button').textContent;`, &target)
	if target != "master" {
		t.Errorf("Enter on a search that matches nothing made the destination %q, want master still", target)
	}
	press(t, ctx, 0, ".picker-search", "Escape")
	waitInFrame(t, ctx, 0, `!d.querySelector(".picker-popup") && d.activeElement === d.querySelector('[data-picker="to"] .picker-button')`, "Escape does not close the picker and return to it")

	// A click anywhere else closes it too.
	click(t, ctx, 0, `[data-picker="to"] .picker-button`)
	waitInFrame(t, ctx, 0, `d.querySelector(".picker-popup")`, "the destination picker does not open")
	inFrame(t, ctx, 0, `d.querySelector("#form-title").dispatchEvent(new MouseEvent("mousedown", { bubbles: true })); return true;`, &focused)
	waitInFrame(t, ctx, 0, `!d.querySelector(".picker-popup")`, "a click outside does not close the picker")

	// So does focus that moves on, as a Tab moves it, and the focus stays
	// where it went.
	click(t, ctx, 0, `[data-picker="to"] .picker-button`)
	waitInFrame(t, ctx, 0, `d.activeElement === d.querySelector(".picker-search")`, "the open picker does not take the focus")
	moveFocus(t, ctx, 0, ".picker-search", "#form-title")
	waitInFrame(t, ctx, 0, `!d.querySelector(".picker-popup") && d.activeElement === d.querySelector("#form-title")`, "focus that moved on did not close the picker, or was lost")
}

// moveFocus moves the focus from one element to another as a Tab does. A
// page in a headless browser has no focus of its own, so a focus change fires
// no focus events there; the event a Tab fires is sent by hand.
func moveFocus(t *testing.T, ctx context.Context, frame int, from, to string) {
	t.Helper()
	var moved bool
	inFrame(t, ctx, frame, fmt.Sprintf(`const a = d.querySelector(%q), b = d.querySelector(%q); if (!a || !b) return false; a.focus(); b.focus(); a.dispatchEvent(new FocusEvent("focusout", { bubbles: true, relatedTarget: b })); return true;`, from, to), &moved)
	if !moved {
		t.Fatalf("frame %d has no %s or %s to move the focus between", frame, from, to)
	}
}

// The swap button exchanges the source and the destination, with Bitbucket's
// label and tooltip, and asks for the reviewers of the branches the other way
// round; the form sends them so.
func TestSwapExchangesTheBranches(t *testing.T) {
	ctx := browser(t, []viewhost.Frame{
		{Title: "form", Mode: "inline", Fullscreen: true, Result: fixtureResult(t, formPayload(newForm(), creating)),
			ToolResults: map[string][]json.RawMessage{
				"suggest_form_values": {suggestions(t)},
				"create_pull_request": {structuredAnswer(t, "created", map[string]any{"pull_request": map[string]any{"id": 45}})},
				"refresh_view":        {toolAnswer(t, "not now", true)},
			}},
	})

	var swap string
	inFrame(t, ctx, 0, `const b = d.querySelector(".swap-button"); return b.title + "|" + b.textContent;`, &swap)
	if swap != "Swap source and destination|Swap" {
		t.Errorf("the swap button reads %q, want Bitbucket's tooltip and label", swap)
	}
	clickButton(t, ctx, 0, "Swap")
	waitInFrame(t, ctx, 0, `d.querySelector('[data-picker="from"] .picker-button').textContent === "master" && d.querySelector('[data-picker="to"] .picker-button').textContent === "feature/retries"`, "the swap did not exchange the branches")
	waitInFrame(t, ctx, 0, `window.parent.bbHost.frames[0].messages.filter((m) => m.method === "tools/call" && m.params.name === "suggest_form_values").length === 2 && !d.querySelector(".form-actions button[disabled]")`, "the swap did not ask for the reviewers")
	calls := toolCalls(t, ctx, 0, "suggest_form_values")
	if calls[0]["from_ref"] != "master" || calls[0]["to_ref"] != "feature/retries" {
		t.Errorf("after the swap the form asked %v, want the branches the other way round", calls)
	}
	// Nobody is named for the branches this way round, and nobody is taken
	// away: a person already chosen stays, as on the create page.
	if names := chipNames(t, ctx, 0); names != "bob,carol,erin" {
		t.Errorf("after the swap the reviewers are %q, want them kept", names)
	}

	clickButton(t, ctx, 0, "Create")
	waitInFrame(t, ctx, 0, `window.parent.bbHost.frames[0].messages.some((m) => m.method === "tools/call" && m.params.name === "create_pull_request")`, "Create sent nothing")
	if created := toolCalls(t, ctx, 0, "create_pull_request"); created[0]["from_ref"] != "master" || created[0]["to_ref"] != "feature/retries" {
		t.Errorf("after the swap the form sent %v, want from master into feature/retries", created[0])
	}
}

// The quick-add buttons offer back the default reviewers and code owners the
// person removed, as the create page's do: each only while it has someone to
// add, saying how many, and adding them.
func TestQuickAddOffersBackWhomBitbucketNames(t *testing.T) {
	form := newForm()
	form.DefaultReviewers = []string{"carol", "bob"}
	ctx := browser(t, []viewhost.Frame{
		{Title: "form", Mode: "inline", Fullscreen: true, Result: fixtureResult(t, formPayload(form, creating)),
			ToolResults: map[string][]json.RawMessage{"suggest_form_values": {suggestions(t)}}},
	})

	quickAdd := func() string {
		t.Helper()
		var buttons string
		inFrame(t, ctx, 0, `return [...d.querySelectorAll(".quick-add button")].map((b) => b.textContent + " (" + b.title + ")").join(", ");`, &buttons)
		return buttons
	}
	if buttons := quickAdd(); buttons != "" {
		t.Errorf("with every default reviewer and code owner chosen the form offers %q, want nothing", buttons)
	}
	click(t, ctx, 0, `[data-reviewer="carol"] .chip-remove`)
	click(t, ctx, 0, `[data-reviewer="bob"] .chip-remove`)
	click(t, ctx, 0, `[data-reviewer="erin"] .chip-remove`)
	if buttons := quickAdd(); buttons != "Default reviewers (2 reviewers), Code owners (1 code owner)" {
		t.Errorf("with two default reviewers and a code owner removed the form offers %q", buttons)
	}
	clickButton(t, ctx, 0, "Default reviewers")
	if names, buttons := chipNames(t, ctx, 0), quickAdd(); names != "carol,bob" || buttons != "Code owners (1 code owner)" {
		t.Errorf("Default reviewers left reviewers %q and offers %q, want carol and bob back and the code owner still offered", names, buttons)
	}
	clickButton(t, ctx, 0, "Code owners")
	if names, buttons := chipNames(t, ctx, 0), quickAdd(); names != "carol,bob,erin" || buttons != "" {
		t.Errorf("Code owners left reviewers %q and offers %q, want erin back and nothing more to offer", names, buttons)
	}
}

// The form checks what it needs before it sends anything, and a reviewer is
// found by name, never typed in.
func TestAFormChecksBeforeSendingAndTakesNoTypedReviewer(t *testing.T) {
	form := newForm()
	form.Title = ""
	ctx := browser(t, []viewhost.Frame{
		{Title: "form", Mode: "inline", Fullscreen: true, Result: fixtureResult(t, formPayload(form, creating)),
			ToolResults: map[string][]json.RawMessage{"suggest_form_values": {suggestions(t)}}},
	})

	typeInto(t, ctx, 0, `[data-draft="form.search"]`, "nobody")
	waitInFrame(t, ctx, 0, `d.querySelector("#form-reviewer-list .picker-note") && d.querySelector("#form-reviewer-list .picker-note").textContent === "No matches found"`, "a search that finds nobody does not say so")
	press(t, ctx, 0, `[data-draft="form.search"]`, "Enter")
	press(t, ctx, 0, `[data-draft="form.search"]`, ",")
	if names := chipNames(t, ctx, 0); names != "bob,carol,erin" {
		t.Errorf("what was typed made the reviewers %q, want them unchanged", names)
	}
	// The list closes once the focus moves on.
	moveFocus(t, ctx, 0, `[data-draft="form.search"]`, "#form-title")
	waitInFrame(t, ctx, 0, `!d.querySelector("#form-reviewer-list") && d.activeElement === d.querySelector("#form-title")`, "focus that moved on did not close the reviewer list, or was lost")

	clickButton(t, ctx, 0, "Create")
	waitInFrame(t, ctx, 0, `d.querySelector(".pr-form .form-error") && d.querySelector(".pr-form .form-error").textContent === "You must supply a title for this pull request."`, "a form without a title does not say so in Bitbucket's words")
	if calls := toolCalls(t, ctx, 0, "create_pull_request"); len(calls) != 0 {
		t.Errorf("a form without a title was sent: %v", calls)
	}
}

// A pull request that exists is saved through update_pull_request, with the
// version the form read, and a draft flag only when the person changed it.
// Its reviewers show as chips it does not change.
func TestAFormSavesAnExistingPullRequest(t *testing.T) {
	form := viewForm{Mode: "edit", FromRef: "fix/rounding", ToRef: "master", Title: "Fix rounding", Description: "Old.", Version: 7,
		Reviewers: []string{"bob", "carol"}, People: map[string]formPerson{"bob": {DisplayName: "Bob Chen"}, "carol": {DisplayName: "Carol Diaz"}},
		RepositoryURL: "https://bitbucket.example.com/projects/PAY/repos/ledger"}
	saved := refreshCardPayload("Fix rounding in refunds", "OPEN", time.Now(), "card")
	saved.Offers = creating
	ctx := browser(t, []viewhost.Frame{
		{Title: "edit form", Mode: "inline", Fullscreen: true, Result: fixtureResult(t, formPayload(form, creating)),
			ToolResults: map[string][]json.RawMessage{
				"update_pull_request": {structuredAnswer(t, "saved", map[string]any{"pull_request": map[string]any{"id": 42}})},
				"refresh_view":        {refreshAnswer(t, &saved, "opened")},
			}},
	})

	var edit map[string]any
	inFrame(t, ctx, 0, `return { pickers: d.querySelectorAll(".picker-button, .swap-button").length, removable: d.querySelectorAll(".chip-remove").length, chips: [...d.querySelectorAll(".reviewer-chip .reviewer-name")].map((n) => n.textContent).join(","), heading: d.querySelector("h1").textContent };`, &edit)
	if edit["pickers"] != float64(0) || edit["removable"] != float64(0) || edit["chips"] != "Bob Chen,Carol Diaz" || edit["heading"] != "Edit Pull Request" {
		t.Errorf("the edit form is %v, want Bitbucket's heading, the reviewers as chips it does not change, and no branch pickers", edit)
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
// offers nothing that would not work: no button to send, no branch to pick
// and no one to search for.
func TestAFormInAHostWithoutToolCallsSaysSo(t *testing.T) {
	ctx := browser(t, []viewhost.Frame{
		{Title: "form", Mode: "inline", Fullscreen: true, Result: fixtureResult(t, formPayload(newForm(), creating))},
	})
	if err := chromedp.Do(ctx, chromedp.Sleep(150*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	text := frameText(t, ctx, 0)
	var title string
	inFrame(t, ctx, 0, `const i = d.querySelector('[data-draft="form.title"]'); return i ? i.value : "";`, &title)
	if title != "Retry payments" || !strings.Contains(text, "cannot submit") {
		t.Fatalf("the form has title %q and reads %q, want the draft and that it cannot be submitted here", title, text)
	}
	var offered map[string]any
	inFrame(t, ctx, 0, `return { create: [...d.querySelectorAll("button")].some((b) => b.textContent === "Create" || b.textContent === "Create as draft"), pickers: [...d.querySelectorAll(".picker-button, .swap-button")].every((b) => b.disabled), search: d.querySelector(".reviewer-search").disabled };`, &offered)
	if offered["create"] != false || offered["pickers"] != true || offered["search"] != true {
		t.Errorf("a host that passes no tool calls is offered %v, want no create button, and the pickers and the search disabled", offered)
	}
}
