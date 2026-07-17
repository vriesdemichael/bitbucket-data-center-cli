package cli

import (
	"fmt"
	"net/url"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
)

var browseURLOpener = openInBrowser
var browseExecCommand = exec.Command

var commitSHARegex = regexp.MustCompile(`^[0-9a-fA-F]{7,40}$`)

type browseTargetKind string

const (
	browseTargetHome     browseTargetKind = "home"
	browseTargetSettings browseTargetKind = "settings"
	browseTargetReleases browseTargetKind = "releases"
	browseTargetPR       browseTargetKind = "pull_request"
	browseTargetCommit   browseTargetKind = "commit"
	browseTargetPath     browseTargetKind = "path"
)

func newBrowseCommand(options *rootOptions) *cobra.Command {
	var repositorySelector string
	var branch string
	var commit string
	var noBrowser bool
	var settings bool
	var releases bool
	var blame bool

	cmd := &cobra.Command{
		Use:   "browse [<number> | <path> | <commit-sha>]",
		Short: "Open repository pages in a web browser",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}

			repoRef, hostOverride, err := resolveBrowseRepositoryReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			target, err := resolveBrowseTarget(args, browseResolveOptions{
				settings: settings,
				releases: releases,
				blame:    blame,
				branch:   branch,
				commit:   commit,
			})
			if err != nil {
				return err
			}

			targetURL, err := buildBitbucketBrowseURL(hostOverride, repoRef.ProjectKey, repoRef.Slug, target)
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{
					"url": targetURL,
					"repository": map[string]string{
						"project_key": repoRef.ProjectKey,
						"slug":        repoRef.Slug,
					},
					"target": map[string]any{
						"kind":   string(target.kind),
						"arg":    target.rawArg,
						"number": target.number,
						"path":   target.path,
						"line":   target.line,
						"branch": target.branch,
						"commit": target.commit,
						"blame":  target.blame,
					},
				})
			}

			if noBrowser {
				fmt.Fprintln(cmd.OutOrStdout(), targetURL)
				return nil
			}

			if err := browseURLOpener(targetURL); err != nil {
				return err
			}

			fmt.Fprintln(cmd.OutOrStdout(), targetURL)
			return nil
		},
	}

	cmd.Flags().StringVarP(&repositorySelector, "repo", "R", "", "Repository as [HOST/]PROJECT/slug (defaults to inferred repository context)")
	cmd.Flags().StringVarP(&branch, "branch", "b", "", "Select another branch")
	cmd.Flags().StringVarP(&commit, "commit", "c", "", "Select another commit")
	cmd.Flags().BoolVarP(&noBrowser, "no-browser", "n", false, "Print destination URL instead of opening the browser")
	cmd.Flags().BoolVarP(&settings, "settings", "s", false, "Open repository settings")
	cmd.Flags().BoolVarP(&releases, "releases", "r", false, "Open repository tags/releases view")
	cmd.Flags().BoolVar(&blame, "blame", false, "Open blame view for a file")

	return cmd
}

type browseResolveOptions struct {
	settings bool
	releases bool
	blame    bool
	branch   string
	commit   string
}

type browseTarget struct {
	kind   browseTargetKind
	rawArg string

	path   string
	line   int
	number int
	branch string
	commit string
	blame  bool
}

func resolveBrowseTarget(args []string, opts browseResolveOptions) (browseTarget, error) {
	selectedQuick := 0
	if opts.settings {
		selectedQuick++
	}
	if opts.releases {
		selectedQuick++
	}
	if selectedQuick > 1 {
		return browseTarget{}, apperrors.New(apperrors.KindValidation, "choose only one of --settings or --releases", nil)
	}

	rawArg := ""
	if len(args) > 0 {
		rawArg = strings.TrimSpace(args[0])
	}

	target := browseTarget{
		kind:   browseTargetHome,
		rawArg: rawArg,
		branch: strings.TrimSpace(opts.branch),
		commit: strings.TrimSpace(opts.commit),
		blame:  opts.blame,
	}

	if target.branch != "" && target.commit != "" {
		return browseTarget{}, apperrors.New(apperrors.KindValidation, "--branch and --commit cannot be used together", nil)
	}

	if opts.settings {
		target.kind = browseTargetSettings
	} else if opts.releases {
		target.kind = browseTargetReleases
	}

	if target.kind != browseTargetHome {
		if rawArg != "" {
			return browseTarget{}, apperrors.New(apperrors.KindValidation, "positional argument cannot be used with --settings/--releases", nil)
		}
		if target.blame {
			return browseTarget{}, apperrors.New(apperrors.KindValidation, "--blame requires a file path argument", nil)
		}
		if target.branch != "" || target.commit != "" {
			return browseTarget{}, apperrors.New(apperrors.KindValidation, "--branch/--commit are only valid for repository content browsing", nil)
		}
		return target, nil
	}

	if rawArg == "" {
		if target.blame {
			return browseTarget{}, apperrors.New(apperrors.KindValidation, "--blame requires a file path argument", nil)
		}
		if target.commit != "" {
			target.kind = browseTargetCommit
		}
		return target, nil
	}

	if id, err := strconv.Atoi(rawArg); err == nil && id > 0 {
		if target.commit != "" || target.branch != "" || target.blame {
			return browseTarget{}, apperrors.New(apperrors.KindValidation, "number targets cannot be combined with --branch, --commit, or --blame", nil)
		}
		target.kind = browseTargetPR
		target.number = id
		return target, nil
	}

	if commitSHARegex.MatchString(rawArg) {
		if target.branch != "" || target.blame {
			return browseTarget{}, apperrors.New(apperrors.KindValidation, "commit targets cannot be combined with --branch or --blame", nil)
		}
		target.kind = browseTargetCommit
		if target.commit == "" {
			target.commit = rawArg
		}
		return target, nil
	}

	target.kind = browseTargetPath
	target.path, target.line = splitPathAndLine(rawArg)
	return target, nil
}

