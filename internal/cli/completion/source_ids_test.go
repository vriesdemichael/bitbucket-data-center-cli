package completion

import (
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
)

// TestProjectScopeIsDecidedByTheCommandBeingCompleted covers the choice that
// decides whether a candidate is an id or a 404.
//
// Webhooks, branch restrictions, default tasks, reviewer conditions and access
// keys all exist on a repository and on a project, configured through two
// routes. Offering one scope's ids to the other's command hands the person a
// list where every entry fails, which no shell can show them.
func TestProjectScopeIsDecidedByTheCommandBeingCompleted(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name        string
		commandPath string
		repoFlag    string
		projectFlag string
		project     bool
	}{
		{
			name:        "a repository webhook is the repository's",
			commandPath: "webhook delete",
		},
		{
			name:        "the same object under bb project is the project's",
			commandPath: "project webhook delete",
			project:     true,
		},
		{
			name:        "a repository branch restriction is the repository's",
			commandPath: "branch restriction delete",
		},
		{
			name:        "a project branch restriction is the project's",
			commandPath: "project branch-restriction delete",
			project:     true,
		},
		{
			name:        "a repository default task is the repository's",
			commandPath: "repo default-task update",
		},
		{
			name:        "a project default task is the project's",
			commandPath: "project default-task update",
			project:     true,
		},
		{
			name:        "the webhook hidden under repo settings is still the repository's",
			commandPath: "repo settings workflow webhooks delete",
		},
		{
			// bb reviewer condition takes both, and reads --repo first.
			name:        "a reviewer condition with no --repo is the project's",
			commandPath: "reviewer condition delete",
			project:     true,
		},
		{
			name:        "a reviewer condition named with --repo is the repository's",
			commandPath: "reviewer condition delete",
			repoFlag:    "PROJ/repo",
		},
		{
			name:        "a reviewer condition named with --project is the project's",
			commandPath: "reviewer condition update",
			projectFlag: "PROJ",
			project:     true,
		},
		{
			// bb repo ssh-key spells the same choice the other way round.
			name:        "an access key with no flag at all is the repository's",
			commandPath: "repo ssh-key remove",
		},
		{
			name:        "an access key named with --project is the project's",
			commandPath: "repo ssh-key remove",
			projectFlag: "PROJ",
			project:     true,
		},
		{
			name:        "an access key named with --repo is the repository's",
			commandPath: "repo ssh-key remove",
			repoFlag:    "PROJ/repo",
		},
		{
			name:        "--access-key-id on a repository restriction takes the repository's keys",
			commandPath: "branch restriction create",
		},
		{
			name:        "--access-key-id on a project restriction takes the project's keys",
			commandPath: "project branch-restriction create",
			project:     true,
		},
		{
			// The prefixes are matched with their trailing space so that a
			// command merely starting with the same letters is not caught.
			name:        "creating a project is not a project-scoped listing by accident",
			commandPath: "projector build",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if scoped := projectScoped(testCase.commandPath, testCase.repoFlag, testCase.projectFlag); scoped != testCase.project {
				t.Errorf("projectScoped(%q, repo=%q, project=%q) = %v, want %v",
					testCase.commandPath, testCase.repoFlag, testCase.projectFlag, scoped, testCase.project)
			}
		})
	}
}

// TestAnInheritedObjectBelongsToTheScopeItWasConfiguredAt covers the half of
// the scope decision the command path cannot make.
//
// A repository's listing of branch restrictions, reviewer conditions and
// default tasks carries the project's as well, marked by scope. Bitbucket does
// not refuse the repository route for one of them: a project restriction
// deleted through it is deleted project-wide, verified live. So the listing
// has to be narrowed after it arrives, not only aimed correctly before.
func TestAnInheritedObjectBelongsToTheScopeItWasConfiguredAt(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name        string
		scope       string
		wantProject bool
		own         bool
	}{
		{name: "a repository object at a repository slot", scope: "REPOSITORY", own: true},
		{name: "a project object at a repository slot", scope: "PROJECT"},
		{name: "a project object at a project slot", scope: "PROJECT", wantProject: true, own: true},
		{name: "a repository object at a project slot", scope: "REPOSITORY", wantProject: true},
		// An endpoint that does not say is answering about the level it was
		// asked about, so silence must not empty the listing.
		{name: "an endpoint that reports no scope at a repository slot", scope: "", own: true},
		{name: "an endpoint that reports no scope at a project slot", scope: "", wantProject: true, own: true},
		{name: "the scope is read regardless of case", scope: "project", own: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if own := ownScope(testCase.scope, testCase.wantProject); own != testCase.own {
				t.Errorf("ownScope(%q, project=%v) = %v, want %v",
					testCase.scope, testCase.wantProject, own, testCase.own)
			}
		})
	}
}

