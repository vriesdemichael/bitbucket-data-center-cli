package completion

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	pullrequestservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequest"
	reviewerservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/reviewer"
)

func init() {
	register(KindUser, userSource)
	register(KindGroup, groupSource)
	register(KindUserOrGroup, userOrGroupSource)
	register(KindReviewerGroup, reviewerGroupSource)
}

const (
	usersPath  = "/rest/api/latest/users"
	groupsPath = "/rest/api/latest/groups"

	// codeOwnersGroupPrefix is how Bitbucket's Code Owners plugin spells a
	// reviewer group, and one of the two spellings --reviewers accepts.
	codeOwnersGroupPrefix = "reviewer-group/"
)

// userSource offers the people a slot can name.
//
// Which people depends on what the command will do with the name, because a
// user Bitbucket will refuse is a candidate that turns into an error:
//
//   - A reviewer slot wants somebody who can review this repository. Bitbucket
//     refuses anybody else outright -- "cannot participate in the pull request
//     as they are not a licensed user" -- so the listing is narrowed to holders
//     of a licence who can read the repository, rather than to everybody on the
//     instance.
//   - `reviewer remove` wants somebody already reviewing this pull request.
//     Every other user is a value the command has nothing to remove.
//   - Everything else -- a permission being granted, a branch restriction, the
//     members of a reviewer group -- takes any user, so it gets the listing.
//
// Inside a reviewer slot a leading @ names a group instead, which is the one
// place a user slot answers with something that is not a user.
func userSource(ctx context.Context, environment *Environment, request Request) (Result, error) {
	path := commandPath(request.Command)

	if reviewerSlot(path, request.Flag) {
		if spelling, isGroup := groupSpelling(request.ToComplete); isGroup {
			return reviewerGroupCandidates(ctx, environment, spelling, true)
		}

		return listUsers(ctx, environment, reviewerAudience(ctx, environment, request.ToComplete))
	}

	if path == "pr review reviewer remove" && request.Flag == "user" {
		return pullRequestReviewers(ctx, environment)
	}

	return listUsers(ctx, environment, filteredListing(request.ToComplete))
}

// groupSource offers the instance's groups.
//
// /groups takes the same filter /users does, matching anywhere in the name, so
// the word being typed narrows the listing server-side before the prefix
// filter in format narrows it again.
func groupSource(ctx context.Context, environment *Environment, request Request) (Result, error) {
	return listGroups(ctx, environment, request.ToComplete)
}

// userOrGroupSource answers the permission commands, whose argument is a user
// or a group depending on a flag beside it.
//
// `bb repo permissions grant <user-or-group>` reads its argument as a username
// and `--group` makes it read it as a group name, so the flag decides which
// listing is the one the command can act on. Offering both would put half a
// screen of values there that this invocation will fail on, which is the same
// reason `bb pr reopen` is not offered an open pull request.
func userOrGroupSource(ctx context.Context, environment *Environment, request Request) (Result, error) {
	if strings.EqualFold(environment.Flag("group"), "true") {
		return listGroups(ctx, environment, request.ToComplete)
	}

	return listUsers(ctx, environment, filteredListing(request.ToComplete))
}

// reviewerGroupSource offers reviewer groups by name.
//
// By name rather than by id even where the placeholder is <reviewer-group-id>:
// `reviewer-group delete`, `update` and `users` all resolve an exact name
// before they read the argument as an id, and a name is what the person
// knows. The id goes in the description, where it disambiguates two groups
// that differ only in scope.
func reviewerGroupSource(ctx context.Context, environment *Environment, request Request) (Result, error) {
	// A group named inside --reviewers carries its @; named by --reviewer-group
	// or as an argument it does not, and returning one that did would leave the
	// @ in the line.
	spelling, _ := groupSpelling(request.ToComplete)

	return reviewerGroupCandidates(ctx, environment, spelling, alsoProjectGroups(commandPath(request.Command)))
}

// reviewerSlot reports a slot that names somebody who will become a reviewer.
//
// The narrowing is worth its own listing only here. Everywhere else a user is
// a user, and a filtered listing would hide names the command accepts.
func reviewerSlot(path, flag string) bool {
	switch path {
	case "pr create", "pr update":
		return flag == "reviewers"
	case "pr review reviewer add":
		// --users and --reviewers are aliases pflag normalises to --user, so
		// the canonical name is the only one that reaches here.
		return flag == "user"
	default:
		return false
	}
}

// alsoProjectGroups reports a slot that resolves a reviewer group through both
// scopes.
//
// `bb pr create --reviewer-group` and the @ inside --reviewers both end up in
// ResolveReviewerGroupUsers, which looks in the repository's groups and then
// in the project's, so both belong in the answer. The reviewer-group commands
// act on one scope at a time and must not be offered the other's groups: a
// project group deleted at repository scope is a 404 the person was handed.
func alsoProjectGroups(path string) bool {
	return !strings.HasPrefix(path, "reviewer-group ")
}

