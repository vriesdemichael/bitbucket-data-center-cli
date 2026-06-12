package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/git"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/transport/httpclient"
	"golang.org/x/term"
)

func newRepoCloneCommand(options *rootOptions) *cobra.Command {
	return newCloneCommand(options)
}

func newCloneCommand(options *rootOptions) *cobra.Command {
	var noUpstream bool
	var upstreamRemoteName string
	var forceSSH bool
	var forceHTTPS bool

	cmd := &cobra.Command{
		Use:   "clone <repository> [directory] [-- <gitflags>...]",
		Short: "Clone a repository to the local filesystem",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}

			transportMode, err := resolveCloneTransportMode(forceSSH, forceHTTPS)
			if err != nil {
				return err
			}

			repo, cloneHost, usedURLInput, err := resolveRepositoryCloneInput(args[0], cfg)
			if err != nil {
				return err
			}

			directory, extraInput := splitCloneDirectoryAndExtraArgs(repo.Slug, args[1:])
			if directory == "" {
				return apperrors.New(apperrors.KindValidation, "clone directory cannot be empty", nil)
			}

			extraCloneArgs, err := normalizeCloneExtraArgs(extraInput)
			if err != nil {
				return err
			}

			resolvedUpstreamName, err := normalizeUpstreamRemoteName(upstreamRemoteName, repo.ProjectKey)
			if err != nil {
				return err
			}

			backend := gitBackendFactory()
			if backend == nil {
				return apperrors.New(apperrors.KindInternal, "git backend is not configured", nil)
			}

			var cloneURL string
			cloneURL, err = cloneRepositoryWithAuthFallback(
				cmd,
				cfg,
				args[0],
				usedURLInput,
				cloneHost,
				repo,
				transportMode,
				git.CloneOptions{
					Directory: directory,
					ExtraArgs: extraCloneArgs,
				},
				backend,
				options.JSON,
			)
			if err != nil {
				return err
			}

			upstreamURL := ""
			upstreamOwner := ""
			upstreamAdded := false
			if !noUpstream {
				upstreamOwner, upstreamURL, err = lookupParentCloneURL(cmd.Context(), cfg, cloneHost, repo)
				if strings.TrimSpace(upstreamURL) != "" {
					if strings.EqualFold(strings.TrimSpace(upstreamRemoteName), "@owner") && strings.TrimSpace(upstreamOwner) != "" {
						resolvedUpstreamName = strings.ToLower(strings.TrimSpace(upstreamOwner))
					}
					if err := backend.AddRemote(cmd.Context(), directory, git.Remote{Name: resolvedUpstreamName, URL: upstreamURL}); err != nil {
						return err
					}
					upstreamAdded = true
				}
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{
					"status": "ok",
					"repository": map[string]string{
						"project_key": repo.ProjectKey,
						"slug":        repo.Slug,
					},
					"clone_url":   cloneURL,
					"directory":   directory,
					"no_upstream": noUpstream,
					"upstream": map[string]any{
						"configured": upstreamAdded,
						"name":       resolvedUpstreamName,
						"url":        upstreamURL,
					},
					"repository_input": map[string]any{
						"raw":      args[0],
						"used_url": usedURLInput,
					},
				})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Cloned %s/%s into %s\n", repo.ProjectKey, repo.Slug, directory)
			if upstreamAdded {
				fmt.Fprintf(cmd.OutOrStdout(), "Added remote %q -> %s\n", resolvedUpstreamName, upstreamURL)
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&forceSSH, "ssh", false, "Use SSH only and disable HTTPS fallback")
	cmd.Flags().BoolVar(&forceHTTPS, "https", false, "Use HTTPS only and skip the SSH clone attempt")
	cmd.Flags().BoolVar(&noUpstream, "no-upstream", false, "Do not add an upstream remote when cloning a fork")
	cmd.Flags().StringVarP(&upstreamRemoteName, "upstream-remote-name", "u", "upstream", "Upstream remote name when cloning a fork")
	cmd.Flags().SetInterspersed(false)

	return cmd
}

