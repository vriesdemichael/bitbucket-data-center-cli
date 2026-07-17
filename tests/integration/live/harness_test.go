//go:build live

package live_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
)

type seededProject struct {
	Key   string
	Repos []seededRepository
}

type seededRepository struct {
	Name      string
	Slug      string
	CommitIDs []string
}

type liveHarness struct {
	t      *testing.T
	config config.AppConfig
	client *openapigenerated.ClientWithResponses
}

func newLiveHarness(t *testing.T) *liveHarness {
	t.Helper()
	applyLocalLiveDefaults(t)

	cfg, err := config.LoadFromEnv()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.BitbucketUsername == "" || cfg.BitbucketPassword == "" {
		t.Skip("BITBUCKET_USERNAME/BITBUCKET_PASSWORD (or ADMIN_USER/ADMIN_PASSWORD) required for live harness")
	}

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git executable is required for commit seeding")
	}

	client, err := newGeneratedClient(cfg)
	if err != nil {
		t.Fatalf("create generated client: %v", err)
	}

	return &liveHarness{t: t, config: cfg, client: client}
}

func applyLocalLiveDefaults(t *testing.T) {
	t.Helper()

	if strings.TrimSpace(os.Getenv("BB_DISABLE_STORED_CONFIG")) == "" {
		t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	}

	bitbucketURL := strings.TrimSpace(os.Getenv("BITBUCKET_URL"))
	if bitbucketURL == "" {
		t.Setenv("BITBUCKET_URL", "http://localhost:7990")
	} else if strings.Contains(bitbucketURL, "://") == false && isLocalBitbucketHost(bitbucketURL) {
		t.Setenv("BITBUCKET_URL", "http://"+bitbucketURL)
	}

	hasExplicitUser := strings.TrimSpace(os.Getenv("BITBUCKET_USERNAME")) != "" || strings.TrimSpace(os.Getenv("BITBUCKET_USER")) != ""
	hasExplicitPassword := strings.TrimSpace(os.Getenv("BITBUCKET_PASSWORD")) != ""
	hasAdminFallback := strings.TrimSpace(os.Getenv("ADMIN_USER")) != "" || strings.TrimSpace(os.Getenv("ADMIN_PASSWORD")) != ""
	if !hasExplicitUser && !hasExplicitPassword && !hasAdminFallback {
		t.Setenv("ADMIN_USER", "admin")
		t.Setenv("ADMIN_PASSWORD", "admin")
	}
}

func isLocalBitbucketHost(host string) bool {
	trimmed := strings.TrimSpace(strings.ToLower(host))
	return strings.HasPrefix(trimmed, "localhost:") || strings.HasPrefix(trimmed, "127.0.0.1:") || trimmed == "localhost" || trimmed == "127.0.0.1"
}

func (h *liveHarness) seedProjectWithRepositories(ctx context.Context, repositoryCount int, commitsPerRepository int) (seededProject, error) {
	if repositoryCount < 1 {
		return seededProject{}, fmt.Errorf("repository count must be >= 1")
	}
	if commitsPerRepository < 1 {
		return seededProject{}, fmt.Errorf("commits per repository must be >= 1")
	}

	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	projectKey := strings.ToUpper("LT" + suffix[len(suffix)-6:])
	projectName := "Live Test " + suffix

	createProjectBody := openapigenerated.CreateProjectJSONRequestBody{Key: &projectKey, Name: &projectName}
	createProjectResponse, err := h.client.CreateProjectWithResponse(ctx, createProjectBody)
	if err != nil {
		return seededProject{}, fmt.Errorf("create project call failed: %w", err)
	}
	if createProjectResponse.StatusCode() < 200 || createProjectResponse.StatusCode() >= 300 {
		return seededProject{}, fmt.Errorf("create project returned status %d", createProjectResponse.StatusCode())
	}

	seeded := seededProject{Key: projectKey, Repos: make([]seededRepository, 0, repositoryCount)}
	h.t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		_ = h.cleanupProject(cleanupCtx, seeded)
	})

	for index := 0; index < repositoryCount; index++ {
		repoName := fmt.Sprintf("lt-repo-%d-%s", index+1, suffix[len(suffix)-4:])
		scmID := "git"
		forkable := true
		createRepoBody := openapigenerated.CreateRepositoryJSONRequestBody{Name: &repoName, ScmId: &scmID, Forkable: &forkable}
		createRepoResponse, createErr := h.client.CreateRepositoryWithResponse(ctx, projectKey, createRepoBody)
		if createErr != nil {
			return seededProject{}, fmt.Errorf("create repository call failed: %w", createErr)
		}
		if createRepoResponse.StatusCode() < 200 || createRepoResponse.StatusCode() >= 300 {
			return seededProject{}, fmt.Errorf("create repository returned status %d", createRepoResponse.StatusCode())
		}

		repoSlug := repoName
		if createRepoResponse.ApplicationjsonCharsetUTF8201 != nil && createRepoResponse.ApplicationjsonCharsetUTF8201.Slug != nil {
			repoSlug = *createRepoResponse.ApplicationjsonCharsetUTF8201.Slug
		}

		if err := h.pushCommitsToRepository(projectKey, repoSlug, commitsPerRepository); err != nil {
			return seededProject{}, err
		}

		commitIDs, err := h.listCommitIDs(ctx, projectKey, repoSlug, commitsPerRepository+2)
		if err != nil {
			return seededProject{}, err
		}

		seeded.Repos = append(seeded.Repos, seededRepository{Name: repoName, Slug: repoSlug, CommitIDs: commitIDs})
	}

	return seeded, nil
}

