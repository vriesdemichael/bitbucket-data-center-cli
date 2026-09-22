//go:build live

package live_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// TestLiveCompletionKeysOnACommit asserts the two kinds that hang off a commit:
// the build statuses reported for it and the code insight reports attached to
// it.
//
// Every assertion is on the seeded key *and* on its description. A key is a
// word somebody's CI chose -- "ci", "sonar", "coverage" -- and a shell showing
// a column of them has told the reader nothing about which build failed or
// which report is the stale one. A source that offered bare keys would pass a
// test that only looked for the key, in every shell, with stderr discarded.
func TestLiveCompletionKeysOnACommit(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	// Two commits, because the scoping is the half a listing cannot show: a
	// source that ignored the commit on the line and listed the repository
	// would offer exactly the right keys on a repository with one commit in it.
	seeded, err := harness.seedRepo(ctx, repoSeed{Commits: 2, WithCommitIDs: true})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repository := seeded.Repos[0]
	selector := seeded.Key + "/" + repository.Slug

	commits, err := harness.listCommitIDs(ctx, seeded.Key, repository.Slug, 2)
	if err != nil || len(commits) < 2 {
		t.Fatalf("list commit ids failed: %v (%d commits)", err, len(commits))
	}
	commit, otherCommit := commits[0], commits[1]

	const (
		buildKey        = "cmpl-build"
		otherBuildKey   = "cmpl-build-elsewhere"
		buildName       = "Completion Suite Build"
		reportKey       = "cmpl-report"
		otherReportKey  = "cmpl-report-elsewhere"
		reportTitle     = "Completion Suite Report"
		reportBodyPass  = `{"title":"Completion Suite Report","result":"PASS","details":"seeded by the live suite"}`
		reportBodyOther = `{"title":"Somewhere Else","result":"FAIL"}`
	)

	seedBuild := func(on string, key string, state string, name string) {
		t.Helper()

		output, err := executeLiveCLIUnscoped(t, "--json", "build", "set", on,
			"--key", key, "--state", state, "--name", name,
			"--url", "http://localhost:65535/builds/1", "--repo", selector)
		if err != nil {
			t.Fatalf("build set %s on %s failed: %v\noutput: %s", key, on, err, output)
		}
	}

	seedReport := func(on string, key string, body string) {
		t.Helper()

		output, err := executeLiveCLIUnscoped(t, "--json", "insights", "report", "set", on, key,
			"--body", body, "--repo", selector)
		if err != nil {
			t.Fatalf("insights report set %s on %s failed: %v\noutput: %s", key, on, err, output)
		}
	}

	seedBuild(commit, buildKey, "FAILED", buildName)
	seedBuild(otherCommit, otherBuildKey, "SUCCESSFUL", "Elsewhere")
	seedReport(commit, reportKey, reportBodyPass)
	seedReport(otherCommit, otherReportKey, reportBodyOther)

	t.Run("a build key is offered with its state and name", func(t *testing.T) {
		candidates, directive := completeLive(t, "build", "get", commit, "--repo", selector, "--key", "")

		description, offered := candidates[buildKey]
		if !offered {
			t.Fatalf("build key %s was not offered for `bb build get --key`; got %v", buildKey, candidates)
		}
		if !strings.Contains(description, "FAILED") {
			t.Errorf("expected the build's state beside its key, got %q", description)
		}
		if !strings.Contains(description, buildName) {
			t.Errorf("expected the build's name beside its key, got %q", description)
		}
		if !forbidsFileNames(directive) {
			t.Errorf("expected the no-file-completion bit to be set, got directive %d", directive)
		}
	})

	t.Run("the keys are the commit's on the line", func(t *testing.T) {
		// The commit is an argument, not the checkout's HEAD and not the
		// repository's newest: `bb build delete <sha> --key` deletes from the
		// commit named beside it, and a key from another commit is a value the
		// command has nothing to delete.
		candidates, _ := completeLive(t, "build", "delete", commit, "--repo", selector, "--key", "")

		if _, offered := candidates[buildKey]; !offered {
			t.Fatalf("build key %s was not offered for `bb build delete --key`; got %v", buildKey, candidates)
		}
		if _, offered := candidates[otherBuildKey]; offered {
			t.Errorf("build key %s from another commit was offered for %s", otherBuildKey, commit)
		}
	})

	t.Run("a commit-scoped verb completes without a repository", func(t *testing.T) {
		// `bb build status set` writes through the endpoint that takes a commit
		// and no repository, so its --key has to complete on a line that names
		// none. It shares the listing with the repository-scoped verbs because
		// Bitbucket keeps one: a status written through /projects/.../builds
		// comes straight back out of /build-status/latest/commits/{id}.
		candidates, _ := completeLive(t, "build", "status", "set", commit, "--key", "")

		description, offered := candidates[buildKey]
		if !offered {
			t.Fatalf("build key %s was not offered for `bb build status set --key`; got %v", buildKey, candidates)
		}
		if !strings.Contains(description, buildName) {
			t.Errorf("expected the build's name beside its key, got %q", description)
		}
	})

	t.Run("a report key is offered with its title and result", func(t *testing.T) {
		candidates, directive := completeLive(t, "insights", "report", "get", commit, "--repo", selector, "")

		description, offered := candidates[reportKey]
		if !offered {
			t.Fatalf("report key %s was not offered for `bb insights report get`; got %v", reportKey, candidates)
		}
		if !strings.Contains(description, reportTitle) {
			t.Errorf("expected the report's title beside its key, got %q", description)
		}
		if !strings.Contains(description, "PASS") {
			t.Errorf("expected the report's result beside its key, got %q", description)
		}
		if !forbidsFileNames(directive) {
			t.Errorf("expected the no-file-completion bit to be set, got directive %d", directive)
		}
	})

	t.Run("an annotation takes the same report key", func(t *testing.T) {
		// The annotation commands spell the argument <report-key> as well, and
		// annotate a report that has to exist: Bitbucket answers a key it does
		// not hold with a 404 rather than creating one.
		candidates, _ := completeLive(t, "insights", "annotation", "add", commit, "--repo", selector, "")

		if _, offered := candidates[reportKey]; !offered {
			t.Fatalf("report key %s was not offered for `bb insights annotation add`; got %v", reportKey, candidates)
		}
		if _, offered := candidates[otherReportKey]; offered {
			t.Errorf("report key %s from another commit was offered for %s", otherReportKey, commit)
		}
	})

	t.Run("the reports are the commit's on the line", func(t *testing.T) {
		candidates, _ := completeLive(t, "insights", "report", "delete", otherCommit, "--repo", selector, "")

		if _, offered := candidates[otherReportKey]; !offered {
			t.Fatalf("report key %s was not offered for its own commit; got %v", otherReportKey, candidates)
		}
		if _, offered := candidates[reportKey]; offered {
			t.Errorf("report key %s from another commit was offered for %s", reportKey, otherCommit)
		}
	})
}

