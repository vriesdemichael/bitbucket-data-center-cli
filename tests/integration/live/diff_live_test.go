//go:build live

package live_test

import (
	"context"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	diffservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/diff"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

func TestLiveDiffRefs(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	service := diffservice.NewService(harness.client)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Three commits, so that the diff spans two. With two, from is the parent
	// of to, and /patch without a since is exactly that one commit: a since that
	// was dropped produced the same patch.
	seeded, err := harness.seedRepo(ctx, repoSeed{Commits: 3, WithCommitIDs: true})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	if len(repo.CommitIDs) < 3 {
		t.Fatalf("expected at least 3 commits, got %d", len(repo.CommitIDs))
	}

	from := repo.CommitIDs[len(repo.CommitIDs)-1]
	to := repo.CommitIDs[0]
	result, err := service.DiffRefs(ctx, diffservice.DiffRefsInput{
		Repository: diffservice.RepositoryRef{ProjectKey: seeded.Key, Slug: repo.Slug},
		From:       from,
		To:         to,
		Output:     diffservice.OutputKindRaw,
	})
	if err != nil {
		t.Fatalf("diff refs failed: %v", err)
	}
	if result.Patch == "" {
		t.Fatal("expected non-empty raw diff output")
	}

	// Each seed commit appends a line to seed.txt. The first commit's line is
	// where the diff starts, so the other two are what it adds; without the
	// since, only the last would be.
	files, added, removed := diffLiveChanges(result.Patch)
	if !slices.Equal(files, []string{"seed.txt"}) || !slices.Equal(added, []string{"commit-2", "commit-3"}) || len(removed) != 0 {
		t.Errorf("the diff touches %v, adding %q and removing %q; want seed.txt gaining commit-2 and commit-3\n%s",
			files, added, removed, result.Patch)
	}
}

func TestLiveDiffPullRequest(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	service := diffservice.NewService(harness.client)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{WithCommitIDs: true})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	branch := testsupport.UniqueName("lt-feature-")
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "feature.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}

	pullRequestID, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, branch, "master")
	if err != nil {
		t.Fatalf("create pull request failed: %v", err)
	}

	result, err := service.DiffPR(ctx, diffservice.DiffPRInput{
		Repository:    diffservice.RepositoryRef{ProjectKey: seeded.Key, Slug: repo.Slug},
		PullRequestID: pullRequestID,
		Output:        diffservice.OutputKindRaw,
	})
	if err != nil {
		t.Fatalf("pull request diff failed: %v", err)
	}
	if result.Patch == "" {
		t.Fatal("expected non-empty pull request diff output")
	}

	// The branch's one commit writes its own name into feature.txt, and the name
	// is unique to this run, so no other pull request's diff can match.
	files, added, removed := diffLiveChanges(result.Patch)
	if !slices.Equal(files, []string{"feature.txt"}) || !slices.Equal(added, []string{"branch=" + branch}) || len(removed) != 0 {
		t.Errorf("pull request %s diff touches %v, adding %q and removing %q; want feature.txt gaining branch=%s\n%s",
			pullRequestID, files, added, removed, branch, result.Patch)
	}
}

