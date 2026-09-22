package completion

import (
	"context"
	"net/url"
	"strings"
	"sync"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	projectservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/project"
)

func init() {
	register(KindRepository, repositorySource)
	register(KindProject, projectSource)
}

// pageSize is how much of a listing one tab press asks for.
//
// Deliberately below maxCandidates: the paging loop walks in windows of 25, so
// asking for a hundred is four round trips inside a budget written for one. A
// menu of twenty-five is already more than anyone reads, and the next letter
// typed narrows it again.
const pageSize = 25

// repositorySource offers repository selectors, in two stages.
//
// PROJECT/slug is two values, and an instance holds thousands of the second
// one. Listing them all to answer the first keystroke is slow and tells the
// user nothing they could not have got by typing another letter, so a word
// with no slash in it is answered with project keys carrying one -- NoSpace
// keeps the cursor against it -- and only a word that already has a slash is
// answered with the slugs inside that project.
func repositorySource(ctx context.Context, environment *Environment, request Request) (Result, error) {
	client, err := environment.APIClient(ctx)
	if err != nil {
		return Result{}, err
	}

	configuration, err := environment.Config(ctx)
	if err != nil {
		return Result{}, err
	}

	projectKey, prefix, scoped := splitSelector(request.ToComplete)

	local := localSelectors(ctx, environment, configuration.BitbucketURL)
	if scoped {
		local = within(local, projectKey)
	}

	if !scoped {
		projects, listErr := listProjects(ctx, client, projectKey)
		if listErr != nil && len(local) == 0 {
			return Result{}, listErr
		}

		// The checkout's own repositories are whole selectors here rather than
		// stages, so one tab press finishes the value everybody wants most of
		// the time. They come back under the same NoSpace as the stems, which
		// costs that press a space it would otherwise have got; a directive
		// belongs to the answer rather than to a candidate, and the stems are
		// unusable without it.
		stems := make([]Candidate, 0, len(projects))
		for _, project := range projects {
			key := strings.TrimSpace(value(project.Key))
			if key == "" {
				continue
			}

			stems = append(stems, Candidate{Value: key + "/", Description: value(project.Name)})
		}

		candidates, keepOrder := order(selectors(local), stems, promotes(request))

		return Result{Candidates: candidates, NoSpace: true, KeepOrder: keepOrder}, nil
	}

	repositories, listErr := listRepositories(ctx, client, projectKey, prefix)
	if listErr != nil && len(local) == 0 {
		return Result{}, listErr
	}

	listed := make([]Candidate, 0, len(repositories))
	for _, repository := range repositories {
		slug := strings.TrimSpace(value(repository.Slug))
		if slug == "" || repository.Project == nil {
			continue
		}

		listed = append(listed, Candidate{
			Value:       repository.Project.Key + "/" + slug,
			Description: describeRepository(repository),
		})
	}

	candidates, keepOrder := order(selectors(local), listed, promotes(request))

	return Result{Candidates: candidates, KeepOrder: keepOrder}, nil
}

// projectSource offers project keys, described by the project's name.
//
// Nothing is promoted here, unlike a repository selector. The ambient project
// is not knowledge the caller supplied, and `bb project delete <project-key>`
// takes this slot.
func projectSource(ctx context.Context, environment *Environment, request Request) (Result, error) {
	client, err := environment.APIClient(ctx)
	if err != nil {
		return Result{}, err
	}

	projects, err := listProjects(ctx, client, request.ToComplete)
	if err != nil {
		return Result{}, err
	}

	candidates := make([]Candidate, 0, len(projects))
	for _, project := range projects {
		key := strings.TrimSpace(value(project.Key))
		if key == "" {
			continue
		}

		candidates = append(candidates, Candidate{Value: key, Description: value(project.Name)})
	}

	return Result{Candidates: candidates}, nil
}

// splitSelector says which stage of PROJECT/slug a typed word is in.
//
// Without a slash the word is a project key being typed, and the answer is a
// project to open. With one it is a slug inside a project already chosen. A
// word whose project part is empty -- "" or "/" -- is the first stage too:
// there is no project to list the contents of.
func splitSelector(word string) (projectKey string, prefix string, scoped bool) {
	before, after, found := strings.Cut(word, "/")
	before = strings.TrimSpace(before)

	if !found || before == "" {
		return before, "", false
	}

	return before, after, true
}

// destructiveRepositoryTargets are the commands whose repository selector is
// the thing they destroy.
var destructiveRepositoryTargets = map[string]bool{
	"repo delete":       true,
	"repo admin delete": true,
}

// promotes reports whether the repository this checkout points at may be
// offered ahead of the listing.
//
// It may not when the slot being completed is what a destructive command will
// act on. `bb repo delete` counts a repository the caller wrote down -- as the
// argument or with --repo -- as an explicit target, and --yes applies only to
// an explicit one, because a safety flag that works on a target you did not
// name is not one (ADR-073). Ranking the inferred repository first is how a
// single tab press promotes it to a named one, which turns --yes back on for
// the repository the caller was merely standing in. The same candidates are
// still offered there, in the order the shell would sort them into.
func promotes(request Request) bool {
	if request.Command == nil {
		return true
	}

	return promotesAmbientRepository(commandPath(request.Command), request.Flag)
}

