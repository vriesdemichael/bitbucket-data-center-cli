//go:build live

package live_test

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/services/repository"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/transport/httpclient"
)

func TestOpenAPIParity(t *testing.T) {
	applyLocalLiveDefaults(t)

	cfg, err := config.LoadFromEnv()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.BitbucketUsername == "" || cfg.BitbucketPassword == "" {
		t.Skip("BITBUCKET_USERNAME/BITBUCKET_PASSWORD (or ADMIN_USER/ADMIN_PASSWORD) required for parity test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	if err := verifyHealthParity(ctx, cfg); err != nil {
		t.Fatalf("health parity failed: %v", err)
	}

	seeded, err := seedParityData(ctx, cfg)
	if err != nil {
		t.Fatalf("seed parity data failed: %v", err)
	}

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = cleanupParityData(cleanupCtx, cfg, seeded)
	})

	if err := verifyRepositoryListParity(ctx, cfg, seeded.ProjectKey, seeded.RepoSlug); err != nil {
		t.Fatalf("repository list parity failed: %v", err)
	}
}

type paritySeed struct {
	ProjectKey string
	RepoSlug   string
	UserName   string
}

func verifyHealthParity(ctx context.Context, cfg config.AppConfig) error {
	manualClient := httpclient.NewFromConfig(cfg)
	manualHealth, err := manualClient.Health(ctx)
	if err != nil {
		return fmt.Errorf("manual health call failed: %w", err)
	}

	generatedClient, err := newGeneratedClient(cfg)
	if err != nil {
		return fmt.Errorf("build generated client: %w", err)
	}

	limit := float32(1)
	response, err := generatedClient.GetProjectsWithResponse(ctx, &openapigenerated.GetProjectsParams{Limit: &limit})
	if err != nil {
		return fmt.Errorf("generated projects call failed: %w", err)
	}

	generatedHealthy := response.StatusCode() >= 200 && response.StatusCode() < 300
	generatedAuthenticated := generatedHealthy
	if response.StatusCode() == http.StatusUnauthorized || response.StatusCode() == http.StatusForbidden || (response.StatusCode() >= 300 && response.StatusCode() < 400) {
		generatedHealthy = true
		generatedAuthenticated = false
	}

	if manualHealth.Healthy != generatedHealthy {
		return fmt.Errorf("healthy mismatch manual=%v generated=%v status=%d", manualHealth.Healthy, generatedHealthy, response.StatusCode())
	}
	if manualHealth.Authenticated != generatedAuthenticated {
		return fmt.Errorf("authenticated mismatch manual=%v generated=%v status=%d", manualHealth.Authenticated, generatedAuthenticated, response.StatusCode())
	}

	return nil
}

func verifyRepositoryListParity(ctx context.Context, cfg config.AppConfig, projectKey, expectedRepoSlug string) error {
	manualClient := httpclient.NewFromConfig(cfg)
	manualService := repository.NewService(manualClient)

	manualRepos, err := manualService.ListByProject(ctx, projectKey, repository.ListOptions{MaxResults: 100})
	if err != nil {
		return fmt.Errorf("manual repo list failed: %w", err)
	}

	generatedRepos, err := listRepositoriesForProjectWithGeneratedClient(ctx, cfg, projectKey, 100)
	if err != nil {
		return fmt.Errorf("generated repo list failed: %w", err)
	}

	if len(manualRepos) != len(generatedRepos) {
		return fmt.Errorf("repo count mismatch manual=%d generated=%d", len(manualRepos), len(generatedRepos))
	}

	manualKeys := normalizeRepoKeys(manualRepos)
	generatedKeys := normalizeRepoKeys(generatedRepos)

	for i := range manualKeys {
		if manualKeys[i] != generatedKeys[i] {
			return fmt.Errorf("repo set mismatch at index %d manual=%s generated=%s", i, manualKeys[i], generatedKeys[i])
		}
	}

	if !containsRepoSlug(manualRepos, expectedRepoSlug) {
		return fmt.Errorf("manual repo list did not include seeded repository slug=%s", expectedRepoSlug)
	}
	if !containsRepoSlug(generatedRepos, expectedRepoSlug) {
		return fmt.Errorf("generated repo list did not include seeded repository slug=%s", expectedRepoSlug)
	}

	return nil
}

