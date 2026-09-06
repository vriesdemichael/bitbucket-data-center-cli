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

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/git/execgit"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
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

	// Before anything is seeded. An expired licence still reports RUNNING and
	// only refuses writes, so without this the run gets several minutes in and
	// then fails at a git push with a message that reads like a product bug.
	requireUsableLicence(t)

	client, err := newGeneratedClient(cfg)
	if err != nil {
		t.Fatalf("create generated client: %v", err)
	}

	return &liveHarness{t: t, config: cfg, client: client}
}

func (h *liveHarness) username() string {
	if u := strings.TrimSpace(h.config.BitbucketUsername); u != "" {
		return u
	}
	if u := strings.TrimSpace(os.Getenv("ADMIN_USER")); u != "" {
		return u
	}
	return "admin"
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

// repoSeed says what a test needs from the repository it is given.
//
// The zero value is one repository with one commit and no commit ids, which is
// what most tests want.
type repoSeed struct {
	// Commits defaults to 1.
	Commits int
	// Repos defaults to 1. More than one is unusual; a test comparing two
	// repositories wants it.
	Repos int
	// WithCommitIDs asks for the ids of the seeded commits. It costs a REST
	// call per repository and, when Bitbucket has not indexed the push yet, a
	// poll behind it. 26 of the 78 files that seed read the ids; the rest were
	// paying for them anyway, which is why this is opt-in rather than always.
	WithCommitIDs bool
}

func (seed repoSeed) counts() (repos int, commits int) {
	repos, commits = seed.Repos, seed.Commits
	if repos < 1 {
		repos = 1
	}
	if commits < 1 {
		commits = 1
	}

	return repos, commits
}

// seedRepo says a test's scope is one clean repository.
//
// It creates a project of its own, the same as seedIsolatedProject. The name is
// the difference, and the name is the point: it records that this test needs
// nothing but a repository, which is what has to be known before any of these
// can run at the same time.
//
// It did share a project between every such test, and that is worth recording
// because the experiment answered its own question. Sharing saved 60ms of a
// 536ms seed -- 11%, and 0.8% of a suite, invisible in it. Against that, four
// tests broke: a `project list`, a `repo list`, and two pull-request dashboard
// queries all read across the instance rather than within a repository, so a
// project holding two hundred other tests' repositories was in their way. Some
// were mine to fix and some were pre-existing fragility, but the trade was
// clear either way: 0.8% is not worth a class of failure that only appears
// under load and only in CI.
//
// The cheapness of the isolated path is what makes that an easy call. If the
// project creation had been the 419ms an early cold-JVM measurement suggested,
// this would have been a real decision. Measured warm, it is 60ms.
func (h *liveHarness) seedRepo(ctx context.Context, seed repoSeed) (seededProject, error) {
	repositoryCount, commitsPerRepository := seed.counts()

	return h.seedIsolatedProjectWith(ctx, repositoryCount, commitsPerRepository, seed.WithCommitIDs)
}

// seedIsolatedProject gives a test a project of its own.
//
// For a test whose scope includes project-level state, or which addresses the
// project as a whole. Identical to seedRepo in what it does; the two names
// differ so that a reader can tell which tests could share a project if one
// were ever worth sharing again.
func (h *liveHarness) seedIsolatedProject(ctx context.Context, repositoryCount int, commitsPerRepository int) (seededProject, error) {
	return h.seedIsolatedProjectWith(ctx, repositoryCount, commitsPerRepository, true)
}

func (h *liveHarness) seedIsolatedProjectWith(ctx context.Context, repositoryCount int, commitsPerRepository int, withCommitIDs bool) (seededProject, error) {
	if repositoryCount < 1 {
		return seededProject{}, fmt.Errorf("repository count must be >= 1")
	}
	if commitsPerRepository < 1 {
		return seededProject{}, fmt.Errorf("commits per repository must be >= 1")
	}

	projectKey, err := h.createProject(ctx, "LT", "Live Test")
	if err != nil {
		return seededProject{}, err
	}

	// The whole project goes at the end of the test, which takes its
	// repositories with it.
	h.t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		_, _ = h.client.DeleteProjectWithResponse(cleanupCtx, projectKey)
	})

	return h.seedRepositories(ctx, projectKey, repositoryCount, commitsPerRepository, withCommitIDs, false)
}

