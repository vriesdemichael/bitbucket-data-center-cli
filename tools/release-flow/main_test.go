package main

import (
	"errors"
	"strings"
	"testing"

	cc "github.com/vriesdemichael/bitbucket-data-center-cli/tools/conventionalcommits"
)

const thisRepo = "vriesdemichael/bitbucket-data-center-cli"

// noCommits is the loader for a case that must be decided before any commit is
// read. A case that reaches it when it should not have fails loudly rather than
// passing for the wrong reason.
func noCommits(t *testing.T) loader {
	t.Helper()

	return func(string, string) ([]cc.Commit, error) {
		t.Error("the commits were read for a pull request that should have been decided on its branch name alone")

		return nil, nil
	}
}

func commits(t *testing.T, pairs ...[2]string) loader {
	t.Helper()

	return func(string, string) ([]cc.Commit, error) {
		built := make([]cc.Commit, 0, len(pairs))
		for _, pair := range pairs {
			built = append(built, cc.Classify("0123456789abcdef0123456789abcdef01234567", pair[0], pair[1]))
		}

		return built, nil
	}
}

func TestAPullRequestIntoNextIsNotThisGatesBusiness(t *testing.T) {
	t.Parallel()

	message, allowed, err := evaluate(request{
		baseRef: "next", headRef: "anything", headRepo: thisRepo, repo: thisRepo,
	}, noCommits(t))
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if !allowed {
		t.Fatalf("expected allowed, refused with: %s", message)
	}
	if !strings.Contains(message, "not main") {
		t.Errorf("message should say the base is not main, got: %s", message)
	}
}

func TestAForkMayNotReachMainHoweverItNamesItsBranch(t *testing.T) {
	t.Parallel()

	message, allowed, err := evaluate(request{
		baseRef: "main", headRef: "hotfix/looks-legitimate",
		headRepo: "someone-else/bitbucket-data-center-cli", repo: thisRepo,
	}, noCommits(t))
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if allowed {
		t.Fatal("expected a fork to be refused")
	}
	if !strings.Contains(message, "someone-else/bitbucket-data-center-cli") {
		t.Errorf("message should name the fork, got: %s", message)
	}
}

func TestNextIsRefusedWithThePromotionAdvice(t *testing.T) {
	t.Parallel()

	message, allowed, err := evaluate(request{
		baseRef: "main", headRef: "next", headRepo: thisRepo, repo: thisRepo,
	}, noCommits(t))
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if allowed {
		t.Fatal("expected the promotion by pull request to be refused")
	}
	if !strings.Contains(message, "fast-forward push, not by pull request") {
		t.Errorf("message should point at the fast-forward, got: %s", message)
	}
	if !strings.Contains(message, "git push origin next:main") {
		t.Errorf("message should give the promotion command, got: %s", message)
	}
}

func TestAStrayBranchIsRefusedAndPointedAtNext(t *testing.T) {
	t.Parallel()

	message, allowed, err := evaluate(request{
		baseRef: "main", headRef: "feat/something-good", headRepo: thisRepo, repo: thisRepo,
	}, noCommits(t))
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if allowed {
		t.Fatal("expected a stray branch to be refused")
	}
	if !strings.Contains(message, "feat/something-good may not be merged into main") {
		t.Errorf("message should name the branch, got: %s", message)
	}
	if !strings.Contains(message, "--base next") {
		t.Errorf("message should offer the redirect, got: %s", message)
	}
}

// The real shape of a Dependabot branch, which carries two separators. A
// matcher whose `*` stopped at a `/` would refuse every dependency bump the
// repository actually receives while passing a test written with a
// one-segment name.
func TestARealDependabotBranchIsAllowed(t *testing.T) {
	t.Parallel()

	message, allowed, err := evaluate(request{
		baseRef: "main", headRef: "dependabot/go_modules/gomod-minor-patch-cf1e5fabad",
		headRepo: thisRepo, repo: thisRepo,
	}, commits(t, [2]string{"chore(deps): bump the gomod group", ""}))
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if !allowed {
		t.Fatalf("expected a dependency bump to be allowed, refused with: %s", message)
	}
}

