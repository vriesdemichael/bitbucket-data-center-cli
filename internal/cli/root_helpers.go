package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/safederef"
	"io"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/git"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/git/execgit"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	branchservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/branch"
	diffservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/diff"
	pullrequestservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequest"
	qualityservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/quality"
	reposettings "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/reposettings"
	tagservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/tag"
)

var gitBackendFactory = func() git.Backend {
	return execgit.New()
}

type inferredRepositoryContext struct {
	Host       string
	ProjectKey string
	Slug       string
	RemoteName string
}

func resolveRepositoryReference(selector string, cfg config.AppConfig) (diffservice.RepositoryRef, error) {
	repo, err := resolveRepositorySelector(selector, cfg)
	if err != nil {
		return diffservice.RepositoryRef{}, err
	}

	return diffservice.RepositoryRef{ProjectKey: repo.ProjectKey, Slug: repo.Slug}, nil
}

type repositorySelector struct {
	ProjectKey string
	Slug       string
}

func resolveRepositorySelector(selector string, cfg config.AppConfig) (repositorySelector, error) {
	trimmed := strings.TrimSpace(selector)
	if trimmed == "" {
		// Read from the resolved configuration rather than the environment, so an
		// inferred context reaches it as a value (issue #458).
		repoSlug := strings.TrimSpace(cfg.RepoSlug)
		if strings.TrimSpace(cfg.ProjectKey) == "" || repoSlug == "" {
			return repositorySelector{}, apperrors.New(
				apperrors.KindValidation,
				"repository is required (use --repo PROJECT/slug or set BITBUCKET_PROJECT_KEY + BITBUCKET_REPO_SLUG)",
				nil,
			)
		}

		return repositorySelector{ProjectKey: cfg.ProjectKey, Slug: repoSlug}, nil
	}

	return parseRepositorySelector(trimmed)
}

func (options *rootOptions) applyInferredRepositoryContext(cmd *cobra.Command, asJSON bool) error {
	if cmd == nil {
		return nil
	}

	repoFlag := cmd.Flags().Lookup("repo")
	if repoFlag == nil {
		return nil
	}

	if repoFlag.Changed && strings.TrimSpace(repoFlag.Value.String()) != "" {
		return nil
	}

	cfg, err := config.LoadFromEnv()
	if err != nil {
		return nil
	}

	inferred, err := inferRepositoryContextFromGit(cfg)
	if err != nil {
		return err
	}
	if inferred == nil {
		return nil
	}

	// Carried as values rather than published to the environment. Writing them
	// meant the inferred context outlived the command and was indistinguishable
	// from an operator having set the same variables (issue #458).
	options.runtime.Host = inferred.Host
	options.runtime.ProjectKey = inferred.ProjectKey
	options.runtime.RepoSlug = inferred.Slug

	repoValue := fmt.Sprintf("%s/%s", inferred.ProjectKey, inferred.Slug)
	if err := repoFlag.Value.Set(repoValue); err != nil {
		return apperrors.New(apperrors.KindInternal, "failed to apply inferred repository to --repo flag", err)
	}

	// Changed is set because commands read the flag to resolve their target,
	// and recorded because for a destructive command it is not the same thing
	// as the caller having named one. bb repo delete treats a named target as
	// what makes --yes apply, and inference marking the flag Changed defeated
	// that check: --yes then applied to the repository you happened to be
	// standing in, which is the hazard #472 reports.
	repoFlag.Changed = true
	options.repositoryInferred = true

	if asJSON {
		return nil
	}

	fmt.Fprintf(
		cmd.ErrOrStderr(),
		"Using repository context from git remote %q: %s/%s on %s\n",
		inferred.RemoteName,
		inferred.ProjectKey,
		inferred.Slug,
		inferred.Host,
	)

	return nil
}