func (h *liveHarness) pushCommitsToRepository(projectKey, repositorySlug string, commitCount int) error {
	tempDir := h.t.TempDir()

	if err := runGit(tempDir, "init"); err != nil {
		return fmt.Errorf("git init failed: %w", err)
	}
	if err := runGit(tempDir, "checkout", "-b", "master"); err != nil {
		return fmt.Errorf("git checkout master failed: %w", err)
	}
	if err := runGit(tempDir, "config", "user.name", "bb-live-test"); err != nil {
		return fmt.Errorf("git config user.name failed: %w", err)
	}
	if err := runGit(tempDir, "config", "user.email", "bb-live-test@example.local"); err != nil {
		return fmt.Errorf("git config user.email failed: %w", err)
	}

	for index := 0; index < commitCount; index++ {
		filePath := filepath.Join(tempDir, "seed.txt")
		file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return fmt.Errorf("open seed file: %w", err)
		}
		_, _ = file.WriteString(fmt.Sprintf("commit-%d\n", index+1))
		_ = file.Close()

		if err := runGit(tempDir, "add", "seed.txt"); err != nil {
			return fmt.Errorf("git add failed: %w", err)
		}
		if err := runGit(tempDir, "commit", "-m", fmt.Sprintf("seed commit %d", index+1)); err != nil {
			return fmt.Errorf("git commit failed: %w", err)
		}
	}

	pushURL, err := repositoryPushURL(h.config, projectKey, repositorySlug)
	if err != nil {
		return err
	}

	if err := runGit(tempDir, "remote", "add", "origin", pushURL); err != nil {
		return fmt.Errorf("git remote add failed: %w", err)
	}
	if err := runGit(tempDir, "push", "-u", "origin", "master"); err != nil {
		return fmt.Errorf("git push failed: %w", err)
	}

	return nil
}

func (h *liveHarness) pushCommitOnBranch(projectKey, repositorySlug, branch, fileName string) error {
	tempDir := h.t.TempDir()

	if err := runGit(tempDir, "init"); err != nil {
		return fmt.Errorf("git init failed: %w", err)
	}
	if err := runGit(tempDir, "config", "user.name", "bb-live-test"); err != nil {
		return fmt.Errorf("git config user.name failed: %w", err)
	}
	if err := runGit(tempDir, "config", "user.email", "bb-live-test@example.local"); err != nil {
		return fmt.Errorf("git config user.email failed: %w", err)
	}

	pushURL, err := repositoryPushURL(h.config, projectKey, repositorySlug)
	if err != nil {
		return err
	}

	if err := runGit(tempDir, "remote", "add", "origin", pushURL); err != nil {
		return fmt.Errorf("git remote add failed: %w", err)
	}
	if err := runGit(tempDir, "fetch", "origin", "master"); err != nil {
		return fmt.Errorf("git fetch origin master failed: %w", err)
	}
	if err := runGit(tempDir, "checkout", "-b", branch, "FETCH_HEAD"); err != nil {
		return fmt.Errorf("git checkout branch failed: %w", err)
	}

	filePath := filepath.Join(tempDir, fileName)
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("open branch file: %w", err)
	}
	_, _ = file.WriteString(fmt.Sprintf("branch=%s\n", branch))
	_ = file.Close()

	if err := runGit(tempDir, "add", fileName); err != nil {
		return fmt.Errorf("git add failed: %w", err)
	}
	if err := runGit(tempDir, "commit", "-m", "seed branch commit"); err != nil {
		return fmt.Errorf("git commit failed: %w", err)
	}
	if err := runGit(tempDir, "push", "-u", "origin", branch); err != nil {
		return fmt.Errorf("git push branch failed: %w", err)
	}

	return nil
}

