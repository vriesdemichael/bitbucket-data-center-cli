package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/style"
	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
)

const (
	planningModeStatic   = "static"
	planningModeStateful = "stateful"

	capabilityFull    = "full"
	capabilityPartial = "partial"
)

type dryRunProfile struct {
	Intent        string
	Action        string
	Stateful      bool
	CapabilityMsg string
}

type dryRunItem struct {
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

type dryRunSummary struct {
	Total       int `json:"total"`
	Supported   int `json:"supported"`
	Unsupported int `json:"unsupported"`

	NoopCount    int `json:"no_op"`
	CreateCount  int `json:"create"`
	UpdateCount  int `json:"update"`
	DeleteCount  int `json:"delete"`
	UnknownCount int `json:"unknown"`
}

type dryRunPreview struct {
	DryRun       bool          `json:"dry_run"`
	PlanningMode string        `json:"planning_mode"`
	Capability   string        `json:"capability"`
	Items        []dryRunItem  `json:"items"`
	Summary      dryRunSummary `json:"summary"`
}

// dryRunProfiles is the single source of truth for dry-run behaviour on every
// mutating command. Stateful: true means the command handler performs its own
// live pre-flight check and writes the dryRunPreview output itself; the
// interceptor passes through to it unchanged. Stateful: false means the
// interceptor generates a static (intent-only) preview using newDryRunPreview.
var dryRunProfiles = map[string]dryRunProfile{
	// branch
	"branch delete":             {Intent: "branch.delete", Action: "delete", Stateful: true},
	"branch create":             {Intent: "branch.create", Action: "create", Stateful: true},
	"branch default set":        {Intent: "branch.default.set", Action: "update", Stateful: true},
	"branch model update":       {Intent: "branch.model.update", Action: "update", Stateful: true},
	"branch restriction create": {Intent: "branch.restriction.create", Action: "create", Stateful: true},
	"branch restriction update": {Intent: "branch.restriction.update", Action: "update", Stateful: true},
	"branch restriction delete": {Intent: "branch.restriction.delete", Action: "delete", Stateful: true},
	// build
	"build status set":      {Intent: "build.status.set", Action: "update", Stateful: true},
	"build required create": {Intent: "build.required.create", Action: "create", Stateful: true},
	"build required update": {Intent: "build.required.update", Action: "update", Stateful: true},
	"build required delete": {Intent: "build.required.delete", Action: "delete", Stateful: true},
	// tag
	"tag create": {Intent: "tag.create", Action: "create", Stateful: true},
	"tag delete": {Intent: "tag.delete", Action: "delete", Stateful: true},
	// repo comment
	"repo comment create": {Intent: "repo.comment.create", Action: "create", Stateful: true},
	"repo comment update": {Intent: "repo.comment.update", Action: "update", Stateful: true},
	"repo comment delete": {Intent: "repo.comment.delete", Action: "delete", Stateful: true},
	// repo settings
	"repo settings workflow webhooks create":           {Intent: "repo.webhook.create", Action: "create", Stateful: true},
	"repo settings workflow webhooks delete":           {Intent: "repo.webhook.delete", Action: "delete", Stateful: true},
	"repo settings pull-requests update":               {Intent: "repo.pull-request-settings.update", Action: "update", Stateful: true},
	"repo settings pull-requests update-approvers":     {Intent: "repo.pull-request-settings.update-approvers", Action: "update", Stateful: true},
	"repo settings pull-requests set-strategy":         {Intent: "repo.pull-request-settings.set-strategy", Action: "update", Stateful: true},
	"repo settings security permissions users grant":   {Intent: "repo.permission.user.grant", Action: "update", Stateful: true},
	"repo settings security permissions users revoke":  {Intent: "repo.permission.user.revoke", Action: "delete", Stateful: true},
	"repo settings security permissions groups grant":  {Intent: "repo.permission.group.grant", Action: "update", Stateful: true},
	"repo settings security permissions groups revoke": {Intent: "repo.permission.group.revoke", Action: "delete", Stateful: true},
	// repo admin
	"repo admin create": {Intent: "repo.admin.create", Action: "create", Stateful: true},
	"repo admin fork":   {Intent: "repo.admin.fork", Action: "create", Stateful: true},
	"repo admin update": {Intent: "repo.admin.update", Action: "update", Stateful: true},
	"repo admin delete": {Intent: "repo.admin.delete", Action: "delete", Stateful: true},
	// insights
	"insights report set":        {Intent: "insights.report.set", Action: "update", Stateful: true},
	"insights report delete":     {Intent: "insights.report.delete", Action: "delete", Stateful: true},
	"insights annotation add":    {Intent: "insights.annotation.add", Action: "create", Stateful: true},
	"insights annotation delete": {Intent: "insights.annotation.delete", Action: "delete", Stateful: true},
	// pr
	"pr create":                 {Intent: "pr.create", Action: "create", Stateful: true},
	"pr update":                 {Intent: "pr.update", Action: "update", Stateful: true},
	"pr merge":                  {Intent: "pr.merge", Action: "update", Stateful: true},
	"pr decline":                {Intent: "pr.decline", Action: "update", Stateful: true},
	"pr reopen":                 {Intent: "pr.reopen", Action: "update", Stateful: true},
	"pr review approve":         {Intent: "pr.review.approve", Action: "update", Stateful: true},
	"pr review unapprove":       {Intent: "pr.review.unapprove", Action: "update", Stateful: true},
	"pr review reviewer add":    {Intent: "pr.review.reviewer.add", Action: "update", Stateful: true},
	"pr review reviewer remove": {Intent: "pr.review.reviewer.remove", Action: "delete", Stateful: true},
	"pr task create":            {Intent: "pr.task.create", Action: "create", Stateful: true},
	"pr task update":            {Intent: "pr.task.update", Action: "update", Stateful: true},
	"pr task delete":            {Intent: "pr.task.delete", Action: "delete", Stateful: true},
	"pr comment add":            {Intent: "pr.comment.add", Action: "create", Stateful: true},
	"pr comment react":          {Intent: "pr.comment.react", Action: "update", Stateful: true},
	"pr comment apply-suggestion": {Intent: "pr.comment.apply-suggestion", Action: "update", Stateful: true},
	"pr auto-merge enable":      {Intent: "pr.auto-merge.enable", Action: "update", Stateful: true},
	"pr auto-merge disable":     {Intent: "pr.auto-merge.disable", Action: "delete", Stateful: true},
	"pr watch":                  {Intent: "pr.watch", Action: "update", Stateful: true},
	"pr unwatch":                {Intent: "pr.unwatch", Action: "delete", Stateful: true},
	"pr rebase":                 {Intent: "pr.rebase", Action: "update", Stateful: true},
	// reviewer conditions
	"reviewer condition create": {Intent: "reviewer.condition.create", Action: "create", Stateful: true},
	"reviewer condition update": {Intent: "reviewer.condition.update", Action: "update", Stateful: true},
	"reviewer condition delete": {Intent: "reviewer.condition.delete", Action: "delete", Stateful: true},
	// reviewer groups
	"reviewer-group create": {Intent: "reviewer-group.create", Action: "create", Stateful: true},
	"reviewer-group update": {Intent: "reviewer-group.update", Action: "update", Stateful: true},
	"reviewer-group delete": {Intent: "reviewer-group.delete", Action: "delete", Stateful: true},
	// hook
	"hook enable":    {Intent: "hook.enable", Action: "update", Stateful: true},
	"hook disable":   {Intent: "hook.disable", Action: "update", Stateful: true},
	"hook configure": {Intent: "hook.configure", Action: "update", Stateful: true},
	// project
	"project create":                    {Intent: "project.create", Action: "create", Stateful: true},
	"project update":                    {Intent: "project.update", Action: "update", Stateful: true},
	"project delete":                    {Intent: "project.delete", Action: "delete", Stateful: true},
	"project permissions users grant":   {Intent: "project.permission.user.grant", Action: "update", Stateful: true},
	"project permissions users revoke":  {Intent: "project.permission.user.revoke", Action: "delete", Stateful: true},
	"project permissions groups grant":  {Intent: "project.permission.group.grant", Action: "update", Stateful: true},
	"project permissions groups revoke": {Intent: "project.permission.group.revoke", Action: "delete", Stateful: true},
}

func registerGlobalDryRunInterceptors(root *cobra.Command, options *rootOptions) {
	if root == nil || options == nil {
		return
	}

	var visit func(*cobra.Command)
	visit = func(command *cobra.Command) {
		if command == nil {
			return
		}

		path := dryRunCommandPath(command)
		profile, hasDryRunProfile := dryRunProfiles[path]
		if hasDryRunProfile && command.RunE != nil {
			originalRun := command.RunE
			command.RunE = func(cmd *cobra.Command, args []string) error {
				if !options.DryRun {
					return originalRun(cmd, args)
				}

				if profile.Stateful {
					return originalRun(cmd, args)
				}

				preview := newDryRunPreview(profile, cmd, args)
				return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
			}
		} else if command.RunE != nil {
			originalRun := command.RunE
			command.RunE = func(cmd *cobra.Command, args []string) error {
				if !options.DryRun {
					return originalRun(cmd, args)
				}

				path := dryRunCommandPath(cmd)
				if !isServerMutatingPath(path) {
					return originalRun(cmd, args)
				}

				return dryRunUnsupportedError(path)
			}
		}

		for _, child := range command.Commands() {
			visit(child)
		}
	}

	visit(root)
}

func isServerMutatingPath(path string) bool {
	trimmedPath := strings.TrimSpace(path)
	if trimmedPath == "" {
		return false
	}
	if strings.EqualFold(trimmedPath, "update") {
		return false
	}

	parts := strings.Fields(trimmedPath)
	if len(parts) == 0 {
		return false
	}

	last := strings.ToLower(strings.TrimSpace(parts[len(parts)-1]))
	switch last {
	case "create", "update", "delete", "set", "grant", "revoke", "enable", "disable", "configure", "merge", "decline", "reopen", "approve", "unapprove", "add", "remove", "fork", "apply":
		return true
	default:
		return false
	}
}

func dryRunCommandPath(command *cobra.Command) string {
	if command == nil {
		return ""
	}

	path := strings.TrimSpace(command.CommandPath())
	path = strings.TrimPrefix(path, "bb ")
	return strings.TrimSpace(path)
}

func dryRunUnsupportedError(path string) error {
	if strings.EqualFold(strings.TrimSpace(path), "bulk apply") {
		return apperrors.New(apperrors.KindValidation, "bulk apply does not support --dry-run; use bulk plan to preview operations", nil)
	}

	return apperrors.New(apperrors.KindNotImplemented, fmt.Sprintf("dry-run is not implemented for %s", path), nil)
}

func newDryRunPreview(profile dryRunProfile, command *cobra.Command, args []string) dryRunPreview {
	target := map[string]any{}
	repository := ""
	if command != nil {
		if flag := command.Flag("repo"); flag != nil {
			repository = strings.TrimSpace(flag.Value.String())
		}
	}
	if repository != "" {
		target["repository"] = repository
	}
	if len(args) > 0 {
		target["args"] = append([]string(nil), args...)
	}

	item := dryRunItem{
		Intent:          profile.Intent,
		Target:          target,
		Action:          profile.Action,
		PredictedAction: profile.Action,
		Supported:       true,
		Reason:          strings.TrimSpace(profile.CapabilityMsg),
		Confidence:      capabilityPartial,
	}

	summary := dryRunSummary{Total: 1, Supported: 1}
	switch profile.Action {
	case "no-op":
		summary.NoopCount = 1
	case "create":
		summary.CreateCount = 1
	case "update":
		summary.UpdateCount = 1
	case "delete":
		summary.DeleteCount = 1
	default:
		summary.UnknownCount = 1
	}

	return dryRunPreview{
		DryRun:       true,
		PlanningMode: planningModeStatic,
		Capability:   capabilityPartial,
		Items:        []dryRunItem{item},
		Summary:      summary,
	}
}

func writeDryRunPreview(writer io.Writer, asJSON bool, preview dryRunPreview) error {
	if asJSON {
		return writeJSON(writer, preview)
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