// groupSpelling reports whether the word being completed names a group, and in
// which of the two spellings --reviewers accepts.
//
// A bare @ is the documented one. "@reviewer-group/" is Bitbucket's own Code
// Owners syntax for the same thing, which the resolution strips as well, so
// somebody who has typed that far keeps completing rather than watching the
// candidates disappear -- the @name form could not match a word that has a
// slash in it.
func groupSpelling(word string) (string, bool) {
	if !strings.HasPrefix(word, "@") {
		return "", false
	}

	if strings.HasPrefix(strings.ToLower(word[1:]), codeOwnersGroupPrefix) {
		return word[:1+len(codeOwnersGroupPrefix)], true
	}

	return "@", true
}

// reviewerAudience is the listing for a reviewer slot.
//
// A repository that cannot be resolved falls back to the plain listing rather
// than to nothing: `bb pr create --reviewers` outside a checkout still has
// people to offer, and the command itself will say what it could not resolve.
func reviewerAudience(ctx context.Context, environment *Environment, word string) map[string]string {
	repository, err := environment.Repository(ctx)
	if err != nil {
		return filteredListing(word)
	}

	return reviewerQuery(word, repository.ProjectKey, repository.Slug)
}

// reviewerQuery narrows /users to the people who can actually review a
// repository.
//
// Both filters are needed and neither is enough. A licence alone leaves
// everybody on the instance who cannot see the repository; repository read
// alone leaves the unlicensed, and Bitbucket refuses exactly those -- verified
// against a live instance, where a user holding REPO_READ without a licence is
// returned by the permission filter and then rejected by the participants
// endpoint with "cannot participate in the pull request as they are not a
// licensed user".
//
// The numbering is the endpoint's: filters are read as permission.1,
// permission.2 and so on, they have to start at 1 and be continuous, and they
// are combined with AND. A filter the server does not understand is dropped
// silently rather than refused -- permission.2 with no permission.1 answers
// with the whole instance -- so the numbering is not a detail that can be got
// wrong safely.
//
// The filter reaches past directly granted permissions: a user who reads the
// repository only through a project grant is returned by it.
func reviewerQuery(word, projectKey, repositorySlug string) map[string]string {
	query := filteredListing(word)

	query["permission.1"] = "REPO_READ"
	query["permission.1.projectKey"] = projectKey
	query["permission.1.repositorySlug"] = repositorySlug
	query["permission.2"] = "LICENSED_USER"

	return query
}

// filteredListing is a page of people or groups, narrowed by the word being
// typed.
//
// The filter matches username, display name and email anywhere in the value,
// which is wider than the prefix match a shell will apply afterwards. It is
// still the filter worth sending: an instance with ten thousand users answers
// an unfiltered page with the first hundred names alphabetically, and nothing
// a person types past "a" would ever be in it.
func filteredListing(word string) map[string]string {
	query := map[string]string{"limit": strconv.Itoa(maxCandidates)}

	if trimmed := strings.TrimSpace(word); trimmed != "" {
		query["filter"] = trimmed
	}

	return query
}

func listUsers(ctx context.Context, environment *Environment, query map[string]string) (Result, error) {
	client, err := environment.HTTPClient(ctx)
	if err != nil {
		return Result{}, err
	}

	var page struct {
		Values []userValue `json:"values"`
	}
	if err := client.GetJSON(ctx, usersPath, query, &page); err != nil {
		return Result{}, err
	}

	candidates := make([]Candidate, 0, len(page.Values))
	for _, value := range page.Values {
		candidates = append(candidates, Candidate{
			Value:       value.username(),
			Description: describeUser(value.DisplayName, value.EmailAddress),
		})
	}

	return Result{Candidates: candidates}, nil
}

func listGroups(ctx context.Context, environment *Environment, word string) (Result, error) {
	client, err := environment.HTTPClient(ctx)
	if err != nil {
		return Result{}, err
	}

	var page struct {
		Values []groupValue `json:"values"`
	}
	if err := client.GetJSON(ctx, groupsPath, filteredListing(word), &page); err != nil {
		return Result{}, err
	}

	candidates := make([]Candidate, 0, len(page.Values))
	for _, value := range page.Values {
		candidates = append(candidates, Candidate{Value: value.Name})
	}

	return Result{Candidates: candidates}, nil
}

// pullRequestReviewers offers the people already reviewing the pull request on
// the line.
//
// The pull request is whichever one the invocation names -- the argument
// before the flag, or --pr -- which the Environment resolves the way the
// command would, so a completion cannot offer a reviewer of a different pull
// request than the one about to be changed.
func pullRequestReviewers(ctx context.Context, environment *Environment) (Result, error) {
	pullRequestID, err := environment.PullRequest(ctx)
	if err != nil {
		return Result{}, err
	}

	repository, err := environment.Repository(ctx)
	if err != nil {
		return Result{}, err
	}

	client, err := environment.HTTPClient(ctx)
	if err != nil {
		return Result{}, err
	}

	pullRequest, err := pullrequestservice.NewService(client).Get(
		ctx,
		pullrequestservice.RepositoryRef{ProjectKey: repository.ProjectKey, Slug: repository.Slug},
		pullRequestID,
	)
	if err != nil {
		return Result{}, err
	}

	candidates := make([]Candidate, 0, len(pullRequest.Reviewers))
	for _, reviewer := range pullRequest.Reviewers {
		candidates = append(candidates, Candidate{
			Value:       reviewer.Name,
			Description: describeUser(reviewer.DisplayName, reviewer.Email),
		})
	}

	return Result{Candidates: candidates}, nil
}