func (h *liveHarness) createPullRequest(ctx context.Context, projectKey, repositorySlug, fromBranch, toBranch string) (string, error) {
	type ref struct {
		Id string `json:"id"`
	}
	type body struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		FromRef     ref    `json:"fromRef"`
		ToRef       ref    `json:"toRef"`
	}

	payload := body{
		Title:       "Live test PR",
		Description: "PR seeded by live harness",
		FromRef:     ref{Id: "refs/heads/" + fromBranch},
		ToRef:       ref{Id: "refs/heads/" + toBranch},
	}

	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal pull request payload: %w", err)
	}

	endpoint := strings.TrimRight(h.config.BitbucketURL, "/") + "/rest/api/latest/projects/" + projectKey + "/repos/" + repositorySlug + "/pull-requests"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(rawPayload))
	if err != nil {
		return "", fmt.Errorf("build pull request request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")

	if h.config.BitbucketToken != "" {
		request.Header.Set("Authorization", "Bearer "+h.config.BitbucketToken)
	} else if h.config.BitbucketUsername != "" && h.config.BitbucketPassword != "" {
		request.SetBasicAuth(h.config.BitbucketUsername, h.config.BitbucketPassword)
	}

	retries := h.config.RetryCount
	if retries < 0 {
		retries = 0
	}
	backoff := h.config.RetryBackoff
	if backoff <= 0 {
		backoff = 250 * time.Millisecond
	}

	var parsed struct {
		Id any `json:"id"`
	}

	for attempt := 0; attempt <= retries; attempt++ {
		activeRequest := request
		if attempt > 0 {
			clone := request.Clone(ctx)
			clone.Body = io.NopCloser(bytes.NewReader(rawPayload))
			activeRequest = clone
		}

		response, callErr := http.DefaultClient.Do(activeRequest)
		if callErr != nil {
			if attempt == retries {
				return "", fmt.Errorf("create pull request call failed: %w", callErr)
			}
			time.Sleep(time.Duration(attempt+1) * backoff)
			continue
		}

		body, readErr := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if readErr != nil {
			if attempt == retries {
				return "", fmt.Errorf("read pull request response: %w", readErr)
			}
			time.Sleep(time.Duration(attempt+1) * backoff)
			continue
		}

		if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 {
			if attempt == retries {
				return "", fmt.Errorf("create pull request returned status %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
			}
			delay := retryAfterFromHeaders(response.Header, attempt, backoff)
			time.Sleep(delay)
			continue
		}

		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return "", fmt.Errorf("create pull request returned status %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
		}

		if decodeErr := json.Unmarshal(body, &parsed); decodeErr != nil {
			return "", fmt.Errorf("decode pull request response: %w", decodeErr)
		}

		if parsed.Id == nil {
			return "", fmt.Errorf("create pull request response missing id")
		}

		return fmt.Sprintf("%v", parsed.Id), nil
	}

	return "", fmt.Errorf("create pull request failed after retries")
}

func (h *liveHarness) listCommitIDs(ctx context.Context, projectKey, repositorySlug string, limit int) ([]string, error) {
	var lastStatus int
	for attempt := 0; attempt < 8; attempt++ {
		limitValue := float32(limit)
		response, err := h.client.GetCommitsWithResponse(ctx, projectKey, repositorySlug, &openapigenerated.GetCommitsParams{Limit: &limitValue})
		if err != nil {
			return nil, fmt.Errorf("list commits call failed: %w", err)
		}

		lastStatus = response.StatusCode()
		if response.StatusCode() >= 200 && response.StatusCode() < 300 && response.ApplicationjsonCharsetUTF8200 != nil && response.ApplicationjsonCharsetUTF8200.Values != nil {
			ids := make([]string, 0, len(*response.ApplicationjsonCharsetUTF8200.Values))
			for _, value := range *response.ApplicationjsonCharsetUTF8200.Values {
				if value.Id != nil && *value.Id != "" {
					ids = append(ids, *value.Id)
				}
			}

			if len(ids) > 0 {
				return ids, nil
			}
		}

		time.Sleep(500 * time.Millisecond)
	}

	return nil, fmt.Errorf("no commit ids found for %s/%s after retries (last status=%d)", projectKey, repositorySlug, lastStatus)
}

func (h *liveHarness) cleanupProject(ctx context.Context, seeded seededProject) error {
	for _, repo := range seeded.Repos {
		_, _ = h.client.DeleteRepositoryWithResponse(ctx, seeded.Key, repo.Slug)
	}

	_, _ = h.client.DeleteProjectWithResponse(ctx, seeded.Key)
	return nil
}

func repositoryPushURL(cfg config.AppConfig, projectKey, repositorySlug string) (string, error) {
	parsed, err := url.Parse(cfg.BitbucketURL)
	if err != nil {
		return "", fmt.Errorf("parse bitbucket url: %w", err)
	}
	parsed.User = url.UserPassword(cfg.BitbucketUsername, cfg.BitbucketPassword)
	parsed.Path = path.Join(parsed.Path, "scm", strings.ToUpper(projectKey), repositorySlug+".git")
	return parsed.String(), nil
}

func runGit(directory string, args ...string) error {
	const maxRetries = 4
	const baseBackoff = 500 * time.Millisecond

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		gitArgs := append([]string{"-c", "credential.helper="}, args...)
		command := exec.Command("git", gitArgs...)
		command.Dir = directory
		command.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
		output, err := command.CombinedOutput()
		if err == nil {
			return nil
		}

		message := strings.TrimSpace(string(output))
		lastErr = fmt.Errorf("git %s failed: %v: %s", strings.Join(args, " "), err, message)

		if attempt >= maxRetries || !isRetriableGitRateLimit(message, args) {
			break
		}

		delay := retryAfterFromGitOutput(message)
		if delay <= 0 {
			delay = time.Duration(attempt+1) * baseBackoff
		}
		time.Sleep(delay)
	}

	return lastErr
}

