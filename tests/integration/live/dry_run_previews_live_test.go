//go:build live

package live_test

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/jsonoutput"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// Dry-run previews, against real state.
//
// The mocks these replace built a preview from state their author supplied and
// asserted its shape. Two things were out of reach that way. The prediction
// itself rests on what the server currently holds -- a create that would
// conflict, a set that is already the value asked for -- and a mock deciding
// that state decides the answer too. And nothing checked the promise the whole
// feature rests on: that a dry run writes nothing.
//
// Each case here reads the state back afterwards. A preview that is right about
// what would happen and wrong about doing it is the failure that matters.
func TestLiveDryRunPreviewsAndLeaveNoTrace(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	repoRef := seeded.Key + "/" + repo.Slug
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	user, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create user failed: %v", err)
	}

	t.Run("granting a repository permission the user does not have", func(t *testing.T) {
		// A grant to somebody with no entry is a create, not an update. The
		// distinction comes from the current permission listing, which is the
		// half a mock decides for itself.
		output := mustLiveCLI(t, "--dry-run", "repo", "settings", "security", "permissions", "users", "grant",
			user.Username, "REPO_READ", "--repo", repoRef)

		assertLivePreview(t, output, jsonoutput.OutcomeWouldApply, "will create")

		listing := mustLiveCLI(t, "repo", "permissions", "list", "--repo", repoRef, "--all")
		if strings.Contains(listing, user.Username) {
			t.Fatalf("the dry run granted the permission:\n%s", listing)
		}
	})

	t.Run("creating a project", func(t *testing.T) {
		// Upper-cased, as Bitbucket stores a project key: the follow-up lookup that
		// proves the dry run created nothing has to ask for the key it would have.
		// The name is as unique as the key, because a name already in use is
		// refused as well, and the preview says so.
		key := strings.ToUpper(testsupport.UniqueName("DRYP"))

		output := mustLiveCLI(t, "--dry-run", "project", "create", key, "--name", "Dry run project "+key)
		assertLivePreview(t, output, jsonoutput.OutcomeWouldApply, "project will be created")

		// Not found, rather than any failure: a read that failed for another
		// reason would pass for a project that is not there.
		if output, err := executeLiveCLI(t, "--json", "project", "get", key); apperrors.ExitCode(err) != 4 {
			t.Fatalf("project %s after the dry run: exit %d, want 4 (not_found): %v\n%s", key, apperrors.ExitCode(err), err, output)
		}
	})

	t.Run("creating a workflow webhook", func(t *testing.T) {
		output := mustLiveCLI(t, "--dry-run", "repo", "settings", "workflow", "webhooks", "create",
			"dry-hook", "http://example.invalid/dry", "--repo", repoRef)

		assertLivePreview(t, output, jsonoutput.OutcomeWouldApply, "webhook will be created")

		listing := mustLiveCLI(t, "repo", "settings", "workflow", "webhooks", "list", "--repo", repoRef)
		if strings.Contains(listing, "dry-hook") {
			t.Fatalf("the dry run created the webhook:\n%s", listing)
		}
	})

	t.Run("setting a default branch it already has", func(t *testing.T) {
		// The prediction here depends on current state, which is the half a
		// mock decides for itself: setting the branch that is already default
		// is a no-op, not an update.
		current := currentLiveDefaultBranch(t)

		output := mustLiveCLI(t, "--dry-run", "branch", "default", "set", current)
		assertLivePreview(t, output, jsonoutput.OutcomeNoOp)

		if after := currentLiveDefaultBranch(t); after != current {
			t.Fatalf("the default branch moved to %q during a dry run", after)
		}
	})
}

// livePreview is the document a dry run writes under --json or --yaml
// (ADR-096): the verdict on the real run, and the command it is about.
//
// The tags are yaml tags because YAML reads JSON too, so one decoder takes
// either format.
type livePreview struct {
	Preview *struct {
		Tier    jsonoutput.Tier     `yaml:"tier"`
		Effects []livePreviewEffect `yaml:"effects"`
		Data    any                 `yaml:"data"`
		Error   *struct {
			Kind     apperrors.Kind `yaml:"kind"`
			Message  string         `yaml:"message"`
			ExitCode int            `yaml:"exitCode"`
		} `yaml:"error"`
	} `yaml:"preview"`
	Meta struct {
		Command   string `yaml:"command"`
		BBVersion string `yaml:"bbVersion"`
	} `yaml:"meta"`
}