func parseRepositorySelector(selector string) (repositorySelector, error) {
	parts := strings.SplitN(selector, "/", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return repositorySelector{}, apperrors.New(apperrors.KindValidation, "--repo must be in PROJECT/slug format", nil)
	}

	projectKey := strings.TrimSpace(parts[0])
	slug := strings.TrimSpace(parts[1])
	if unescaped, err := url.PathUnescape(projectKey); err == nil {
		projectKey = unescaped
	}
	if unescaped, err := url.PathUnescape(slug); err == nil {
		slug = unescaped
	}

	return repositorySelector{ProjectKey: projectKey, Slug: slug}, nil
}

func inferRepositoryContextFromGit(cfg config.AppConfig) (*inferredRepositoryContext, error) {
	backend := gitBackendFactory()
	if backend == nil {
		return nil, nil
	}

	cwd, err := os.Getwd()
	if err != nil {
		return nil, nil
	}

	repoRoot, err := backend.RepositoryRoot(context.Background(), cwd)
	if err != nil {
		if isNonRepositoryError(err) {
			return nil, nil
		}
		return nil, err
	}

	remotes, err := backend.ListRemotes(context.Background(), repoRoot)
	if err != nil {
		if isNonRepositoryError(err) {
			return nil, nil
		}
		return nil, err
	}

	if len(remotes) == 0 {
		return nil, nil
	}

	stored, _ := config.LoadStoredConfig()
	authenticatedHosts := authenticatedHostLookup(cfg, stored)
	if len(authenticatedHosts) == 0 {
		return nil, nil
	}

	candidates := make([]inferredRepositoryContext, 0)
	for _, remote := range remotes {
		_, projectKey, slug, ok := parseBitbucketRemote(remote.URL)
		if !ok {
			continue
		}

		resolvedHost, ok := authenticatedHosts[normalizeRemoteEndpoint(remote.URL)]
		if !ok {
			continue
		}

		candidates = append(candidates, inferredRepositoryContext{
			Host:       resolvedHost,
			ProjectKey: projectKey,
			Slug:       slug,
			RemoteName: remote.Name,
		})
	}

	if len(candidates) == 0 {
		return nil, nil
	}

	sort.SliceStable(candidates, func(left, right int) bool {
		if candidates[left].RemoteName == candidates[right].RemoteName {
			if candidates[left].ProjectKey == candidates[right].ProjectKey {
				return candidates[left].Slug < candidates[right].Slug
			}
			return candidates[left].ProjectKey < candidates[right].ProjectKey
		}
		if candidates[left].RemoteName == "origin" {
			return true
		}
		if candidates[right].RemoteName == "origin" {
			return false
		}
		return candidates[left].RemoteName < candidates[right].RemoteName
	})

	unique := map[string]inferredRepositoryContext{}
	for _, candidate := range candidates {
		key := candidate.Host + "\x00" + candidate.ProjectKey + "\x00" + candidate.Slug + "\x00" + candidate.RemoteName
		unique[key] = candidate
	}

	if len(unique) > 1 {
		// A side remote next to origin — a contributor's fork, a mirror — does
		// not make the context ambiguous. Git's convention is that origin is
		// the repository this clone belongs to, and honouring it costs nothing
		// while --repo stays available to override.
		//
		// bb pr checkout makes this more than a nicety: it adds a remote for
		// the fork a pull request comes from, so without this every later bb
		// command in that repository would refuse to infer context — a trap
		// laid by the previous command.
		//
		// upstream is the deliberate exception. It is the one remote name that
		// conventionally outranks origin, so a repository with both really is
		// ambiguous about which one bb should act on, and saying so is better
		// than picking.
		if origin, found := unambiguousOriginCandidate(unique); found {
			return &origin, nil
		}

		ordered := make([]inferredRepositoryContext, 0, len(unique))
		for _, candidate := range unique {
			ordered = append(ordered, candidate)
		}
		sort.SliceStable(ordered, func(left, right int) bool {
			if ordered[left].RemoteName == ordered[right].RemoteName {
				if ordered[left].ProjectKey == ordered[right].ProjectKey {
					return ordered[left].Slug < ordered[right].Slug
				}
				return ordered[left].ProjectKey < ordered[right].ProjectKey
			}
			if ordered[left].RemoteName == "origin" {
				return true
			}
			if ordered[right].RemoteName == "origin" {
				return false
			}
			return ordered[left].RemoteName < ordered[right].RemoteName
		})

		descriptions := make([]string, 0, len(ordered))
		for _, candidate := range ordered {
			descriptions = append(descriptions, fmt.Sprintf("%s=%s/%s@%s", candidate.RemoteName, candidate.ProjectKey, candidate.Slug, candidate.Host))
		}

		return nil, apperrors.New(
			apperrors.KindValidation,
			fmt.Sprintf("ambiguous git remote context (%s); specify --repo PROJECT/slug and/or set active server with auth server use --host", strings.Join(descriptions, ", ")),
			nil,
		)
	}

	for _, candidate := range unique {
		selected := candidate
		return &selected, nil
	}

	return nil, nil
}

