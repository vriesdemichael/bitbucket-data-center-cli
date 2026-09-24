package dryrunpreview

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/style"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// A Tier says how a prediction was arrived at (ADR-078).
//
// It is the input a command states, and the preview reports the weakest tier
// among its items rather than any label an author typed. #479 is what the
// typed label permitted: the strongest claim the contract can make, attached to
// a prediction made from a single state field, on the one irreversible pull
// request operation.
type Tier = jsonoutput.Tier

const (
	// TierServerValidated means Bitbucket answered the exact question, through
	// its own dry-run endpoint or an equivalent authoritative call.
	TierServerValidated = jsonoutput.TierServerValidated

	// TierPreconditionsChecked means the caller's permission and the current
	// state were both fetched, and the preconditions for this operation were
	// evaluated against them.
	TierPreconditionsChecked = jsonoutput.TierPreconditionsChecked

	// TierPredicted means the answer was derived from partial state.
	TierPredicted = jsonoutput.TierPredicted
)

// Item is one change a dry run checked, as the command that checked it states
// it.
type Item struct {
	// Intent names the operation, such as pr.merge.
	Intent string
	// Target names what the change is made to.
	Target map[string]any
	// Action is create, update or delete.
	Action string
	// PredictedAction is what the check found the run would do: the action
	// itself, no-op, or blocked or conflict for a run that would fail.
	PredictedAction string
	// Reason says why, in a sentence.
	Reason string
	// Tier is how the prediction was reached. An item that names none is
	// predicted: if the code cannot say what it checked, it did not check
	// enough to claim more.
	Tier Tier
	// BlockingReasons are what stops a run predicted blocked or conflict.
	BlockingReasons []string
	// Fails is the kind of error the real run would fail with, for an item
	// predicted blocked or conflict. It defaults to conflict, which is what
	// Bitbucket answers for most refusals of a change; an item refused for
	// another reason -- a missing target, a missing permission -- says so.
	Fails apperrors.Kind
}

// The actions a preview can predict.
//
// Named because they were magic strings at 176 sites, and the outcome an item
// reports is chosen by comparing against them.
const (
	PredictedCreate = "create"
	PredictedUpdate = "update"
	PredictedDelete = "delete"
	PredictedNoop   = "no-op"

	// PredictedConflict and PredictedBlocked both mean the run would not do
	// what was asked, for different reasons: something is already there, or
	// something forbids it. Both are would-fail.
	PredictedConflict = "conflict"
	PredictedBlocked  = "blocked"
)

// Preview is a dry run's answer, built from the items the command checked.
type Preview struct {
	items []Item
}

// New builds a preview from the items a command checked.
func New(items ...Item) Preview {
	// A copy, not the caller's slice. Called variadically with items... the
	// argument is the caller's backing array, and a preview aliasing it would
	// change when the caller appended to it afterwards.
	owned := make([]Item, len(items))
	copy(owned, items)

	return Preview{items: owned}
}

// Document is the preview as --dry-run reports it (ADR-096): the weakest tier
// behind the verdict, one effect per item, and the error the real run would
// fail with when any item says it would.
func (preview Preview) Document() jsonoutput.Preview {
	document := jsonoutput.Preview{Tier: TierServerValidated, Effects: make([]jsonoutput.Effect, 0, len(preview.items))}
	if len(preview.items) == 0 {
		// Nothing was checked, so nothing stronger than predicted is claimed.
		document.Tier = TierPredicted
	}

	for _, item := range preview.items {
		tier := item.Tier
		if tier == "" {
			tier = TierPredicted
		}
		if tier.Weaker(document.Tier) {
			document.Tier = tier
		}

		outcome := outcomeOf(item.PredictedAction)
		target := item.Target
		if target == nil {
			target = map[string]any{}
		}
		document.Effects = append(document.Effects, jsonoutput.Effect{
			Action:  item.Action,
			Target:  target,
			Outcome: outcome,
			Reasons: reasonsOf(item, outcome),
		})

		if outcome == jsonoutput.OutcomeWouldFail && document.Error == nil {
			failure := failureOf(item)
			document.Error = &failure
		}
	}

	return document
}

// outcomeOf maps what a check predicted onto the three outcomes the document
// reports.
func outcomeOf(predicted string) jsonoutput.Outcome {
	switch predicted {
	case PredictedNoop:
		return jsonoutput.OutcomeNoOp
	case PredictedBlocked, PredictedConflict:
		return jsonoutput.OutcomeWouldFail
	default:
		return jsonoutput.OutcomeWouldApply
	}
}