// livePreviewEffect is one change a dry run says the real run would make.
type livePreviewEffect struct {
	Action  string             `yaml:"action"`
	Target  map[string]any     `yaml:"target"`
	Outcome jsonoutput.Outcome `yaml:"outcome"`
	Reasons []string           `yaml:"reasons"`
}

// parseLivePreview reads the preview document in output, if output is one.
//
// It starts at the first line that opens a document, because the live helpers
// hand back stdout and stderr together, and a warning can come first.
func parseLivePreview(output string) (livePreview, bool) {
	var document livePreview

	start := -1
	for _, opening := range []string{"{", "preview:"} {
		if index := strings.Index(output, opening); index >= 0 && (start < 0 || index < start) {
			start = index
		}
	}
	if start < 0 {
		return document, false
	}

	decoder := yaml.NewDecoder(strings.NewReader(output[start:]))
	if err := decoder.Decode(&document); err != nil || document.Preview == nil || document.Preview.Tier == "" {
		return livePreview{}, false
	}

	return document, true
}

// decodeLivePreview reads a dry run's document, failing the test when the
// output is not one.
func decodeLivePreview(t *testing.T, output string) livePreview {
	t.Helper()

	document, ok := parseLivePreview(output)
	if !ok {
		t.Fatalf("expected a dry run's preview document, got:\n%s", output)
	}
	if strings.TrimSpace(document.Meta.BBVersion) == "" {
		t.Fatalf("expected the preview to carry meta.bbVersion:\n%s", output)
	}

	return document
}

// effect is the one change a dry run of a single change reports.
func (document livePreview) effect(t *testing.T, output string) livePreviewEffect {
	t.Helper()

	if len(document.Preview.Effects) != 1 {
		t.Fatalf("expected one effect in the preview, got %d:\n%s", len(document.Preview.Effects), output)
	}

	return document.Preview.Effects[0]
}

// assertLivePreview checks a dry run of one change predicts outcome for the
// command it ran, with reasons that say each of reasons.
//
// The old preview named create and update apart; the verdict calls both
// would-apply, so where a test is about which one it is, the reason says.
func assertLivePreview(t *testing.T, output string, outcome jsonoutput.Outcome, reasons ...string) livePreview {
	t.Helper()

	document := decodeLivePreview(t, output)
	effect := document.effect(t, output)
	if effect.Outcome != outcome {
		t.Fatalf("expected outcome %s, got %s (reasons %q):\n%s", outcome, effect.Outcome, effect.Reasons, output)
	}
	if len(effect.Reasons) == 0 {
		t.Errorf("the effect gives no reason for its outcome:\n%s", output)
	}
	for _, want := range reasons {
		if !strings.Contains(strings.Join(effect.Reasons, "; "), want) {
			t.Errorf("expected a reason saying %q, got %q", want, effect.Reasons)
		}
	}

	// error is there exactly when the run would fail.
	if failing := outcome == jsonoutput.OutcomeWouldFail; failing != (document.Preview.Error != nil) {
		t.Errorf("an effect that is %s beside error %v:\n%s", outcome, document.Preview.Error, output)
	}

	return document
}

// assertLiveRefusal checks a dry run predicts the real run fails, and with the
// kind of error kind.
func assertLiveRefusal(t *testing.T, output string, kind apperrors.Kind, reasons ...string) livePreview {
	t.Helper()

	document := assertLivePreview(t, output, jsonoutput.OutcomeWouldFail, reasons...)
	if document.Preview.Error.Kind != kind {
		t.Fatalf("expected the run to fail with %s, the preview says %s:\n%s", kind, document.Preview.Error.Kind, output)
	}

	return document
}

// liveDryRunHeader is the first line a dry run prints in text: the command,
// the verdict, and its tier.
var liveDryRunHeader = regexp.MustCompile(`(?m)^Dry run: bb (.+?) (?:would fail|would go through|would change nothing) \(([a-z-]+)\)\s*$`)

// liveANSI is a terminal escape sequence, which a styled line may carry.
var liveANSI = regexp.MustCompile("\x1b\\[[0-9;]*m")