func resolveRepositoryCloneInput(input string, cfg config.AppConfig) (repositorySelector, string, bool, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return repositorySelector{}, "", false, apperrors.New(apperrors.KindValidation, "repository is required", nil)
	}

	host, projectKey, slug, parsed := parseBitbucketRemote(trimmed)
	if parsed {
		return repositorySelector{ProjectKey: projectKey, Slug: slug}, normalizeCloneHost(trimmed, host), true, nil
	}

	host, repoFromHostSelector, ok := parseHostQualifiedRepositorySelector(trimmed)
	if ok {
		hostURL := host
		if !strings.Contains(hostURL, "://") {
			hostURL = "https://" + hostURL
		}
		return repoFromHostSelector, hostURL, true, nil
	}

	repo, parsedSelector, err := parseCloneSelector(trimmed, cfg.ProjectKey)
	if err != nil {
		return repositorySelector{}, "", false, err
	}

	if strings.TrimSpace(cfg.BitbucketURL) == "" {
		if parsedSelector == cloneSelectorProjectSlug {
			return repositorySelector{}, "", false, apperrors.New(apperrors.KindValidation, "BITBUCKET_URL is required to clone repositories by PROJECT/slug", nil)
		}
		return repositorySelector{}, "", false, apperrors.New(apperrors.KindValidation, "BITBUCKET_URL is required to clone repositories by slug", nil)
	}

	return repo, strings.TrimSpace(cfg.BitbucketURL), false, nil
}

type cloneSelectorKind int

type cloneTransportMode int

const (
	cloneSelectorProjectSlug cloneSelectorKind = iota
	cloneSelectorSlugOnly
)

const (
	cloneTransportAuto cloneTransportMode = iota
	cloneTransportSSH
	cloneTransportHTTPS
)

func parseCloneSelector(value, defaultProjectKey string) (repositorySelector, cloneSelectorKind, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return repositorySelector{}, cloneSelectorProjectSlug, apperrors.New(apperrors.KindValidation, "repository must be in PROJECT/slug format or a slug with BITBUCKET_PROJECT_KEY set", nil)
	}

	parts := strings.Split(trimmed, "/")
	if len(parts) == 1 {
		project := strings.TrimSpace(defaultProjectKey)
		if project == "" {
			return repositorySelector{}, cloneSelectorSlugOnly, apperrors.New(apperrors.KindValidation, "repository must be in PROJECT/slug format when BITBUCKET_PROJECT_KEY is not set", nil)
		}
		return repositorySelector{ProjectKey: project, Slug: trimmed}, cloneSelectorSlugOnly, nil
	}

	if len(parts) != 2 {
		return repositorySelector{}, cloneSelectorProjectSlug, apperrors.New(apperrors.KindValidation, "repository must be in PROJECT/slug format", nil)
	}

	projectKey := strings.TrimSpace(parts[0])
	slug := strings.TrimSpace(parts[1])
	if projectKey == "" || slug == "" {
		return repositorySelector{}, cloneSelectorProjectSlug, apperrors.New(apperrors.KindValidation, "repository must be in PROJECT/slug format", nil)
	}

	return repositorySelector{ProjectKey: projectKey, Slug: slug}, cloneSelectorProjectSlug, nil
}

func resolveCloneTransportMode(forceSSH bool, forceHTTPS bool) (cloneTransportMode, error) {
	if forceSSH && forceHTTPS {
		return cloneTransportAuto, apperrors.New(apperrors.KindValidation, "--ssh and --https cannot be used together", nil)
	}
	if forceSSH {
		return cloneTransportSSH, nil
	}
	if forceHTTPS {
		return cloneTransportHTTPS, nil
	}
	return cloneTransportAuto, nil
}

func resolveHTTPCloneURL(rawInput string, usedURLInput bool, cloneHost string, repo repositorySelector) (string, error) {
	if usedURLInput && isExplicitHTTPCloneURL(rawInput) {
		return strings.TrimSpace(rawInput), nil
	}
	return buildBitbucketCloneURL(normalizeHTTPCloneHost(cloneHost), repo.ProjectKey, repo.Slug)
}