// createProject makes a project and returns its key.
//
// The key is derived from the clock, so it can collide with one still held by a
// project an earlier test deleted -- Bitbucket removes projects asynchronously,
// so the key outlives the delete call. A collision is answered with 409 and is
// not a fault in the test being run, so it retries with a fresh key rather than
// failing an unrelated assertion.
func (h *liveHarness) createProject(ctx context.Context, prefix string, namePrefix string) (string, error) {
	const seedAttempts = 5

	var projectKey string
	var lastStatus int

	for attempt := 0; attempt < seedAttempts; attempt++ {
		suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
		projectKey = strings.ToUpper(prefix + suffix[len(suffix)-6:])
		projectName := namePrefix + " " + suffix

		createProjectBody := openapigenerated.CreateProjectJSONRequestBody{Key: &projectKey, Name: &projectName}
		createProjectResponse, err := h.client.CreateProjectWithResponse(ctx, createProjectBody)
		if err != nil {
			return "", fmt.Errorf("create project call failed: %w", err)
		}

		lastStatus = createProjectResponse.StatusCode()
		if lastStatus >= 200 && lastStatus < 300 {
			return projectKey, nil
		}
		if lastStatus != http.StatusConflict {
			return "", fmt.Errorf("create project %s returned status %d", projectKey, lastStatus)
		}

		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		time.Sleep(150 * time.Millisecond)
	}

	return "", fmt.Errorf("create project failed after %d attempts, last key %s returned status %d", seedAttempts, projectKey, lastStatus)
}

