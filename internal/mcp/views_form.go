package mcp

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/safederef"
	branchservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/branch"
	codeownersservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/codeowners"
	pullrequestservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequest"
	reviewerservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/reviewer"
)

// The pull request form (#686): the model drafts a pull request, and the
// person finishes it in a view and submits it. Nothing is created or changed
// until they do: the form is a view like any other, and submitting it calls
// create_pull_request or update_pull_request through the host, so the scope,
// the audit trail and the confirmation of a tool that asks apply.
//
// It works as Bitbucket's own create page does. The branches are picked from
// the repository's, never typed. The reviewers start as the page fills them
// in: whoever the model named, then the default reviewers and the code owners
// for the branches, less the author. One the person removes is offered back
// by the page's quick-add buttons, and picking other branches adds the
// default reviewers and code owners for those, as continuing with other
// branches does on the page.

// viewForm is a pull request for the person to finish: what the model
// drafted, or, for one that exists, what it says now with the model's draft
// over it.
type viewForm struct {
	// Mode is create for a new pull request, and edit for one that exists.
	Mode        string `json:"mode"`
	FromRef     string `json:"from_ref,omitempty"`
	ToRef       string `json:"to_ref,omitempty"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	// Reviewers are the usernames the form starts with: for a new pull
	// request the model's, then the default reviewers and code owners.
	Reviewers []string `json:"reviewers,omitempty"`
	Draft     bool     `json:"draft,omitempty"`
	// Version is the pull request's as the form read it, which an edit
	// sends back so that it does not overwrite a change made since.
	Version int `json:"version,omitempty"`
	// DefaultBranch is the repository's, which a new pull request targets
	// unless the person picks another.
	DefaultBranch string `json:"default_branch,omitempty"`
	// RepositoryURL is the repository's page, under which a pull request the
	// form made is linked.
	RepositoryURL string `json:"repository_url,omitempty"`
	// DefaultReviewers and CodeOwners are the usernames Bitbucket names for
	// the branches, less the author, which the form offers to add back once
	// the person removes one. Unread says Bitbucket could not be asked, so
	// the form says so rather than suggest there are none.
	DefaultReviewers       []string `json:"default_reviewers,omitempty"`
	CodeOwners             []string `json:"code_owners,omitempty"`
	DefaultReviewersUnread bool     `json:"default_reviewers_unread,omitempty"`
	CodeOwnersUnread       bool     `json:"code_owners_unread,omitempty"`
	// People are the reviewers' names as Bitbucket shows them, by username;
	// their avatars are in the payload's.
	People map[string]formPerson `json:"people,omitempty"`
}

// formPerson is how the form names a person.
type formPerson struct {
	DisplayName string `json:"display_name,omitempty"`
}

// formForView reads what the form starts from: for a new pull request the
// repository's default branch and the reviewers Bitbucket's create page
// fills in, for one that exists the pull request itself. It returns the
// people whose avatars the form draws, by username, with the slug each is
// addressed by.
func formForView(ctx context.Context, c Clients, in ShowInput) (viewForm, *viewPullRequest, map[string]string, viewSummary, error) {
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
		// create_pull_request adds only the reviewers it is given, so the
		// form fills in whom the create page would, for the person to keep
		// or remove.
		if form.FromRef != "" && form.ToRef != "" {
			found := reviewersForBranches(ctx, c, in.Project, in.Repo, form.FromRef, form.ToRef)
			form.DefaultReviewers, form.DefaultReviewersUnread = found.defaults, found.defaultsErr != nil
			form.CodeOwners, form.CodeOwnersUnread = found.owners, found.ownersErr != nil
		}
		// The author is no reviewer of their own pull request, whoever names
		// them: Bitbucket refuses it, and its reviewer picker never offers
		// them.
		form.Reviewers = withoutPerson(joinPeople(parseCommaList(in.Reviewers), form.DefaultReviewers, form.CodeOwners), currentUsername(ctx, c))
		people := peopleNamed(ctx, c, form.Reviewers)
		form.People = displayNames(people)
		return form, nil, avatarSlugs(people), viewSummary{
			subject: "a new pull request in " + repository,
			form:    "a form they can edit and submit",
			state: fmt.Sprintf("from %s into %s, titled %q%s. Nothing is created until they submit it.",
				form.FromRef, orUnset(form.ToRef), form.Title, reviewersNote(form.Reviewers)),
		}, nil
	}

	pr, err := pullrequestservice.NewService(c.HTTP).Get(ctx, pullrequestservice.RepositoryRef{ProjectKey: in.Project, Slug: in.Repo}, in.ID)
	if err != nil {
		return viewForm{}, nil, nil, viewSummary{}, err
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
		People:        map[string]formPerson{},
	}
	slugs := map[string]string{}
	for _, reviewer := range pr.Reviewers {
		form.Reviewers = append(form.Reviewers, reviewer.Name)
		form.People[reviewer.Name] = formPerson{DisplayName: reviewer.DisplayName}
		if reviewer.Slug != "" && len(slugs) < maxViewPeople {
			slugs[reviewer.Name] = reviewer.Slug
		}
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
	return form, &view, slugs, viewSummary{
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

// reviewersNote names the reviewers a new pull request starts with, for the
// model, which did not choose all of them.
func reviewersNote(reviewers []string) string {
	switch len(reviewers) {
	case 0:
		return ""
	case 1:
		return ", with reviewer " + reviewers[0]
	default:
		return ", with reviewers " + strings.Join(reviewers[:len(reviewers)-1], ", ") + " and " + reviewers[len(reviewers)-1]
	}
}

// branchReviewers are whom Bitbucket names for a pair of branches, less the
// author, each with why Bitbucket could not be asked, if it could not.
type branchReviewers struct {
	defaults, owners       []string
	defaultsErr, ownersErr error
}

// reviewersForBranches asks Bitbucket for the default reviewers and the code
// owners of a pull request between two branches, at once.
func reviewersForBranches(ctx context.Context, c Clients, project, repo, from, to string) branchReviewers {
	var found branchReviewers
	var wait sync.WaitGroup
	wait.Add(2)
	go func() {
		defer wait.Done()
		found.defaults, found.defaultsErr = defaultReviewersFor(ctx, c, project, repo, from, to)
	}()
	go func() {
		defer wait.Done()
		found.owners, found.ownersErr = codeOwnersFor(ctx, c, project, repo, from, to)
	}()
	wait.Wait()
	return found
}

// defaultReviewersFor and codeOwnersFor are whom Bitbucket names for a pull
// request between two branches. The author is left out of both: Bitbucket
// refuses a pull request whose author reviews it, and neither answer knows
// who opens it (ADR-080). Two branches that are the same have no pull
// request between them, and nobody to name for one.
func defaultReviewersFor(ctx context.Context, c Clients, project, repo, from, to string) ([]string, error) {
	if sameBranch(from, to) {
		return nil, nil
	}
	names, err := reviewerservice.NewService(c.OpenAPI).ResolveDefaultReviewers(ctx, project, repo,
		reviewerservice.DefaultReviewerQuery{SourceRef: from, TargetRef: to})
	if err != nil {
		return nil, err
	}
	return withoutPerson(joinPeople(names), currentUsername(ctx, c)), nil
}

func codeOwnersFor(ctx context.Context, c Clients, project, repo, from, to string) ([]string, error) {
	if sameBranch(from, to) {
		return nil, nil
	}
	names, err := codeownersservice.NewService(c.HTTP).Owners(ctx, codeownersservice.RepositoryRef{ProjectKey: project, Slug: repo},
		reviewerservice.NormalizeRefID(from), reviewerservice.NormalizeRefID(to), nil)
	// A Bitbucket without the code-owners module has no code owners to name,
	// which is not a failure to read them.
	if openapi.IsRouteMissing(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return withoutPerson(joinPeople(names), currentUsername(ctx, c)), nil
}

func sameBranch(from, to string) bool {
	return reviewerservice.NormalizeRefID(from) == reviewerservice.NormalizeRefID(to)
}

// joinPeople is the usernames of every list in order, each once: Bitbucket's
// usernames do not differ by case alone.
func joinPeople(lists ...[]string) []string {
	var joined []string
	seen := map[string]bool{}
	for _, list := range lists {
		for _, name := range list {
			name = strings.TrimSpace(name)
			if name == "" || seen[strings.ToLower(name)] {
				continue
			}
			seen[strings.ToLower(name)] = true
			joined = append(joined, name)
		}
	}
	return joined
}

// withoutPerson is names without the one given.
func withoutPerson(names []string, person string) []string {
	if person == "" {
		return names
	}
	kept := names[:0:0]
	for _, name := range names {
		if !strings.EqualFold(name, person) {
			kept = append(kept, name)
		}
	}
	return kept
}

// A person as Bitbucket's user endpoints answer with them.
type bitbucketUser struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Slug        string `json:"slug"`
}

// knownPeople caches each person the form looked up, for the life of the
// server, keyed by the Bitbucket and the username: a display name and a slug
// do not change while it runs, and the form asks again as branches change.
var knownPeople sync.Map

// peopleNamed looks up the people with these usernames, in order, at most
// maxViewPeople of them. One that cannot be found in time is named by the
// username alone, and drawn with initials.
func peopleNamed(ctx context.Context, c Clients, names []string) []bitbucketUser {
	if len(names) > maxViewPeople {
		names = names[:maxViewPeople]
	}
	people := make([]bitbucketUser, len(names))
	if c.HTTP == nil {
		for i, name := range names {
			people[i] = bitbucketUser{Name: name}
		}
		return people
	}
	ctx, cancel := context.WithTimeout(ctx, avatarBudget)
	defer cancel()
	var wait sync.WaitGroup
	slots := make(chan struct{}, maxAvatarFetches)
	for i, name := range names {
		key := c.BaseURL + "\x00" + strings.ToLower(name)
		if cached, ok := knownPeople.Load(key); ok {
			if user, isUser := cached.(bitbucketUser); isUser {
				people[i] = user
				continue
			}
		}
		people[i] = bitbucketUser{Name: name}
		wait.Add(1)
		go func(i int, name, key string) {
			defer wait.Done()
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
			case <-ctx.Done():
				return
			}
			if user, err := findUser(ctx, c, name); err == nil {
				knownPeople.Store(key, user)
				people[i] = user
			}
		}(i, name, key)
	}
	wait.Wait()
	return people
}

// findUser looks a person up by username. Bitbucket addresses a person by
// slug, which is the username for most people; for the rest a search finds
// them.
func findUser(ctx context.Context, c Clients, name string) (bitbucketUser, error) {
	var user bitbucketUser
	if err := c.HTTP.GetJSON(ctx, "/rest/api/latest/users/"+url.PathEscape(name), nil, &user); err == nil && strings.EqualFold(user.Name, name) {
		return user, nil
	}
	var page struct {
		Values []bitbucketUser `json:"values"`
	}
	if err := c.HTTP.GetJSON(ctx, "/rest/api/latest/users", map[string]string{"filter": name, "limit": "100"}, &page); err != nil {
		return bitbucketUser{}, err
	}
	for _, candidate := range page.Values {
		if strings.EqualFold(candidate.Name, name) {
			return candidate, nil
		}
	}
	return bitbucketUser{}, fmt.Errorf("no user named %q", name)
}

func displayNames(people []bitbucketUser) map[string]formPerson {
	if len(people) == 0 {
		return nil
	}
	names := make(map[string]formPerson, len(people))
	for _, person := range people {
		names[person.Name] = formPerson{DisplayName: person.DisplayName}
	}
	return names
}

// avatarSlugs maps each person whose slug is known to it, which is what an
// avatar is fetched by.
func avatarSlugs(people []bitbucketUser) map[string]string {
	slugs := map[string]string{}
	for _, person := range people {
		if person.Slug != "" {
			slugs[person.Name] = person.Slug
		}
	}
	return slugs
}

// Suggestions for the form: the branches the person picks from, the people
// they add as reviewers, and whom Bitbucket names for the branches they pick.
// The model has no tool for these, so the form has a tool of its own, offered
// to views and not to the model.

// maxFormSuggestions is how many suggestions one answer carries.
const maxFormSuggestions = 20

// The fields suggest_form_values answers for.
const (
	formFieldBranch           = "branch"
	formFieldReviewer         = "reviewer"
	formFieldDefaultReviewers = "default_reviewers"
	formFieldCodeOwners       = "code_owners"
)

// SuggestFormValuesInput is what the person typed into a field of the form,
// or the branches they picked.
type SuggestFormValuesInput struct {
	Project string `json:"project" jsonschema:"Bitbucket project key"`
	Repo    string `json:"repo" jsonschema:"Repository slug"`
	Field   string `json:"field" jsonschema:"branch or reviewer for what matches the text; default_reviewers or code_owners for whom Bitbucket names for from_ref into to_ref"`
	Text    string `json:"text,omitempty" jsonschema:"For branch and reviewer: what the person has typed so far"`
	FromRef string `json:"from_ref,omitempty" jsonschema:"For default_reviewers and code_owners: the source branch"`
	ToRef   string `json:"to_ref,omitempty" jsonschema:"For default_reviewers and code_owners: the target branch"`
}

// SuggestFormValuesOutput is what the field suggests.
type SuggestFormValuesOutput struct {
	Values []formValue `json:"values"`
}

// formValue is one suggestion: what the field takes, what the person reads,
// and, for a person, their avatar as a data: URI.
type formValue struct {
	Value  string `json:"value"`
	Label  string `json:"label,omitempty"`
	Avatar string `json:"avatar,omitempty"`
}

func specSuggestFormValues() Spec {
	tool := &mcp.Tool{
		Name: "suggest_form_values",
		Description: "Called by bb's pull request form, not by the model: suggests the repository's branches, or the people " +
			"who can read it as reviewers, matching what the person typed, and names the default reviewers and code owners " +
			"for the branches they picked.",
		Annotations: readOnly("Suggest form values"),
		InputSchema: enumInputSchema[SuggestFormValuesInput](map[string][]string{
			"field": {formFieldBranch, formFieldReviewer, formFieldDefaultReviewers, formFieldCodeOwners},
		}),
		Meta: appOnlyToolMeta(),
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
	case formFieldBranch:
		// Most recently changed first, as the create page's branch picker
		// lists them.
		details := false
		branches, err := branchservice.NewService(c.OpenAPI).List(ctx, branchservice.RepositoryRef{ProjectKey: in.Project, Slug: in.Repo},
			branchservice.ListOptions{FilterText: text, MaxResults: maxFormSuggestions, OrderBy: "MODIFICATION", Details: &details})
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
	case formFieldReviewer:
		// The people Bitbucket's own reviewer picker offers: those who can
		// read the repository. The person bb acts for is left out: an author
		// cannot review their own pull request.
		var page struct {
			Values []bitbucketUser `json:"values"`
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
		people := make([]bitbucketUser, 0, len(page.Values))
		for _, user := range page.Values {
			if user.Name == "" || strings.EqualFold(user.Name, me) {
				continue
			}
			people = append(people, user)
		}
		return peopleValues(ctx, c, people), nil
	case formFieldDefaultReviewers, formFieldCodeOwners:
		from, to := strings.TrimSpace(in.FromRef), strings.TrimSpace(in.ToRef)
		if from == "" || to == "" {
			return nil, fmt.Errorf("field %s needs from_ref and to_ref", in.Field)
		}
		lookUp := defaultReviewersFor
		if in.Field == formFieldCodeOwners {
			lookUp = codeOwnersFor
		}
		names, err := lookUp(ctx, c, in.Project, in.Repo, from, to)
		if err != nil {
			return nil, err
		}
		return peopleValues(ctx, c, peopleNamed(ctx, c, names)), nil
	}
	return nil, fmt.Errorf("field must be %s, %s, %s or %s, not %q", formFieldBranch, formFieldReviewer, formFieldDefaultReviewers, formFieldCodeOwners, in.Field)
}

// peopleValues are people as the form offers them: by username, named as
// Bitbucket shows them, with their avatars, fetched through bb as a view's
// are (ADR-101).
func peopleValues(ctx context.Context, c Clients, people []bitbucketUser) []formValue {
	avatars := fetchAvatars(ctx, c, avatarSlugs(people))
	values := make([]formValue, 0, len(people))
	for _, person := range people {
		values = append(values, formValue{Value: person.Name, Label: person.DisplayName, Avatar: avatars[person.Name]})
	}
	return values
}

// viewHelperTools are the tools only views call, beside refresh_view, which a
// view is offered when the server exposes them.
var viewHelperTools = []string{"suggest_form_values"}