func promotesAmbientRepository(path string, flag string) bool {
	if !destructiveRepositoryTargets[path] {
		return true
	}

	return flag != "" && flag != "repo"
}

// order puts what the checkout points at in front of what the server listed.
//
// The local ones are kept either way, so the two orderings offer the same
// values; only their rank differs. KeepOrder goes with the promotion, because
// a shell that sorts the answer would put the repository you are standing in
// wherever the alphabet says and undo it.
func order(local []Candidate, listed []Candidate, promote bool) (candidates []Candidate, keepOrder bool) {
	combined := make([]Candidate, 0, len(local)+len(listed))

	if !promote {
		// Behind the listing rather than dropped: a local repository the first
		// page did not reach is still a candidate, and format() keeps the
		// server's description for one that appears in both.
		return append(append(combined, listed...), local...), false
	}

	if len(local) == 0 {
		return append(combined, listed...), false
	}

	return append(append(combined, local...), listed...), true
}

// selectors renders repositories as the selector a command accepts.
//
// The description is the remote rather than the repository's name, which is
// the one thing a listing would have added and the one thing this answer is
// not waiting for a server to tell it. It also says why the candidate is
// first.
func selectors(repositories []Repository) []Candidate {
	candidates := make([]Candidate, 0, len(repositories))
	for _, repository := range repositories {
		candidates = append(candidates, Candidate{
			Value:       repository.ProjectKey + "/" + repository.Slug,
			Description: describeRemote(repository.RemoteName),
		})
	}

	return candidates
}

func describeRemote(remoteName string) string {
	if strings.TrimSpace(remoteName) == "" {
		return "this checkout"
	}

	return "remote " + remoteName
}

func describeRepository(repository openapigenerated.RestRepository) string {
	description := strings.TrimSpace(value(repository.Name))

	detail := strings.TrimSpace(value(repository.Description))
	switch {
	case detail == "":
		return description
	case description == "":
		return detail
	default:
		return description + " - " + detail
	}
}

func within(repositories []Repository, projectKey string) []Repository {
	kept := make([]Repository, 0, len(repositories))
	for _, repository := range repositories {
		if strings.EqualFold(repository.ProjectKey, projectKey) {
			kept = append(kept, repository)
		}
	}

	return kept
}

// localSelectors are the repositories this checkout's git remotes point at.
//
// A clone is nearly always the repository being asked about, and which one it
// is costs no request to find out -- so it is offered ahead of a listing that
// otherwise begins at whatever is alphabetically first on the whole instance.
//
// No checkout is not a failure: every other slot still completes, and this one
// falls back to the listing alone.
func localSelectors(ctx context.Context, environment *Environment, bitbucketURL string) []Repository {
	host := hostOf(bitbucketURL)
	if host == "" {
		return nil
	}

	// Through the Environment rather than by reading the remotes here: the
	// invocation already reads them to decide what repository it is in, and
	// two readings could disagree about what this checkout is (ADR-088).
	repositories := onHost(environment.LocalRepositories(ctx), host)

	// The repository the command resolved to leads, when the resolution was
	// the inference -- that is the one the invocation being completed would
	// have acted on, decided by the command's own code rather than by this
	// source reading the remotes a second time.
	if repository, err := environment.Repository(ctx); err == nil && repository.Inferred() {
		repositories = lead(repositories, repository)
	}

	return repositories
}

// onHost keeps the checkout's repositories that live on the instance being
// completed against, in the order the invocation read them.
//
// The host has to match. A checkout can have remotes on several instances, and
// a repository from the wrong one is a selector no call here can resolve --
// it would complete to a value that 404s.
func onHost(repositories []Repository, host string) []Repository {
	kept := make([]Repository, 0, len(repositories))
	seen := make(map[string]bool, len(repositories))

	for _, repository := range repositories {
		if !strings.EqualFold(hostOf(repository.Host), host) {
			continue
		}

		key := strings.ToLower(repository.ProjectKey + "/" + repository.Slug)
		if seen[key] {
			continue
		}
		seen[key] = true

		kept = append(kept, repository)
	}

	return kept
}

// lead moves the resolved repository to the front, adding it when no remote
// produced it.
func lead(repositories []Repository, leading Repository) []Repository {
	reordered := make([]Repository, 0, len(repositories)+1)
	reordered = append(reordered, leading)

	for _, repository := range repositories {
		if strings.EqualFold(repository.ProjectKey, leading.ProjectKey) &&
			strings.EqualFold(repository.Slug, leading.Slug) {
			continue
		}

		reordered = append(reordered, repository)
	}

	return reordered
}

func hostOf(rawURL string) string {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return ""
	}
	if !strings.Contains(trimmed, "://") {
		trimmed = "https://" + trimmed
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return ""
	}

	return strings.ToLower(parsed.Hostname())
}