func cloneRepositoryWithAuthFallback(
	cmd *cobra.Command,
	cfg config.AppConfig,
	rawInput string,
	usedURLInput bool,
	cloneHost string,
	repo repositorySelector,
	transportMode cloneTransportMode,
	cloneOptions git.CloneOptions,
	backend git.Backend,
	jsonOutput bool,
) (string, error) {
	httpCloneURL, err := resolveHTTPCloneURL(rawInput, usedURLInput, cloneHost, repo)
	if err != nil {
		return "", err
	}

	sshCloneURL, hasSSHCloneURL, err := resolveSSHCloneURL(rawInput, usedURLInput, cloneHost, repo)
	if err != nil {
		return "", err
	}

	var sshErr error
	if transportMode != cloneTransportHTTPS && hasSSHCloneURL {
		sshErr = backend.Clone(cmd.Context(), sshCloneURL, cloneOptions)
		if sshErr == nil {
			return sshCloneURL, nil
		}
	}
	if transportMode == cloneTransportSSH {
		return "", sshErr
	}

	cloneAuth, resolvedHTTPCloneHost, hasStoredHTTPAuth, err := resolveCloneHTTPAuth(cfg, cloneHost)
	if err != nil {
		return "", err
	}
	if hasStoredHTTPAuth && normalizeHTTPCloneBaseURL(httpCloneURL) != normalizeHTTPCloneBaseURL(resolvedHTTPCloneHost) {
		httpCloneURL, err = buildBitbucketCloneURL(normalizeHTTPCloneBaseURL(resolvedHTTPCloneHost), repo.ProjectKey, repo.Slug)
		if err != nil {
			return "", err
		}
	}

	if hasStoredHTTPAuth {
		opts := cloneOptions
		if cloneAuth.AuthMode() == "token" {
			opts.AuthToken = cloneAuth.BitbucketToken
		} else if cloneAuth.AuthMode() == "basic" {
			opts.AuthUsername = cloneAuth.BitbucketUsername
			opts.AuthPassword = cloneAuth.BitbucketPassword
		}

		if err := backend.Clone(cmd.Context(), httpCloneURL, opts); err == nil {
			return httpCloneURL, nil
		} else {
			return "", err
		}
	}

	if jsonOutput || !canPromptForCloneLoginFunc(cmd.InOrStdin()) {
		return "", newCloneLoginRequiredError(cloneHost, sshErr, transportMode == cloneTransportAuto)
	}

	promptedAuth, prompted, err := promptForCloneLogin(cmd, cfg, cloneHost, transportMode == cloneTransportAuto)
	if err != nil {
		return "", err
	}
	if !prompted {
		return "", newCloneLoginRequiredError(cloneHost, sshErr, transportMode == cloneTransportAuto)
	}

	opts := cloneOptions
	if promptedAuth.AuthMode() == "token" {
		opts.AuthToken = promptedAuth.BitbucketToken
	} else if promptedAuth.AuthMode() == "basic" {
		opts.AuthUsername = promptedAuth.BitbucketUsername
		opts.AuthPassword = promptedAuth.BitbucketPassword
	}

	if err := backend.Clone(cmd.Context(), httpCloneURL, opts); err != nil {
		return "", err
	}

	return httpCloneURL, nil
}

func resolveCloneHTTPAuth(cfg config.AppConfig, cloneHost string) (config.AppConfig, string, bool, error) {
	// Match on the normalized network endpoint so explicit ports and http↔https variants
	// of the same server can reuse the same stored credentials.
	if sameCloneHost(cfg.BitbucketURL, cloneHost) && cfg.AuthMode() != "none" {
		return cfg, cfg.BitbucketURL, true, nil
	}

	if os.Getenv("BB_DISABLE_STORED_CONFIG") == "1" {
		return config.AppConfig{}, "", false, nil
	}

	if matched, ok, err := config.MatchStoredHost(cloneHost); err != nil {
		return config.AppConfig{}, "", false, err
	} else if ok {
		storedAuth, found, err := config.LoadStoredAuthForHost(matched.Host)
		if err != nil {
			return config.AppConfig{}, "", false, err
		}
		if found && storedAuth.AuthMode() != "none" {
			return storedAuth, matched.Host, true, nil
		}
	}

	storedAuth, ok, err := config.LoadStoredAuthForHost(cloneHost)
	if err != nil {
		return config.AppConfig{}, "", false, err
	}
	if !ok || storedAuth.AuthMode() == "none" {
		return config.AppConfig{}, "", false, nil
	}

	return storedAuth, storedAuth.BitbucketURL, true, nil
}

func promptForCloneLogin(cmd *cobra.Command, cfg config.AppConfig, cloneHost string, attemptedSSH bool) (config.AppConfig, bool, error) {
	input := cmd.InOrStdin()
	output := cmd.ErrOrStderr()
	if attemptedSSH {
		fmt.Fprintf(output, "SSH clone failed and no stored HTTP credentials were found for %s.\n", cloneHost)
	} else {
		fmt.Fprintf(output, "No stored HTTP credentials were found for %s.\n", cloneHost)
	}
	fmt.Fprintf(output, "Create a token with `bb auth token-url --host %s`, then paste it to store and retry.\n", cloneHost)
	fmt.Fprint(output, "Token: ")

	tokenValue, err := readCloneToken(input, output)
	if err != nil {
		return config.AppConfig{}, false, apperrors.New(apperrors.KindAuthentication, "failed to read clone login token", err)
	}
	if strings.TrimSpace(tokenValue) == "" {
		return config.AppConfig{}, false, nil
	}

	// Preserve the full REST base URL (including context path) when the configured
	// server URL resolves to the same host as the clone URL.
	saveHost := cloneHost
	if sameCloneHost(cfg.BitbucketURL, cloneHost) {
		saveHost = cfg.BitbucketURL
	}

	if _, err := config.SaveLogin(config.LoginInput{Host: saveHost, Token: tokenValue, SetDefault: false}); err != nil {
		return config.AppConfig{}, false, err
	}

	savedCfg := cfg
	savedCfg.BitbucketToken = tokenValue
	return savedCfg, true, nil
}