// reviewerGroupCandidates lists the reviewer groups the slot can name, in the
// spelling the word being completed asked for.
func reviewerGroupCandidates(
	ctx context.Context,
	environment *Environment,
	spelling string,
	alsoProject bool,
) (Result, error) {
	client, err := environment.APIClient(ctx)
	if err != nil {
		return Result{}, err
	}

	service := reviewerservice.NewService(client)

	// The reviewer-group commands refuse --project and --repo together, so a
	// named project is the whole scope and the repository is not resolved at
	// all -- which matters outside a checkout, where resolving it would fail.
	if project := environment.Flag("project"); project != "" && environment.Flag("repo") == "" {
		groups, err := service.ListProjectReviewerGroups(ctx, project)
		if err != nil {
			return Result{}, err
		}

		return reviewerGroupResult(groups, spelling), nil
	}

	repository, err := environment.Repository(ctx)
	if err != nil {
		return Result{}, err
	}

	groups, err := service.ListRepositoryReviewerGroups(ctx, repository.ProjectKey, repository.Slug)
	if err != nil {
		return Result{}, err
	}

	if alsoProject {
		// The project's groups are the second half of an answer, not the
		// answer: a listing that failed leaves the repository's groups worth
		// offering, and the resolution looks in the repository first anyway.
		if projectGroups, err := service.ListProjectReviewerGroups(ctx, repository.ProjectKey); err == nil {
			groups = append(groups, projectGroups...)
		}
	}

	return reviewerGroupResult(groups, spelling), nil
}

func reviewerGroupResult(groups []openapigenerated.RestReviewerGroup, spelling string) Result {
	candidates := make([]Candidate, 0, len(groups))
	seen := make(map[string]bool, len(groups))

	for _, group := range groups {
		if group.Name == nil {
			continue
		}

		name := strings.TrimSpace(*group.Name)
		if name == "" || seen[strings.ToLower(name)] {
			continue
		}
		seen[strings.ToLower(name)] = true

		candidates = append(candidates, Candidate{
			Value:       spelling + name,
			Description: describeReviewerGroup(group),
		})
	}

	return Result{Candidates: candidates}
}

// userValue is the part of a Bitbucket user a completion has anything to do
// with.
type userValue struct {
	Name         string `json:"name"`
	Slug         string `json:"slug"`
	DisplayName  string `json:"displayName"`
	EmailAddress string `json:"emailAddress"`
}

// username is the name the commands take. The slug is the fallback because a
// directory that supplies no name still supplies one Bitbucket can address.
func (value userValue) username() string {
	if name := strings.TrimSpace(value.Name); name != "" {
		return name
	}

	return strings.TrimSpace(value.Slug)
}

// groupValue is a group name, however the instance spells a listing.
//
// /groups answers with an array of plain strings and the administrative
// listing beside it answers with objects carrying a name, and an instance has
// been seen doing either. Reading both costs four lines and removes a version
// difference that would otherwise be an empty completion nobody could explain.
type groupValue struct {
	Name string
}

func (value *groupValue) UnmarshalJSON(raw []byte) error {
	var name string
	if err := json.Unmarshal(raw, &name); err == nil {
		value.Name = strings.TrimSpace(name)

		return nil
	}

	var object struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &object); err != nil {
		return err
	}

	value.Name = strings.TrimSpace(object.Name)

	return nil
}

// describeUser is what a shell shows beside a username.
//
// The display name is what makes a username recognisable, and the email
// address is what tells two people with the same display name apart. It is the
// person's own directory data shown in their own terminal, which is where it
// stays: nothing here is logged.
func describeUser(displayName, email string) string {
	name := strings.TrimSpace(displayName)
	address := strings.TrimSpace(email)

	switch {
	case name != "" && address != "":
		return name + " <" + address + ">"
	case name != "":
		return name
	default:
		return address
	}
}

// describeReviewerGroup is what a shell shows beside a reviewer group's name.
//
// The description when the group carries one, and the size when it does not:
// two groups called "backend" and "backend-oncall" are told apart by their
// description, and a group with none is still told apart from an empty one by
// how many people are in it.
func describeReviewerGroup(group openapigenerated.RestReviewerGroup) string {
	if group.Description != nil {
		if description := strings.TrimSpace(*group.Description); description != "" {
			return description
		}
	}

	if group.Users == nil || len(*group.Users) == 0 {
		return ""
	}

	if len(*group.Users) == 1 {
		return "1 member"
	}

	return strconv.Itoa(len(*group.Users)) + " members"
}