// reasonsOf says why an item's outcome is what it is: what stops it, for a run
// that would fail, and the reason the check gave otherwise.
func reasonsOf(item Item, outcome jsonoutput.Outcome) []string {
	reasons := []string{}

	if outcome == jsonoutput.OutcomeWouldFail {
		for _, reason := range item.BlockingReasons {
			if trimmed := strings.TrimSpace(reason); trimmed != "" {
				reasons = append(reasons, trimmed)
			}
		}
	}

	if len(reasons) == 0 {
		if trimmed := strings.TrimSpace(item.Reason); trimmed != "" {
			reasons = append(reasons, trimmed)
		}
	}

	return reasons
}

// failureOf is the error the real run would fail with, for an item that says
// it would.
func failureOf(item Item) jsonoutput.EnvelopeError {
	kind := item.Fails
	if kind == "" {
		kind = apperrors.KindConflict
	}

	message := strings.TrimSpace(item.Reason)
	if message == "" {
		message = "the run would be refused"
	}

	return jsonoutput.EnvelopeErrorOf(apperrors.New(kind, message, nil))
}

// Write emits a preview: the document under --json or --yaml, and the verdict
// with its reasons as text otherwise.
//
// In text, a preview predicting failure makes the process exit with the code
// the real run would (ADR-096), so `bb pr merge 42 --dry-run && bb pr merge 42`
// stops at the check. The document exits 0, since the verdict is in it: under
// --json a non-zero exit always comes with a top-level error.
func Write(writer io.Writer, asJSON bool, preview Preview) error {
	document := preview.Document()
	if asJSON {
		return jsonoutput.WritePreview(writer, document)
	}

	settings, _ := jsonoutput.SettingsOf(writer)
	if err := WriteText(writer, settings.Command, document); err != nil {
		return err
	}

	if document.Error != nil {
		return &apperrors.StateExit{Code: document.Error.ExitCode}
	}

	return nil
}

// WriteText prints a preview's verdict for a person: the verdict and tier on
// one line, then each effect with the reasons for its outcome.
func WriteText(writer io.Writer, command string, document jsonoutput.Preview) error {
	subject := "the command"
	if trimmed := strings.TrimSpace(command); trimmed != "" {
		subject = "bb " + trimmed
	}

	header := fmt.Sprintf("Dry run: %s %s (%s)", subject, verdictOf(document), document.Tier)
	if _, err := fmt.Fprintln(writer, style.DryRun.Render(header)); err != nil {
		return err
	}

	for _, effect := range document.Effects {
		line := strings.TrimSpace(fmt.Sprintf("%s %s", effect.Action, targetSummary(effect.Target)))
		if _, err := fmt.Fprintf(writer, "- %s: %s\n", line, outcomeText(effect.Outcome)); err != nil {
			return err
		}
		for _, reason := range effect.Reasons {
			if _, err := fmt.Fprintf(writer, "    %s\n", reason); err != nil {
				return err
			}
		}
	}

	// A failure found before any effect was checked -- an invalid invocation,
	// a target that does not exist -- has no effect to hang its reason on.
	if document.Error != nil && len(document.Effects) == 0 {
		if _, err := fmt.Fprintf(writer, "    %s\n", document.Error.Message); err != nil {
			return err
		}
	}

	return nil
}

// verdictOf is the verdict in words: would fail when anything would, would
// change nothing when nothing would, and would go through otherwise.
func verdictOf(document jsonoutput.Preview) string {
	if document.Error != nil {
		return "would fail"
	}

	for _, effect := range document.Effects {
		if effect.Outcome != jsonoutput.OutcomeNoOp {
			return "would go through"
		}
	}

	if len(document.Effects) == 0 {
		return "would go through"
	}

	return "would change nothing"
}

func outcomeText(outcome jsonoutput.Outcome) string {
	switch outcome {
	case jsonoutput.OutcomeNoOp:
		return "no change"
	case jsonoutput.OutcomeWouldFail:
		return "would fail"
	default:
		return "would apply"
	}
}

// targetSummary names a target in one line: its repository first, then the
// rest by key, and the arguments a static preview recorded last.
func targetSummary(target map[string]any) string {
	var parts []string

	if repository, ok := target["repository"].(string); ok && strings.TrimSpace(repository) != "" {
		parts = append(parts, style.Resource.Render(repository))
	}

	keys := make([]string, 0, len(target))
	for key := range target {
		if key != "repository" && key != "args" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", key, target[key]))
	}

	if args, ok := target["args"].([]string); ok && len(args) > 0 {
		parts = append(parts, strings.Join(args, " "))
	}

	return strings.Join(parts, " ")
}
