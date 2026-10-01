//go:build live

package live_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// The repo admin spellings of create, fork and delete are aliases of the
// canonical repo commands, and an alias is one only while it writes the
// canonical command's output byte for byte (ADR-050).
//
// Two repositories cannot share a name, so each test runs the two spellings on
// two equivalent fresh targets in one project, and the documents differ in what
// has to differ between two repositories: each one's id, and its name, which is
// also its slug. Exactly those are replaced before the comparison, by the
// values a separate read says each repository has. Nothing else is: these
// documents carry no timestamp, and meta.command names the canonical command
// for both spellings (ADR-096), so it is compared like the rest.
//
// The --dry-run previews are compared too, on one shared target and with
// nothing replaced: the alias's dry run is a branch of its own in the code.

func TestLiveRepoAdminCreateWritesTheRepoCreateOutput(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	configureLiveCLIEnv(t, harness, seeded.Key, seeded.Repos[0].Slug)

	// A value other than the default for every flag both spellings take, so
	// an alias that dropped one could not match by accident.
	const description = "alias parity description"
	flags := func(name string) []string {
		return []string{"--project", seeded.Key, "--name", name, "--description", description, "--forkable=false"}
	}

	previewName := testsupport.UniqueName("alias-create-preview-")
	assertAliasParity(t, "repo admin create --dry-run",
		mustLiveCLI(t, append([]string{"--dry-run", "repo", "create"}, flags(previewName)...)...), "repo create",
		mustLiveCLI(t, append([]string{"--dry-run", "repo", "admin", "create"}, flags(previewName)...)...))

	canonicalName := testsupport.UniqueName("alias-create-canonical-")
	aliasName := testsupport.UniqueName("alias-create-alias-")
	canonical := mustLiveCLI(t, append([]string{"repo", "create"}, flags(canonicalName)...)...)
	alias := mustLiveCLI(t, append([]string{"repo", "admin", "create"}, flags(aliasName)...)...)

	canonicalStored := repoAdminReadBack(t, seeded.Key+"/"+canonicalName)
	aliasStored := repoAdminReadBack(t, seeded.Key+"/"+aliasName)
	for name, stored := range map[string]map[string]any{canonicalName: canonicalStored, aliasName: aliasStored} {
		if stored["projectKey"] != seeded.Key || stored["name"] != name || stored["slug"] != name ||
			stored["description"] != description || stored["forkable"] != false {
			t.Fatalf("%s reads back as %v, want it in %s, named and slugged %s, described %q and not forkable",
				name, stored, seeded.Key, name, description)
		}
	}

	assertAliasParity(t, "repo admin create",
		withoutRepositoryIdentity(t, canonical, canonicalStored, true), "repo create",
		withoutRepositoryIdentity(t, alias, aliasStored, true))
}

func TestLiveRepoAdminForkWritesTheRepoForkOutput(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	origin := seeded.Repos[0]
	originRef := seeded.Key + "/" + origin.Slug
	configureLiveCLIEnv(t, harness, seeded.Key, origin.Slug)

	// Into the seeded project rather than the personal one a fork without
	// --project goes to, so the project's cleanup takes the forks with it.
	flags := func(name string) []string {
		return []string{"--repo", originRef, "--name", name, "--project", seeded.Key}
	}

	previewName := testsupport.UniqueName("alias-fork-preview-")
	assertAliasParity(t, "repo admin fork --dry-run",
		mustLiveCLI(t, append([]string{"--dry-run", "repo", "fork"}, flags(previewName)...)...), "repo fork",
		mustLiveCLI(t, append([]string{"--dry-run", "repo", "admin", "fork"}, flags(previewName)...)...))

	canonicalName := testsupport.UniqueName("alias-fork-canonical-")
	aliasName := testsupport.UniqueName("alias-fork-alias-")
	canonical := mustLiveCLI(t, append([]string{"repo", "fork"}, flags(canonicalName)...)...)
	alias := mustLiveCLI(t, append([]string{"repo", "admin", "fork"}, flags(aliasName)...)...)

	canonicalStored := repoAdminReadBack(t, seeded.Key+"/"+canonicalName)
	aliasStored := repoAdminReadBack(t, seeded.Key+"/"+aliasName)
	for name, stored := range map[string]map[string]any{canonicalName: canonicalStored, aliasName: aliasStored} {
		forkedFrom, _ := stored["origin"].(map[string]any)
		if stored["projectKey"] != seeded.Key || stored["name"] != name || stored["slug"] != name ||
			forkedFrom["projectKey"] != seeded.Key || forkedFrom["slug"] != origin.Slug {
			t.Fatalf("%s reads back as %v, want it in %s, named and slugged %s, forked from %s",
				name, stored, seeded.Key, name, originRef)
		}
	}

	assertAliasParity(t, "repo admin fork",
		withoutRepositoryIdentity(t, canonical, canonicalStored, true), "repo fork",
		withoutRepositoryIdentity(t, alias, aliasStored, true))
}

