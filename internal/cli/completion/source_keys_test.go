package completion

import (
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	tokenservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/token"
)

// TestTokenScopeFollowsTheFlagsOnTheLine covers the decision `bb auth token`
// completion turns on.
//
// Getting it wrong is not a wrong suggestion but a listing from another scope:
// the ids of a project's tokens offered to a revoke that will look for them
// among a user's. The command refuses an ambiently inferred repository
// (ADR-039), so nothing but these three flags may decide it.
func TestTokenScopeFollowsTheFlagsOnTheLine(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		user       string
		project    string
		repository string
		scope      tokenservice.ScopeType
		target     string
	}{
		{
			// Nothing named is the authenticated user, whose slug the source
			// asks the instance for rather than guessing here.
			name:  "no scope named defers to the authenticated user",
			scope: tokenservice.ScopeUser,
		},
		{name: "a named user is the scope", user: "alice", scope: tokenservice.ScopeUser, target: "alice"},
		{name: "a project is the scope", project: "PROJ", scope: tokenservice.ScopeProject, target: "PROJ"},
		{
			name:       "a repository is the scope",
			repository: "PROJ/api",
			scope:      tokenservice.ScopeRepo,
			target:     "PROJ/api",
		},
		{
			// Whitespace is not a scope. A flag given as "" or " " is one the
			// shell passed through, not one the person chose.
			name:  "a blank flag names nothing",
			user:  "   ",
			scope: tokenservice.ScopeUser,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scope, target, err := tokenScope(testCase.user, testCase.project, testCase.repository)
			if err != nil {
				t.Fatalf("tokenScope(%q, %q, %q) failed: %v",
					testCase.user, testCase.project, testCase.repository, err)
			}
			if scope != testCase.scope {
				t.Errorf("scope is %q, want %q", scope, testCase.scope)
			}
			if target != testCase.target {
				t.Errorf("target is %q, want %q", target, testCase.target)
			}
		})
	}
}

// TestTwoScopesAtOnceCompleteNothing pins the case where guessing would be
// worst.
//
// The command refuses the line outright, so there is no scope to list and no
// way to pick one that is not a coin toss. It comes back as a validation
// failure, which is the class run.go turns into an Active Help line rather
// than into silence.
func TestTwoScopesAtOnceCompleteNothing(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name       string
		user       string
		project    string
		repository string
	}{
		{name: "user and project", user: "alice", project: "PROJ"},
		{name: "user and repository", user: "alice", repository: "PROJ/api"},
		{name: "project and repository", project: "PROJ", repository: "PROJ/api"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, _, err := tokenScope(testCase.user, testCase.project, testCase.repository)
			if err == nil {
				t.Fatalf("tokenScope(%q, %q, %q) picked a scope from a line the command refuses",
					testCase.user, testCase.project, testCase.repository)
			}
			if apperrors.KindOf(err) != apperrors.KindValidation {
				t.Errorf("error is kind %v, want a validation failure so the shell can say why", apperrors.KindOf(err))
			}
		})
	}
}

// TestTokenListingPathAddressesTheScopeItNames holds the three listings apart.
//
// A project and the repositories inside it keep separate sets of tokens, and
// the paths differ by one segment, so a path built for the wrong one answers
// with somebody else's ids rather than failing.
func TestTokenListingPathAddressesTheScopeItNames(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		scope  tokenservice.ScopeType
		target string
		path   string
	}{
		{
			name:   "a user",
			scope:  tokenservice.ScopeUser,
			target: "alice",
			path:   "/rest/access-tokens/latest/users/alice",
		},
		{
			name:   "a project",
			scope:  tokenservice.ScopeProject,
			target: "PROJ",
			path:   "/rest/access-tokens/latest/projects/PROJ",
		},
		{
			name:   "a repository",
			scope:  tokenservice.ScopeRepo,
			target: "PROJ/api",
			path:   "/rest/access-tokens/latest/projects/PROJ/repos/api",
		},
		{
			// A personal project is spelled ~user, and the tilde has to reach
			// the server as a tilde rather than as %7E.
			name:   "a personal project keeps its tilde",
			scope:  tokenservice.ScopeProject,
			target: "~admin",
			path:   "/rest/access-tokens/latest/projects/~admin",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			path, err := tokenListingPath(testCase.scope, testCase.target)
			if err != nil {
				t.Fatalf("tokenListingPath(%q, %q) failed: %v", testCase.scope, testCase.target, err)
			}
			if path != testCase.path {
				t.Errorf("path is %q, want %q", path, testCase.path)
			}
		})
	}
}

// TestAnUnusableTokenScopeIsRefusedRatherThanSent covers the targets that would
// address the wrong collection.
//
// "PROJ" under a repository scope is half a selector, and the path built from
// it would be the project's listing -- a different set of tokens, answered with
// a 200.
func TestAnUnusableTokenScopeIsRefusedRatherThanSent(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name   string
		scope  tokenservice.ScopeType
		target string
	}{
		{name: "a repository without a slug", scope: tokenservice.ScopeRepo, target: "PROJ"},
		{name: "a repository without a project", scope: tokenservice.ScopeRepo, target: "/api"},
		{name: "an empty target", scope: tokenservice.ScopeUser, target: "  "},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			path, err := tokenListingPath(testCase.scope, testCase.target)
			if err == nil {
				t.Fatalf("tokenListingPath(%q, %q) built %q from a target the command would refuse",
					testCase.scope, testCase.target, path)
			}
			if apperrors.KindOf(err) != apperrors.KindValidation {
				t.Errorf("error is kind %v, want a validation failure", apperrors.KindOf(err))
			}
		})
	}
}