func isRetriableGitRateLimit(message string, args []string) bool {
	if len(args) == 0 {
		return false
	}

	command := strings.ToLower(strings.TrimSpace(args[0]))
	if command != "push" && command != "fetch" && command != "pull" && command != "clone" {
		return false
	}

	lowered := strings.ToLower(message)
	return strings.Contains(lowered, "error: 429") || strings.Contains(lowered, "http 429") || strings.Contains(lowered, "status 429")
}

func retryAfterFromGitOutput(message string) time.Duration {
	retryAfterRegex := regexp.MustCompile(`(?i)retry-after\s*[:=]\s*([^\s]+)`)
	if match := retryAfterRegex.FindStringSubmatch(message); len(match) == 2 {
		value := strings.TrimSpace(match[1])
		if seconds, err := strconv.Atoi(value); err == nil {
			if seconds < 0 {
				seconds = 0
			}
			return time.Duration(seconds) * time.Second
		}
		if retryAt, err := http.ParseTime(value); err == nil {
			delay := time.Until(retryAt)
			if delay < 0 {
				return 0
			}
			return delay
		}
	}

	lines := strings.Split(message, "\n")
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}

		if strings.Contains(strings.ToLower(line), "retry-after") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) != 2 {
				continue
			}
			value := strings.TrimSpace(parts[1])
			if seconds, err := strconv.Atoi(value); err == nil {
				if seconds < 0 {
					seconds = 0
				}
				return time.Duration(seconds) * time.Second
			}
			if retryAt, err := http.ParseTime(value); err == nil {
				delay := time.Until(retryAt)
				if delay < 0 {
					return 0
				}
				return delay
			}
		}
	}

	return 0
}

