package dryrunpreview

import (
	"fmt"
	"io"
	"strings"

	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/style"
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
	PredictedAction string         `json:"predicted_action,omitempty"`
	Supported       bool           `json:"supported"`
	Reason          string         `json:"reason,omitempty"`
	Confidence      string         `json:"confidence,omitempty"`
	RequiredState   []string       `json:"required_state,omitempty"`
	BlockingReasons []string       `json:"blocking_reasons,omitempty"`
}

type Summary struct {
	Total       int `json:"total"`
	Supported   int `json:"supported"`
	Unsupported int `json:"unsupported"`

	NoopCount    int `json:"no_op"`
	CreateCount  int `json:"create"`
	UpdateCount  int `json:"update"`
	DeleteCount  int `json:"delete"`
	UnknownCount int `json:"unknown"`
}

type Preview struct {
	DryRun       bool    `json:"dry_run"`
	PlanningMode string  `json:"planning_mode"`
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
			line += fmt.Sprintf(" %s=%s", style.Secondary.Render("predicted_action"), item.PredictedAction)
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
