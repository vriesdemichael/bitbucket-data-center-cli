package cli

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/git"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/git/execgit"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
	branchservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/branch"
	commentservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/comment"
	diffservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/diff"
	pullrequestservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/pullrequest"
	pullrequestactivityservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/pullrequestactivity"
	qualityservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/quality"
	reposettings "github.com/vriesdemichael/bitbucket-server-cli/internal/services/reposettings"
	tagservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/tag"
)

var gitBackendFactory = func() git.Backend {
	return execgit.New()
}

// canPromptForCloneLoginFunc is a variable so tests can override the TTY guard.
var canPromptForCloneLoginFunc = func(r io.Reader) bool {
	return canPromptForCloneLogin(r)
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
		repoSlug := strings.TrimSpace(os.Getenv("BITBUCKET_REPO_SLUG"))
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

func applyInferredRepositoryContext(cmd *cobra.Command, asJSON bool) error {
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

	if err := os.Setenv("BITBUCKET_URL", inferred.Host); err != nil {
		return apperrors.New(apperrors.KindInternal, "failed to set inferred host", err)
	}
	if err := os.Setenv("BITBUCKET_PROJECT_KEY", inferred.ProjectKey); err != nil {
		return apperrors.New(apperrors.KindInternal, "failed to set inferred project key", err)
	}
	if err := os.Setenv("BITBUCKET_REPO_SLUG", inferred.Slug); err != nil {
		return apperrors.New(apperrors.KindInternal, "failed to set inferred repository slug", err)
	}

	repoValue := fmt.Sprintf("%s/%s", inferred.ProjectKey, inferred.Slug)
	if err := repoFlag.Value.Set(repoValue); err != nil {
		return apperrors.New(apperrors.KindInternal, "failed to apply inferred repository to --repo flag", err)
	}
	repoFlag.Changed = true

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

func resolvePullRequestRepositoryReference(selector string, cfg config.AppConfig) (pullrequestservice.RepositoryRef, error) {
	repo, err := resolveRepositorySelector(selector, cfg)
	if err != nil {
		return pullrequestservice.RepositoryRef{}, err
	}

	return pullrequestservice.RepositoryRef{ProjectKey: repo.ProjectKey, Slug: repo.Slug}, nil
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

func resolveCommentTarget(selector string, commitID string, pullRequestID string, cfg config.AppConfig) (commentservice.Target, error) {
	repo, err := resolveRepositorySelector(selector, cfg)
	if err != nil {
		return commentservice.Target{}, err
	}

	trimmedCommitID := strings.TrimSpace(commitID)
	trimmedPullRequestID := strings.TrimSpace(pullRequestID)
	hasCommit := trimmedCommitID != ""
	hasPullRequest := trimmedPullRequestID != ""

	if hasCommit == hasPullRequest {
		return commentservice.Target{}, apperrors.New(apperrors.KindValidation, "exactly one of --commit or --pr is required", nil)
	}

	return commentservice.Target{
		Repository:    commentservice.RepositoryRef{ProjectKey: repo.ProjectKey, Slug: repo.Slug},
		CommitID:      trimmedCommitID,
		PullRequestID: trimmedPullRequestID,
	}, nil
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
	if state := safeString(comment.State); state != "" {
		lines = append(lines, fmt.Sprintf("State: %s", state))
	}
	if text := strings.TrimSpace(safeString(comment.Text)); text != "" {
		lines = append(lines, "")
		lines = append(lines, text)
	}

	return strings.Join(lines, "\n")
}

func formatPullRequestActivitySummary(activity pullrequestactivityservice.Activity) string {
	action := strings.TrimSpace(activity.Action)
	if action == "" {
		action = "UNKNOWN"
	}
	if activity.Comment != nil {
		return fmt.Sprintf("[%d %s] %s", activity.ID, action, formatCommentSummary(*activity.Comment))
	}

	return fmt.Sprintf("[%d %s]", activity.ID, action)
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
	if comment.Author.DisplayName != nil && strings.TrimSpace(*comment.Author.DisplayName) != "" {
		return strings.TrimSpace(*comment.Author.DisplayName)
	}
	if comment.Author.Name != nil {
		return strings.TrimSpace(*comment.Author.Name)
	}

	return ""
}

func safeString(value *string) string {
	if value == nil {
		return ""
	}

	return *value
}

func safeInt32(value *int32) int32 {
	if value == nil {
		return 0
	}

	return *value
}

func safeInt64(value *int64) int64 {
	if value == nil {
		return 0
	}

	return *value
}

func safeStringSlice(values *[]string) []string {
	if values == nil {
		return []string{}
	}

	return *values
}

func safeUsers(values *[]openapigenerated.RestApplicationUser) []openapigenerated.RestApplicationUser {
	if values == nil {
		return []openapigenerated.RestApplicationUser{}
	}

	return *values
}

func normalizeAccessKeyIDs(values []int) ([]int32, error) {
	const maxInt32Value = int(^uint32(0) >> 1)

	normalized := make([]int32, 0, len(values))
	for _, value := range values {
		if value < 0 || value > maxInt32Value {
			return nil, apperrors.New(apperrors.KindValidation, "access-key-id must be between 0 and 2147483647", nil)
		}
		normalized = append(normalized, int32(value))
	}

	return normalized, nil
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
