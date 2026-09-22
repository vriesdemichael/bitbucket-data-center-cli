package completion

import (
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/git"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

// TestSplitSelectorPicksTheStage covers the decision the whole repository
// source turns on: whether a typed word is still choosing a project or has
// already chosen one.
//
// Getting it wrong is not a wrong suggestion but a listing of the entire
// instance for the first keystroke, which is what the two stages exist to
// avoid.
func TestSplitSelectorPicksTheStage(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		word       string
		projectKey string
		prefix     string
		scoped     bool
	}{
		{name: "nothing typed offers projects", word: ""},
		{name: "a partial key is still a project", word: "PRO", projectKey: "PRO"},
		{name: "a trailing slash opens the project", word: "PROJ/", projectKey: "PROJ", scoped: true},
		{name: "a slug prefix narrows inside it", word: "PROJ/ba", projectKey: "PROJ", prefix: "ba", scoped: true},
		{
			name:       "a personal project keeps its tilde",
			word:       "~admin/to",
			projectKey: "~admin",
			prefix:     "to",
			scoped:     true,
		},
		{
			// There is no project to list the contents of, so this is still
			// the first stage rather than an instance-wide listing.
			name: "a bare slash has chosen no project",
			word: "/",
		},
		{
			// A slug cannot contain a slash, so everything after the first one
			// is the prefix and will simply match nothing.
			name:       "only the first slash divides",
			word:       "PROJ/a/b",
			projectKey: "PROJ",
			prefix:     "a/b",
			scoped:     true,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			projectKey, prefix, scoped := splitSelector(testCase.word)

			if projectKey != testCase.projectKey {
				t.Errorf("project key: got %q, want %q", projectKey, testCase.projectKey)
			}
			if prefix != testCase.prefix {
				t.Errorf("prefix: got %q, want %q", prefix, testCase.prefix)
			}
			if scoped != testCase.scoped {
				t.Errorf("scoped: got %v, want %v", scoped, testCase.scoped)
			}
		})
	}
}

// TestADestructiveTargetDoesNotPromoteTheAmbientRepository pins ADR-073 in the
// place completion can undo it.
//
// `bb repo delete` treats a repository named as its argument or with --repo as
// explicit, and --yes applies only to an explicit target. Offering the inferred
// repository first turns one tab press into that naming, so the safety flag
// starts applying to the repository the caller was merely standing in.
func TestADestructiveTargetDoesNotPromoteTheAmbientRepository(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		path     string
		flag     string
		promotes bool
	}{
		{name: "an ordinary flag promotes", path: "pr merge", flag: "repo", promotes: true},
		{name: "an ordinary argument promotes", path: "repo clone", promotes: true},
		{name: "the delete argument does not", path: "repo delete"},
		{name: "the delete flag does not either", path: "repo delete", flag: "repo"},
		{name: "the alias is the same command", path: "repo admin delete", flag: "repo"},
		{
			// Not every repository slot on a destructive command is its
			// target; a source repository is read, not deleted.
			name:     "another repository slot on it is not the target",
			path:     "repo delete",
			flag:     "from-repo",
			promotes: true,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := promotesAmbientRepository(testCase.path, testCase.flag); got != testCase.promotes {
				t.Errorf("promotesAmbientRepository(%q, %q) = %v, want %v",
					testCase.path, testCase.flag, got, testCase.promotes)
			}
		})
	}
}

// TestOrderOffersTheSameCandidatesEitherWay is the other half of that rule.
//
// Refusing to promote is a change of rank, not of content: a repository the
// checkout points at is still a repository `bb repo delete` can be told to
// delete. Dropping it would make the destructive command the one slot where a
// value is missing, which is its own kind of surprise.
func TestOrderOffersTheSameCandidatesEitherWay(t *testing.T) {
	t.Parallel()

	local := []Candidate{{Value: "PROJ/checkout", Description: "remote origin"}}
	listed := []Candidate{
		{Value: "PROJ/alpha", Description: "Alpha"},
		{Value: "PROJ/checkout", Description: "Checkout"},
	}

	promoted, keepOrder := order(local, listed, true)
	if !keepOrder {
		t.Error("expected the shell to be asked to keep the ranking, which is the point of promoting")
	}
	if len(promoted) == 0 || promoted[0].Value != "PROJ/checkout" {
		t.Fatalf("expected the checkout's repository first, got %v", promoted)
	}

	ordinary, keepOrder := order(local, listed, false)
	if keepOrder {
		t.Error("expected no ranking to preserve when the ambient repository is not promoted")
	}
	if len(ordinary) == 0 || ordinary[0].Value != "PROJ/alpha" {
		t.Fatalf("expected the listing's own order to lead, got %v", ordinary)
	}

	if promotedValues, ordinaryValues := set(promoted), set(ordinary); len(promotedValues) != len(ordinaryValues) {
		t.Fatalf("the two orderings offered different values: %v and %v", promotedValues, ordinaryValues)
	} else {
		for value := range promotedValues {
			if !ordinaryValues[value] {
				t.Errorf("%q is offered when promoting and missing when not", value)
			}
		}
	}
}