// TestTheAnyRefMatcherIsNotARefName keeps a constant out of a description.
//
// Bitbucket takes ANY_REF on a create and answers every read with
// ANY_REF_MATCHER_ID, neither of which is a branch. Printed as one, a rule
// that applies everywhere reads as a rule about a branch nobody has.
func TestTheAnyRefMatcherIsNotARefName(t *testing.T) {
	t.Parallel()

	for _, matcher := range []refMatcher{
		{ID: "ANY_REF", DisplayID: "ANY_REF"},
		{ID: "ANY_REF_MATCHER_ID", DisplayID: "ANY_REF_MATCHER_ID"},
		{ID: "any_ref_matcher_id"},
		{},
	} {
		if ref := matcherRef(matcher); ref != "" {
			t.Errorf("matcherRef(%+v) = %q, want the empty string", matcher, ref)
		}
	}

	if ref := matcherRef(refMatcher{ID: "refs/heads/master", DisplayID: "master"}); ref != "master" {
		t.Errorf("a real matcher should keep its display form, got %q", ref)
	}
	if ref := matcherRef(refMatcher{ID: "refs/heads/release/*"}); ref != "refs/heads/release/*" {
		t.Errorf("a matcher with no display form should fall back to its id, got %q", ref)
	}
}

// TestADescriptionNamesTheThingBehindTheID is the property every kind in this
// file exists for.
//
// An id is not a suggestion. "7" tells nobody which webhook they are about to
// delete, so a candidate whose description is empty has completed a number and
// nothing else -- and each of these builders has a fallback for exactly the
// field Bitbucket may not have.
func TestADescriptionNamesTheThingBehindTheID(t *testing.T) {
	t.Parallel()

	t.Run("a comment carries its author and its opening line", func(t *testing.T) {
		t.Parallel()

		description := describeComment("Ada Lovelace", "ada", "This raises after the record is written.\nSee the runner.")
		if description != "Ada Lovelace: This raises after the record is written." {
			t.Errorf("unexpected comment description %q", description)
		}

		if got := describeComment("", "ada", "Agreed."); got != "ada: Agreed." {
			t.Errorf("a comment whose author has no display name should fall back to the username, got %q", got)
		}
		if got := describeComment("Ada Lovelace", "ada", "   "); got != "Ada Lovelace" {
			t.Errorf("an empty body should leave the author alone, got %q", got)
		}
		if got := describeComment("", "", "Agreed."); got != "Agreed." {
			t.Errorf("an authorless comment should still show its text, got %q", got)
		}
	})

	t.Run("a webhook carries its name and where it posts", func(t *testing.T) {
		t.Parallel()

		if got := describeWebhook("CI", "https://ci.example/hook", true); got != "CI → https://ci.example/hook" {
			t.Errorf("unexpected webhook description %q", got)
		}
		if got := describeWebhook("", "https://ci.example/hook", true); got != "https://ci.example/hook" {
			t.Errorf("an unnamed webhook should be described by its URL, got %q", got)
		}
		// The difference between the webhook that is failing and its disabled
		// twin, which is otherwise invisible in a list of ids.
		if got := describeWebhook("CI", "https://ci.example/hook", false); !strings.Contains(got, "inactive") {
			t.Errorf("a disabled webhook should say so, got %q", got)
		}
	})

	t.Run("a restriction carries its type and its matcher", func(t *testing.T) {
		t.Parallel()

		if got := describeRestriction("no-deletes", "master"); got != "no-deletes on master" {
			t.Errorf("unexpected restriction description %q", got)
		}
		// A restriction that matches everything is a fact about it, not a
		// missing field.
		if got := describeRestriction("pull-request-only", ""); got != "pull-request-only on any ref" {
			t.Errorf("unexpected restriction description %q", got)
		}
		if got := describeRestriction("", "master"); got != "master" {
			t.Errorf("a typeless restriction should still name its matcher, got %q", got)
		}
	})

	t.Run("a default task carries its own text", func(t *testing.T) {
		t.Parallel()

		if got := describeDefaultTask("Update the changelog", "", ""); got != "Update the changelog" {
			t.Errorf("unexpected default task description %q", got)
		}
		if got := describeDefaultTask("Update the changelog", "develop", "master"); got != "Update the changelog (from develop to master)" {
			t.Errorf("a scoped default task should say which refs it applies to, got %q", got)
		}
		if got := describeDefaultTask("Line one\nLine two", "", ""); got != "Line one" {
			t.Errorf("a multi-line task should be cut to its first line, got %q", got)
		}
	})

	t.Run("a reviewer condition carries the refs it matches", func(t *testing.T) {
		t.Parallel()

		if got := describeReviewerCondition("develop", "master", 2); got != "develop → master, 2 approvals" {
			t.Errorf("unexpected condition description %q", got)
		}
		// A condition with no matcher covers everything, which is a fact about
		// it rather than a missing field.
		if got := describeReviewerCondition("", "", 0); got != "any → any" {
			t.Errorf("an unmatched condition should say it covers anything, got %q", got)
		}
	})

	t.Run("a required build carries its ref pattern and its checks", func(t *testing.T) {
		t.Parallel()

		if got := describeRequiredBuild("master", []string{"build", "lint"}); got != "master: build, lint" {
			t.Errorf("unexpected required build description %q", got)
		}
		if got := describeRequiredBuild("", nil); got != "any ref" {
			t.Errorf("a check with neither should still say what it guards, got %q", got)
		}
	})

	t.Run("an access key carries its label and its permission", func(t *testing.T) {
		t.Parallel()

		if got := describeAccessKey("deploy", "REPO_READ", "SHA256:abc"); got != "deploy (REPO_READ)" {
			t.Errorf("unexpected access key description %q", got)
		}
		// An unlabelled key is the case where the id says least, so the
		// fingerprint stands in rather than leaving the permission alone.
		if got := describeAccessKey("", "REPO_WRITE", "SHA256:abc"); got != "SHA256:abc (REPO_WRITE)" {
			t.Errorf("an unlabelled key should fall back to its fingerprint, got %q", got)
		}
	})
}