// TestLiveCompletionKeysOnTheAccount asserts the kinds that hang off the
// account rather than off a repository: the caller's own SSH keys, and the HTTP
// access tokens of whichever scope the line names.
//
// The token half is the one worth a live test rather than a unit test. `bb auth
// token` refuses an ambiently inferred repository (ADR-039) and reads its scope
// from --user, --project or --repo, so three separate collections answer the
// same argument -- and offering the wrong one hands the caller an id that
// `revoke` will look for somewhere else.
func TestLiveCompletionKeysOnTheAccount(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	selector := seeded.Key + "/" + seeded.Repos[0].Slug

	t.Run("an ssh key is offered with its label and fingerprint", func(t *testing.T) {
		label := testsupport.UniqueName("cmpl-ssh-")
		publicKey := generateSSHPublicKey(t, label)

		addOutput, err := executeLiveCLIUnscoped(t, "--json", "ssh-key", "add", publicKey, "--label", label)
		if err != nil {
			t.Fatalf("ssh-key add failed: %v\noutput: %s", err, addOutput)
		}

		added := decodeJSONMap(t, addOutput)
		keyID, ok := numericOrStringID(added["id"])
		if !ok {
			t.Fatalf("expected a key id in the add output: %s", addOutput)
		}
		t.Cleanup(func() {
			_, _ = executeLiveCLIUnscoped(t, "--json", "ssh-key", "remove", keyID, "--yes")
		})

		fingerprint, _ := added["fingerprint"].(string)
		if strings.TrimSpace(fingerprint) == "" {
			t.Fatalf("expected the server to report a fingerprint for the added key: %s", addOutput)
		}

		candidates, directive := completeLive(t, "ssh-key", "remove", "")

		description, offered := candidates[keyID]
		if !offered {
			t.Fatalf("ssh key %s was not offered for `bb ssh-key remove`; got %v", keyID, candidates)
		}
		if !strings.Contains(description, label) {
			t.Errorf("expected the key's label beside its id, got %q", description)
		}
		if !strings.Contains(description, fingerprint) {
			t.Errorf("expected the key's fingerprint beside its id, got %q", description)
		}
		if !forbidsFileNames(directive) {
			t.Errorf("expected the no-file-completion bit to be set, got directive %d", directive)
		}
	})

	t.Run("an access token is offered in its own scope and no other", func(t *testing.T) {
		userToken := testsupport.UniqueName("cmpl-user-")
		projectToken := testsupport.UniqueName("cmpl-proj-")
		repositoryToken := testsupport.UniqueName("cmpl-repo-")

		userID := createLiveToken(t, userToken, "REPO_READ", "--user", "admin")
		projectID := createLiveToken(t, projectToken, "PROJECT_ADMIN", "--project", seeded.Key)
		repositoryID := createLiveToken(t, repositoryToken, "REPO_WRITE", "--repo", selector)

		t.Run("a named user", func(t *testing.T) {
			candidates, directive := completeLive(t, "auth", "token", "revoke", "--user", "admin", "")

			description, offered := candidates[userID]
			if !offered {
				t.Fatalf("token %s was not offered for `bb auth token revoke --user admin`; got %v", userID, candidates)
			}
			if !strings.Contains(description, userToken) {
				t.Errorf("expected the token's name beside its id, got %q", description)
			}
			// The permissions are what revoking takes away, and a name like
			// "ci" does not say whether that was reading one repository or
			// administering every project.
			if !strings.Contains(description, "REPO_READ") {
				t.Errorf("expected the token's permissions beside its id, got %q", description)
			}
			if _, offered := candidates[projectID]; offered {
				t.Errorf("project token %s was offered to a user-scoped revoke", projectID)
			}
			if _, offered := candidates[repositoryID]; offered {
				t.Errorf("repository token %s was offered to a user-scoped revoke", repositoryID)
			}
			if !forbidsFileNames(directive) {
				t.Errorf("expected the no-file-completion bit to be set, got directive %d", directive)
			}
		})

		t.Run("no scope at all is the authenticated user", func(t *testing.T) {
			// The command's own default. Who that is comes from the instance
			// rather than from the configuration, which need not hold a
			// username at all when the credential is a token.
			candidates, _ := completeLive(t, "auth", "token", "get", "")

			if _, offered := candidates[userID]; !offered {
				t.Fatalf("token %s was not offered for `bb auth token get` with no scope named; got %v",
					userID, candidates)
			}
		})

		t.Run("a project", func(t *testing.T) {
			candidates, _ := completeLive(t, "auth", "token", "update", "--project", seeded.Key, "")

			description, offered := candidates[projectID]
			if !offered {
				t.Fatalf("token %s was not offered for `bb auth token update --project %s`; got %v",
					projectID, seeded.Key, candidates)
			}
			if !strings.Contains(description, projectToken) {
				t.Errorf("expected the token's name beside its id, got %q", description)
			}
			if !strings.Contains(description, "PROJECT_ADMIN") {
				t.Errorf("expected the token's permissions beside its id, got %q", description)
			}
			if _, offered := candidates[userID]; offered {
				t.Errorf("user token %s was offered to a project-scoped update", userID)
			}
			if _, offered := candidates[repositoryID]; offered {
				t.Errorf("repository token %s was offered to a project-scoped update", repositoryID)
			}
		})

		t.Run("a repository", func(t *testing.T) {
			// The scope a project's tokens are nearest to, and the one a path
			// built from half a selector would silently become.
			candidates, _ := completeLive(t, "auth", "token", "revoke", "--repo", selector, "")

			description, offered := candidates[repositoryID]
			if !offered {
				t.Fatalf("token %s was not offered for `bb auth token revoke --repo %s`; got %v",
					repositoryID, selector, candidates)
			}
			if !strings.Contains(description, repositoryToken) {
				t.Errorf("expected the token's name beside its id, got %q", description)
			}
			if !strings.Contains(description, "REPO_WRITE") {
				t.Errorf("expected the token's permissions beside its id, got %q", description)
			}
			if _, offered := candidates[projectID]; offered {
				t.Errorf("project token %s was offered to a repository-scoped revoke", projectID)
			}
		})
	})
}