func listRepositoriesForProjectWithGeneratedClient(ctx context.Context, cfg config.AppConfig, projectKey string, limit int) ([]repository.Repository, error) {
	generatedClient, err := newGeneratedClient(cfg)
	if err != nil {
		return nil, err
	}

	start := float32(0)
	pageLimit := float32(limit)
	results := make([]repository.Repository, 0, limit)

	for {
		response, err := generatedClient.GetRepositoriesWithResponse(ctx, projectKey, &openapigenerated.GetRepositoriesParams{
			Limit: &pageLimit,
			Start: &start,
		})
		if err != nil {
			return nil, err
		}
		if response.StatusCode() < 200 || response.StatusCode() >= 300 {
			return nil, fmt.Errorf("unexpected status %d", response.StatusCode())
		}
		if response.ApplicationjsonCharsetUTF8200 == nil || response.ApplicationjsonCharsetUTF8200.Values == nil {
			break
		}

		for _, value := range *response.ApplicationjsonCharsetUTF8200.Values {
			repo := repository.Repository{}
			if value.Project != nil {
				repo.ProjectKey = value.Project.Key
			}
			if value.Slug != nil {
				repo.Slug = *value.Slug
			}
			if value.Name != nil {
				repo.Name = *value.Name
			}
			if value.Public != nil {
				repo.Public = *value.Public
			}
			results = append(results, repo)
		}

		if response.ApplicationjsonCharsetUTF8200.IsLastPage != nil && *response.ApplicationjsonCharsetUTF8200.IsLastPage {
			break
		}
		if response.ApplicationjsonCharsetUTF8200.NextPageStart == nil {
			break
		}
		start = float32(*response.ApplicationjsonCharsetUTF8200.NextPageStart)
	}

	return results, nil
}

func seedParityData(ctx context.Context, cfg config.AppConfig) (paritySeed, error) {
	client, err := newGeneratedClient(cfg)
	if err != nil {
		return paritySeed{}, err
	}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	projectKey := strings.ToUpper("pt" + suffix[len(suffix)-6:])
	projectName := "Parity Test " + suffix
	repoName := "parity-repo-" + suffix
	userName := "parity-user-" + suffix[len(suffix)-8:]
	userEmail := userName + "@example.local"
	password := "ParityPass123!"

	createUserResponse, err := client.CreateUserWithResponse(ctx, &openapigenerated.CreateUserParams{
		Name:         userName,
		DisplayName:  "Parity User",
		EmailAddress: userEmail,
		Password:     &password,
	})
	if err != nil {
		return paritySeed{}, fmt.Errorf("create user call failed: %w", err)
	}
	if createUserResponse.StatusCode() < 200 || createUserResponse.StatusCode() >= 300 {
		return paritySeed{}, fmt.Errorf("create user returned status %d", createUserResponse.StatusCode())
	}

	createProjectBody := openapigenerated.CreateProjectJSONRequestBody{Key: &projectKey, Name: &projectName}
	createProjectResponse, err := client.CreateProjectWithResponse(ctx, createProjectBody)
	if err != nil {
		return paritySeed{}, fmt.Errorf("create project call failed: %w", err)
	}
	if createProjectResponse.StatusCode() < 200 || createProjectResponse.StatusCode() >= 300 {
		return paritySeed{}, fmt.Errorf("create project returned status %d", createProjectResponse.StatusCode())
	}

	scmID := "git"
	forkable := true
	createRepoBody := openapigenerated.CreateRepositoryJSONRequestBody{Name: &repoName, ScmId: &scmID, Forkable: &forkable}
	createRepoResponse, err := client.CreateRepositoryWithResponse(ctx, projectKey, createRepoBody)
	if err != nil {
		return paritySeed{}, fmt.Errorf("create repository call failed: %w", err)
	}
	if createRepoResponse.StatusCode() < 200 || createRepoResponse.StatusCode() >= 300 {
		return paritySeed{}, fmt.Errorf("create repository returned status %d", createRepoResponse.StatusCode())
	}

	repoSlug := repoName
	if createRepoResponse.ApplicationjsonCharsetUTF8201 != nil && createRepoResponse.ApplicationjsonCharsetUTF8201.Slug != nil {
		repoSlug = *createRepoResponse.ApplicationjsonCharsetUTF8201.Slug
	}

	return paritySeed{ProjectKey: projectKey, RepoSlug: repoSlug, UserName: userName}, nil
}

func cleanupParityData(ctx context.Context, cfg config.AppConfig, seeded paritySeed) error {
	client, err := newGeneratedClient(cfg)
	if err != nil {
		return err
	}

	_, _ = client.DeleteRepositoryWithResponse(ctx, seeded.ProjectKey, seeded.RepoSlug)
	_, _ = client.DeleteProjectWithResponse(ctx, seeded.ProjectKey)
	_, _ = client.DeleteUserWithResponse(ctx, &openapigenerated.DeleteUserParams{Name: seeded.UserName})
	return nil
}

func newGeneratedClient(cfg config.AppConfig) (*openapigenerated.ClientWithResponses, error) {
	return openapi.NewClientWithResponsesFromConfig(cfg)
}

func normalizeRepoKeys(repos []repository.Repository) []string {
	keys := make([]string, 0, len(repos))
	for _, repo := range repos {
		keys = append(keys, repo.ProjectKey+"/"+repo.Slug)
	}
	sort.Strings(keys)
	return keys
}

func containsRepoSlug(repos []repository.Repository, slug string) bool {
	for _, repo := range repos {
		if repo.Slug == slug {
			return true
		}
	}

	return false
}