// TestAVerbNarrowsTheCommentsItIsOffered keeps a completion from suggesting
// values the command refuses.
//
// The same rule `bb pr reopen` follows for pull requests: reopening a comment
// that is already open, or applying a suggestion from a comment that has none,
// is an error the person was handed by the completion.
func TestAVerbNarrowsTheCommentsItIsOffered(t *testing.T) {
	t.Parallel()

	open := result.Comment{ID: 1, State: "OPEN", Text: "Please rename this."}
	resolved := result.Comment{ID: 2, State: "RESOLVED", Text: "Done."}
	// Bitbucket leaves state off an ordinary comment entirely; resolving one
	// is still allowed, so an absent state must not read as resolved.
	stateless := result.Comment{ID: 3, Text: "Looks good."}
	suggesting := result.Comment{ID: 4, Text: "Try:\n```suggestion\nreturn nil\n```"}

	every := []result.Comment{open, resolved, stateless, suggesting}

	for _, testCase := range []struct {
		name        string
		commandPath string
		offered     []int64
	}{
		{name: "getting one is not a statement about its state", commandPath: "pr comment get", offered: []int64{1, 2, 3, 4}},
		{name: "reacting is not either", commandPath: "pr comment react", offered: []int64{1, 2, 3, 4}},
		{name: "replying takes any comment", commandPath: "pr comment add", offered: []int64{1, 2, 3, 4}},
		{name: "reopening takes a resolved one", commandPath: "pr comment reopen", offered: []int64{2}},
		{name: "resolving takes everything else", commandPath: "pr comment resolve", offered: []int64{1, 3, 4}},
		{name: "applying a suggestion takes one that has a suggestion", commandPath: "pr comment apply-suggestion", offered: []int64{4}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			offered := []int64{}
			keep := commentFilterFor(testCase.commandPath)
			for _, comment := range every {
				if keep == nil || keep(comment) {
					offered = append(offered, comment.ID)
				}
			}

			if len(offered) != len(testCase.offered) {
				t.Fatalf("%s offered %v, want %v", testCase.commandPath, offered, testCase.offered)
			}
			for index, id := range testCase.offered {
				if offered[index] != id {
					t.Fatalf("%s offered %v, want %v", testCase.commandPath, offered, testCase.offered)
				}
			}
		})
	}
}

// TestCommentsAreOfferedNewestFirst pins the order the KeepOrder directive
// then asks the shell to leave alone.
//
// Comment ids sorted as text put 1389396 above 98, which is the order a shell
// would impose on its own. The comment being answered is the last one written,
// so the sort is by when rather than by number -- and it has to be done here,
// because the two endpoints these sources read order differently.
func TestCommentsAreOfferedNewestFirst(t *testing.T) {
	t.Parallel()

	answer := commentResult([]result.Comment{
		{ID: 98, CreatedDate: 300, Text: "newest"},
		{ID: 1389396, CreatedDate: 100, Text: "oldest"},
		{ID: 500, CreatedDate: 200, Text: "middle"},
		// No id: nothing a command could be given.
		{CreatedDate: 400, Text: "unidentified"},
	}, nil)

	if !answer.KeepOrder {
		t.Fatal("a ranked answer must ask the shell to keep its order")
	}

	got := make([]string, 0, len(answer.Candidates))
	for _, candidate := range answer.Candidates {
		got = append(got, candidate.Value)
	}

	want := []string{"98", "500", "1389396"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("comments were offered as %v, want %v", got, want)
	}
}