// seedRepositories creates repositories in a project that already exists.
//
// Shared by both seeding paths, because what a test gets -- a repository with
// commits in it -- is the same either way. Only who owns the project differs.
func (h *liveHarness) seedRepositories(ctx context.Context, projectKey string, repositoryCount, commitsPerRepository int, withCommitIDs bool, removeRepositories bool) (seededProject, error) {
	seeded := seededProject{Key: projectKey, Repos: make([]seededRepository, 0, repositoryCount)}

	for index := 0; index < repositoryCount; index++ {
		// Enough of the clock to be unique among the several hundred
		// repositories the shared project accumulates in a run. Four digits
		// was fine when each project held one; across 267 it is a collision
		// waiting for a slow afternoon.
		suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
		repoName := fmt.Sprintf("lt-repo-%d-%s", index+1, suffix[len(suffix)-9:])
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

		// Only where the project outlives the test. In an isolated project the
		// project delete takes the repositories with it, and asking for each of
		// them first is a REST call per repository that buys nothing.
		if removeRepositories {
			h.t.Cleanup(func() {
				cleanupCtx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
				defer cancel()
				_, _ = h.client.DeleteRepositoryWithResponse(cleanupCtx, projectKey, repoSlug)
			})
		}

		if err := h.pushCommitsToRepository(projectKey, repoSlug, commitsPerRepository); err != nil {
			return seededProject{}, err
		}

		repo := seededRepository{Name: repoName, Slug: repoSlug}
		if withCommitIDs {
			commitIDs, err := h.listCommitIDs(ctx, projectKey, repoSlug, commitsPerRepository+2)
			if err != nil {
				return seededProject{}, err
			}
			repo.CommitIDs = commitIDs
		}

		seeded.Repos = append(seeded.Repos, repo)
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

// pushCommitOnBranch writes a file naming the branch and pushes it.
func (h *liveHarness) pushCommitOnBranch(projectKey, repositorySlug, branch, fileName string) error {
	return h.pushFileOnBranch(projectKey, repositorySlug, branch, fileName, fmt.Sprintf("branch=%s\n", branch))
}

// pushFileOnBranch is the same with the content chosen by the caller, for tests
// that need two sides to conflict rather than merely differ.
func (h *liveHarness) pushFileOnBranch(projectKey, repositorySlug, branch, fileName, content string) error {
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

	// fileName may name a path, not just a leaf: ".bitbucket/CODEOWNERS" is one
	// the pull request commands read.
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return fmt.Errorf("create branch file directory: %w", err)
	}

	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("open branch file: %w", err)
	}
	// The caller's content, not a value derived from the branch. Writing the
	// latter regardless of the argument made every caller that asked for
	// specific content get the same bytes on both sides -- which quietly
	// disarmed the one test that pushes two conflicting versions of a file and
	// then asserts the conflict.
	_, _ = file.WriteString(content)
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

func repositoryPushURL(cfg config.AppConfig, projectKey, repositorySlug string) (string, error) {
	parsed, err := url.Parse(cfg.BitbucketURL)
	if err != nil {
		return "", fmt.Errorf("parse bitbucket url: %w", err)
	}
	parsed.User = url.UserPassword(cfg.BitbucketUsername, cfg.BitbucketPassword)
	parsed.Path = path.Join(parsed.Path, "scm", strings.ToUpper(projectKey), repositorySlug+".git")
	return parsed.String(), nil
}

// runGitCapture is runGit for the cases that need to read git's answer rather
// than only know it succeeded — verifying which branch is checked out, or what
// its upstream resolved to. No retry: these are local queries against a
// repository on disk, so a failure is a real one rather than the remote rate
// limiting that runGit exists to ride out.
func runGitCapture(directory string, args ...string) (string, error) {
	gitArgs := append([]string{"-c", "credential.helper="}, args...)
	command := exec.Command("git", gitArgs...)
	command.Dir = directory
	command.Env = append(execgit.ScopeFreeEnv(), "GIT_TERMINAL_PROMPT=0")

	output, err := command.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("git %s failed: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}

	return string(output), nil
}

func runGit(directory string, args ...string) error {
	const maxRetries = 4
	const baseBackoff = 500 * time.Millisecond

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		gitArgs := append([]string{"-c", "credential.helper="}, args...)
		command := exec.Command("git", gitArgs...)
		command.Dir = directory
		command.Env = append(execgit.ScopeFreeEnv(), "GIT_TERMINAL_PROMPT=0")
		output, err := command.CombinedOutput()
		if err == nil {
			return nil
		}

		message := strings.TrimSpace(string(output))
		lastErr = fmt.Errorf("git %s failed: %w: %s", strings.Join(args, " "), err, message)
		if hint := licenceExpiryHint(message); hint != "" {
			// The licence can lapse mid-run even when the preflight passed.
			// Saying so here is the difference between one clear line and an
			// afternoon spent debugging the wrong thing.
			return fmt.Errorf("%s (%w)", hint, lastErr)
		}

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

// licensedGroup is the group Bitbucket ships carrying LICENSED_USER. A user
// outside it cannot participate in a pull request at all -- the server refuses
// them as a reviewer with "not a licensed user" -- which is a different thing
// from having no permission on a repository.
const licensedGroup = "stash-users"

// licenseUser puts a user in the licensed group so they can be named as a
// reviewer.
//
// This is deliberately not folded into createRestrictedUser: several tests
// depend on that user being unlicensed and unprivileged, and licensing grants a
// global permission that would quietly change what they assert.
func (h *liveHarness) licenseUser(ctx context.Context, username string) error {
	// /admin/groups/add-user reads context as the group and itemName as the
	// user, which is the opposite way round from the sibling endpoint that adds
	// a group to a user.
	group := licensedGroup
	body := openapigenerated.AddUserToGroupJSONRequestBody{Context: &group, ItemName: &username}

	resp, err := h.client.AddUserToGroup(ctx, body)
	if err != nil {
		return fmt.Errorf("add user to licensed group call failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("add user to licensed group returned status %d", resp.StatusCode)
	}

	return nil
}

// createLicensedUser is createRestrictedUser plus a licence, for tests that
// need a second person who can actually take part in a review.
func (h *liveHarness) createLicensedUser(ctx context.Context) (restrictedUser, error) {
	user, err := h.createRestrictedUser(ctx)
	if err != nil {
		return restrictedUser{}, err
	}
	if err := h.licenseUser(ctx, user.Username); err != nil {
		return restrictedUser{}, err
	}

	return user, nil
}

// liveJSON sends an authenticated request to the Bitbucket REST API and decodes
// the response, using whichever credential the harness was configured with.
func (h *liveHarness) liveJSON(ctx context.Context, method, path string, payload any) (map[string]any, error) {
	return h.sendLiveJSON(ctx, func(request *http.Request) {
		if h.config.BitbucketToken != "" {
			request.Header.Set("Authorization", "Bearer "+h.config.BitbucketToken)
		} else if h.config.BitbucketUsername != "" && h.config.BitbucketPassword != "" {
			request.SetBasicAuth(h.config.BitbucketUsername, h.config.BitbucketPassword)
		}
	}, method, path, payload)
}

// liveJSONAs is liveJSON as somebody else. A review status belongs to the
// participant who holds it, so a test that needs a reviewer to have asked for
// changes has to ask as that reviewer.
func (h *liveHarness) liveJSONAs(ctx context.Context, user restrictedUser, method, path string, payload any) (map[string]any, error) {
	return h.sendLiveJSON(ctx, func(request *http.Request) {
		request.SetBasicAuth(user.Username, user.Password)
	}, method, path, payload)
}

func (h *liveHarness) sendLiveJSON(ctx context.Context, authorize func(*http.Request), method, path string, payload any) (map[string]any, error) {
	var reader io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("marshal %s %s payload: %w", method, path, err)
		}
		reader = bytes.NewReader(encoded)
	}

	endpoint := strings.TrimRight(h.config.BitbucketURL, "/") + path
	request, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return nil, fmt.Errorf("build %s %s request: %w", method, path, err)
	}
	request.Header.Set("Accept", "application/json")
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	authorize(request)

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%s %s failed: %w", method, path, err)
	}
	defer func() { _ = response.Body.Close() }()

	body, _ := io.ReadAll(response.Body)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("%s %s returned status %d: %s", method, path, response.StatusCode, body)
	}

	decoded := map[string]any{}
	if len(bytes.TrimSpace(body)) > 0 {
		if err := json.Unmarshal(body, &decoded); err != nil {
			return nil, fmt.Errorf("decode %s %s response: %w (body %s)", method, path, err, body)
		}
	}

	return decoded, nil
}