func canPromptForCloneLogin(input io.Reader) bool {
	file, ok := input.(*os.File)
	if !ok {
		// Non-file readers (pipes, test buffers, etc.) cannot be verified as a TTY,
		// so conservatively disable prompting.
		return false
	}

	return term.IsTerminal(int(file.Fd()))
}

func readCloneToken(input io.Reader, output io.Writer) (string, error) {
	if file, ok := input.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		value, err := term.ReadPassword(int(file.Fd()))
		fmt.Fprintln(output)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(value)), nil
	}

	reader := bufio.NewReader(input)
	value, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.TrimSpace(value), nil
}

func newCloneLoginRequiredError(cloneHost string, cause error, attemptedSSH bool) error {
	message := fmt.Sprintf(
		"no stored HTTP credentials are configured for %s; run 'bb auth login %s --token <token>' and retry",
		cloneHost,
		cloneHost,
	)
	if attemptedSSH {
		message = fmt.Sprintf(
			"ssh clone failed and %s",
			message,
		)
	}
	return apperrors.New(apperrors.KindAuthentication, message, cause)
}

func resolveSSHCloneURL(rawInput string, usedURLInput bool, cloneHost string, repo repositorySelector) (string, bool, error) {
	if usedURLInput {
		trimmed := strings.TrimSpace(rawInput)
		if strings.HasPrefix(trimmed, "git@") || strings.HasPrefix(trimmed, "ssh://") {
			return trimmed, true, nil
		}
	}

	sshCloneURL, err := buildBitbucketSSHCloneURL(cloneHost, repo.ProjectKey, repo.Slug)
	if err != nil {
		return "", false, err
	}
	return sshCloneURL, true, nil
}


func isExplicitCloneURL(rawInput string) bool {
	trimmed := strings.TrimSpace(rawInput)
	return strings.Contains(trimmed, "://") || strings.HasPrefix(trimmed, "git@")
}

func isExplicitHTTPCloneURL(rawInput string) bool {
	trimmed := strings.TrimSpace(rawInput)
	return strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://")
}

// sameCloneHost returns true when left and right normalize to the same network
// endpoint. Schemes are ignored, so http and https variants of the same host match.
func sameCloneHost(left string, right string) bool {
	leftNormalized := normalizeHostEndpointLoose(left)
	rightNormalized := normalizeHostEndpointLoose(right)
	return leftNormalized != "" && leftNormalized == rightNormalized
}

func splitCloneDirectoryAndExtraArgs(defaultDirectory string, values []string) (string, []string) {
	trimmedDefault := strings.TrimSpace(defaultDirectory)
	if len(values) == 0 {
		return trimmedDefault, nil
	}

	first := strings.TrimSpace(values[0])
	if first == "" {
		return "", values[1:]
	}
	if first == "--" || strings.HasPrefix(first, "-") {
		return trimmedDefault, values
	}

	return first, values[1:]
}

func normalizeCloneHost(rawInput, parsedHost string) string {
	trimmed := strings.TrimSpace(rawInput)
	if strings.Contains(trimmed, "://") {
		parsed, err := url.Parse(trimmed)
		if err == nil && strings.TrimSpace(parsed.Scheme) != "" && strings.TrimSpace(parsed.Host) != "" {
			if strings.EqualFold(parsed.Scheme, "ssh") {
				parsed.User = nil
				parsed.Scheme = "https"
			}
			parsed.Path = ""
			parsed.RawQuery = ""
			parsed.Fragment = ""
			return strings.TrimSuffix(parsed.String(), "/")
		}
	}

	host := strings.TrimSpace(parsedHost)
	if host == "" {
		return ""
	}

	return "https://" + host
}

func normalizeHTTPCloneHost(cloneHost string) string {
	parsed, err := url.Parse(strings.TrimSpace(cloneHost))
	if err != nil || strings.TrimSpace(parsed.Host) == "" {
		return cloneHost
	}

	parsed.User = nil
	if !strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https") {
		parsed.Scheme = "https"
	}
	parsed.Path = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""

	return strings.TrimSuffix(parsed.String(), "/")
}