// unambiguousOriginCandidate returns the remote named origin when it is the
// only reasonable reading of the repository's context.
//
// Exactly one origin, not the first: git cannot have two remotes of one name,
// but one origin with several URLs yields several candidates, and those may
// name different repositories. Picking either would be a guess.
//
// An upstream remote disqualifies origin outright, because the convention that
// puts a fork on origin puts the repository people actually work against on
// upstream. There is no defensible default between the two.
func unambiguousOriginCandidate(candidates map[string]inferredRepositoryContext) (inferredRepositoryContext, bool) {
	var found inferredRepositoryContext
	count := 0

	for _, candidate := range candidates {
		if strings.EqualFold(candidate.RemoteName, "upstream") {
			return inferredRepositoryContext{}, false
		}
		if candidate.RemoteName != "origin" {
			continue
		}
		found = candidate
		count++
	}

	return found, count == 1
}

func authenticatedHostLookup(cfg config.AppConfig, stored config.StoredConfig) map[string]string {
	lookup := map[string]string{}
	if strings.TrimSpace(cfg.BitbucketURL) != "" {
		if normalized := normalizeHostEndpointLoose(cfg.BitbucketURL); normalized != "" {
			lookup[normalized] = cfg.BitbucketURL
		}
	}

	for _, profile := range stored.Hosts {
		host := strings.TrimSpace(profile.URL)
		if host == "" {
			continue
		}
		if normalized := normalizeHostEndpointLoose(host); normalized != "" {
			if _, exists := lookup[normalized]; !exists {
				lookup[normalized] = host
			}
		}
		for _, alias := range profile.Aliases {
			if normalized := normalizeHostEndpointLoose(alias); normalized != "" {
				if _, exists := lookup[normalized]; !exists {
					lookup[normalized] = host
				}
			}
		}
	}

	return lookup
}

func normalizeHostEndpoint(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}

	if strings.HasPrefix(trimmed, "git@") {
		at := strings.LastIndex(trimmed, "@")
		colon := strings.Index(trimmed[at+1:], ":")
		if at >= 0 && colon >= 0 {
			host := strings.TrimSpace(trimmed[at+1 : at+1+colon])
			if host == "" {
				return ""
			}
			return strings.ToLower(host + ":22")
		}
		return ""
	}

	if !strings.Contains(trimmed, "://") {
		trimmed = "https://" + trimmed
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return ""
	}

	hostname := strings.TrimSpace(parsed.Hostname())
	if hostname == "" {
		return ""
	}
	port := parsed.Port()
	if port == "" {
		switch strings.ToLower(parsed.Scheme) {
		case "http":
			port = "80"
		case "ssh":
			port = "22"
		default:
			port = "443"
		}
	}
	return strings.ToLower(hostname + ":" + port)
}