// TestADescriptionNamesTheThingBeingActedOn is the assertion the whole file
// exists for.
//
// A key or an id on its own says nothing about what deleting it removes, and
// the failure is invisible: a shell showing twelve digits with no description
// looks exactly like a shell showing twelve digits with one. So each builder is
// held against what the server returns, including the halves that can be
// missing.
func TestADescriptionNamesTheThingBeingActedOn(t *testing.T) {
	t.Parallel()

	t.Run("a build status leads with its state", func(t *testing.T) {
		t.Parallel()

		state := openapigenerated.RestBuildStatusState("FAILED")
		name, description := "Nightly", "set by CI"

		if got := describeBuildStatus(openapigenerated.RestBuildStatus{
			State: &state,
			Name:  &name,
		}); got != "FAILED: Nightly" {
			t.Errorf("described as %q, want the state and the name", got)
		}

		// A build with no name is still worth telling from the others, and its
		// description is the only other thing it carries.
		if got := describeBuildStatus(openapigenerated.RestBuildStatus{
			State:       &state,
			Description: &description,
		}); got != "FAILED: set by CI" {
			t.Errorf("described as %q, want the description standing in for the name", got)
		}

		if got := describeBuildStatus(openapigenerated.RestBuildStatus{State: &state}); got != "FAILED" {
			t.Errorf("described as %q, want the state alone", got)
		}
	})

	t.Run("an insight report carries its title and result", func(t *testing.T) {
		t.Parallel()

		title := "Coverage"
		result := openapigenerated.RestInsightReportResult("FAIL")

		if got := describeInsightReport(openapigenerated.RestInsightReport{
			Title:  &title,
			Result: &result,
		}); got != "Coverage (FAIL)" {
			t.Errorf("described as %q, want the title and the result", got)
		}

		if got := describeInsightReport(openapigenerated.RestInsightReport{Title: &title}); got != "Coverage" {
			t.Errorf("described as %q, want the title alone", got)
		}

		// A report with no title is still pass or fail, which is the part
		// worth showing beside a key that says nothing.
		if got := describeInsightReport(openapigenerated.RestInsightReport{Result: &result}); got != "FAIL" {
			t.Errorf("described as %q, want the result alone", got)
		}
	})

	t.Run("an ssh key carries its label and fingerprint", func(t *testing.T) {
		t.Parallel()

		label, fingerprint, algorithm := "laptop", "SHA256:abc", "ED25519"

		if got := describeSSHKey(openapigenerated.RestSshKey{
			Label:       &label,
			Fingerprint: &fingerprint,
		}); got != "laptop (SHA256:abc)" {
			t.Errorf("described as %q, want the label and the fingerprint", got)
		}

		// An instance that reports no fingerprint still says what kind of key
		// it is, which is better than an id on its own.
		if got := describeSSHKey(openapigenerated.RestSshKey{
			Label:         &label,
			AlgorithmType: &algorithm,
		}); got != "laptop (ED25519)" {
			t.Errorf("described as %q, want the algorithm standing in for the fingerprint", got)
		}

		if got := describeSSHKey(openapigenerated.RestSshKey{Fingerprint: &fingerprint}); got != "SHA256:abc" {
			t.Errorf("described as %q, want the fingerprint alone", got)
		}
	})

	t.Run("an access token carries its name and permissions", func(t *testing.T) {
		t.Parallel()

		got := describeAccessToken(accessTokenValue{
			ID:          "017706436507",
			Name:        "ci",
			Permissions: []string{"PROJECT_ADMIN", "REPO_ADMIN"},
		})
		if got != "ci (PROJECT_ADMIN, REPO_ADMIN)" {
			t.Errorf("described as %q, want the name and what the token can do", got)
		}

		if got := describeAccessToken(accessTokenValue{ID: "1", Name: "ci"}); got != "ci" {
			t.Errorf("described as %q, want the name alone", got)
		}
	})
}

// TestAGPGKeyIsOfferedInOneSpellingAndDescribedInTheOther covers the argument
// that takes either.
//
// `bb auth gpg-key remove <id-or-fingerprint>` resolves both, so the value is
// whichever Bitbucket put first and the other one goes beside it -- the
// fingerprint being the half a person can check against their own keyring.
func TestAGPGKeyIsOfferedInOneSpellingAndDescribedInTheOther(t *testing.T) {
	t.Parallel()

	id := "55f74ab3de0252e3"
	fingerprint := "04f49bfb9271acc0e0ef25d055f74ab3de0252e3"
	email := "dev@example.invalid"

	candidate, ok := gpgKeyCandidate(openapigenerated.RestGpgKey{
		Id:           &id,
		Fingerprint:  &fingerprint,
		EmailAddress: &email,
	})
	if !ok {
		t.Fatal("a key carrying both an id and a fingerprint was dropped")
	}
	if candidate.Value != id {
		t.Errorf("offered %q, want the id Bitbucket returned", candidate.Value)
	}
	if candidate.Description != fingerprint+" "+email {
		t.Errorf("described as %q, want the fingerprint and the address", candidate.Description)
	}

	// An instance that identifies a key only by its fingerprint offers that,
	// which the command resolves just as well.
	candidate, ok = gpgKeyCandidate(openapigenerated.RestGpgKey{Fingerprint: &fingerprint})
	if !ok {
		t.Fatal("a key carrying only a fingerprint was dropped")
	}
	if candidate.Value != fingerprint {
		t.Errorf("offered %q, want the fingerprint", candidate.Value)
	}

	// Neither spelling is nothing to complete, and a blank candidate would be
	// a line the shell shows with no value in it.
	if _, ok := gpgKeyCandidate(openapigenerated.RestGpgKey{EmailAddress: &email}); ok {
		t.Error("a key with neither an id nor a fingerprint was offered")
	}
}