// createReviewerGroup creates a repository-scoped reviewer group holding one
// user.
//
// This goes straight at the API because bb cannot do it: `reviewer-group
// create` and `update` take only a name and a description, and there is no
// command that adds a member, so a group made through the CLI is empty -- and
// the server refuses an empty one outright.
//
// The payload is fussy in a way worth recording. The scope is required, and the
// member is only recognised by numeric id: {"name": ...} and {"slug": ...} are
// both accepted and both silently yield "Reviewer groups must contain 1 or more
// reviewer(s)".
// createReviewerGroup makes a reviewer group holding the named users.
//
// Members are variadic because a group of one cannot show what a selection
// strategy does: random(1) and least_busy(1) both pick the only member, so a
// test of either needs at least two to choose between.
func (h *liveHarness) createReviewerGroup(ctx context.Context, projectKey, repositorySlug, groupName string, usernames ...string) error {
	repository, err := h.liveJSON(ctx, http.MethodGet,
		fmt.Sprintf("/rest/api/latest/projects/%s/repos/%s", projectKey, repositorySlug), nil)
	if err != nil {
		return fmt.Errorf("look up repository id: %w", err)
	}
	repositoryID, ok := repository["id"].(float64)
	if !ok {
		return fmt.Errorf("repository %s/%s has no numeric id", projectKey, repositorySlug)
	}

	members := make([]map[string]any, 0, len(usernames))
	for _, username := range usernames {
		user, err := h.liveJSON(ctx, http.MethodGet, "/rest/api/latest/users/"+username, nil)
		if err != nil {
			return fmt.Errorf("look up user id: %w", err)
		}
		userID, ok := user["id"].(float64)
		if !ok {
			return fmt.Errorf("user %s has no numeric id", username)
		}
		members = append(members, map[string]any{"id": int64(userID)})
	}

	_, err = h.liveJSON(ctx, http.MethodPost,
		fmt.Sprintf("/rest/api/latest/projects/%s/repos/%s/settings/reviewer-groups", projectKey, repositorySlug),
		map[string]any{
			"name":  groupName,
			"scope": map[string]any{"resourceId": int64(repositoryID), "type": "REPOSITORY"},
			"users": members,
		})
	if err != nil {
		return fmt.Errorf("create reviewer group %s: %w", groupName, err)
	}

	return nil
}