// holdLiveDryRunToDeclaredTier fails the test when output is a dry run's
// verdict claiming a stronger tier than its command declares.
//
// Every command the live suite runs passes through here, so each declaration
// is held to what the previews report against a real Bitbucket, not to what
// whoever wrote the declaration expected. A preview can report less than the
// declared tier -- pr merge on a pull request Bitbucket does not say whether it
// can merge -- and never more. A verdict found as a failure during the check is
// not written by the command, and in-process it arrives as an error, so it
// never reaches this.
func holdLiveDryRunToDeclaredTier(t *testing.T, output string) {
	t.Helper()

	command, tier, found := liveDryRunTier(output)
	if !found {
		return
	}

	declared, known := cli.DeclaredDryRunTier(command)
	if !known {
		// A command that only reads, which ran for real and declares nothing.
		return
	}
	if declared.Weaker(tier) {
		t.Fatalf("bb %s --dry-run reported tier %s, stronger than the %s its profile declares in internal/cli/dryrun.go:\n%s",
			command, tier, declared, output)
	}
}

// liveDryRunTier is the command and tier of the verdict in output: a document
// under --json or --yaml, or the header line in text.
func liveDryRunTier(output string) (string, jsonoutput.Tier, bool) {
	if document, ok := parseLivePreview(output); ok {
		return document.Meta.Command, document.Preview.Tier, true
	}

	match := liveDryRunHeader.FindStringSubmatch(liveANSI.ReplaceAllString(output, ""))
	if match == nil {
		return "", "", false
	}

	return match[1], jsonoutput.Tier(match[2]), true
}

// TestLiveAuthIdentity covers `auth identity` and its `whoami` alias against
// the account actually being used.
//
// The mock answered with a user it invented and asserted bb printed that slug,
// which proves the formatter and the fixture agree. Asking the server who it
// thinks you are is the only version of this question worth answering.
func TestLiveAuthIdentity(t *testing.T) {
	harness := newLiveHarness(t)

	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", harness.config.BitbucketURL)
	t.Setenv("BITBUCKET_USERNAME", harness.config.BitbucketUsername)
	t.Setenv("BITBUCKET_PASSWORD", harness.config.BitbucketPassword)
	t.Setenv("BITBUCKET_TOKEN", harness.config.BitbucketToken)

	expected := harness.username()

	t.Run("identity names the authenticated account", func(t *testing.T) {
		output := mustLiveCLI(t, "auth", "identity")
		if !strings.Contains(output, expected) {
			t.Fatalf("expected %q in the identity output:\n%s", expected, output)
		}
	})

	t.Run("whoami is the same answer", func(t *testing.T) {
		output, err := executeLiveCLI(t, "auth", "whoami")
		if err != nil {
			t.Fatalf("auth whoami failed: %v\noutput: %s", err, output)
		}
		if !strings.Contains(output, expected) {
			t.Fatalf("expected %q from whoami:\n%s", expected, output)
		}
	})
}

// TestLiveReviewerConditionCreateDryRun completes the dry-run set.
func TestLiveReviewerConditionCreateDryRun(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	repoRef := seeded.Key + "/" + repo.Slug
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	before := mustLiveCLI(t, "reviewer", "condition", "list", "--repo", repoRef)

	// A reviewer, by numeric id. This condition had none, and Bitbucket
	// refuses a condition without one: the create it was predicted to be was
	// never going to happen.
	reviewerID, err := harness.userID(ctx, harness.username())
	if err != nil {
		t.Fatalf("look up the reviewer's id failed: %v", err)
	}
	condition := fmt.Sprintf(`{"sourceMatcher":{"id":"ANY_REF","type":{"id":"ANY_REF"}},`+
		`"targetMatcher":{"id":"refs/heads/master","type":{"id":"BRANCH"}},"reviewers":[{"id":%d}],"requiredApprovals":1}`, reviewerID)
	output := mustLiveCLI(t, "--dry-run", "reviewer", "condition", "create", condition, "--repo", repoRef)

	assertLivePreview(t, output, jsonoutput.OutcomeWouldApply, "will be created")

	if after := mustLiveCLI(t, "reviewer", "condition", "list", "--repo", repoRef); after != before {
		t.Fatalf("the dry run changed the conditions\nbefore: %s\nafter:  %s", before, after)
	}
}