// listProjects asks for the projects a typed prefix could become.
//
// Two listings rather than one, because Bitbucket's filter and the value being
// completed are not the same thing: `name` matches the project's display name,
// while the candidate is its key. Filtering is what reaches past the first page
// on an instance with more projects than one -- PLATFORM against "Platform
// Services" -- and it matches nothing at all where the two do not resemble each
// other, so the unfiltered page is asked for as well.
func listProjects(
	ctx context.Context,
	client *openapigenerated.ClientWithResponses,
	prefix string,
) ([]openapigenerated.RestProject, error) {
	service := projectservice.NewService(client)

	listings := []func(context.Context) ([]openapigenerated.RestProject, error){
		func(ctx context.Context) ([]openapigenerated.RestProject, error) {
			return service.List(ctx, projectservice.ListOptions{MaxResults: pageSize})
		},
	}

	if strings.TrimSpace(prefix) != "" {
		listings = append(listings, func(ctx context.Context) ([]openapigenerated.RestProject, error) {
			return service.List(ctx, projectservice.ListOptions{Name: prefix, MaxResults: pageSize})
		})
	}

	return together(ctx, listings)
}

// listRepositories asks for the repositories of one project, narrowed by what
// has been typed after the slash.
//
// The filter is the server's. `projectkey` is what scopes it -- the
// project-scoped listing at /projects/{key}/repos accepts a `name` parameter
// and ignores it, which returns the whole project and looks exactly like a
// filter that matched everything.
//
// `name` matches the repository's name, while the value being completed is its
// slug, and the two part company as soon as a name has a space in it: "Data
// Center CLI" is data-center-cli, and the slug's prefix matches neither. So the
// project's own page is asked for beside the filtered one, which is the answer
// for every project small enough to fit in it.
func listRepositories(
	ctx context.Context,
	client *openapigenerated.ClientWithResponses,
	projectKey string,
	prefix string,
) ([]openapigenerated.RestRepository, error) {
	listings := []func(context.Context) ([]openapigenerated.RestRepository, error){
		func(ctx context.Context) ([]openapigenerated.RestRepository, error) {
			return repositoryPage(ctx, client, projectKey, prefix)
		},
	}

	if strings.TrimSpace(prefix) != "" {
		listings = append(listings, func(ctx context.Context) ([]openapigenerated.RestRepository, error) {
			return repositoryPage(ctx, client, projectKey, "")
		})
	}

	return together(ctx, listings)
}

// repositoryPage is one page of the instance-wide repository listing.
//
// The generated client rather than internal/services/repository, whose
// ListOptions carries `name` and `projectname` but not `projectkey`: filtering
// by the project's display name would be filtering by something the caller did
// not type.
func repositoryPage(
	ctx context.Context,
	client *openapigenerated.ClientWithResponses,
	projectKey string,
	prefix string,
) ([]openapigenerated.RestRepository, error) {
	limit := float32(pageSize)
	params := &openapigenerated.GetRepositories1Params{Limit: &limit}

	if key := strings.TrimSpace(projectKey); key != "" {
		params.Projectkey = &key
	}
	if name := strings.TrimSpace(prefix); name != "" {
		params.Name = &name
	}

	response, err := client.GetRepositories1WithResponse(ctx, params)
	if err != nil {
		return nil, apperrors.Transport("failed to list repositories", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return nil, err
	}

	page := response.ApplicationjsonCharsetUTF8200
	if page == nil || page.Values == nil {
		return nil, nil
	}

	return *page.Values, nil
}

// together runs the listings of one stage at the same time.
//
// A stage asks for two pages -- one narrowed by what was typed, one not -- and
// a tab press has a single deadline covering both. Run in turn they would
// spend it twice over on an instance that is merely far away. The results are
// concatenated in the order asked for, the narrowed one first, and format()
// drops the duplicates.
//
// An error is returned only when every listing failed: one page is a complete
// answer, and half an answer beats none in a shell.
func together[T any](ctx context.Context, listings []func(context.Context) ([]T, error)) ([]T, error) {
	results := make([][]T, len(listings))
	failures := make([]error, len(listings))

	var waiting sync.WaitGroup
	for index, listing := range listings {
		waiting.Add(1)

		go func() {
			defer waiting.Done()
			defer func() {
				// run.go recovers the source's own goroutine, not the ones it
				// starts, so a panic here would end the process with a stack
				// trace the shell has nowhere to put.
				if recovered := recover(); recovered != nil {
					failures[index] = apperrors.New(apperrors.KindInternal, "listing failed", nil)
					debugf("parallel listing failed: %v", recovered)
				}
			}()

			results[index], failures[index] = listing(ctx)
		}()
	}
	waiting.Wait()

	gathered := make([]T, 0, len(listings)*pageSize)
	answered := false

	for index := range listings {
		if failures[index] != nil {
			continue
		}

		answered = true
		gathered = append(gathered, results[index]...)
	}

	if !answered {
		if len(failures) == 0 {
			return nil, nil
		}

		return nil, failures[0]
	}

	return gathered, nil
}

func value(pointer *string) string {
	if pointer == nil {
		return ""
	}

	return *pointer
}
