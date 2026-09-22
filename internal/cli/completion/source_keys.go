package completion

import (
	"context"
	"net/url"
	"strconv"
	"strings"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/safederef"
	gpgkeyservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/gpgkey"
	qualityservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/quality"
	sshkeyservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/sshkey"
	tokenservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/token"
)

// The keys and ids that hang off a commit or off the account, where the value
// alone says nothing.
//
// A build key is "ci", a token id is twelve digits, a GPG key id is sixteen hex
// characters: none of them tells the person which of the four they are about to
// delete. So every candidate here carries what the thing is -- the state and
// name of the build, the title and result of the report, the label of the key,
// the name and permissions of the token -- and the live tests assert the
// description as well as the value, because a listing of bare ids is a
// completion that has not helped.
//
// KindDeploymentKey and KindEnvironmentKey are declared in the vocabulary and
// deliberately left without a source: Bitbucket has no listing to build one
// from. The only deployments endpoint is
// /projects/{key}/repos/{slug}/commits/{id}/deployments, and its GET refuses
// every request that does not name all three of key, environmentKey and
// deploymentSequenceNumber -- verified against 10.4, where dropping any one of
// them answers "The query parameter 'key' is required." There is no page of
// deployments for a commit, for a repository or for an environment, so
// `bb deployment get --key <tab>` would have to know the answer to offer it.
func init() {
	register(KindBuildKey, buildKeySource)
	register(KindInsightReport, insightReportSource)
	register(KindSSHKey, sshKeySource)
	register(KindGPGKey, gpgKeySource)
	register(KindAccessToken, accessTokenSource)
}

// accessTokensPath is where HTTP access tokens live. Its own REST namespace,
// not /api, and the three scopes hang off it.
const accessTokensPath = "/rest/access-tokens/latest"

// buildKeySource offers the build status keys already reported for the commit
// on the line.
//
// The listing is the commit's rather than the repository's, because that is the
// only one Bitbucket offers: /rest/api/latest/projects/{key}/repos/{slug}/
// commits/{id}/builds answers for one key at a time and refuses a request
// without it, while /rest/build-status/latest/commits/{id} returns the page.
// The two agree on what exists -- a status written through the repository-scoped
// endpoint comes straight back out of the commit-scoped page -- which is what
// makes the one listing right for `bb build get|delete|set` and for
// `bb build status set`, the latter having no repository in scope at all.
func buildKeySource(ctx context.Context, environment *Environment, _ Request) (Result, error) {
	commit, err := environment.Commit(ctx)
	if err != nil {
		return Result{}, err
	}

	client, err := environment.APIClient(ctx)
	if err != nil {
		return Result{}, err
	}

	statuses, err := qualityservice.NewService(client).GetBuildStatuses(ctx, commit, maxCandidates, "")
	if err != nil {
		return Result{}, err
	}

	candidates := make([]Candidate, 0, len(statuses))
	for _, status := range statuses {
		key := strings.TrimSpace(safederef.String(status.Key))
		if key == "" {
			continue
		}

		candidates = append(candidates, Candidate{Value: key, Description: describeBuildStatus(status)})
	}

	return Result{Candidates: candidates}, nil
}

// insightReportSource offers the code insight reports attached to the commit on
// the line.
//
// The same keys answer `bb insights report get|delete|set` and the annotation
// commands beside them, which take the report they annotate by the same key --
// an annotation on a report that does not exist is a 404 rather than a new
// report.
func insightReportSource(ctx context.Context, environment *Environment, _ Request) (Result, error) {
	commit, err := environment.Commit(ctx)
	if err != nil {
		return Result{}, err
	}

	repository, err := environment.Repository(ctx)
	if err != nil {
		return Result{}, err
	}

	client, err := environment.APIClient(ctx)
	if err != nil {
		return Result{}, err
	}

	reports, err := qualityservice.NewService(client).ListReports(
		ctx,
		qualityservice.RepositoryRef{ProjectKey: repository.ProjectKey, Slug: repository.Slug},
		commit,
		maxCandidates,
	)
	if err != nil {
		return Result{}, err
	}

	candidates := make([]Candidate, 0, len(reports))
	for _, report := range reports {
		key := strings.TrimSpace(safederef.String(report.Key))
		if key == "" {
			continue
		}

		candidates = append(candidates, Candidate{Value: key, Description: describeInsightReport(report)})
	}

	return Result{Candidates: candidates}, nil
}