// TestLiveCompletionKeysForGPGKeys asserts the argument that takes either
// spelling of a GPG key.
//
// It runs as a user of its own, which the two account-scoped kinds beside it do
// not need. A GPG key is unique across the whole instance -- a second account
// adding the same block is refused with DuplicateGpgKeyException -- and
// TestLiveGPGKeyLifecycle empties the administrator's keyring outright, so a
// test that added a key as the administrator would race a test that removes
// every one of them.
func TestLiveCompletionKeysForGPGKeys(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	user, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create a licensed user failed: %v", err)
	}
	setLiveCredentials(t, user)

	keyPath, err := filepath.Abs(filepath.Join("testdata", "live-suite-completion-gpg-public-key.asc"))
	if err != nil {
		t.Fatalf("resolve the fixture key path failed: %v", err)
	}

	addOutput, err := executeLiveCLIUnscoped(t, "--json", "auth", "gpg-key", "add", keyPath)
	if err != nil {
		t.Fatalf("auth gpg-key add failed: %v\noutput: %s", err, addOutput)
	}

	added := firstOfJSONArray(t, addOutput)
	keyID, _ := added["id"].(string)
	fingerprint, _ := added["fingerprint"].(string)
	if strings.TrimSpace(keyID) == "" || strings.TrimSpace(fingerprint) == "" {
		t.Fatalf("expected an id and a fingerprint from the add: %s", addOutput)
	}

	// Before the user goes, because the key is unique instance-wide: one left
	// behind by a run that could not delete its user makes the next run's add
	// a 409. Cleanups run in reverse, and the user's was registered first.
	t.Cleanup(func() {
		_, _ = executeLiveCLIUnscoped(t, "--json", "auth", "gpg-key", "remove", keyID, "--yes")
	})

	candidates, directive := completeLive(t, "auth", "gpg-key", "remove", "")

	description, offered := candidates[keyID]
	if !offered {
		t.Fatalf("gpg key %s was not offered for `bb auth gpg-key remove`; got %v", keyID, candidates)
	}
	// The argument resolves an id or a fingerprint, so the one not offered is
	// the one that identifies the key to its owner: it is what `gpg
	// --list-keys` prints and what the key was added with.
	if !strings.Contains(description, fingerprint) {
		t.Errorf("expected the key's fingerprint beside its id, got %q", description)
	}
	if !forbidsFileNames(directive) {
		t.Errorf("expected the no-file-completion bit to be set, got directive %d", directive)
	}
}

// createLiveToken creates an HTTP access token in one scope and returns its id,
// revoking it when the test ends.
func createLiveToken(t *testing.T, name string, permission string, scope ...string) string {
	t.Helper()

	arguments := append([]string{"--json", "auth", "token", "create", name, "--permission", permission}, scope...)

	output, err := executeLiveCLIUnscoped(t, arguments...)
	if err != nil {
		t.Fatalf("auth token create %v failed: %v\noutput: %s", scope, err, output)
	}

	id, ok := numericOrStringID(decodeJSONMap(t, output)["id"])
	if !ok {
		t.Fatalf("expected a token id in the create output: %s", output)
	}

	revoke := append([]string{"--json", "auth", "token", "revoke", id}, scope...)
	t.Cleanup(func() {
		_, _ = executeLiveCLIUnscoped(t, append(revoke, "--yes")...)
	})

	return id
}
