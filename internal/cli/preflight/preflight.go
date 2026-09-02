// Package preflight runs the permission checks a dry run makes before it
// predicts anything.
//
// A --dry-run that answers "this would succeed" without knowing whether the
// caller may perform the operation is guessing. Every stateful command
// therefore asks the server what the caller can do first, and the check is
// skipped only where the dependency was not wired -- which is how the unit
// tests run without a server.
//
// The check existed at 101 call sites, 80 of them the same six lines, and the
// divergences between copies are what this package is for. One of the twenty
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
	"reflect"

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

// resolve builds the checker and reports whether there is one to ask.
//
// One copy of the guard, because the first version of this package wrote it
// out four times -- in the package whose whole argument is that copied guards
// drift.
func resolve[C any](newChecker func(*openapigenerated.ClientWithResponses) C, client *openapigenerated.ClientWithResponses) (C, bool) {
	var zero C

	if newChecker == nil {
		return zero, false
	}

	checker := newChecker(client)
	if isNil(checker) {
		return zero, false
	}

	return checker, true
}

// isNil reports whether a checker is absent, however it is spelled.
//
// This was `any(checker) == nil`, which is correct only while every call site
// instantiates C with an interface. Nothing in the constraint requires that --
// a concrete pointer satisfies it just as well -- and rootOptions.
// permissionCheckerFor already returns the concrete *permissionchecker.
// PermissionChecker, nil when there is no client. Instantiating on that type
// made the guard silently pass and the call below panic, turning a documented
// skip into a crash.
//
// It also catches the case neither the old inline guard nor its replacement
// saw: an interface holding a typed nil pointer, which is what the root
// command's wiring actually produces.
func isNil(checker any) bool {
	if checker == nil {
		return true
	}

	value := reflect.ValueOf(checker)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
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
	checker, ok := resolve(newChecker, client)
	if !ok {
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
	checker, ok := resolve(newChecker, client)
	if !ok {
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
	checker, ok := resolve(newChecker, client)
	if !ok {
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
	checker, ok := resolve(newChecker, client)
	if !ok {
		return nil
	}

	return checker.CheckProjectCreate(ctx)
}
