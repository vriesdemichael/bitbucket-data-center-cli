package dryrunpreview

import (
	"fmt"
	"io"
	"strings"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/style"
)

const (
	PlanningModeStatic   = "static"
	PlanningModeStateful = "stateful"

	CapabilityFull    = "full"
	CapabilityPartial = "partial"
)

// A Tier says how a prediction was arrived at, and the confidence label is
// derived from it rather than written beside it.
//
// Capability and Confidence used to be strings typed out at each of a hundred
// and three construction sites, with nothing relating the label to what the
// code had actually done. The only thing keeping them honest was the author of
// each site remembering to be modest, and #479 is what happened when one did
// not: the strongest claim the contract can make, attached to a prediction made
// from a single state field, on the one irreversible pull request operation.
//
// ADR-035 already asked for explicit capability signalling. This is what makes
// "explicit" mean something other than "someone typed it".
type Tier string

const (
	// TierServerValidated means Bitbucket answered the exact question, through
	// its own dry-run endpoint or an equivalent authoritative call.
	TierServerValidated Tier = "server-validated"

	// TierPreconditionsChecked means the caller's permission and the current
	// state were both fetched, and the preconditions for this operation were
	// evaluated against them.
	TierPreconditionsChecked Tier = "preconditions-checked"

	// TierPredicted means the answer was derived from partial state. It cannot
	// report full confidence, which is the point of naming it.
	TierPredicted Tier = "predicted"
)

// Confidence is the published label for a tier.
//
// Only the two tiers that checked something earn "full". A prediction made from
// partial state reports "partial" whatever its author believed, because the
// tier is the input and the label is computed from it.
func (tier Tier) Confidence() string {
	switch tier {
	case TierServerValidated, TierPreconditionsChecked:
		return CapabilityFull
	case TierPredicted:
		return CapabilityPartial
	default:
		return CapabilityPartial
	}
}

type Item struct {
	Intent          string         `json:"intent"`
	Target          map[string]any `json:"target"`
	Action          string         `json:"action"`
	PredictedAction string         `json:"predictedAction,omitempty"`
	Supported       bool           `json:"supported"`
	Reason          string         `json:"reason,omitempty"`
	Tier            Tier           `json:"tier,omitempty"`
	Confidence      string         `json:"confidence,omitempty"`
	RequiredState   []string       `json:"requiredState,omitempty"`
	BlockingReasons []string       `json:"blockingReasons,omitempty"`
}

type Summary struct {
	Total       int `json:"total"`
	Supported   int `json:"supported"`
	Unsupported int `json:"unsupported"`

	NoopCount    int `json:"noOp"`
	CreateCount  int `json:"create"`
	UpdateCount  int `json:"update"`
	DeleteCount  int `json:"delete"`
	UnknownCount int `json:"unknown"`
}

type Preview struct {
	DryRun       bool    `json:"dryRun"`
	PlanningMode string  `json:"planningMode"`
	Capability   string  `json:"capability"`
	Items        []Item  `json:"items"`
	Summary      Summary `json:"summary"`
}

func Write(writer io.Writer, asJSON bool, preview Preview) error {
	if asJSON {
		return jsonoutput.Write(writer, preview)
	}

	if _, err := fmt.Fprintf(writer, "%s\n", style.DryRun.Render(fmt.Sprintf("Dry-run (%s, capability=%s)", preview.PlanningMode, preview.Capability))); err != nil {
		return err
	}

	for _, item := range preview.Items {
		line := fmt.Sprintf("- %s=%s %s=%s", style.Secondary.Render("intent"), item.Intent, style.Secondary.Render("action"), item.Action)
		if item.PredictedAction != "" {
			line += fmt.Sprintf(" %s=%s", style.Secondary.Render("predictedAction"), item.PredictedAction)
		}
		if _, err := fmt.Fprintln(writer, line); err != nil {
			return err
		}
		if repository, ok := item.Target["repository"].(string); ok && strings.TrimSpace(repository) != "" {
			if _, err := fmt.Fprintf(writer, "  %s=%s\n", style.Secondary.Render("repository"), style.Resource.Render(repository)); err != nil {
				return err
			}
		}
		if args, ok := item.Target["args"].([]string); ok && len(args) > 0 {
			if _, err := fmt.Fprintf(writer, "  %s=%s\n", style.Secondary.Render("args"), strings.Join(args, " ")); err != nil {
				return err
			}
		}
		if item.Reason != "" {
			if _, err := fmt.Fprintf(writer, "  %s=%s\n", style.Secondary.Render("note"), style.Warning.Render(item.Reason)); err != nil {
				return err
			}
		}
	}

	return nil
}

// The actions a preview can predict.
//
// Named because they were magic strings at 176 sites, and the summary counter a
// preview lands in is chosen by comparing against them. "noop" instead of
// "no-op" reads the same to a person and silently counts as unknown, which is
// the sort of thing that looks like a product bug in a report nobody reruns.
const (
	PredictedCreate = "create"
	PredictedUpdate = "update"
	PredictedDelete = "delete"
	PredictedNoop   = "no-op"

	// PredictedConflict and PredictedBlocked both mean the run would not do
	// what was asked, for different reasons: something is already there, or
	// something forbids it. Neither is a mutation, so both count as unknown.
	PredictedConflict = "conflict"
	PredictedBlocked  = "blocked"
)

// New builds a preview and derives its summary from the items.
//
// The summary used to be written by hand at every site, either as a literal
// beside the item or by an if-ladder after it -- 139 assignments across 103
// previews, all restating what the items already said. A hand-written tally can
// disagree with the items it summarises, and nothing would notice: the report
// is read by an agent deciding whether to proceed, so a preview claiming one
// supported update while carrying a blocked item is worse than no preview.
//
// DryRun is always true. A preview only exists because --dry-run was passed.
func New(planningMode string, capability string, items ...Item) Preview {
	// A copy, not the caller's slice. Called variadically with items... the
	// argument is the caller's backing array, so deriving confidence in place
	// would edit their data and leave preview.Items aliasing it -- an append
	// after this returns would then reach inside a preview already built.
	owned := make([]Item, len(items))
	copy(owned, items)

	// Confidence is computed from the tier rather than taken from the item, so
	// a site cannot claim full for a prediction the tier does not support. An
	// item that names no tier is Predicted, which is the honest default: if the
	// code cannot say what it checked, it did not check enough to claim full.
	for index := range owned {
		if owned[index].Tier == "" {
			owned[index].Tier = TierPredicted
		}
		owned[index].Confidence = owned[index].Tier.Confidence()
	}
	items = owned

	preview := Preview{
		DryRun:       true,
		PlanningMode: planningMode,
		Capability:   capability,
		Items:        items,
		Summary:      Summary{Total: len(items)},
	}

	for _, item := range items {
		if item.Supported {
			preview.Summary.Supported++
		} else {
			preview.Summary.Unsupported++
		}

		switch item.PredictedAction {
		case PredictedCreate:
			preview.Summary.CreateCount++
		case PredictedUpdate:
			preview.Summary.UpdateCount++
		case PredictedDelete:
			preview.Summary.DeleteCount++
		case PredictedNoop:
			preview.Summary.NoopCount++
		default:
			// Conflict, blocked, and anything a future command predicts that
			// this switch has not been taught. Counting it as unknown is the
			// honest answer: the run would not perform a mutation, and saying
			// which is not something this function can invent.
			preview.Summary.UnknownCount++
		}
	}

	return preview
}
