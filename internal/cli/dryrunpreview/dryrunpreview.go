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

type Item struct {
	Intent          string         `json:"intent"`
	Target          map[string]any `json:"target"`
	Action          string         `json:"action"`
	PredictedAction string         `json:"predictedAction,omitempty"`
	Supported       bool           `json:"supported"`
	Reason          string         `json:"reason,omitempty"`
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
