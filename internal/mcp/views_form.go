package mcp

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/safederef"
	branchservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/branch"
	pullrequestservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequest"
	reviewerservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/reviewer"
)

// The pull request form (#686): the model drafts a pull request, and the
// person finishes it in a view and submits it. Nothing is created or changed
// until they do: the form is a view like any other, and submitting it calls
// create_pull_request or update_pull_request through the host, so the scope,
// the audit trail and the confirmation of a tool that asks apply.

// viewForm is a pull request for the person to finish: what the model
// drafted, or, for one that exists, what it says now with the model's draft
// over it.
type viewForm struct {
	// Mode is create for a new pull request, and edit for one that exists.
	Mode        string   `json:"mode"`
	FromRef     string   `json:"from_ref,omitempty"`
	ToRef       string   `json:"to_ref,omitempty"`
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description,omitempty"`
	Reviewers   []string `json:"reviewers,omitempty"`
	Draft       bool     `json:"draft,omitempty"`
	// Version is the pull request's as the form read it, which an edit
	// sends back so that it does not overwrite a change made since.
	Version int `json:"version,omitempty"`
	// DefaultBranch is the repository's, which a new pull request targets
	// unless the person picks another.
	DefaultBranch string `json:"default_branch,omitempty"`
	// RepositoryURL is the repository's page, under which a pull request the
	// form made is linked.
	RepositoryURL string `json:"repository_url,omitempty"`
}

// formForView reads what the form starts from: for a new pull request the
// repository's default branch, for one that exists the pull request itself.
func formForView(ctx context.Context, c Clients, in ShowInput) (viewForm, *viewPullRequest, viewSummary, error) {
	repository := fmt.Sprintf("%s/%s", in.Project, in.Repo)
	repositoryURL := fmt.Sprintf("%s/projects/%s/repos/%s", strings.TrimRight(c.BaseURL, "/"), url.PathEscape(in.Project), url.PathEscape(in.Repo))
	if in.ID == "" {
		form := viewForm{
			Mode:          "create",
			RepositoryURL: repositoryURL,
			FromRef:       strings.TrimSpace(in.FromRef),
			ToRef:         strings.TrimSpace(in.ToRef),
			Title:         in.Title,
			Description:   in.Description,
			Reviewers:     parseCommaList(in.Reviewers),
			Draft:         in.Draft,
		}
		// A repository whose default branch cannot be read leaves the target
		// to the person, rather than failing the form.
		if ref, err := branchservice.NewService(c.OpenAPI).GetDefault(ctx, branchservice.RepositoryRef{ProjectKey: in.Project, Slug: in.Repo}); err == nil {
			form.DefaultBranch = safederef.String(ref.DisplayId)
		}
		if form.ToRef == "" {
			form.ToRef = form.DefaultBranch
		}
		// Bitbucket's own create page fills in the default reviewers for the
		// branches, and create_pull_request adds only the reviewers it is
		// given, so the form fills them in, beside the model's, for the
		// person to keep or remove. A lookup that fails leaves them out.
		if form.FromRef != "" && form.ToRef != "" {
			defaults, err := reviewerservice.NewService(c.OpenAPI).ResolveDefaultReviewers(ctx, in.Project, in.Repo,
				reviewerservice.DefaultReviewerQuery{SourceRef: form.FromRef, TargetRef: form.ToRef})
			if err == nil {
				for _, name := range defaults {
					if !slices.Contains(form.Reviewers, name) {
						form.Reviewers = append(form.Reviewers, name)
					}
				}
			}
		}
		return form, nil, viewSummary{
			subject: "a new pull request in " + repository,
			form:    "a form they can edit and submit",
			state:   fmt.Sprintf("from %s into %s, titled %q. Nothing is created until they submit it.", form.FromRef, orUnset(form.ToRef), form.Title),
		}, nil
	}

	pr, err := pullrequestservice.NewService(c.HTTP).Get(ctx, pullrequestservice.RepositoryRef{ProjectKey: in.Project, Slug: in.Repo}, in.ID)
	if err != nil {
		return viewForm{}, nil, viewSummary{}, err
	}
	form := viewForm{
		Mode:          "edit",
		RepositoryURL: repositoryURL,
		FromRef:       pr.SourceBranch,
		ToRef:         pr.TargetBranch,
		Title:         pr.Title,
		Description:   pr.Description,
		Draft:         pr.Draft,
		Version:       pr.Version,
	}
	for _, reviewer := range pr.Reviewers {
		form.Reviewers = append(form.Reviewers, reviewer.Name)
	}
	// What the model drafted goes over what the pull request says now.
	if in.Title != "" {
		form.Title = in.Title
	}
	if in.Description != "" {
		form.Description = in.Description
	}
	if in.Draft {
		form.Draft = true
	}
	view := viewPullRequest{PullRequest: pr, URL: pullRequestURL(c.BaseURL, in.Project, in.Repo, in.ID)}
	return form, &view, viewSummary{
		subject: fmt.Sprintf("pull request %s#%s to edit", repository, in.ID),
		form:    "a form they can edit and save",
		state:   fmt.Sprintf("titled %q. Nothing changes until they save it.", form.Title),
	}, nil
}

