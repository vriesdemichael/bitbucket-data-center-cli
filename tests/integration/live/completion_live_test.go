//go:build live

package live_test

import (
	"bytes"
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// TestLiveCompletionOffersPullRequests is the live test every completion
// source is written against.
//
// Completion is the one surface where a wrong answer is invisible: a source
// that returns nothing looks exactly like a repository with nothing to offer,
// in every shell, with stderr discarded. So the assertions are on the values
// themselves -- the seeded pull request's id, carrying its title -- rather
// than on the call having succeeded.
func TestLiveCompletionOffersPullRequests(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{WithCommitIDs: true})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	branch := testsupport.UniqueName("lt-completion-")
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "completion-feature.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}

	pullRequestID, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, branch, "master")
	if err != nil {
		t.Fatalf("create pull request failed: %v", err)
	}

	selector := seeded.Key + "/" + repo.Slug

	t.Run("the open pull request is offered with its title", func(t *testing.T) {
		candidates, directive := completeLive(t, "pr", "merge", "--repo", selector, "")

		description, offered := candidates[pullRequestID]
		if !offered {
			t.Fatalf("pull request %s was not offered for `bb pr merge`; got %v", pullRequestID, candidates)
		}
		if !strings.Contains(description, "Live test PR") {
			t.Errorf("expected the pull request's title beside its id, got %q", description)
		}
		if !forbidsFileNames(directive) {
			t.Errorf("expected the shell to be told not to fall back to file names, got directive %d", directive)
		}
	})

	t.Run("a verb that cannot act on it is not offered it", func(t *testing.T) {
		// bb pr reopen takes a declined pull request. Offering an open one is
		// offering a value the command refuses, which is the difference
		// between completion and a listing.
		candidates, _ := completeLive(t, "pr", "reopen", "--repo", selector, "")

		if _, offered := candidates[pullRequestID]; offered {
			t.Errorf("open pull request %s was offered to `bb pr reopen`, which only accepts declined ones", pullRequestID)
		}
	})

	t.Run("an argument that is not a path never offers file names", func(t *testing.T) {
		// The shell falls back to listing the working directory whenever a
		// completion says nothing and does not forbid it, which is what `bb pr
		// merge <tab>` used to do. Asserted against a repository that does
		// have candidates, so the directive is not right by accident.
		_, directive := completeLive(t, "pr", "merge", "--repo", selector, "zzz-no-such-pull-request")

		if !forbidsFileNames(directive) {
			t.Errorf("expected the no-file-completion bit to be set, got directive %d", directive)
		}
	})
}

// TestLiveCompletionIsSilentWhenTheServerCannotBeReached pins the behaviour
// that makes completion usable on a laptop that is not on the VPN.
//
// A tab press that printed an error would write it over the line being typed.
// So an unreachable instance completes nothing, says nothing, and exits 0 --
// and it has to do it within the press rather than after it, which is what the
// deadline in internal/cli/completion is for.
func TestLiveCompletionIsSilentWhenTheServerCannotBeReached(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	overrides := liveCLIOverrides(t)
	// Reserved by RFC 6335 as "do not use": nothing listens, and the refusal
	// is immediate rather than a hang the deadline has to cut short.
	overrides.Host = "http://127.0.0.1:9"

	command := cli.NewRootCommandWithOverrides(overrides)
	output := &bytes.Buffer{}
	command.SetOut(output)
	command.SetErr(output)
	command.SetArgs([]string{"__complete", "pr", "merge", "--repo", "PRJ/nothing", ""})

	started := time.Now()
	if err := command.Execute(); err != nil {
		t.Fatalf("completion against an unreachable instance failed instead of completing nothing: %v", err)
	}
	elapsed := time.Since(started)

	candidates, directive := parseCompletionOutput(t, output.String())
	if len(candidates) != 0 {
		t.Errorf("expected no candidates from an unreachable instance, got %v", candidates)
	}
	if !forbidsFileNames(directive) {
		t.Errorf("expected the no-file-completion bit to be set, got directive %d", directive)
	}
	if elapsed > 5*time.Second {
		t.Errorf("a tab press against an unreachable instance took %s; it is bounded to about a second", elapsed)
	}

	_ = harness
}

// forbidsFileNames reports the bit that stops the shell listing the working
// directory when a completion has nothing to add.
//
// A bit rather than the whole value: a source that ranks its answer also sets
// the keep-order bit, so the directive for a good completion is 36 rather
// than 4, and an equality check here would fail on the sources that are
// working hardest.
func forbidsFileNames(directive int) bool {
	return directive&int(cobra.ShellCompDirectiveNoFileComp) != 0
}

// completeLive runs one tab press through the real command tree, the way a
// shell does: the hidden __complete command, the words typed so far, and the
// word being completed last.
func completeLive(t *testing.T, words ...string) (map[string]string, int) {
	t.Helper()

	output, err := executeLiveCLIUnscoped(t, append([]string{"__complete"}, words...)...)
	if err != nil {
		t.Fatalf("completion failed: %v\noutput: %s", err, output)
	}

	return parseCompletionOutput(t, output)
}

// parseCompletionOutput reads Cobra's completion protocol: one candidate per
// line as value<tab>description, then a line holding the directive.
func parseCompletionOutput(t *testing.T, output string) (map[string]string, int) {
	t.Helper()

	candidates := map[string]string{}
	directive := -1

	for _, line := range strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n") {
		line = strings.TrimRight(line, "\r")
		switch {
		case strings.TrimSpace(line) == "":
			continue
		case strings.HasPrefix(line, ":"):
			parsed, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, ":")))
			if err != nil {
				t.Fatalf("completion directive %q is not a number: %v", line, err)
			}
			directive = parsed
		case strings.HasPrefix(line, "Completion ended with directive:"):
			continue
		default:
			value, description, _ := strings.Cut(line, "\t")
			candidates[strings.TrimSpace(value)] = description
		}
	}

	if directive < 0 {
		t.Fatalf("completion output carried no directive line: %q", output)
	}

	return candidates, directive
}