func splitPathAndLine(input string) (string, int) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return "", 0
	}

	lastColon := strings.LastIndex(trimmed, ":")
	if lastColon <= 0 || lastColon == len(trimmed)-1 {
		return trimmed, 0
	}

	lineValue := strings.TrimSpace(trimmed[lastColon+1:])
	line, err := strconv.Atoi(lineValue)
	if err != nil || line <= 0 {
		return trimmed, 0
	}

	return strings.TrimSpace(trimmed[:lastColon]), line
}

func buildBitbucketBrowseURL(baseURL, projectKey, slug string, target browseTarget) (string, error) {
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
	repoPrefix := fmt.Sprintf("%s/projects/%s/repos/%s", basePath, url.PathEscape(trimmedProject), url.PathEscape(trimmedSlug))

	switch target.kind {
	case browseTargetSettings:
		parsed.Path = repoPrefix + "/settings"
	case browseTargetReleases:
		parsed.Path = repoPrefix + "/tags"
	case browseTargetPR:
		parsed.Path = fmt.Sprintf("%s/pull-requests/%d", repoPrefix, target.number)
	case browseTargetCommit:
		parsed.Path = fmt.Sprintf("%s/commits/%s", repoPrefix, url.PathEscape(target.commit))
	case browseTargetPath:
		pathValue := strings.TrimPrefix(strings.TrimSpace(target.path), "/")
		parsed.Path = fmt.Sprintf("%s/browse/%s", repoPrefix, encodePathSegments(pathValue))
		query := parsed.Query()
		query.Del("at")
		if strings.TrimSpace(target.commit) != "" {
			query.Set("at", strings.TrimSpace(target.commit))
		} else if strings.TrimSpace(target.branch) != "" {
			query.Set("at", strings.TrimSpace(target.branch))
		}
		if target.line > 0 {
			query.Set("line", strconv.Itoa(target.line))
		}
		if target.blame {
			query.Set("blame", "true")
		}
		parsed.RawQuery = query.Encode()
	default:
		parsed.Path = repoPrefix
		query := parsed.Query()
		query.Del("at")
		if strings.TrimSpace(target.branch) != "" {
			query.Set("at", strings.TrimSpace(target.branch))
		}
		parsed.RawQuery = query.Encode()
	}

	if target.kind != browseTargetPath && target.kind != browseTargetHome {
		parsed.RawQuery = ""
	}

	parsed.Fragment = ""
	return parsed.String(), nil
}

func encodePathSegments(pathValue string) string {
	if strings.TrimSpace(pathValue) == "" {
		return ""
	}
	parts := strings.Split(pathValue, "/")
	encoded := make([]string, 0, len(parts))
	for _, part := range parts {
		segment := strings.TrimSpace(part)
		if segment == "" {
			continue
		}
		encoded = append(encoded, url.PathEscape(segment))
	}
	return strings.Join(encoded, "/")
}

func resolveBrowseRepositoryReference(selector string, cfg config.AppConfig) (repositorySelector, string, error) {
	trimmed := strings.TrimSpace(selector)
	if trimmed == "" {
		repo, err := resolveRepositorySelector("", cfg)
		if err != nil {
			return repositorySelector{}, "", err
		}
		return repo, cfg.BitbucketURL, nil
	}

	host, repo, ok := parseHostQualifiedRepositorySelector(trimmed)
	if ok {
		hostURL := host
		if !strings.Contains(hostURL, "://") {
			hostURL = "https://" + hostURL
		}
		return repo, hostURL, nil
	}

	repo, err := parseRepositorySelector(trimmed)
	if err != nil {
		return repositorySelector{}, "", err
	}

	return repo, cfg.BitbucketURL, nil
}

func parseHostQualifiedRepositorySelector(value string) (string, repositorySelector, bool) {
	trimmed := strings.Trim(strings.TrimSpace(value), "/")
	parts := strings.Split(trimmed, "/")
	if len(parts) != 3 {
		return "", repositorySelector{}, false
	}

	host := strings.TrimSpace(parts[0])
	project := strings.TrimSpace(parts[1])
	slug := strings.TrimSpace(parts[2])
	if host == "" || project == "" || slug == "" {
		return "", repositorySelector{}, false
	}

	if unescaped, err := url.PathUnescape(project); err == nil {
		project = unescaped
	}
	if unescaped, err := url.PathUnescape(slug); err == nil {
		slug = unescaped
	}

	return host, repositorySelector{ProjectKey: project, Slug: slug}, true
}

func openInBrowser(target string) error {
	commandName, commandArgs := browserCommand(runtime.GOOS, target)
	command := browseExecCommand(commandName, commandArgs...)

	if err := command.Run(); err != nil {
		return apperrors.New(apperrors.KindPermanent, fmt.Sprintf("failed to open browser for %s", target), err)
	}

	return nil
}

func browserCommand(goos, target string) (string, []string) {
	switch goos {
	case "darwin":
		return "open", []string{target}
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", target}
	default:
		return "xdg-open", []string{target}
	}
}
