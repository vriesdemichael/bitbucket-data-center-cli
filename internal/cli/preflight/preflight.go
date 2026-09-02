// Package preflight runs the permission checks a dry run makes before it
// predicts anything.
//
// A --dry-run that answers "this would succeed" without knowing whether the
// caller may perform the operation is guessing. Every stateful command
// therefore asks the server what the caller can do first, and the check is
// skipped only where the dependency was not wired -- which is how the unit
// tests run without a server.
//
// The check existed at eighty call sites as the same six lines, and the
// divergences between copies are what this package is for. One of the fourteen
// pre-flights in the pull request package asked for read access on an operation
// that writes a commit (#481); one command in a file of thirteen had no check
// at all. Both look right in review, because the diff for a new command matches
// its neighbours and the neighbour it was copied from may be the wrong one.
//
// Reducing the site to a single argument does not make the wrong level
// impossible, but it makes it visible: the level is the only thing left to get
// wrong, and "which commands check nothing" becomes a grep rather than an
// audit.
package preflight

import (
	"context"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

// RepoChecker is the one method this package needs.
//
// Each command package declares its own PermissionChecker interface, carrying
// whichever methods that package uses. They all satisfy this, so the helper
// stays generic over the concrete type rather than forcing the packages onto a
// shared interface they do not all want.
type RepoChecker interface {
	CheckRepoPermission(ctx context.Context, projectKey, repoSlug string, permission openapigenerated.GetRepositories1ParamsPermission) error
}

// RepoPermission checks that the caller holds permission on a repository.
//
// It returns nil when the checker was not wired, which is what the two nil
// guards at every call site did: a command constructed without a
// PermissionChecker, or a factory that declines to build one, runs without the
// pre-flight rather than failing. Tests rely on that.
func RepoPermission[C RepoChecker](
	ctx context.Context,
	newChecker func(*openapigenerated.ClientWithResponses) C,
	client *openapigenerated.ClientWithResponses,
	projectKey string,
	repoSlug string,
	permission openapi.RepositoryPermission,
) error {
	if newChecker == nil {
		return nil
	}

	checker := newChecker(client)
	// Compared as an interface, because C is one: a factory that returns a nil
	// checker is the second guard the call sites had, and it is how a test
	// wires the dependency without providing a server to ask.
	if any(checker) == nil {
		return nil
	}

	// No conversion: openapi.RepositoryPermission is an alias for the generated
	// type, so the named constants and the wire type are the same thing.
	return checker.CheckRepoPermission(ctx, projectKey, repoSlug, permission)
}

// ProjectAdminChecker, ProjectWriteChecker and ProjectCreateChecker are one
// method each, for the same reason RepoChecker is: the command packages declare
// different subsets, and a helper that demanded all of them would exclude the
// packages that only need one.
type ProjectAdminChecker interface {
	CheckProjectAdmin(ctx context.Context, projectKey string) error
}

type ProjectWriteChecker interface {
	CheckProjectWrite(ctx context.Context, projectKey string) error
}

type ProjectCreateChecker interface {
	CheckProjectCreate(ctx context.Context) error
}

// ProjectAdmin checks that the caller administers a project.
func ProjectAdmin[C ProjectAdminChecker](
	ctx context.Context,
	newChecker func(*openapigenerated.ClientWithResponses) C,
	client *openapigenerated.ClientWithResponses,
	projectKey string,
) error {
	if newChecker == nil {
		return nil
	}
	checker := newChecker(client)
	if any(checker) == nil {
		return nil
	}

	return checker.CheckProjectAdmin(ctx, projectKey)
}

// ProjectWrite checks that the caller may write to a project.
func ProjectWrite[C ProjectWriteChecker](
	ctx context.Context,
	newChecker func(*openapigenerated.ClientWithResponses) C,
	client *openapigenerated.ClientWithResponses,
	projectKey string,
) error {
	if newChecker == nil {
		return nil
	}
	checker := newChecker(client)
	if any(checker) == nil {
		return nil
	}

	return checker.CheckProjectWrite(ctx, projectKey)
}

// ProjectCreate checks that the caller may create a project. It takes no key,
// because there is no project yet.
func ProjectCreate[C ProjectCreateChecker](
	ctx context.Context,
	newChecker func(*openapigenerated.ClientWithResponses) C,
	client *openapigenerated.ClientWithResponses,
) error {
	if newChecker == nil {
		return nil
	}
	checker := newChecker(client)
	if any(checker) == nil {
		return nil
	}

	return checker.CheckProjectCreate(ctx)
}
