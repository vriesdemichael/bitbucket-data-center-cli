package reposel

import (
	"os"
	"strings"

	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/giturl"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
)

// Parse splits a "PROJECT/slug" selector or Bitbucket URL into its project and slug components.
func Parse(selector string) (projectKey, slug string, err error) {
	trimmed := strings.TrimSpace(selector)
	if trimmed == "" {
		return "", "", apperrors.New(
			apperrors.KindValidation,
			"invalid repository selector (expected PROJECT/slug)",
			nil,
		)
	}

	if strings.Contains(trimmed, "://") || strings.HasPrefix(trimmed, "git@") || strings.HasPrefix(trimmed, "ssh://") {
		_, proj, repoSlug, ok := giturl.ParseBitbucketRemote(trimmed)
		if ok {
			return proj, repoSlug, nil
		}
	}

	if proj, repoSlug, ok := giturl.ParseBitbucketPath(trimmed); ok {
		return proj, repoSlug, nil
	}

	return "", "", apperrors.New(
		apperrors.KindValidation,
		"invalid repository selector (expected PROJECT/slug)",
		nil,
	)
}

// Resolve resolves the project key and repo slug from the explicit selector if provided,
// or falls back to environment variables and configuration.
func Resolve(selector string, cfg config.AppConfig) (projectKey, slug string, err error) {
	trimmed := strings.TrimSpace(selector)
	if trimmed == "" {
		repoSlug := strings.TrimSpace(os.Getenv("BITBUCKET_REPO_SLUG"))
		if strings.TrimSpace(cfg.ProjectKey) == "" || repoSlug == "" {
			return "", "", apperrors.New(
				apperrors.KindValidation,
				"repository is required (use --repo PROJECT/slug or set BITBUCKET_PROJECT_KEY + BITBUCKET_REPO_SLUG)",
				nil,
			)
		}

		return cfg.ProjectKey, repoSlug, nil
	}

	return Parse(trimmed)
}