// TestLiveDiffOutputModes covers the output kinds beside raw, and a ref that is
// not there.
//
// The mocks these replace served a canned patch and asserted what bb made of
// it -- which files it listed for name_only, what it counted for stat, how it
// mapped a 404. Each is a claim about what Bitbucket sends and when. A real
// diff of a real commit settles all of them, and a ref that does not exist
// produces the 404 rather than describing it.
func TestLiveDiffOutputModes(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	service := diffservice.NewService(harness.client)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{Commits: 2, WithCommitIDs: true})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	if len(repo.CommitIDs) < 2 {
		t.Fatalf("expected at least 2 commits, got %d", len(repo.CommitIDs))
	}
	repository := diffservice.RepositoryRef{ProjectKey: seeded.Key, Slug: repo.Slug}
	from := repo.CommitIDs[len(repo.CommitIDs)-1]

	// The far end is a branch one commit past master, not master's head. The
	// compare endpoint behind stat puts the default branch in place of a ref it
	// did not get, so an end on master's head could go missing without changing
	// the count. And with the ends two commits apart, a since that /patch did
	// not get leaves the first of them out of what name_only lists.
	const branch = "feature/output-modes"
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "modes.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}
	to := branch

	t.Run("name_only lists the files that changed", func(t *testing.T) {
		result, err := service.DiffRefs(ctx, diffservice.DiffRefsInput{
			Repository: repository, From: from, To: to, Output: diffservice.OutputKindNameOnly,
		})
		if err != nil {
			t.Fatalf("name_only diff failed: %v", err)
		}
		if len(result.Names) == 0 {
			t.Fatalf("expected at least one changed file, got %#v", result)
		}

		// seed.txt changes in the commit on master and modes.txt on the branch.
		if names := slices.Sorted(slices.Values(result.Names)); !slices.Equal(names, []string{"modes.txt", "seed.txt"}) {
			t.Errorf("name_only named %v, want modes.txt and seed.txt", result.Names)
		}
	})

	t.Run("stat counts the change", func(t *testing.T) {
		result, err := service.DiffRefs(ctx, diffservice.DiffRefsInput{
			Repository: repository, From: from, To: to, Output: diffservice.OutputKindStat,
		})
		if err != nil {
			t.Fatalf("stat diff failed: %v", err)
		}
		if len(result.Stats) == 0 {
			t.Fatalf("expected stat to report a summary, got %#v", result)
		}

		// The change name_only lists, counted: one line added to each file. The
		// summary endpoint counts what its from has and its to lacks, the other
		// way round from /patch, so refs passed through in the order the patch
		// takes them count nothing at all.
		want := map[string]float64{"filesChanged": 2, "totalInsertions": 2, "totalDeletions": 0}
		for field, value := range want {
			if got, ok := result.Stats[field].(float64); !ok || got != value {
				t.Errorf("stats %s = %v, want %v (summary %v)", field, result.Stats[field], value, result.Stats)
			}
		}
	})

	t.Run("a ref that does not exist is not found", func(t *testing.T) {
		_, err := service.DiffRefs(ctx, diffservice.DiffRefsInput{
			Repository: repository, From: "refs/heads/does-not-exist", To: to,
			Output: diffservice.OutputKindRaw,
		})
		if err == nil {
			t.Fatal("expected a missing ref to fail")
		}
		if apperrors.IsKind(err, apperrors.KindTransient) {
			t.Errorf("a missing ref is not a transient failure: %v", err)
		}
		if !apperrors.IsKind(err, apperrors.KindNotFound) {
			t.Errorf("a missing ref failed as %v, want not found", err)
		}
	})
}

// diffLiveHunkHeader reads the line counts off a hunk header. A count left out
// is one.
var diffLiveHunkHeader = regexp.MustCompile(`^@@ -\d+(?:,(\d+))? \+\d+(?:,(\d+))? @@`)

// diffLiveChanges reads the files a unified diff touches and the lines it adds
// and removes, so a diff can be compared exactly rather than for being there.
//
// Hunk lines are counted off each header rather than told apart by their first
// character: /patch answers with a series of mails, and the "---" separator and
// "-- " signature between them would read as removed lines.
func diffLiveChanges(patch string) (files, added, removed []string) {
	seen := map[string]bool{}
	record := func(side string) {
		path, _, _ := strings.Cut(side, "\t")
		// a/ and b/ from git; src:// and dst:// are how a pull request diff names
		// its two sides.
		for _, prefix := range []string{"a/", "b/", "src://", "dst://"} {
			path = strings.TrimPrefix(path, prefix)
		}
		if path != "" && path != "/dev/null" && !seen[path] {
			seen[path] = true
			files = append(files, path)
		}
	}

	count := func(value string) int {
		if value == "" {
			return 1
		}
		parsed, _ := strconv.Atoi(value)

		return parsed
	}

	lines := strings.Split(strings.ReplaceAll(patch, "\r\n", "\n"), "\n")
	for index := 0; index < len(lines); index++ {
		line := lines[index]
		switch {
		case strings.HasPrefix(line, "--- "):
			record(strings.TrimPrefix(line, "--- "))
		case strings.HasPrefix(line, "+++ "):
			record(strings.TrimPrefix(line, "+++ "))
		}

		match := diffLiveHunkHeader.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		for fromLeft, toLeft := count(match[1]), count(match[2]); (fromLeft > 0 || toLeft > 0) && index+1 < len(lines); {
			index++
			body := lines[index]
			switch {
			case strings.HasPrefix(body, `\`):
				// "\ No newline at end of file" annotates the line before it.
			case strings.HasPrefix(body, "+"):
				added = append(added, body[1:])
				toLeft--
			case strings.HasPrefix(body, "-"):
				removed = append(removed, body[1:])
				fromLeft--
			default:
				fromLeft--
				toLeft--
			}
		}
	}

	return files, added, removed
}