// TestOrderWithoutACheckoutLeavesTheListingAlone keeps the shell's own sort
// for the answer that is nothing but a listing.
func TestOrderWithoutACheckoutLeavesTheListingAlone(t *testing.T) {
	t.Parallel()

	listed := []Candidate{{Value: "PROJ/alpha"}, {Value: "PROJ/beta"}}

	candidates, keepOrder := order(nil, listed, true)

	if keepOrder {
		t.Error("expected no ranking to preserve when nothing was promoted")
	}
	if len(candidates) != len(listed) {
		t.Fatalf("expected the listing unchanged, got %v", candidates)
	}
}

// TestBitbucketRemotesKeepsOnlyThisInstance is the check that stops a
// repository on another host being offered as a selector.
//
// A remote URL is parsed leniently: a bare owner/name path is accepted, which
// is exactly the shape of a GitHub remote. Without the host comparison,
// `--repo <tab>` in a checkout that has both would offer a selector no
// Bitbucket call can resolve.
func TestBitbucketRemotesKeepsOnlyThisInstance(t *testing.T) {
	t.Parallel()

	remotes := []git.Remote{
		{Name: "upstream", URL: "https://bitbucket.example.com/scm/PLAT/service.git"},
		{Name: "github", URL: "git@github.com:someone/service.git"},
		{Name: "origin", URL: "ssh://git@bitbucket.example.com:7999/plat/fork.git"},
	}

	repositories := bitbucketRemotes(remotes, "bitbucket.example.com")

	if len(repositories) != 2 {
		t.Fatalf("expected the two Bitbucket remotes, got %v", repositories)
	}
	if repositories[0].Slug != "fork" || repositories[0].RemoteName != "origin" {
		t.Errorf("expected origin first, got %+v", repositories[0])
	}
	if repositories[1].ProjectKey != "PLAT" || repositories[1].Slug != "service" {
		t.Errorf("expected the upstream remote second, got %+v", repositories[1])
	}
	for _, repository := range repositories {
		if repository.Slug == "service" && repository.ProjectKey == "someone" {
			t.Error("a GitHub remote was offered as a Bitbucket repository")
		}
	}
}

// TestBitbucketRemotesOffersEachRepositoryOnce covers the fork-and-mirror
// checkout, where two remotes name the same repository.
func TestBitbucketRemotesOffersEachRepositoryOnce(t *testing.T) {
	t.Parallel()

	remotes := []git.Remote{
		{Name: "mirror", URL: "https://bb.example.com/scm/PLAT/service.git"},
		{Name: "origin", URL: "https://bb.example.com/scm/plat/service.git"},
	}

	repositories := bitbucketRemotes(remotes, "bb.example.com")

	if len(repositories) != 1 {
		t.Fatalf("expected one repository from two remotes naming it, got %v", repositories)
	}
	if repositories[0].RemoteName != "origin" {
		t.Errorf("expected origin to be the one kept, got %+v", repositories[0])
	}
}

// TestLeadPutsTheResolvedRepositoryFirst covers the case the git remotes alone
// do not answer: the command resolved to a repository, and that is the one the
// invocation being completed would have acted on.
func TestLeadPutsTheResolvedRepositoryFirst(t *testing.T) {
	t.Parallel()

	repositories := []Repository{
		{ProjectKey: "PLAT", Slug: "fork", RemoteName: "origin"},
		{ProjectKey: "PLAT", Slug: "service", RemoteName: "upstream"},
	}

	led := lead(repositories, Repository{ProjectKey: "plat", Slug: "SERVICE", RemoteName: "upstream"})

	if len(led) != 2 {
		t.Fatalf("expected the same two repositories, got %v", led)
	}
	if led[0].Slug != "SERVICE" {
		t.Errorf("expected the resolved repository first, got %+v", led[0])
	}
	if led[1].Slug != "fork" {
		t.Errorf("expected the remaining remote to follow, got %+v", led[1])
	}
}

// TestWithinKeepsTheProjectAlreadyChosen is the second stage's half of the
// split: once PROJ/ has been typed, a checkout pointing somewhere else has
// nothing to say.
func TestWithinKeepsTheProjectAlreadyChosen(t *testing.T) {
	t.Parallel()

	repositories := []Repository{
		{ProjectKey: "PLAT", Slug: "service"},
		{ProjectKey: "OTHER", Slug: "service"},
	}

	kept := within(repositories, "plat")

	if len(kept) != 1 || kept[0].ProjectKey != "PLAT" {
		t.Fatalf("expected only the chosen project's repositories, got %v", kept)
	}
}

// TestDescribeRepositoryCarriesNameAndDescription keeps the slug's column
// readable: a slug is an abbreviation, and the name beside it is what makes it
// recognisable.
func TestDescribeRepositoryCarriesNameAndDescription(t *testing.T) {
	t.Parallel()

	name, description := "Platform Service", "Runs the platform"

	if got := describeRepository(restRepository(&name, &description)); got != "Platform Service - Runs the platform" {
		t.Errorf("got %q", got)
	}
	if got := describeRepository(restRepository(&name, nil)); got != "Platform Service" {
		t.Errorf("a repository with no description got %q", got)
	}
	if got := describeRepository(restRepository(nil, nil)); got != "" {
		t.Errorf("a repository with neither got %q", got)
	}
}

func restRepository(name *string, description *string) openapigenerated.RestRepository {
	return openapigenerated.RestRepository{Name: name, Description: description}
}

func set(candidates []Candidate) map[string]bool {
	values := make(map[string]bool, len(candidates))
	for _, candidate := range candidates {
		values[candidate.Value] = true
	}

	return values
}
