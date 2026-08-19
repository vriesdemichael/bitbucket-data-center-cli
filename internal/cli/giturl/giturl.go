package giturl

import (
	"fmt"
	"net/url"
	"strings"

	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
)

// ParseBitbucketRemote extracts host, project key, and repository slug from HTTP/HTTPS or SSH remote URLs.
func ParseBitbucketRemote(rawRemoteURL string) (host string, projectKey string, slug string, ok bool) {
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
		projectKey, slug, ok = ParseBitbucketPath(parsed.Path)
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
		projectKey, slug, ok = ParseBitbucketPath(path)
		return host, projectKey, slug, ok
	}

	return "", "", "", false
}

// ParseBitbucketPath extracts project key and repo slug from a URL path.
func ParseBitbucketPath(path string) (projectKey string, slug string, ok bool) {
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

// BuildBitbucketCloneURL creates the standard HTTP clone URL for a Bitbucket DC repository.
func BuildBitbucketCloneURL(baseURL, projectKey, slug string) (string, error) {
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

// NormalizeHTTPCloneHost normalizes the base URL for clone operations by enforcing http/https scheme and stripping path/query/user.
func NormalizeHTTPCloneHost(cloneHost string) string {
	parsed, err := url.Parse(strings.TrimSpace(cloneHost))
	if err != nil || strings.TrimSpace(parsed.Host) == "" {
		return strings.TrimRight(strings.TrimSpace(cloneHost), "/")
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

// IsNonRepositoryError returns true if the error indicates execution outside a git work tree.
func IsNonRepositoryError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "not a git repository") ||
		strings.Contains(message, "must be run in a work tree")
}
