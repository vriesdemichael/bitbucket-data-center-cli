package main

import (
	"strings"
	"testing"
)

const thisRepo = "vriesdemichael/bitbucket-data-center-cli"

const redirectCommand = "gh pr edit <number> --base next"

func TestAPullRequestIntoNextIsNotThisGatesBusiness(t *testing.T) {
	t.Parallel()

	for _, head := range []string{"feat/something-good", "dependabot/go_modules/gomod-minor-patch-cf1e5fabad"} {
		t.Run(head, func(t *testing.T) {
			t.Parallel()

			message, allowed := evaluate(request{baseRef: "next", headRef: head, headRepo: thisRepo, repo: thisRepo})
			if !allowed {
				t.Fatalf("expected allowed, refused with: %s", message)
			}
			if !strings.Contains(message, "not main") {
				t.Errorf("message should say the base is not main, got: %s", message)
			}
		})
	}
}

func TestNextIsRefusedWithThePromotionAdvice(t *testing.T) {
	t.Parallel()

	message, allowed := evaluate(request{baseRef: "main", headRef: "next", headRepo: thisRepo, repo: thisRepo})
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

// Every head is refused, the ones that used to be let through included: a
// Dependabot update (in the real shape of its branch name, which carries two
// separators) and a hotfix. Each is checked for this refusal's own words, so a
// refusal for some other reason does not pass for this one.
func TestEveryOtherHeadIsRefusedAndPointedAtNext(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		headRef  string
		headRepo string
		named    string
	}{
		"a feature":         {"feat/something-good", thisRepo, "feat/something-good"},
		"a dependabot bump": {"dependabot/go_modules/gomod-minor-patch-cf1e5fabad", thisRepo, "dependabot/go_modules/gomod-minor-patch-cf1e5fabad"},
		"a dependabot action bump": {
			"dependabot/github_actions/actions/checkout-7.0.1", thisRepo, "dependabot/github_actions/actions/checkout-7.0.1",
		},
		"a hotfix":             {"hotfix/broken-paging", thisRepo, "hotfix/broken-paging"},
		"a fork's hotfix":      {"hotfix/looks-legitimate", "someone-else/bitbucket-data-center-cli", "someone-else/bitbucket-data-center-cli:hotfix/looks-legitimate"},
		"a fork's next":        {"next", "someone-else/bitbucket-data-center-cli", "someone-else/bitbucket-data-center-cli:next"},
		"main onto itself":     {"main", thisRepo, "main"},
		"a branch named fixes": {"fix/repair-the-paging-guard", thisRepo, "fix/repair-the-paging-guard"},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			message, allowed := evaluate(request{
				baseRef: "main", headRef: testCase.headRef, headRepo: testCase.headRepo, repo: thisRepo,
			})
			if allowed {
				t.Fatalf("expected %s to be refused, allowed with: %s", testCase.named, message)
			}

			firstLine := strings.SplitN(message, "\n", 2)[0]
			if want := testCase.named + " may not be merged into main."; firstLine != want {
				t.Errorf("the annotation line should be %q, got %q", want, firstLine)
			}
			if !strings.Contains(message, redirectCommand) {
				t.Errorf("message should offer the redirect to next, got: %s", message)
			}
			if !strings.Contains(message, "ADR-066") {
				t.Errorf("message should cite the record, got: %s", message)
			}
			if strings.Contains(message, "git push origin next:main") {
				t.Errorf("only this repository's next gets the promotion advice, got: %s", message)
			}
		})
	}
}