func TestACleanHotfixIsAllowed(t *testing.T) {
	t.Parallel()

	message, allowed, err := evaluate(request{
		baseRef: "main", headRef: "hotfix/broken-paging", headRepo: thisRepo, repo: thisRepo,
	}, commits(t, [2]string{"fix: repair the paging guard", ""}))
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if !allowed {
		t.Fatalf("expected a clean hotfix to be allowed, refused with: %s", message)
	}
	if !strings.Contains(message, "carries no breaking change") {
		t.Errorf("message should say why it passed, got: %s", message)
	}
}

func TestAnAllowlistedBranchIsStillRefusedForABreakingCommit(t *testing.T) {
	t.Parallel()

	for name, pair := range map[string][2]string{
		"bang":   {"fix!: reject an out-of-range expiry", ""},
		"footer": {"fix: reject an out-of-range expiry", "BREAKING CHANGE: --expiry-days now validates"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			message, allowed, err := evaluate(request{
				baseRef: "main", headRef: "hotfix/probe", headRepo: thisRepo, repo: thisRepo,
			}, commits(t, pair))
			if err != nil {
				t.Fatalf("evaluate: %v", err)
			}
			if allowed {
				t.Fatal("expected a breaking hotfix to be refused")
			}
			if !strings.Contains(message, "carries a breaking change") {
				t.Errorf("message should say what is wrong, got: %s", message)
			}
			if !strings.Contains(message, pair[0]) {
				t.Errorf("message should name the offending commit, got: %s", message)
			}
			if !strings.Contains(message, "ADR-066") {
				t.Errorf("message should cite the record, got: %s", message)
			}
		})
	}
}

// A hotfix carrying several commits is refused for the breaking one among them,
// and only for that one.
func TestOnlyTheBreakingCommitsAreListed(t *testing.T) {
	t.Parallel()

	message, allowed, err := evaluate(request{
		baseRef: "main", headRef: "hotfix/probe", headRepo: thisRepo, repo: thisRepo,
	}, commits(t,
		[2]string{"fix: something ordinary", ""},
		[2]string{"feat!: something that breaks", ""},
		[2]string{"docs: a note", ""},
	))
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if allowed {
		t.Fatal("expected the pull request to be refused")
	}
	if strings.Contains(message, "something ordinary") || strings.Contains(message, "a note") {
		t.Errorf("only the breaking commit should be listed, got: %s", message)
	}
	if !strings.Contains(message, "something that breaks") {
		t.Errorf("the breaking commit should be listed, got: %s", message)
	}
}

// git failing is not the same as a pull request passing. A loader error has to
// reach the caller, because the alternative is a gate that opens when it cannot
// see.
func TestAFailureToReadTheCommitsIsNotAPass(t *testing.T) {
	t.Parallel()

	failing := func(string, string) ([]cc.Commit, error) { return nil, errors.New("git exploded") }

	_, allowed, err := evaluate(request{
		baseRef: "main", headRef: "hotfix/probe", headRepo: thisRepo, repo: thisRepo,
	}, failing)
	if err == nil {
		t.Fatal("expected the error to reach the caller")
	}
	if allowed {
		t.Fatal("expected a failure not to count as allowed")
	}
}

func TestMatchGlobSpansPathSeparators(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		pattern string
		name    string
		want    bool
	}{
		"one segment":                {"hotfix/*", "hotfix/probe", true},
		"several segments":           {"dependabot/*", "dependabot/go_modules/group-abc123", true},
		"prefix only is not a match": {"hotfix/*", "hotfixes/probe", false},
		"a different branch":         {"hotfix/*", "feat/thing", false},
		"the bare prefix":            {"hotfix/*", "hotfix/", true},
		"no dot metacharacter":       {"hotfix/*", "hotfixXprobe", false},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := matchGlob(testCase.pattern, testCase.name); got != testCase.want {
				t.Errorf("matchGlob(%q, %q) = %v, want %v", testCase.pattern, testCase.name, got, testCase.want)
			}
		})
	}
}
