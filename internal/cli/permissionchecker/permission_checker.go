package permissionchecker

import (
	"context"
	"errors"
	"fmt"
	"strings"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

// PermissionChecker provides pre-flight permission checking during dry-run.
type PermissionChecker struct {
	client *openapigenerated.ClientWithResponses
	cache  map[string]error
}

// New creates a new PermissionChecker.
func New(client *openapigenerated.ClientWithResponses) *PermissionChecker {
	return &PermissionChecker{
		client: client,
		cache:  make(map[string]error),
	}
}

// Client returns the underlying OpenAPI client.
func (p *PermissionChecker) Client() *openapigenerated.ClientWithResponses {
	return p.client
}

// CheckRepoPermission verifies if the caller has the specified permission on a repository.
func (p *PermissionChecker) CheckRepoPermission(ctx context.Context, projectKey, repoSlug string, permission openapigenerated.GetRepositories1ParamsPermission) error {
	cacheKey := fmt.Sprintf("repo:%s/%s:%s", projectKey, repoSlug, permission)
	if err, ok := p.cache[cacheKey]; ok {
		return err
	}

	limit := float32(25)
	var start float32
	for {
		params := &openapigenerated.GetRepositories1Params{
			Projectkey: &projectKey,
			Permission: &permission,
			Limit:      &limit,
			Start:      &start,
		}

		resp, err := p.client.GetRepositories1WithResponse(ctx, params)
		if err != nil {
			err = transportFailure(err)
			p.cache[cacheKey] = err
			return err
		}
		if resp.StatusCode() >= 400 {
			err := openapi.MapStatusError(resp.StatusCode(), resp.Body)
			p.cache[cacheKey] = err
			return err
		}

		if resp.ApplicationjsonCharsetUTF8200 != nil && resp.ApplicationjsonCharsetUTF8200.Values != nil {
			for _, repo := range *resp.ApplicationjsonCharsetUTF8200.Values {
				if repo.Slug == nil || !strings.EqualFold(strings.TrimSpace(*repo.Slug), repoSlug) {
					continue
				}
				if repo.Project != nil && strings.EqualFold(strings.TrimSpace(repo.Project.Key), projectKey) {
					p.cache[cacheKey] = nil
					return nil
				}
			}
		}

		// Stop paginating if this is the last page or the response is empty
		if resp.ApplicationjsonCharsetUTF8200 == nil ||
			resp.ApplicationjsonCharsetUTF8200.IsLastPage == nil ||
			*resp.ApplicationjsonCharsetUTF8200.IsLastPage ||
			resp.ApplicationjsonCharsetUTF8200.NextPageStart == nil {
			break
		}
		start = float32(*resp.ApplicationjsonCharsetUTF8200.NextPageStart)
	}

	err := apperrors.New(apperrors.KindAuthorization, fmt.Sprintf("insufficient permission: %s required on repository %s/%s", permission, projectKey, repoSlug), nil)
	p.cache[cacheKey] = err
	return err
}

// CheckProjectWrite verifies if the caller has PROJECT_WRITE on a project.
func (p *PermissionChecker) CheckProjectWrite(ctx context.Context, projectKey string) error {
	return p.checkProjectListed(ctx, projectKey, "PROJECT_WRITE")
}

// checkProjectListed verifies the caller holds a permission on a project by
// finding the project among those Bitbucket lists for that permission.
//
// Bitbucket has no key filter on that listing, only a name filter, and the
// name filter matches every project whose name contains the one given, sorted
// by name. Asked for one project named "Probe X", it answered with "Alpha
// Probe X", so the listing is read page by page until the key turns up
// (observed on 10.4.3).
func (p *PermissionChecker) checkProjectListed(ctx context.Context, projectKey, permission string) error {
	cacheKey := fmt.Sprintf("project:%s:%s", projectKey, permission)
	if err, ok := p.cache[cacheKey]; ok {
		return err
	}

	// First resolve the project name
	projResp, err := p.client.GetProjectWithResponse(ctx, projectKey)
	if err != nil {
		err = transportFailure(err)
		p.cache[cacheKey] = err
		return err
	}
	if projResp.StatusCode() >= 400 {
		err := openapi.MapStatusError(projResp.StatusCode(), projResp.Body)
		p.cache[cacheKey] = err
		return err
	}
	if projResp.ApplicationjsonCharsetUTF8200 == nil || projResp.ApplicationjsonCharsetUTF8200.Name == nil {
		err := apperrors.New(apperrors.KindInternal, fmt.Sprintf("failed to resolve project name for key %s", projectKey), nil)
		p.cache[cacheKey] = err
		return err
	}

	name := *projResp.ApplicationjsonCharsetUTF8200.Name
	limit := float32(25)
	var start float32
	for {
		params := &openapigenerated.GetProjectsParams{
			Name:       &name,
			Permission: &permission,
			Limit:      &limit,
			Start:      &start,
		}

		resp, err := p.client.GetProjectsWithResponse(ctx, params)
		if err != nil {
			err = transportFailure(err)
			p.cache[cacheKey] = err
			return err
		}
		if resp.StatusCode() >= 400 {
			err := openapi.MapStatusError(resp.StatusCode(), resp.Body)
			p.cache[cacheKey] = err
			return err
		}

		page := resp.ApplicationjsonCharsetUTF8200
		if page != nil && page.Values != nil {
			for _, proj := range *page.Values {
				if proj.Key != nil && strings.EqualFold(*proj.Key, projectKey) {
					p.cache[cacheKey] = nil
					return nil
				}
			}
		}

		if page == nil || page.IsLastPage == nil || *page.IsLastPage || page.NextPageStart == nil {
			break
		}
		start = float32(*page.NextPageStart)
	}

	err = apperrors.New(apperrors.KindAuthorization, fmt.Sprintf("insufficient permission: %s required on project %s", permission, projectKey), nil)
	p.cache[cacheKey] = err
	return err
}

// CheckProjectAdmin verifies if the caller has PROJECT_ADMIN on a project.
func (p *PermissionChecker) CheckProjectAdmin(ctx context.Context, projectKey string) error {
	cacheKey := fmt.Sprintf("project:%s:PROJECT_ADMIN", projectKey)
	if err, ok := p.cache[cacheKey]; ok {
		return err
	}

	limit := float32(1)
	params := &openapigenerated.GetUsersWithAnyPermission1Params{
		Limit: &limit,
	}

	resp, err := p.client.GetUsersWithAnyPermission1WithResponse(ctx, projectKey, params)
	if err != nil {
		err = transportFailure(err)
		p.cache[cacheKey] = err
		return err
	}
	if resp.StatusCode() >= 400 {
		err := openapi.MapStatusError(resp.StatusCode(), resp.Body)
		p.cache[cacheKey] = err
		return err
	}

	p.cache[cacheKey] = nil
	return nil
}

// CheckProjectRead verifies if the caller has PROJECT_READ on a project.
func (p *PermissionChecker) CheckProjectRead(ctx context.Context, projectKey string) error {
	return p.checkProjectListed(ctx, projectKey, "PROJECT_READ")
}

// InspectRepoPermissions probes REPO_READ, REPO_WRITE, and REPO_ADMIN for the caller on the
// given repository and returns a map of permission name to granted bool.
func (p *PermissionChecker) InspectRepoPermissions(ctx context.Context, projectKey, repoSlug string) (map[string]bool, error) {
	levels := []openapigenerated.GetRepositories1ParamsPermission{
		openapi.RepoRead,
		openapi.RepoWrite,
		openapi.RepoAdmin,
	}

	result := make(map[string]bool, len(levels))
	for _, level := range levels {
		err := p.CheckRepoPermission(ctx, projectKey, repoSlug, level)
		if err != nil {
			if apperrors.IsKind(err, apperrors.KindAuthorization) {
				result[string(level)] = false
				continue
			}
			return nil, err
		}
		result[string(level)] = true
	}
	return result, nil
}

// InspectProjectPermissions probes PROJECT_READ, PROJECT_WRITE, and PROJECT_ADMIN for the caller
// on the given project and returns a map of permission name to granted bool.
func (p *PermissionChecker) InspectProjectPermissions(ctx context.Context, projectKey string) (map[string]bool, error) {
	result := make(map[string]bool, 3)

	readErr := p.CheckProjectRead(ctx, projectKey)
	if readErr != nil {
		if !apperrors.IsKind(readErr, apperrors.KindAuthorization) {
			return nil, readErr
		}
		result["PROJECT_READ"] = false
	} else {
		result["PROJECT_READ"] = true
	}

	writeErr := p.CheckProjectWrite(ctx, projectKey)
	if writeErr != nil {
		if !apperrors.IsKind(writeErr, apperrors.KindAuthorization) {
			return nil, writeErr
		}
		result["PROJECT_WRITE"] = false
	} else {
		result["PROJECT_WRITE"] = true
	}

	adminErr := p.CheckProjectAdmin(ctx, projectKey)
	if adminErr != nil {
		if !apperrors.IsKind(adminErr, apperrors.KindAuthorization) {
			return nil, adminErr
		}
		result["PROJECT_ADMIN"] = false
	} else {
		result["PROJECT_ADMIN"] = true
	}

	return result, nil
}

// projectCreateProbeKey is a project key Bitbucket can never accept, so the
// probe below can never create a project.
const projectCreateProbeKey = "!!"

// CheckProjectCreate verifies if the caller can create projects by intentionally
// sending an invalid create payload.
//
// The payload names a key, and an invalid one. Bitbucket checks that a key is
// present before it checks the caller, so an empty payload answered 400 to
// everyone and every account was told it could create projects. A key that is
// present but malformed gets past that check: 401 for an account without
// PROJECT_CREATE, 400 for the key for one with it (observed on 10.4.3).
func (p *PermissionChecker) CheckProjectCreate(ctx context.Context) error {
	cacheKey := "global:PROJECT_CREATE"
	if err, ok := p.cache[cacheKey]; ok {
		return err
	}

	probeKey := projectCreateProbeKey
	resp, err := p.client.CreateProjectWithResponse(ctx, openapigenerated.RestProject{Key: &probeKey})
	if err != nil {
		err = transportFailure(err)
		p.cache[cacheKey] = err
		return err
	}

	switch resp.StatusCode() {
	case 400:
		p.cache[cacheKey] = nil
		return nil
	case 401, 403:
		err := openapi.MapStatusError(resp.StatusCode(), resp.Body)
		p.cache[cacheKey] = err
		return err
	default:
		err := apperrors.New(apperrors.KindPermanent, fmt.Sprintf("project create permission probe returned unexpected status %d", resp.StatusCode()), nil)
		p.cache[cacheKey] = err
		return err
	}
}

// transportFailure classifies a request that never reached Bitbucket.
//
// Every service wraps these as transient; the permission checker returned them
// raw, so the same unreachable host produced kind=transient on a real run and
// kind=internal under --dry-run, because the pre-flight is the only request the
// dry-run path makes. A consumer branching on kind was told to report a bug in
// bb when the network was down (#478).
func transportFailure(err error) error {
	if err == nil {
		return nil
	}
	// A cancelled context is the caller stopping, not the network failing.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if apperrors.KindOf(err) != apperrors.KindInternal {
		return err
	}

	return apperrors.Transport("permission pre-flight could not reach Bitbucket", err)
}
