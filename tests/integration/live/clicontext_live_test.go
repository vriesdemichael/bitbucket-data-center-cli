//go:build live

package live_test

import (
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
)

// Repository context for the CLI, carried per test instead of published to the
// process.
//
// configureLiveCLIEnv used to set BITBUCKET_PROJECT_KEY and
// BITBUCKET_REPO_SLUG with t.Setenv. Six variables went in; four of them --
// the URL and the three credentials -- are the same for every test in the run
// and are set once in TestMain, where being process-wide costs nothing because
// nothing ever changes them. The two that differ per test are the reason no
// live test could declare t.Parallel: a second test setting them is visible to
// the first, and t.Setenv refuses to be called from a parallel test at all.
//
// They travel as a --repo flag now, which is the CLI's own way of being told
// which repository to act on, and which also suppresses the git-remote
// inference that would otherwise read the checkout the tests run inside.
var liveRepoContexts sync.Map // test name -> "PROJECT/slug"

// setLiveRepoContext records the repository a test's CLI calls should address.
//
// An empty project or slug records nothing. Callers pass "" to say the test has
// no repository context on purpose -- auth token create is scoped by --user,
// and bb rejects a second scope flag -- and storing "/" for that would have
// injected a --repo the command refuses.
func setLiveRepoContext(t *testing.T, projectKey, repositorySlug string) {
	t.Helper()

	if strings.TrimSpace(projectKey) == "" || strings.TrimSpace(repositorySlug) == "" {
		liveRepoContexts.Delete(t.Name())

		return
	}

	liveRepoContexts.Store(t.Name(), projectKey+"/"+repositorySlug)
	t.Cleanup(func() { liveRepoContexts.Delete(t.Name()) })
}

// liveRepoContextFor finds the context a test or any of its subtests should use.
//
// Keyed by name and matched by prefix because a subtest gets its own *testing.T
// while the context was registered by its parent, and the testing package
// exposes no way to walk from one to the other. Subtest names are the parent's
// name plus a suffix, so the longest registered prefix is the nearest ancestor
// that set one.
func liveRepoContextFor(t *testing.T) (string, bool) {
	name := t.Name()

	best, found := "", ""
	liveRepoContexts.Range(func(key, value any) bool {
		candidate, _ := key.(string)
		if candidate != name && !strings.HasPrefix(name, candidate+"/") {
			return true
		}
		if len(candidate) > len(best) {
			best, _ = candidate, ""
			found, _ = value.(string)
		}

		return true
	})

	return found, found != ""
}

// withLiveRepoContext adds --repo to args when the command takes one and the
// caller has not named a repository itself.
//
// Injected here rather than written into 979 call sites. The alternative was to
// edit every invocation, which is the same change made 979 times and impossible
// to review; this way the rule is stated once and a test that passes its own
// --repo still wins.
//
// Only --repo is read as the caller naming its own scope. Treating --project
// and --user that way too was tried and was wrong: --user names a reviewer on
// `pr review reviewer remove` and --project names the fork's destination on
// `repo admin fork`, so six tests lost the context they needed. The commands
// where those flags really are a competing scope are few enough to say so at
// the call site, with executeLiveCLIUnscoped.
func withLiveRepoContext(t *testing.T, root *cobra.Command, args []string) []string {
	context, ok := liveRepoContextFor(t)
	if !ok {
		return args
	}

	for _, arg := range args {
		if arg == "--repo" || strings.HasPrefix(arg, "--repo=") {
			return args
		}
	}

	// Ask cobra which command these arguments actually reach, and whether it
	// accepts a repository. Commands that take a project key positionally, or
	// address no repository at all, are left alone.
	target, _, err := root.Find(args)
	if err != nil || target == nil {
		return args
	}
	if target.Flags().Lookup("repo") == nil && target.InheritedFlags().Lookup("repo") == nil {
		return args
	}
	if hasCompetingScope(target, args) {
		return args
	}

	return append(append([]string{}, args...), "--repo", context)
}

// competingScopes names the commands where a flag the caller passed already
// says what the call is about, so a --repo beside it is wrong.
//
// Keyed by command path prefix and checked against the resolved command,
// because whether a flag competes with --repo is a property of the command and
// not of the flag: --user names a scope on `auth token create` and a reviewer
// on `pr review reviewer remove`, and --project names a scope on
// `reviewer-group create` and the destination on `repo admin fork`. A rule
// written on the flag alone was tried, and it broke six tests that needed the
// context it withheld.
//
// Two of these refuse the combination outright and one does not, which is why
// the list is worth having rather than leaving each call to fail visibly:
// `reviewer condition list --project X` with a --repo added lists the
// repository's conditions and reports success, so a test asking about a
// project that does not exist got an answer instead of the error it was
// checking for.
var competingScopes = map[string][]string{
	"auth token":          {"--user", "--project"},
	"reviewer-group":      {"--project"},
	"reviewer condition":  {"--project"},
	"search prs":          {"--role"},
	"search pull-request": {"--role"},
}