func TestLiveRepoAdminDeleteWritesTheRepoDeleteOutput(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	// Two repositories seeded alike, one for each spelling to delete.
	seeded, err := harness.seedIsolatedProject(ctx, 2, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	canonicalTarget := seeded.Key + "/" + seeded.Repos[0].Slug
	aliasTarget := seeded.Key + "/" + seeded.Repos[1].Slug
	configureLiveCLIEnv(t, harness, seeded.Key, seeded.Repos[0].Slug)

	// Named with --repo, which both spellings take, so --yes applies.
	assertAliasParity(t, "repo admin delete --dry-run",
		mustLiveCLI(t, "--dry-run", "repo", "delete", "--repo", canonicalTarget, "--yes"), "repo delete",
		mustLiveCLI(t, "--dry-run", "repo", "admin", "delete", "--repo", canonicalTarget, "--yes"))

	canonicalStored := repoAdminReadBack(t, canonicalTarget)
	aliasStored := repoAdminReadBack(t, aliasTarget)

	canonical := mustLiveCLI(t, "repo", "delete", "--repo", canonicalTarget, "--yes")
	alias := mustLiveCLI(t, "repo", "admin", "delete", "--repo", aliasTarget, "--yes")

	// Bitbucket answers a delete of a repository that is not there with 204,
	// so the status in either document does not say that anything went.
	assertRepoAdminGone(t, canonicalTarget)
	assertRepoAdminGone(t, aliasTarget)

	assertAliasParity(t, "repo admin delete",
		withoutRepositoryIdentity(t, canonical, canonicalStored, false), "repo delete",
		withoutRepositoryIdentity(t, alias, aliasStored, false))
}

// withoutRepositoryIdentity replaces, in one command's output, the values that
// identify the repository it acted on, as a separate read gave them: its name,
// which is also its slug in these tests, and its id where the document carries
// one. A delete reports only where the repository was, so it carries no id. A
// document missing a value it should carry fails the test rather than being
// compared without it: a value that is not there was not normalised but lost.
func withoutRepositoryIdentity(t *testing.T, output string, stored map[string]any, carriesID bool) string {
	t.Helper()

	name, _ := stored["name"].(string)
	if name == "" || stored["slug"] != name {
		t.Fatalf("the read-back gives no name that is also the slug: %v", stored)
	}
	quotedName := fmt.Sprintf("%q", name)
	if !strings.Contains(output, quotedName) {
		t.Fatalf("%s is not in the output to normalise:\n%s", quotedName, output)
	}
	output = strings.ReplaceAll(output, quotedName, `"<name>"`)

	if !carriesID {
		return output
	}
	id, _ := stored["id"].(float64)
	idField := fmt.Sprintf(`"id": %d`, int64(id))
	if id == 0 || strings.Count(output, idField) != 1 {
		t.Fatalf("the output does not carry the id the read-back gives (%v) exactly once:\n%s", stored["id"], output)
	}

	return strings.Replace(output, idField, `"id": "<id>"`, 1)
}