// sshKeySource offers the authenticated account's own SSH keys.
//
// `bb ssh-key remove` takes the numeric id, which is the one thing about a key
// nobody knows by heart, so the label and the fingerprint are what the
// description is for. The server's order is kept: the ids are numbers, and a
// shell sorting them as text puts 10 above 9.
func sshKeySource(ctx context.Context, environment *Environment, _ Request) (Result, error) {
	client, err := environment.APIClient(ctx)
	if err != nil {
		return Result{}, err
	}

	keys, err := sshkeyservice.NewService(client).ListUserKeys(ctx, maxCandidates, 0)
	if err != nil {
		return Result{}, err
	}

	candidates := make([]Candidate, 0, len(keys))
	for _, key := range keys {
		if key.Id == nil {
			continue
		}

		candidates = append(candidates, Candidate{
			Value:       strconv.FormatInt(int64(safederef.Int32(key.Id)), 10),
			Description: describeSSHKey(key),
		})
	}

	return Result{Candidates: candidates, KeepOrder: true}, nil
}

// gpgKeySource offers the authenticated account's own GPG keys.
//
// `bb auth gpg-key remove <id-or-fingerprint>` accepts either spelling, so the
// candidate is the id Bitbucket returns and the description is the fingerprint
// beside it -- which is the half a person recognises, because it is what `gpg
// --list-keys` prints and what the key was added with. An instance that returns
// only a fingerprint offers that instead; both are values the command resolves.
func gpgKeySource(ctx context.Context, environment *Environment, _ Request) (Result, error) {
	client, err := environment.APIClient(ctx)
	if err != nil {
		return Result{}, err
	}

	keys, err := gpgkeyservice.NewService(client).ListGpgKeys(ctx, maxCandidates)
	if err != nil {
		return Result{}, err
	}

	candidates := make([]Candidate, 0, len(keys))
	for _, key := range keys {
		if candidate, ok := gpgKeyCandidate(key); ok {
			candidates = append(candidates, candidate)
		}
	}

	return Result{Candidates: candidates}, nil
}

// accessTokenSource offers the HTTP access tokens of the scope on the line.
//
// The scope is the command's, not the checkout's: `bb auth token` refuses an
// ambiently inferred repository (ADR-039), so which tokens exist is decided by
// --user, --project or --repo and by nothing else. Standing in a repository
// does not make its tokens the ones `bb auth token revoke` would revoke, and
// offering them there would be offering ids that belong to another scope.
//
// The listing is read directly rather than through the token service because
// the generated RestAccessToken has no permissions field, and a token id beside
// its name alone does not say what revoking it takes away.
func accessTokenSource(ctx context.Context, environment *Environment, _ Request) (Result, error) {
	scope, target, err := tokenScope(environment.Flag("user"), environment.Flag("project"), environment.Flag("repo"))
	if err != nil {
		return Result{}, err
	}

	client, err := environment.HTTPClient(ctx)
	if err != nil {
		return Result{}, err
	}

	// The command's own default: with no scope named the tokens are the
	// authenticated user's, and who that is comes from the instance rather
	// than from the configuration, which need not hold a username at all when
	// the credential is a token.
	if scope == tokenservice.ScopeUser && target == "" {
		target, err = client.CurrentUserSlug(ctx)
		if err != nil {
			return Result{}, err
		}
	}

	path, err := tokenListingPath(scope, target)
	if err != nil {
		return Result{}, err
	}

	var page struct {
		Values []accessTokenValue `json:"values"`
	}
	if err := client.GetJSON(ctx, path, map[string]string{"limit": strconv.Itoa(maxCandidates)}, &page); err != nil {
		return Result{}, err
	}

	candidates := make([]Candidate, 0, len(page.Values))
	for _, value := range page.Values {
		id := strings.TrimSpace(value.ID)
		if id == "" {
			continue
		}

		candidates = append(candidates, Candidate{Value: id, Description: describeAccessToken(value)})
	}

	return Result{Candidates: candidates}, nil
}

// accessTokenValue is the part of an HTTP access token a completion has
// anything to do with.
//
// Read from the payload rather than from the generated model, which stops at id
// and name: permissions is in every response Bitbucket sends and in none of the
// types oapi-codegen produced for them.
type accessTokenValue struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Permissions []string `json:"permissions"`
}

// tokenScope is the scope `bb auth token` would act on, from the flags on the
// line.
//
// It mirrors resolveTokenScope in internal/cli/cmd/auth: at most one of the
// three, project before repository, and the authenticated user when none was
// named -- reported here as an empty target, because resolving it costs a
// request the other two scopes do not need. Two scopes at once is the error the
// command gives, and Active Help says so rather than the shell offering the
// tokens of whichever one happened to win.
func tokenScope(user, project, repository string) (tokenservice.ScopeType, string, error) {
	user, project, repository = strings.TrimSpace(user), strings.TrimSpace(project), strings.TrimSpace(repository)

	named := 0
	for _, value := range []string{user, project, repository} {
		if value != "" {
			named++
		}
	}
	if named > 1 {
		return "", "", apperrors.New(
			apperrors.KindValidation,
			"only one of --user, --project, or --repo scope can be specified",
			nil,
		)
	}

	switch {
	case project != "":
		return tokenservice.ScopeProject, project, nil
	case repository != "":
		return tokenservice.ScopeRepo, repository, nil
	default:
		return tokenservice.ScopeUser, user, nil
	}
}