func orUnset(value string) string {
	if value == "" {
		return "a branch they pick"
	}
	return value
}

// Suggestions for the form: the branches and the reviewers the person picks
// from as they type. The model has no tool for reviewers, so the form has a
// tool of its own, offered to views and not to the model.

// maxFormSuggestions is how many suggestions one answer carries.
const maxFormSuggestions = 20

// SuggestFormValuesInput is what the person typed into a field of the form.
type SuggestFormValuesInput struct {
	Project string `json:"project" jsonschema:"Bitbucket project key"`
	Repo    string `json:"repo" jsonschema:"Repository slug"`
	Field   string `json:"field" jsonschema:"branch or reviewer"`
	Text    string `json:"text,omitempty" jsonschema:"What the person has typed so far"`
}

// SuggestFormValuesOutput is what the field suggests.
type SuggestFormValuesOutput struct {
	Values []formValue `json:"values"`
}

// formValue is one suggestion: what the field takes, and what the person
// reads.
type formValue struct {
	Value string `json:"value"`
	Label string `json:"label,omitempty"`
}

func specSuggestFormValues() Spec {
	tool := &mcp.Tool{
		Name: "suggest_form_values",
		Description: "Called by bb's pull request form, not by the model: suggests the repository's branches, or the people " +
			"who can read it as reviewers, matching what the person typed.",
		Annotations: readOnly("Suggest form values"),
		InputSchema: enumInputSchema[SuggestFormValuesInput](map[string][]string{"field": {"branch", "reviewer"}}),
		Meta:        appOnlyToolMeta(),
	}
	spec := toolSpec(tool, func(c Clients) mcp.ToolHandlerFor[SuggestFormValuesInput, SuggestFormValuesOutput] {
		return func(ctx context.Context, _ *mcp.CallToolRequest, in SuggestFormValuesInput) (*mcp.CallToolResult, SuggestFormValuesOutput, error) {
			values, err := suggestFormValues(ctx, c, in)
			if err != nil {
				return nil, SuggestFormValuesOutput{}, fmt.Errorf("suggest_form_values failed: %w", err)
			}
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{
				Text: fmt.Sprintf("%d suggestions for the %s field.", len(values), in.Field),
			}}}, SuggestFormValuesOutput{Values: values}, nil
		}
	})
	// It goes with the pull request form, which is a kind of show.
	spec.Needs = []string{"show"}
	return spec
}

func suggestFormValues(ctx context.Context, c Clients, in SuggestFormValuesInput) ([]formValue, error) {
	text := strings.TrimSpace(in.Text)
	switch in.Field {
	case "branch":
		branches, err := branchservice.NewService(c.OpenAPI).List(ctx, branchservice.RepositoryRef{ProjectKey: in.Project, Slug: in.Repo},
			branchservice.ListOptions{FilterText: text, MaxResults: maxFormSuggestions})
		if err != nil {
			return nil, err
		}
		values := make([]formValue, 0, len(branches))
		for _, branch := range branches {
			if name := safederef.String(branch.DisplayId); name != "" {
				values = append(values, formValue{Value: name})
			}
		}
		return values, nil
	case "reviewer":
		// The people Bitbucket's own reviewer picker offers: those who can
		// read the repository. The person bb acts for is left out: an author
		// cannot review their own pull request.
		var page struct {
			Values []struct {
				Name        string `json:"name"`
				DisplayName string `json:"displayName"`
			} `json:"values"`
		}
		err := c.HTTP.GetJSON(ctx, "/rest/api/latest/users", map[string]string{
			"filter":                    text,
			"permission":                "REPO_READ",
			"permission.projectKey":     in.Project,
			"permission.repositorySlug": in.Repo,
			"limit":                     fmt.Sprint(maxFormSuggestions),
		}, &page)
		if err != nil {
			return nil, err
		}
		me := currentUsername(ctx, c)
		values := make([]formValue, 0, len(page.Values))
		for _, user := range page.Values {
			if user.Name == "" || strings.EqualFold(user.Name, me) {
				continue
			}
			values = append(values, formValue{Value: user.Name, Label: user.DisplayName})
		}
		return values, nil
	}
	return nil, fmt.Errorf("field must be branch or reviewer, not %q", in.Field)
}

// viewHelperTools are the tools only views call, beside refresh_view, which a
// view is offered when the server exposes them.
var viewHelperTools = []string{"suggest_form_values"}
