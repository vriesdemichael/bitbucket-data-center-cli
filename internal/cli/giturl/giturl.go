package giturl

import (
	"fmt"
	"net/url"
	"strconv"
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
	if q := strings.IndexAny(path, "?#"); q >= 0 {
		path = path[:q]
	}
	trimmed := strings.Trim(strings.TrimSpace(path), "/")
	if trimmed == "" {
		return "", "", false
	}

	parts := strings.Split(trimmed, "/")

	// Check /projects/{project}/repos/{slug}
	if len(parts) >= 4 {
		for index := 0; index+3 < len(parts); index++ {
			if strings.EqualFold(parts[index], "projects") && strings.EqualFold(parts[index+2], "repos") {
				project := strings.TrimSpace(parts[index+1])
				repo := strings.TrimSuffix(strings.TrimSpace(parts[index+3]), ".git")
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

	// Check /users/{user}/repos/{slug}
	if len(parts) >= 4 {
		for index := 0; index+3 < len(parts); index++ {
			if strings.EqualFold(parts[index], "users") && strings.EqualFold(parts[index+2], "repos") {
				user := strings.TrimPrefix(strings.TrimSpace(parts[index+1]), "~")
				repo := strings.TrimSuffix(strings.TrimSpace(parts[index+3]), ".git")
				if user == "" || repo == "" {
					return "", "", false
				}
				if unescaped, err := url.PathUnescape(user); err == nil {
					user = unescaped
				}
				if unescaped, err := url.PathUnescape(repo); err == nil {
					repo = unescaped
				}
				return "~" + user, repo, true
			}
		}
	}

	// Check /scm/{project}/{slug}
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

	if len(parts) == 2 {
		project := strings.TrimSpace(parts[0])
		repo := strings.TrimSuffix(strings.TrimSpace(parts[1]), ".git")
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

// ParseBitbucketPR extracts host, project key, repo slug, and pull request ID from a Bitbucket PR URL.
func ParseBitbucketPR(rawURL string) (host string, projectKey string, slug string, prID string, ok bool) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return "", "", "", "", false
	}

	var path string
	if strings.Contains(trimmed, "://") {
		parsed, err := url.Parse(trimmed)
		if err != nil {
			return "", "", "", "", false
		}
		host = parsed.Hostname()
		path = parsed.Path
	} else if strings.HasPrefix(trimmed, "/") {
		path = trimmed
	} else {
		if slash := strings.Index(trimmed, "/"); slash > 0 && strings.Contains(trimmed[slash:], "/pull-requests/") {
			host = trimmed[:slash]
			path = trimmed[slash:]
		} else {
			return "", "", "", "", false
		}
	}

	if q := strings.IndexAny(path, "?#"); q >= 0 {
		path = path[:q]
	}

	trimmedPath := strings.Trim(strings.TrimSpace(path), "/")
	if trimmedPath == "" {
		return "", "", "", "", false
	}

	parts := strings.Split(trimmedPath, "/")
	for i := 0; i+5 < len(parts); i++ {
		// Check /projects/{project}/repos/{slug}/pull-requests/{id}
		if strings.EqualFold(parts[i], "projects") && strings.EqualFold(parts[i+2], "repos") && strings.EqualFold(parts[i+4], "pull-requests") {
			project := strings.TrimSpace(parts[i+1])
			repo := strings.TrimSuffix(strings.TrimSpace(parts[i+3]), ".git")
			id := strings.TrimSpace(parts[i+5])
			if unescaped, err := url.PathUnescape(project); err == nil {
				project = unescaped
			}
			if unescaped, err := url.PathUnescape(repo); err == nil {
				repo = unescaped
			}
			if _, err := strconv.ParseInt(id, 10, 64); err == nil && project != "" && repo != "" {
				return host, project, repo, id, true
			}
		}

		// Check /users/{user}/repos/{slug}/pull-requests/{id}
		if strings.EqualFold(parts[i], "users") && strings.EqualFold(parts[i+2], "repos") && strings.EqualFold(parts[i+4], "pull-requests") {
			user := strings.TrimPrefix(strings.TrimSpace(parts[i+1]), "~")
			repo := strings.TrimSuffix(strings.TrimSpace(parts[i+3]), ".git")
			id := strings.TrimSpace(parts[i+5])
			if unescaped, err := url.PathUnescape(user); err == nil {
				user = unescaped
			}
			if unescaped, err := url.PathUnescape(repo); err == nil {
				repo = unescaped
			}
			if _, err := strconv.ParseInt(id, 10, 64); err == nil && user != "" && repo != "" {
				return host, "~" + user, repo, id, true
			}
		}
	}

	return "", "", "", "", false
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