func normalizeHostEndpointLoose(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}

	if strings.HasPrefix(trimmed, "git@") {
		at := strings.LastIndex(trimmed, "@")
		colon := strings.Index(trimmed[at+1:], ":")
		if at >= 0 && colon >= 0 {
			host := strings.TrimSpace(trimmed[at+1 : at+1+colon])
			if host == "" {
				return ""
			}
			return strings.ToLower(host + ":22")
		}
		return ""
	}

	if !strings.Contains(trimmed, "://") {
		trimmed = "https://" + trimmed
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return ""
	}
	hostname := strings.TrimSpace(parsed.Hostname())
	if hostname == "" {
		return ""
	}
	port := parsed.Port()
	if port == "" {
		switch strings.ToLower(parsed.Scheme) {
		case "http":
			port = "80"
		case "ssh":
			port = "22"
		default:
			port = "443"
		}
	}
	if (strings.EqualFold(parsed.Scheme, "https") && port == "443") || (strings.EqualFold(parsed.Scheme, "http") && port == "80") {
		return strings.ToLower(hostname)
	}
	return strings.ToLower(hostname + ":" + port)
}

func normalizeRemoteEndpoint(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "git@") {
		return normalizeHostEndpointLoose(trimmed)
	}
	return normalizeHostEndpointLoose(trimmed)
}

func parseBitbucketRemote(rawRemoteURL string) (host string, projectKey string, slug string, ok bool) {
	trimmed := strings.TrimSpace(rawRemoteURL)
	if trimmed == "" {
		return "", "", "", false
	}

	if strings.Contains(trimmed, "://") {
		parsed, err := url.Parse(trimmed)
		if err != nil {
			return "", "", "", false
		}

		host = parsed.Hostname()
		projectKey, slug, ok = parseBitbucketPath(parsed.Path)
		return host, projectKey, slug, ok
	}

	if at := strings.LastIndex(trimmed, "@"); at >= 0 {
		remainder := trimmed[at+1:]
		colon := strings.Index(remainder, ":")
		if colon < 0 {
			return "", "", "", false
		}

		host = remainder[:colon]
		path := remainder[colon+1:]
		projectKey, slug, ok = parseBitbucketPath(path)
		return host, projectKey, slug, ok
	}

	return "", "", "", false
}

func parseBitbucketPath(path string) (projectKey string, slug string, ok bool) {
	trimmed := strings.Trim(strings.TrimSpace(path), "/")
	if trimmed == "" {
		return "", "", false
	}

	parts := strings.Split(trimmed, "/")
	if len(parts) >= 3 {
		for index := 0; index+2 < len(parts); index++ {
			if strings.EqualFold(parts[index], "scm") {
				project := strings.TrimSpace(parts[index+1])
				repo := strings.TrimSuffix(strings.TrimSpace(parts[index+2]), ".git")
				if project == "" || repo == "" {
					return "", "", false
				}
				if unescaped, err := url.PathUnescape(project); err == nil {
					project = unescaped
				}
				if unescaped, err := url.PathUnescape(repo); err == nil {
					repo = unescaped
				}
				return project, repo, true
			}
		}
	}

	if len(parts) >= 2 {
		project := strings.TrimSpace(parts[len(parts)-2])
		repo := strings.TrimSuffix(strings.TrimSpace(parts[len(parts)-1]), ".git")
		if project == "" || repo == "" {
			return "", "", false
		}
		if unescaped, err := url.PathUnescape(project); err == nil {
			project = unescaped
		}
		if unescaped, err := url.PathUnescape(repo); err == nil {
			repo = unescaped
		}
		return project, repo, true
	}

	return "", "", false
}

func isNonRepositoryError(err error) bool {
	if err == nil {
		return false
	}

	message := strings.ToLower(err.Error())
	return strings.Contains(message, "not a git repository") || strings.Contains(message, "cannot find the current directory")
}