// tokenListingPath is where a scope's tokens are listed.
//
// A repository target is the same PROJECT/slug the command parses, and a target
// that is not one is refused rather than sent: half a selector would address
// the project's tokens, which are a different set from the repository's.
func tokenListingPath(scope tokenservice.ScopeType, target string) (string, error) {
	if strings.TrimSpace(target) == "" {
		return "", apperrors.New(apperrors.KindValidation, "no access token scope in context", nil)
	}

	switch scope {
	case tokenservice.ScopeProject:
		return accessTokensPath + "/projects/" + url.PathEscape(target), nil

	case tokenservice.ScopeRepo:
		projectKey, slug, split := strings.Cut(target, "/")
		projectKey, slug = strings.TrimSpace(projectKey), strings.TrimSpace(slug)
		if !split || projectKey == "" || slug == "" {
			return "", apperrors.New(
				apperrors.KindValidation,
				"repository scope must be given as PROJECT/slug",
				nil,
			)
		}

		return accessTokensPath + "/projects/" + url.PathEscape(projectKey) + "/repos/" + url.PathEscape(slug), nil

	case tokenservice.ScopeUser:
		return accessTokensPath + "/users/" + url.PathEscape(target), nil

	default:
		return "", apperrors.New(apperrors.KindValidation, "no access token scope in context", nil)
	}
}

// describeBuildStatus is what a shell shows beside a build status key.
//
// The state first, because it is why somebody is looking: the key they want is
// nearly always the one that failed. The name is what tells two keys from the
// same CI server apart, and the build's description stands in when it has no
// name.
func describeBuildStatus(status openapigenerated.RestBuildStatus) string {
	state := ""
	if status.State != nil {
		state = strings.TrimSpace(string(*status.State))
	}

	name := strings.TrimSpace(safederef.String(status.Name))
	if name == "" {
		name = strings.TrimSpace(safederef.String(status.Description))
	}

	switch {
	case state != "" && name != "":
		return state + ": " + name
	case state != "":
		return state
	default:
		return name
	}
}

// describeInsightReport is what a shell shows beside a report key.
//
// The title says what the report is and the result says whether it passed,
// which between them are the reason to pick one report key over another.
func describeInsightReport(report openapigenerated.RestInsightReport) string {
	title := strings.TrimSpace(safederef.String(report.Title))

	result := ""
	if report.Result != nil {
		result = strings.TrimSpace(string(*report.Result))
	}

	switch {
	case title != "" && result != "":
		return title + " (" + result + ")"
	case title != "":
		return title
	default:
		return result
	}
}

// describeSSHKey is what a shell shows beside an SSH key id.
//
// The label first: it is the name the person gave the key and the only part of
// it they chose. The fingerprint is what identifies the key itself -- the same
// string `ssh-keygen -lf` prints -- and the algorithm stands in for an instance
// that reports no fingerprint.
func describeSSHKey(key openapigenerated.RestSshKey) string {
	label := strings.TrimSpace(safederef.String(key.Label))

	identity := strings.TrimSpace(safederef.String(key.Fingerprint))
	if identity == "" {
		identity = strings.TrimSpace(safederef.String(key.AlgorithmType))
	}

	switch {
	case label != "" && identity != "":
		return label + " (" + identity + ")"
	case label != "":
		return label
	default:
		return identity
	}
}

// gpgKeyCandidate is one GPG key as the argument spells it, and reports false
// for a key Bitbucket identified with neither an id nor a fingerprint.
func gpgKeyCandidate(key openapigenerated.RestGpgKey) (Candidate, bool) {
	id := strings.TrimSpace(safederef.String(key.Id))
	fingerprint := strings.TrimSpace(safederef.String(key.Fingerprint))

	value, other := id, fingerprint
	if value == "" {
		value, other = fingerprint, id
	}
	if value == "" {
		return Candidate{}, false
	}

	description := other
	if email := strings.TrimSpace(safederef.String(key.EmailAddress)); email != "" {
		description = strings.TrimSpace(description + " " + email)
	}

	return Candidate{Value: value, Description: description}, true
}

// describeAccessToken is what a shell shows beside a token id.
//
// The permissions are half of it. `bb auth token revoke` takes away whatever
// the token could do, and a name on its own -- "ci", "laptop" -- does not say
// whether that was reading one repository or administering every project.
func describeAccessToken(value accessTokenValue) string {
	name := strings.TrimSpace(value.Name)

	permissions := make([]string, 0, len(value.Permissions))
	for _, permission := range value.Permissions {
		if trimmed := strings.TrimSpace(permission); trimmed != "" {
			permissions = append(permissions, trimmed)
		}
	}

	granted := strings.Join(permissions, ", ")

	switch {
	case name != "" && granted != "":
		return name + " (" + granted + ")"
	case name != "":
		return name
	default:
		return granted
	}
}