func retryAfterFromHeaders(headers http.Header, attempt int, fallbackBackoff time.Duration) time.Duration {
	if fallbackBackoff <= 0 {
		fallbackBackoff = 250 * time.Millisecond
	}

	if headers != nil {
		retryAfter := strings.TrimSpace(headers.Get("Retry-After"))
		if retryAfter != "" {
			if seconds, err := strconv.Atoi(retryAfter); err == nil {
				if seconds < 0 {
					seconds = 0
				}
				return time.Duration(seconds) * time.Second
			}
			if retryAt, err := http.ParseTime(retryAfter); err == nil {
				delay := time.Until(retryAt)
				if delay < 0 {
					return 0
				}
				return delay
			}
		}
	}

	return time.Duration(attempt+1) * fallbackBackoff
}

// restrictedUser holds the credentials of a temporarily created test user.
type restrictedUser struct {
	Username string
	Password string
}

// createRestrictedUser creates a Bitbucket user via the admin API and registers a cleanup to delete it.
// The caller is responsible for granting permissions on the new user after creation.
func (h *liveHarness) createRestrictedUser(ctx context.Context) (restrictedUser, error) {
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	username := "ltuser" + suffix[len(suffix)-8:]
	password := "Ltp@ss" + suffix[len(suffix)-6:] + "!"
	displayName := "Live Test User " + suffix[len(suffix)-6:]
	email := username + "@example.local"

	addToDefaultGroup := false
	params := openapigenerated.CreateUserParams{
		Name:              username,
		Password:          &password,
		DisplayName:       displayName,
		EmailAddress:      email,
		AddToDefaultGroup: &addToDefaultGroup,
	}

	resp, err := h.client.CreateUser(ctx, &params)
	if err != nil {
		return restrictedUser{}, fmt.Errorf("create restricted user call failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return restrictedUser{}, fmt.Errorf("create restricted user returned status %d", resp.StatusCode)
	}

	user := restrictedUser{Username: username, Password: password}

	h.t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = h.deleteRestrictedUser(cleanupCtx, username)
	})

	return user, nil
}

// deleteRestrictedUser removes a Bitbucket user via the admin API.
func (h *liveHarness) deleteRestrictedUser(ctx context.Context, username string) error {
	resp, err := h.client.DeleteUser(ctx, &openapigenerated.DeleteUserParams{Name: username})
	if err != nil {
		return fmt.Errorf("delete restricted user call failed: %w", err)
	}
	defer resp.Body.Close()
	return nil
}