func hasCompetingScope(target *cobra.Command, args []string) bool {
	// CommandPath is "bb auth token create"; the keys above are what follows
	// the binary name.
	path := strings.TrimPrefix(target.CommandPath(), target.Root().Name()+" ")

	for prefix, flags := range competingScopes {
		if path != prefix && !strings.HasPrefix(path, prefix+" ") {
			continue
		}
		for _, arg := range args {
			for _, flag := range flags {
				if arg == flag || strings.HasPrefix(arg, flag+"=") {
					return true
				}
			}
		}
	}

	return false
}

// configureLiveCLIConstants publishes the settings every test shares.
//
// The URL and the credentials do not vary across the run, so writing them to
// the process once is safe in a way that writing the repository context never
// was: nothing mutates them afterwards, so no test can observe another's value.
// Set here rather than through t.Setenv because t.Setenv is what disqualifies a
// test from t.Parallel, and every live test called a helper that used it.
func configureLiveCLIConstants() {
	applyLocalLiveDefaultsToProcess()

	cfg, err := config.LoadFromEnv()
	if err != nil {
		return
	}

	set := func(key, value string) {
		if strings.TrimSpace(value) != "" {
			_ = os.Setenv(key, value)
		}
	}

	_ = os.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	set("BITBUCKET_URL", cfg.BitbucketURL)
	set("BITBUCKET_USERNAME", cfg.BitbucketUsername)
	set("BITBUCKET_PASSWORD", cfg.BitbucketPassword)
	set("BITBUCKET_TOKEN", cfg.BitbucketToken)
}

// applyLocalLiveDefaultsToProcess is applyLocalLiveDefaults without a *testing.T.
//
// The same defaults, written once to the process rather than once per test.
// They were per test only because t.Setenv restores them afterwards, which is a
// property nothing here needed and which cost every live test its ability to
// run alongside another.
func applyLocalLiveDefaultsToProcess() {
	if strings.TrimSpace(os.Getenv("BB_DISABLE_STORED_CONFIG")) == "" {
		_ = os.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	}

	bitbucketURL := strings.TrimSpace(os.Getenv("BITBUCKET_URL"))
	switch {
	case bitbucketURL == "":
		_ = os.Setenv("BITBUCKET_URL", "http://localhost:7990")
	case !strings.Contains(bitbucketURL, "://") && isLocalBitbucketHost(bitbucketURL):
		_ = os.Setenv("BITBUCKET_URL", "http://"+bitbucketURL)
	}

	hasUser := strings.TrimSpace(os.Getenv("BITBUCKET_USERNAME")) != "" || strings.TrimSpace(os.Getenv("BITBUCKET_USER")) != ""
	hasPassword := strings.TrimSpace(os.Getenv("BITBUCKET_PASSWORD")) != ""
	hasAdmin := strings.TrimSpace(os.Getenv("ADMIN_USER")) != "" || strings.TrimSpace(os.Getenv("ADMIN_PASSWORD")) != ""
	if !hasUser && !hasPassword && !hasAdmin {
		_ = os.Setenv("ADMIN_USER", "admin")
		_ = os.Setenv("ADMIN_PASSWORD", "admin")
	}
}

// uniqueSuffix returns something no other test in this process will produce.
//
// Names used to be the last six digits of the clock in nanoseconds. Those cycle
// every millisecond, so sequentially they were fine and in parallel they were
// not: two tests seeding at once picked the same project key, and Bitbucket
// answered the duplicate insert with a 500 rather than the 409 the retry loop
// was written for. A counter cannot collide with itself, and the clock prefix
// keeps keys sortable and distinct across runs against the same instance.
func uniqueSuffix() string {
	return strconv.FormatInt(time.Now().Unix()%100000, 10) + strconv.FormatUint(uniqueCounter.Add(1), 10)
}

var uniqueCounter atomic.Uint64

// configureLiveCLIEnvVars publishes the repository context the way a user's
// shell would, for the tests whose subject is that mechanism.
//
// The context-inference tests set a deliberately wrong project and slug and
// then assert the git remote wins. A --repo flag also wins over inference, so
// carrying their context as a flag would have made them pass by suppressing the
// thing they exist to check. They chdir into a fixture checkout anyway, so they
// were never going to be parallel, and t.Setenv costs them nothing.
func configureLiveCLIEnvVars(t *testing.T, projectKey, repositorySlug string) {
	t.Helper()

	t.Setenv("BITBUCKET_PROJECT_KEY", projectKey)
	t.Setenv("BITBUCKET_REPO_SLUG", repositorySlug)
}

// dashboardPage is how many rows an instance-wide listing asks for.
//
// Sized against the suite rather than the assertion: the dashboard spans every
// repository on the instance, so a page of 25 was filled by other tests' pull
// requests long before it reached the one under test, and the failure read as
// "the filter dropped it" rather than "the page ended". A test that fills even
// this page fails saying so instead of guessing.
const dashboardPage = 1000

const dashboardPageArg = "1000"