func resolveRepositorySettingsReference(selector string, cfg config.AppConfig) (reposettings.RepositoryRef, error) {
	repo, err := resolveRepositorySelector(selector, cfg)
	if err != nil {
		return reposettings.RepositoryRef{}, err
	}

	return reposettings.RepositoryRef{ProjectKey: repo.ProjectKey, Slug: repo.Slug}, nil
}

func resolveTagRepositoryReference(selector string, cfg config.AppConfig) (tagservice.RepositoryRef, error) {
	repo, err := resolveRepositorySelector(selector, cfg)
	if err != nil {
		return tagservice.RepositoryRef{}, err
	}

	return tagservice.RepositoryRef{ProjectKey: repo.ProjectKey, Slug: repo.Slug}, nil
}

func resolveBranchRepositoryReference(selector string, cfg config.AppConfig) (branchservice.RepositoryRef, error) {
	repo, err := resolveRepositorySelector(selector, cfg)
	if err != nil {
		return branchservice.RepositoryRef{}, err
	}

	return branchservice.RepositoryRef{ProjectKey: repo.ProjectKey, Slug: repo.Slug}, nil
}

func resolveQualityRepositoryReference(selector string, cfg config.AppConfig) (qualityservice.RepositoryRef, error) {
	repo, err := resolveRepositorySelector(selector, cfg)
	if err != nil {
		return qualityservice.RepositoryRef{}, err
	}

	return qualityservice.RepositoryRef{ProjectKey: repo.ProjectKey, Slug: repo.Slug}, nil
}

func resolveDiffOutputMode(patch, stat, nameOnly bool) (diffservice.OutputKind, error) {
	selected := 0
	if patch {
		selected++
	}
	if stat {
		selected++
	}
	if nameOnly {
		selected++
	}
	if selected > 1 {
		return "", apperrors.New(apperrors.KindValidation, "choose only one output mode: --patch, --stat, or --name-only", nil)
	}

	if patch {
		return diffservice.OutputKindPatch, nil
	}
	if stat {
		return diffservice.OutputKindStat, nil
	}
	if nameOnly {
		return diffservice.OutputKindNameOnly, nil
	}

	return diffservice.OutputKindRaw, nil
}

func writeDiffResult(writer io.Writer, asJSON bool, mode diffservice.OutputKind, result diffservice.Result) error {
	if asJSON {
		switch mode {
		case diffservice.OutputKindNameOnly:
			return writeJSON(writer, map[string]any{"names": result.Names})
		case diffservice.OutputKindStat:
			return writeJSON(writer, map[string]any{"stats": result.Stats})
		default:
			return writeJSON(writer, map[string]any{"patch": result.Patch})
		}
	}

	switch mode {
	case diffservice.OutputKindNameOnly:
		for _, name := range result.Names {
			fmt.Fprintln(writer, name)
		}
		return nil
	case diffservice.OutputKindStat:
		return writeJSON(writer, result.Stats)
	default:
		fmt.Fprint(writer, result.Patch)
		if result.Patch != "" && !strings.HasSuffix(result.Patch, "\n") {
			fmt.Fprintln(writer)
		}
		return nil
	}
}

func commentIDString(comment openapigenerated.RestComment) string {
	if comment.Id == nil {
		return "unknown"
	}

	return strconv.FormatInt(*comment.Id, 10)
}

func formatCommentSummary(comment openapigenerated.RestComment) string {
	text := ""
	if comment.Text != nil {
		text = strings.TrimSpace(*comment.Text)
	}
	if text == "" {
		text = "<empty>"
	}

	version := "?"
	if comment.Version != nil {
		version = strconv.Itoa(int(*comment.Version))
	}

	return fmt.Sprintf("[%s v%s] %s", commentIDString(comment), version, text)
}