// grantProjectPermission grants a project-level permission to a user.
// permission must be one of: PROJECT_READ, PROJECT_WRITE, PROJECT_ADMIN.
func (h *liveHarness) grantProjectPermission(ctx context.Context, projectKey, username, permission string) error {
	params := openapigenerated.SetPermissionForUsers1Params{
		Name:       &username,
		Permission: &permission,
	}
	resp, err := h.client.SetPermissionForUsers1(ctx, projectKey, &params)
	if err != nil {
		return fmt.Errorf("set project permission call failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("set project permission returned status %d", resp.StatusCode)
	}
	return nil
}

// grantRepoPermission grants a repo-level permission to a user.
// permission must be one of: REPO_READ, REPO_WRITE, REPO_ADMIN.
func (h *liveHarness) grantRepoPermission(ctx context.Context, projectKey, repoSlug, username string, permission openapigenerated.SetPermissionForUserParamsPermission) error {
	params := openapigenerated.SetPermissionForUserParams{
		Name:       []string{username},
		Permission: permission,
	}
	resp, err := h.client.SetPermissionForUser(ctx, projectKey, repoSlug, &params)
	if err != nil {
		return fmt.Errorf("set repo permission call failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("set repo permission returned status %d", resp.StatusCode)
	}
	return nil
}

// configureLiveCLIEnvForUser sets env vars to run the CLI as the given restricted user
// (not as the admin from harness.config).
func configureLiveCLIEnvForUser(t *testing.T, harness *liveHarness, projectKey, repositorySlug string, user restrictedUser) {
	t.Helper()

	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", harness.config.BitbucketURL)
	t.Setenv("BITBUCKET_PROJECT_KEY", projectKey)
	t.Setenv("BITBUCKET_REPO_SLUG", repositorySlug)
	t.Setenv("BITBUCKET_USERNAME", user.Username)
	t.Setenv("BITBUCKET_PASSWORD", user.Password)
	t.Setenv("BITBUCKET_TOKEN", "")
}

func TestApplyLocalLiveDefaults(t *testing.T) {
	t.Run("local live defaults are applied when env is absent", func(t *testing.T) {
		t.Setenv("BB_DISABLE_STORED_CONFIG", "")
		t.Setenv("BITBUCKET_URL", "")
		t.Setenv("BITBUCKET_USERNAME", "")
		t.Setenv("BITBUCKET_USER", "")
		t.Setenv("BITBUCKET_PASSWORD", "")
		t.Setenv("ADMIN_USER", "")
		t.Setenv("ADMIN_PASSWORD", "")

		applyLocalLiveDefaults(t)

		if got := os.Getenv("BB_DISABLE_STORED_CONFIG"); got != "1" {
			t.Fatalf("expected BB_DISABLE_STORED_CONFIG=1, got %q", got)
		}
		if got := os.Getenv("BITBUCKET_URL"); got != "http://localhost:7990" {
			t.Fatalf("expected BITBUCKET_URL=http://localhost:7990, got %q", got)
		}
		if got := os.Getenv("ADMIN_USER"); got != "admin" {
			t.Fatalf("expected ADMIN_USER=admin, got %q", got)
		}
		if got := os.Getenv("ADMIN_PASSWORD"); got != "admin" {
			t.Fatalf("expected ADMIN_PASSWORD=admin, got %q", got)
		}
	})

	t.Run("local live defaults preserve explicit env", func(t *testing.T) {
		t.Setenv("BB_DISABLE_STORED_CONFIG", "0")
		t.Setenv("BITBUCKET_URL", "http://custom.local:7990")
		t.Setenv("BITBUCKET_USERNAME", "alice")
		t.Setenv("BITBUCKET_PASSWORD", "secret")
		t.Setenv("ADMIN_USER", "root")
		t.Setenv("ADMIN_PASSWORD", "toor")

		applyLocalLiveDefaults(t)

		if got := os.Getenv("BB_DISABLE_STORED_CONFIG"); got != "0" {
			t.Fatalf("expected BB_DISABLE_STORED_CONFIG to remain explicit, got %q", got)
		}
		if got := os.Getenv("BITBUCKET_URL"); got != "http://custom.local:7990" {
			t.Fatalf("expected BITBUCKET_URL to remain explicit, got %q", got)
		}
		if got := os.Getenv("ADMIN_USER"); got != "root" {
			t.Fatalf("expected ADMIN_USER to remain explicit, got %q", got)
		}
		if got := os.Getenv("ADMIN_PASSWORD"); got != "toor" {
			t.Fatalf("expected ADMIN_PASSWORD to remain explicit, got %q", got)
		}
	})

	t.Run("schemeless local bitbucket url is normalized to http", func(t *testing.T) {
		t.Setenv("BITBUCKET_URL", "localhost:7990")

		applyLocalLiveDefaults(t)

		if got := os.Getenv("BITBUCKET_URL"); got != "http://localhost:7990" {
			t.Fatalf("expected schemeless localhost url to normalize to http, got %q", got)
		}
	})
}

func TestRetryAfterParsingHelpers(t *testing.T) {
	t.Run("git output helper parses retry-after seconds", func(t *testing.T) {
		delay := retryAfterFromGitOutput("fatal: HTTP 429\nRetry-After: 2")
		if delay != 2*time.Second {
			t.Fatalf("expected 2s delay, got %s", delay)
		}
	})

	t.Run("header helper parses retry-after http date", func(t *testing.T) {
		retryAt := time.Now().Add(2 * time.Second).UTC().Format(http.TimeFormat)
		delay := retryAfterFromHeaders(http.Header{"Retry-After": []string{retryAt}}, 0, time.Millisecond)
		if delay <= 0 || delay > 3*time.Second {
			t.Fatalf("expected positive delay <=3s, got %s", delay)
		}
	})

	t.Run("header helper falls back", func(t *testing.T) {
		delay := retryAfterFromHeaders(nil, 1, 250*time.Millisecond)
		if delay != 500*time.Millisecond {
			t.Fatalf("expected fallback delay 500ms, got %s", delay)
		}
	})

	t.Run("git retry detection limits commands", func(t *testing.T) {
		if isRetriableGitRateLimit("HTTP 429", []string{"commit"}) {
			t.Fatal("expected non-network git command to be non-retriable")
		}
		if !isRetriableGitRateLimit("fatal: error: 429", []string{"push", "origin", "master"}) {
			t.Fatal("expected git push 429 to be retriable")
		}
	})
}