func normalizeHTTPCloneBaseURL(baseURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || strings.TrimSpace(parsed.Host) == "" {
		return baseURL
	}

	parsed.User = nil
	if !strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https") {
		parsed.Scheme = "https"
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""

	return strings.TrimSuffix(parsed.String(), "/")
}

func normalizeCloneExtraArgs(extra []string) ([]string, error) {
	if len(extra) == 0 {
		return nil, nil
	}

	result := make([]string, 0, len(extra))
	for _, value := range extra {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" || trimmed == "--" {
			continue
		}
		if !strings.HasPrefix(trimmed, "-") {
			return nil, apperrors.New(apperrors.KindValidation, "additional git clone arguments must be passed after --", nil)
		}
		result = append(result, trimmed)
	}

	return result, nil
}

func normalizeUpstreamRemoteName(name string, fallbackOwner string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", apperrors.New(apperrors.KindValidation, "--upstream-remote-name cannot be empty", nil)
	}
	if trimmed == "@owner" {
		if strings.TrimSpace(fallbackOwner) == "" {
			return "owner", nil
		}
		return strings.ToLower(strings.TrimSpace(fallbackOwner)), nil
	}
	if strings.Contains(trimmed, " ") {
		return "", apperrors.New(apperrors.KindValidation, "--upstream-remote-name cannot contain spaces", nil)
	}
	return trimmed, nil
}

func lookupParentCloneURL(ctx context.Context, cfg config.AppConfig, cloneHost string, repo repositorySelector) (string, string, error) {
	probeCfg := cfg
	probeCfg.BitbucketURL = cloneHost

	client := httpclient.NewFromConfig(probeCfg)
	path := fmt.Sprintf("/rest/api/1.0/projects/%s/repos/%s", url.PathEscape(repo.ProjectKey), url.PathEscape(repo.Slug))

	var response struct {
		Origin *struct {
			Project *struct {
				Key string `json:"key"`
			} `json:"project"`
			Slug string `json:"slug"`
		} `json:"origin"`
	}

	err := client.GetJSON(ctx, path, nil, &response)
	if err != nil {
		return "", "", nil
	}

	if response.Origin == nil || response.Origin.Project == nil {
		return "", "", nil
	}
	if strings.TrimSpace(response.Origin.Project.Key) == "" || strings.TrimSpace(response.Origin.Slug) == "" {
		return "", "", nil
	}

	parentURL, err := buildBitbucketCloneURL(cloneHost, response.Origin.Project.Key, response.Origin.Slug)
	if err != nil {
		return "", "", err
	}

	return response.Origin.Project.Key, parentURL, nil
}

func buildBitbucketCloneURL(baseURL, projectKey, slug string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || strings.TrimSpace(parsed.Scheme) == "" || strings.TrimSpace(parsed.Host) == "" {
		return "", apperrors.New(apperrors.KindValidation, "BITBUCKET_URL must include a valid scheme and host", err)
	}

	trimmedProject := strings.TrimSpace(projectKey)
	trimmedSlug := strings.TrimSpace(slug)
	if trimmedProject == "" || trimmedSlug == "" {
		return "", apperrors.New(apperrors.KindValidation, "repository selector must include project key and slug", nil)
	}

	basePath := strings.TrimSuffix(parsed.Path, "/")
	parsed.Path = fmt.Sprintf("%s/scm/%s/%s.git", basePath, url.PathEscape(trimmedProject), url.PathEscape(trimmedSlug))
	parsed.RawQuery = ""
	parsed.Fragment = ""

	return parsed.String(), nil
}

func buildBitbucketSSHCloneURL(baseURL, projectKey, slug string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || strings.TrimSpace(parsed.Scheme) == "" || strings.TrimSpace(parsed.Host) == "" {
		return "", apperrors.New(apperrors.KindValidation, "BITBUCKET_URL must include a valid scheme and host", err)
	}

	trimmedProject := strings.TrimSpace(projectKey)
	trimmedSlug := strings.TrimSpace(slug)
	if trimmedProject == "" || trimmedSlug == "" {
		return "", apperrors.New(apperrors.KindValidation, "repository selector must include project key and slug", nil)
	}

	host := parsed.Hostname()
	if strings.TrimSpace(host) == "" {
		return "", apperrors.New(apperrors.KindValidation, "BITBUCKET_URL must include a valid host", nil)
	}

	return fmt.Sprintf("git@%s:scm/%s/%s.git", host, url.PathEscape(trimmedProject), url.PathEscape(trimmedSlug)), nil
}