func formatCommentDetail(comment openapigenerated.RestComment) string {
	lines := []string{formatCommentSummary(comment)}
	if anchorPath := commentAnchorPath(comment); anchorPath != "" {
		lines = append(lines, fmt.Sprintf("Path: %s", anchorPath))
	}
	if author := commentAuthorName(comment); author != "" {
		lines = append(lines, fmt.Sprintf("Author: %s", author))
	}
	if state := safederef.String(comment.State); state != "" {
		lines = append(lines, fmt.Sprintf("State: %s", state))
	}
	if text := strings.TrimSpace(safederef.String(comment.Text)); text != "" {
		lines = append(lines, "")
		lines = append(lines, text)
	}

	return strings.Join(lines, "\n")
}

func commentAnchorPath(comment openapigenerated.RestComment) string {
	if comment.Anchor == nil || comment.Anchor.Path == nil {
		return ""
	}
	if comment.Anchor.Path.Parent != nil && comment.Anchor.Path.Name != nil {
		parent := strings.TrimSpace(*comment.Anchor.Path.Parent)
		name := strings.TrimSpace(*comment.Anchor.Path.Name)
		if parent == "" {
			return name
		}
		if name == "" {
			return parent
		}
		return parent + "/" + name
	}
	if comment.Anchor.Path.Name != nil {
		return strings.TrimSpace(*comment.Anchor.Path.Name)
	}

	return ""
}

func commentAuthorName(comment openapigenerated.RestComment) string {
	if comment.Author == nil {
		return ""
	}
	if displayName := strings.TrimSpace(comment.Author.DisplayName); displayName != "" {
		return displayName
	}

	return strings.TrimSpace(comment.Author.Name)
}

func safeUsers(values *[]openapigenerated.RestApplicationUser) []openapigenerated.RestApplicationUser {
	if values == nil {
		return []openapigenerated.RestApplicationUser{}
	}

	return *values
}

func safeStringFromTagType(tagType *openapigenerated.RestTagType) string {
	if tagType == nil {
		return ""
	}

	return string(*tagType)
}

func safeStringFromBuildState(state *openapigenerated.RestBuildStatusState) string {
	if state == nil {
		return ""
	}

	return string(*state)
}

func safeStringFromInsightResult(result *openapigenerated.RestInsightReportResult) string {
	if result == nil {
		return ""
	}

	return string(*result)
}

// normalizeJSONShape round-trips a value through JSON so the result contains only
// plain maps, slices and scalars.
//
// It matters where a payload is echoed back to the caller: a typed value carrying
// custom marshalling would otherwise reach the output encoder untouched and print
// differently from every other command.
func normalizeJSONShape(value any) any {
	raw, err := json.Marshal(value)
	if err != nil {
		return value
	}

	var normalized any
	if err := json.Unmarshal(raw, &normalized); err != nil {
		return value
	}

	return normalized
}

func hasApprovedReviewer(reviewers []pullrequestservice.Reviewer) bool {
	for _, reviewer := range reviewers {
		if reviewer.Approved || strings.EqualFold(strings.TrimSpace(reviewer.Status), "APPROVED") {
			return true
		}
	}

	return false
}

func hasReviewer(reviewers []pullrequestservice.Reviewer, username string) bool {
	trimmed := strings.TrimSpace(username)
	for _, reviewer := range reviewers {
		if strings.EqualFold(strings.TrimSpace(reviewer.Name), trimmed) {
			return true
		}
	}

	return false
}

func reviewerApprovedByUser(reviewers []pullrequestservice.Reviewer, username string) bool {
	trimmed := strings.TrimSpace(username)
	if trimmed == "" {
		return false
	}
	for _, reviewer := range reviewers {
		if strings.EqualFold(strings.TrimSpace(reviewer.Name), trimmed) && (reviewer.Approved || strings.EqualFold(strings.TrimSpace(reviewer.Status), "APPROVED")) {
			return true
		}
	}
	return false
}