// userID resolves a username to the numeric id Bitbucket wants in several
// payloads.
//
// The reviewer-condition endpoint is one: a reviewer given as {"name": ...} is
// read as id -1 and refused with "User with ID -1 does not exist", the same
// shape as the reviewer-group members quirk. The name is what a person knows,
// so the lookup lives here rather than in each test.
func (h *liveHarness) userID(ctx context.Context, username string) (int64, error) {
	user, err := h.liveJSON(ctx, http.MethodGet, "/rest/api/latest/users/"+username, nil)
	if err != nil {
		return 0, fmt.Errorf("look up user %s: %w", username, err)
	}

	id, ok := user["id"].(float64)
	if !ok {
		return 0, fmt.Errorf("user %s has no numeric id", username)
	}

	return int64(id), nil
}

// renameFileOnBranch moves a file that already exists on master and pushes the
// result as a branch.
//
// A rename is one commit that removes one path and adds another with the same
// content, which git reports as a rename and Bitbucket reports as a MOVE with
// the source path alongside. It cannot be expressed by writing a file, so it
// needs its own helper rather than a flag on pushFileOnBranch.
func (h *liveHarness) renameFileOnBranch(projectKey, repositorySlug, branch, from, to string) error {
	tempDir := h.t.TempDir()

	for _, args := range [][]string{
		{"init"},
		{"config", "user.name", "bb-live-test"},
		{"config", "user.email", "bb-live-test@example.local"},
	} {
		if err := runGit(tempDir, args...); err != nil {
			return fmt.Errorf("git %s failed: %w", args[0], err)
		}
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

	if err := os.MkdirAll(filepath.Dir(filepath.Join(tempDir, to)), 0o755); err != nil {
		return fmt.Errorf("create destination directory: %w", err)
	}
	if err := runGit(tempDir, "mv", from, to); err != nil {
		return fmt.Errorf("git mv failed: %w", err)
	}
	if err := runGit(tempDir, "commit", "-m", "rename "+from+" to "+to); err != nil {
		return fmt.Errorf("git commit failed: %w", err)
	}
	if err := runGit(tempDir, "push", "-u", "origin", branch); err != nil {
		return fmt.Errorf("git push branch failed: %w", err)
	}

	return nil
}

// pushCommitsOnBranch puts several commits on one branch, each adding a file.
//
// pushFileOnBranch branches from master every time it is called, so calling it
// twice for the same branch pushes two unrelated roots and the second is
// rejected. A listing that has to cross a page boundary, or a cap that has to
// have something to cut, needs the commits stacked rather than parallel.
func (h *liveHarness) pushCommitsOnBranch(projectKey, repositorySlug, branch string, commitCount int) error {
	tempDir := h.t.TempDir()

	for _, args := range [][]string{
		{"init"},
		{"config", "user.name", "bb-live-test"},
		{"config", "user.email", "bb-live-test@example.local"},
	} {
		if err := runGit(tempDir, args...); err != nil {
			return fmt.Errorf("git %s failed: %w", args[0], err)
		}
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

	for index := range commitCount {
		name := fmt.Sprintf("%s-%d.txt", branch, index)
		if err := os.WriteFile(filepath.Join(tempDir, name), []byte(fmt.Sprintf("file %d\n", index)), 0o600); err != nil {
			return fmt.Errorf("write %s: %w", name, err)
		}
		if err := runGit(tempDir, "add", name); err != nil {
			return fmt.Errorf("git add %s failed: %w", name, err)
		}
		if err := runGit(tempDir, "commit", "-m", fmt.Sprintf("add %s", name)); err != nil {
			return fmt.Errorf("git commit %s failed: %w", name, err)
		}
	}

	if err := runGit(tempDir, "push", "-u", "origin", branch); err != nil {
		return fmt.Errorf("git push branch failed: %w", err)
	}

	return nil
}
