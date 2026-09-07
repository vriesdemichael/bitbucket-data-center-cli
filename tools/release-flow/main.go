// Command release-flow refuses a pull request into main that is not part of
// the release flow.
//
// main is a release pointer. Everything that lands on it either arrives by the
// fast-forward promotion from next, or is a dependency bump or a hotfix -- and
// a release workflow that reads conventional commits off main will cut a
// version from whatever else appears there. A single `feat!:` merged into main
// outside the flow cuts a major release, which is the thing next exists to
// manage (ADR-066).
//
// The rules, and why each one:
//
//   - A branch outside the allowlist is refused. The flow is only worth having
//     if it cannot be left by accident.
//   - next is refused too, with its own message. Merging a next pull request
//     would use rebase-merge -- the only method main allows -- which replays
//     every commit onto main with new shas while next keeps the originals, so
//     the two branches end up holding duplicate copies of the same work. That
//     is the exact thing the fast-forward promotion avoids, so the promotion is
//     a push and never a pull request.
//   - A breaking commit is refused wherever it comes from, because a hotfix
//     branch is allowed into main and a breaking hotfix would cut a major
//     outside the flow just as surely as a feature would.
//   - The head must be a branch in this repository. Anyone can name a branch on
//     a fork `hotfix/anything`, so matching the name alone would let a fork
//     choose its own way in.
//
// The breaking-change reading comes from the conventionalcommits package, which
// the release workflow uses too. A private copy here could disagree with it,
// and the disagreement that matters is this saying "not breaking, allow it"
// while the releaser says "breaking, cut a major" -- the guard failing in the
// direction it exists to prevent.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	cc "github.com/vriesdemichael/bitbucket-data-center-cli/tools/conventionalcommits"
)

// allowedHeadPatterns are the branches that may open a pull request into main.
// next is deliberately absent: it reaches main by fast-forward push, never by
// pull request.
var allowedHeadPatterns = []string{"dependabot/*", "hotfix/*"}

const promotionAdvice = "next reaches main by fast-forward push, not by pull request. Merging this " +
	"would replay every commit onto main with new shas while next keeps the " +
	"originals, leaving two copies of the same history.\n" +
	"  Promote with: task release:promote:check && git push origin next:main"

const redirectAdvice = "Open this against next instead:\n" +
	"  gh pr edit <number> --base next\n" +
	"Only dependabot/* and hotfix/* may go straight to main, and only when they " +
	"carry no breaking change."

// request is the pull request as GitHub describes it.
type request struct {
	baseRef  string
	headRef  string
	baseSHA  string
	headSHA  string
	headRepo string
	repo     string
}

// loader reads the commits a pull request adds.
type loader func(baseSHA, headSHA string) ([]cc.Commit, error)

// matchGlob reports whether name matches a shell-style pattern in which `*`
// spans any characters, path separators included.
//
// Not path.Match, whose `*` stops at a `/`. Dependabot names branches like
// dependabot/go_modules/gomod-minor-patch-cf1e5fabad, so a matcher that stopped
// at the separator would refuse every real dependency bump while still passing
// a test written with a one-segment name.
func matchGlob(pattern, name string) bool {
	var expression strings.Builder
	expression.WriteString("^")
	for _, part := range strings.Split(pattern, "*") {
		if expression.Len() > 1 {
			expression.WriteString(".*")
		}
		expression.WriteString(regexp.QuoteMeta(part))
	}
	expression.WriteString("$")

	matched, err := regexp.MatchString(expression.String(), name)

	return err == nil && matched
}

// gitLog is the production loader.
func gitLog(baseSHA, headSHA string) ([]cc.Commit, error) {
	command := exec.Command("git", cc.LogArgs(baseSHA+".."+headSHA)...)
	command.Stderr = os.Stderr

	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("read the commits between %s and %s: %w", baseSHA, headSHA, err)
	}

	return cc.ParseLog(string(output)), nil
}

// evaluate decides whether the pull request may reach its base, and says why.
func evaluate(req request, load loader) (message string, allowed bool, err error) {
	if req.baseRef != "main" {
		return fmt.Sprintf("Base is %s, not main; the release flow does not apply.", req.baseRef), true, nil
	}

	if req.headRepo != req.repo {
		return fmt.Sprintf(
			"A pull request into main must come from a branch in %s, and this one comes from %s.\n\n%s",
			req.repo, req.headRepo, redirectAdvice,
		), false, nil
	}

	if req.headRef == "next" {
		return promotionAdvice, false, nil
	}

	if !anyPatternMatches(req.headRef) {
		return fmt.Sprintf("%s may not be merged into main.\n\n%s", req.headRef, redirectAdvice), false, nil
	}

	commits, err := load(req.baseSHA, req.headSHA)
	if err != nil {
		return "", false, err
	}

	var listed []string
	for _, commit := range commits {
		if commit.Breaking {
			listed = append(listed, fmt.Sprintf("  %s %s", commit.ShortSHA(), commit.Subject))
		}
	}

	if len(listed) > 0 {
		return fmt.Sprintf(
			"%s carries a breaking change, which cuts a major release:\n%s\n\n"+
				"A major release goes through next, so that it ships with the rest of "+
				"the work meant for it (ADR-066). Open this against next, or drop the "+
				"breaking change from the hotfix.",
			req.headRef, strings.Join(listed, "\n"),
		), false, nil
	}

	return fmt.Sprintf("%s into main: allowed, and carries no breaking change.", req.headRef), true, nil
}

func anyPatternMatches(headRef string) bool {
	for _, pattern := range allowedHeadPatterns {
		if matchGlob(pattern, headRef) {
			return true
		}
	}

	return false
}

func main() {
	var req request
	flag.StringVar(&req.baseRef, "base-ref", "", "branch being merged into")
	flag.StringVar(&req.headRef, "head-ref", "", "branch being merged from")
	flag.StringVar(&req.baseSHA, "base-sha", "", "commit the base branch is at")
	flag.StringVar(&req.headSHA, "head-sha", "", "commit the head branch is at")
	flag.StringVar(&req.headRepo, "head-repo", "", "owner/name the head branch lives in")
	flag.StringVar(&req.repo, "repo", "", "owner/name of this repository")
	flag.Parse()

	for name, value := range map[string]string{
		"base-ref":  req.baseRef,
		"head-ref":  req.headRef,
		"base-sha":  req.baseSHA,
		"head-sha":  req.headSHA,
		"head-repo": req.headRepo,
		"repo":      req.repo,
	} {
		if value == "" {
			fmt.Fprintf(os.Stderr, "-%s is required\n", name)
			os.Exit(2)
		}
	}

	message, allowed, err := evaluate(req, gitLog)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	if allowed {
		fmt.Println(message)

		return
	}

	// The annotation carries the first line, which is what shows on the checks
	// tab; the whole message goes to the log underneath it.
	fmt.Printf("::error::%s\n", strings.SplitN(message, "\n", 2)[0])
	fmt.Printf("\n%s\n\n", message)
	os.Exit(1)
}
