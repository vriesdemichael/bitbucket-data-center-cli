// Command release-flow refuses every pull request into main.
//
// main is a release pointer. Every change integrates on next through a pull
// request, and main moves only when an administrator fast-forwards next onto it
// (ADR-066). A release workflow that reads conventional commits off main cuts a
// version from whatever else appears there: a single `feat!:` merged into main
// outside the promotion cuts a major release, which is the thing next exists to
// manage.
//
// The rules, and why each one:
//
//   - Every head is refused, whatever its name. There is no hotfix route and
//     no backport: a fix, a security fix included, ships in the next release.
//     A Dependabot security update, which Dependabot opens against main
//     whatever its configuration says, is retargeted to next like the rest
//     (ADR-069).
//   - next gets its own message. Merging a next pull request would use
//     rebase-merge -- the only method main allows -- which replays every commit
//     onto main with new shas while next keeps the originals, so the two
//     branches end up holding duplicate copies of the same work. That is the
//     exact thing the fast-forward promotion avoids, so the promotion is a push
//     and never a pull request.
//   - Only this repository's next gets that message. A fork's branch named
//     next is not the branch that gets promoted, so it is pointed at next like
//     any other head.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

const promotionAdvice = "next reaches main by fast-forward push, not by pull request. Merging this " +
	"would replay every commit onto main with new shas while next keeps the " +
	"originals, leaving two copies of the same history.\n" +
	"  Promote with: task release:promote:check && git push origin next:main"

const redirectAdvice = "Every change integrates on next through a pull request, and main moves only " +
	"when next is promoted onto it (ADR-066). Open this against next instead:\n" +
	"  gh pr edit <number> --base next"

// request is the pull request as GitHub describes it.
type request struct {
	baseRef  string
	headRef  string
	headRepo string
	repo     string
}

// evaluate decides whether the pull request may reach its base, and says why.
func evaluate(req request) (message string, allowed bool) {
	if req.baseRef != "main" {
		return fmt.Sprintf("Base is %s, not main; the release flow does not apply.", req.baseRef), true
	}

	if req.headRepo == req.repo && req.headRef == "next" {
		return promotionAdvice, false
	}

	// GitHub labels a fork's head as owner/name:branch, and so does this, so
	// that a fork's next is not read as the branch that gets promoted.
	head := req.headRef
	if req.headRepo != req.repo {
		head = req.headRepo + ":" + req.headRef
	}

	return fmt.Sprintf("%s may not be merged into main.\n\n%s", head, redirectAdvice), false
}

func main() {
	var req request
	flag.StringVar(&req.baseRef, "base-ref", "", "branch being merged into")
	flag.StringVar(&req.headRef, "head-ref", "", "branch being merged from")
	flag.StringVar(&req.headRepo, "head-repo", "", "owner/name the head branch lives in")
	flag.StringVar(&req.repo, "repo", "", "owner/name of this repository")
	flag.Parse()

	for name, value := range map[string]string{
		"base-ref":  req.baseRef,
		"head-ref":  req.headRef,
		"head-repo": req.headRepo,
		"repo":      req.repo,
	} {
		if value == "" {
			fmt.Fprintf(os.Stderr, "-%s is required\n", name)
			os.Exit(2)
		}
	}

	message, allowed := evaluate(req)
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
