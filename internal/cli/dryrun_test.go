package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
)

type failWriter struct{}

func (failWriter) Write(_ []byte) (int, error) {
	return 0, errors.New("write failed")
}

type failAfterWriter struct {
	writes    int
	failAfter int
}

func (writer *failAfterWriter) Write(value []byte) (int, error) {
	if writer.writes >= writer.failAfter {
		return 0, errors.New("write failed")
	}
	writer.writes++
	return len(value), nil
}

func TestIsServerMutatingPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{path: "project create", want: true},
		{path: "pr merge", want: true},
		{path: "repo settings workflow webhooks delete", want: true},
		{path: "project list", want: false},
		{path: "repo settings security permissions users list", want: false},
		{path: "", want: false},
	}

	for _, tc := range tests {
		if got := isServerMutatingPath(tc.path); got != tc.want {
			t.Fatalf("isServerMutatingPath(%q)=%t want %t", tc.path, got, tc.want)
		}
	}
}

func TestRegisterGlobalDryRunInterceptorsBulkApplyRejected(t *testing.T) {
	options := &rootOptions{DryRun: true, JSON: true}
	root := &cobra.Command{Use: "bb", SilenceErrors: true, SilenceUsage: true}
	root.PersistentFlags().BoolVar(&options.DryRun, "dry-run", false, "")
	root.PersistentFlags().BoolVar(&options.JSON, "json", false, "")

	originalCalled := false
	bulkCmd := &cobra.Command{Use: "bulk"}
	applyCmd := &cobra.Command{
		Use: "apply",
		RunE: func(cmd *cobra.Command, args []string) error {
			originalCalled = true
			return nil
		},
	}
	bulkCmd.AddCommand(applyCmd)
	root.AddCommand(bulkCmd)

	registerGlobalDryRunInterceptors(root, options)

	buffer := &bytes.Buffer{}
	root.SetOut(buffer)
	root.SetErr(buffer)
	root.SetArgs([]string{"--dry-run", "--json", "bulk", "apply"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected bulk apply dry-run to be rejected")
	}
	if originalCalled {
		t.Fatal("expected command execution to be intercepted in dry-run mode")
	}
	if apperrors.KindOf(err) != apperrors.KindValidation {
		t.Fatalf("expected validation kind, got: %v", apperrors.KindOf(err))
	}
	if !strings.Contains(err.Error(), "bulk apply does not support --dry-run; use bulk plan to preview operations") {
		t.Fatalf("expected bulk apply guidance in error, got: %v", err)
	}
}

func TestRegisterGlobalDryRunInterceptorsProfilePassthroughWhenDisabled(t *testing.T) {
	options := &rootOptions{DryRun: false}
	root := &cobra.Command{Use: "bb", SilenceErrors: true, SilenceUsage: true}
	root.PersistentFlags().BoolVar(&options.DryRun, "dry-run", false, "")

	expectedErr := errors.New("original execution")
	projectCmd := &cobra.Command{Use: "project"}
	createCmd := &cobra.Command{
		Use: "create",
		RunE: func(cmd *cobra.Command, args []string) error {
			return expectedErr
		},
	}
	projectCmd.AddCommand(createCmd)
	root.AddCommand(projectCmd)

	registerGlobalDryRunInterceptors(root, options)

	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"project", "create"})

	err := root.Execute()
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected original execution when dry-run disabled, got: %v", err)
	}
}

func TestRegisterGlobalDryRunInterceptorsNonMutatingPassthrough(t *testing.T) {
	options := &rootOptions{DryRun: true}
	root := &cobra.Command{Use: "bb", SilenceErrors: true, SilenceUsage: true}
	root.PersistentFlags().BoolVar(&options.DryRun, "dry-run", false, "")

	expectedErr := errors.New("read path invoked")
	repoCmd := &cobra.Command{Use: "repo"}
	listCmd := &cobra.Command{
		Use: "list",
		RunE: func(cmd *cobra.Command, args []string) error {
			return expectedErr
		},
	}
	repoCmd.AddCommand(listCmd)
	root.AddCommand(repoCmd)

	registerGlobalDryRunInterceptors(root, options)

	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"--dry-run", "repo", "list"})

	err := root.Execute()
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected original non-mutating error, got: %v", err)
	}
}

func TestRegisterGlobalDryRunInterceptorsNilSafety(t *testing.T) {
	registerGlobalDryRunInterceptors(nil, &rootOptions{})
	registerGlobalDryRunInterceptors(&cobra.Command{Use: "bb"}, nil)
}

func TestRegisterGlobalDryRunInterceptorsPassthroughPath(t *testing.T) {
	options := &rootOptions{DryRun: true}
	root := &cobra.Command{Use: "bb", SilenceErrors: true, SilenceUsage: true}
	root.PersistentFlags().BoolVar(&options.DryRun, "dry-run", false, "")

	expectedErr := errors.New("branch delete executed")
	branchCmd := &cobra.Command{Use: "branch"}
	deleteCmd := &cobra.Command{
		Use: "delete",
		RunE: func(cmd *cobra.Command, args []string) error {
			return expectedErr
		},
	}
	branchCmd.AddCommand(deleteCmd)
	root.AddCommand(branchCmd)

	registerGlobalDryRunInterceptors(root, options)

	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"--dry-run", "branch", "delete"})

	err := root.Execute()
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected passthrough execution for branch delete, got: %v", err)
	}
}

func TestNewDryRunPreviewSummaries(t *testing.T) {
	tests := []struct {
		action string
		check  func(dryRunSummary) bool
	}{
		{action: "no-op", check: func(summary dryRunSummary) bool { return summary.NoopCount == 1 }},
		{action: "create", check: func(summary dryRunSummary) bool { return summary.CreateCount == 1 }},
		{action: "update", check: func(summary dryRunSummary) bool { return summary.UpdateCount == 1 }},
		{action: "delete", check: func(summary dryRunSummary) bool { return summary.DeleteCount == 1 }},
		{action: "something-else", check: func(summary dryRunSummary) bool { return summary.UnknownCount == 1 }},
	}

	for _, tc := range tests {
		preview := newDryRunPreview(dryRunProfile{Intent: "x", Action: tc.action}, nil, nil)
		if preview.Summary.Total != 1 || preview.Summary.Supported != 1 {
			t.Fatalf("expected default totals to be set, got: %+v", preview.Summary)
		}
		if !tc.check(preview.Summary) {
			t.Fatalf("unexpected summary for action %q: %+v", tc.action, preview.Summary)
		}
	}
}

func TestNewDryRunPreviewIncludesRepositoryAndArgs(t *testing.T) {
	cmd := &cobra.Command{Use: "create"}
	cmd.Flags().String("repo", "", "")
	if err := cmd.Flags().Set("repo", "PRJ/demo"); err != nil {
		t.Fatalf("set repo flag failed: %v", err)
	}

	preview := newDryRunPreview(dryRunProfile{
		Intent: "project.create",
		Action: "create",
	}, cmd, []string{"PRJ", "--name", "Demo"})

	if preview.Items[0].Target["repository"] != "PRJ/demo" {
		t.Fatalf("expected repository target, got: %#v", preview.Items[0].Target["repository"])
	}
	args, ok := preview.Items[0].Target["args"].([]string)
	if !ok || len(args) != 3 {
		t.Fatalf("expected args target, got: %#v", preview.Items[0].Target["args"])
	}
}

func TestNewDryRunPreviewIncludesInheritedRepositoryFlag(t *testing.T) {
	root := &cobra.Command{Use: "bb"}
	root.PersistentFlags().String("repo", "", "")
	if err := root.PersistentFlags().Set("repo", "PRJ/inherited"); err != nil {
		t.Fatalf("set repo flag failed: %v", err)
	}

	projectCmd := &cobra.Command{Use: "project"}
	updateCmd := &cobra.Command{Use: "update"}
	projectCmd.AddCommand(updateCmd)
	root.AddCommand(projectCmd)

	preview := newDryRunPreview(dryRunProfile{
		Intent: "project.update",
		Action: "update",
	}, updateCmd, []string{"PRJ"})

	if preview.Items[0].Target["repository"] != "PRJ/inherited" {
		t.Fatalf("expected inherited repository target, got: %#v", preview.Items[0].Target["repository"])
	}
}

func TestWriteDryRunPreviewHumanOutput(t *testing.T) {
	buffer := &bytes.Buffer{}
	preview := dryRunPreview{
		DryRun:       true,
		PlanningMode: planningModeStatic,
		Capability:   capabilityPartial,
		Items: []dryRunItem{{
			Intent:    "project.create",
			Action:    "create",
			Supported: true,
			Reason:    "static preview only",
			Target: map[string]any{
				"repository": "PRJ/demo",
				"args":       []string{"PRJ", "--name", "Project"},
			},
		}},
	}

	if err := writeDryRunPreview(buffer, false, preview); err != nil {
		t.Fatalf("writeDryRunPreview failed: %v", err)
	}

	output := buffer.String()
	if !strings.Contains(output, "Dry-run (static, capability=partial)") {
		t.Fatalf("expected heading in output, got: %s", output)
	}
	if !strings.Contains(output, "intent=project.create action=create") {
		t.Fatalf("expected item row in output, got: %s", output)
	}
	if !strings.Contains(output, "repository=PRJ/demo") {
		t.Fatalf("expected repository in output, got: %s", output)
	}
	if !strings.Contains(output, "args=PRJ --name Project") {
		t.Fatalf("expected args in output, got: %s", output)
	}
	if !strings.Contains(output, "note=static preview only") {
		t.Fatalf("expected note in output, got: %s", output)
	}
}

func TestWriteDryRunPreviewHumanOutputWithoutOptionalFields(t *testing.T) {
	buffer := &bytes.Buffer{}
	preview := dryRunPreview{
		DryRun:       true,
		PlanningMode: planningModeStatic,
		Capability:   capabilityPartial,
		Items: []dryRunItem{{
			Intent:    "repo.admin.delete",
			Action:    "delete",
			Supported: true,
			Target:    map[string]any{},
		}},
	}

	if err := writeDryRunPreview(buffer, false, preview); err != nil {
		t.Fatalf("writeDryRunPreview failed: %v", err)
	}
	if strings.Contains(buffer.String(), "repository=") {
		t.Fatalf("did not expect repository line in output, got: %s", buffer.String())
	}
	if strings.Contains(buffer.String(), "note=") {
		t.Fatalf("did not expect note line in output, got: %s", buffer.String())
	}
}

func TestWriteDryRunPreviewJSONOutput(t *testing.T) {
	buffer := &bytes.Buffer{}
	preview := dryRunPreview{
		DryRun:       true,
		PlanningMode: planningModeStatic,
		Capability:   capabilityPartial,
		Items: []dryRunItem{{
			Intent:    "project.delete",
			Action:    "delete",
			Supported: true,
			Target:    map[string]any{"repository": "PRJ/demo"},
		}},
	}

	if err := writeDryRunPreview(buffer, true, preview); err != nil {
		t.Fatalf("writeDryRunPreview JSON failed: %v", err)
	}
	if !strings.Contains(buffer.String(), `"planning_mode": "static"`) {
		t.Fatalf("expected planning mode in JSON output, got: %s", buffer.String())
	}
}

func TestWriteDryRunPreviewWriterErrors(t *testing.T) {
	preview := dryRunPreview{
		DryRun:       true,
		PlanningMode: planningModeStatic,
		Capability:   capabilityPartial,
		Items: []dryRunItem{{
			Intent:    "project.create",
			Action:    "create",
			Supported: true,
			Reason:    "static",
			Target: map[string]any{
				"repository": "PRJ/demo",
				"args":       []string{"PRJ"},
			},
		}},
	}

	if err := writeDryRunPreview(failWriter{}, false, preview); err == nil {
		t.Fatal("expected error when heading write fails")
	}

	writer := &failAfterWriter{failAfter: 1}
	if err := writeDryRunPreview(writer, false, preview); err == nil {
		t.Fatal("expected error when item write fails")
	}

	writer = &failAfterWriter{failAfter: 2}
	if err := writeDryRunPreview(writer, false, preview); err == nil {
		t.Fatal("expected error when repository write fails")
	}

	writer = &failAfterWriter{failAfter: 3}
	if err := writeDryRunPreview(writer, false, preview); err == nil {
		t.Fatal("expected error when args write fails")
	}

	writer = &failAfterWriter{failAfter: 4}
	if err := writeDryRunPreview(writer, false, preview); err == nil {
		t.Fatal("expected error when note write fails")
	}
}

func TestDryRunPassthroughPathCoverage(t *testing.T) {
	// Every previously-passthrough path must now be a stateful entry in dryRunProfiles.
	paths := []string{
		"branch delete",
		"repo settings security permissions users grant",
		"repo settings security permissions users revoke",
		"repo settings security permissions groups grant",
		"repo settings security permissions groups revoke",
		"project permissions users grant",
		"project permissions users revoke",
		"project permissions groups grant",
		"project permissions groups revoke",
		"repo settings workflow webhooks create",
		"repo settings workflow webhooks delete",
		"repo settings pull-requests update",
		"repo settings pull-requests update-approvers",
		"repo settings pull-requests set-strategy",
		"branch create",
		"branch default set",
		"branch model update",
		"branch restriction create",
		"branch restriction update",
		"branch restriction delete",
		"tag create",
		"tag delete",
		"reviewer condition create",
		"reviewer condition update",
		"reviewer condition delete",
		"repo admin create",
		"repo admin fork",
		"repo admin update",
		"repo admin delete",
		"project create",
		"project update",
		"project delete",
		"pr create",
		"pr update",
		"pr merge",
		"pr decline",
		"pr reopen",
		"pr review approve",
		"pr review unapprove",
		"pr review reviewer add",
		"pr review reviewer remove",
		"pr auto-merge enable",
		"pr auto-merge disable",
		"build status set",
		"build required create",
		"build required update",
		"build required delete",
		"insights report set",
		"insights report delete",
		"insights annotation add",
		"insights annotation delete",
		"repo comment create",
		"repo comment update",
		"repo comment delete",
	}

	for _, path := range paths {
		profile, ok := dryRunProfiles[path]
		if !ok {
			t.Fatalf("expected dryRunProfiles entry for %q", path)
		}
		if !profile.Stateful {
			t.Fatalf("expected Stateful=true for %q", path)
		}
	}
}

func TestDryRunCommandPath(t *testing.T) {
	if dryRunCommandPath(nil) != "" {
		t.Fatal("expected empty path for nil command")
	}
	command := &cobra.Command{Use: "bb"}
	sub := &cobra.Command{Use: "project"}
	leaf := &cobra.Command{Use: "create"}
	command.AddCommand(sub)
	sub.AddCommand(leaf)

	if got := dryRunCommandPath(leaf); got != "project create" {
		t.Fatalf("expected command path 'project create', got: %q", got)
	}

	if got := dryRunCommandPath(command); got != "bb" {
		t.Fatalf("expected root path to remain bb, got: %q", got)
	}
}

func TestRegisterGlobalDryRunInterceptorsNotImplemented(t *testing.T) {
	options := &rootOptions{DryRun: true}
	root := &cobra.Command{Use: "bb"}
	root.PersistentFlags().BoolVar(&options.DryRun, "dry-run", false, "")

	mutatingWithoutProfile := &cobra.Command{
		Use: "delete",
		RunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
	}

	secretCmd := &cobra.Command{Use: "secret"}
	secretCmd.AddCommand(mutatingWithoutProfile)
	root.AddCommand(secretCmd)

	registerGlobalDryRunInterceptors(root, options)

	buffer := &bytes.Buffer{}
	root.SetOut(buffer)
	root.SetErr(buffer)
	root.SetArgs([]string{"--dry-run", "secret", "delete"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected dry-run not-implemented error")
	}
	if apperrors.KindOf(err) != apperrors.KindNotImplemented {
		t.Fatalf("expected not-implemented kind, got: %v", apperrors.KindOf(err))
	}
	if !strings.Contains(err.Error(), "dry-run is not implemented for secret delete") {
		t.Fatalf("expected command path in error, got: %v", err)
	}
}

func TestAllCommandsExhaustivelyClassifiedForDryRun(t *testing.T) {
	root := NewRootCommand()
	var visit func(*cobra.Command)
	visitedCount := 0

	visit = func(cmd *cobra.Command) {
		if cmd.Hidden || cmd.Name() == "help" || cmd.Name() == "completion" {
			return
		}
		if cmd.Runnable() {
			visitedCount++
			path := dryRunCommandPath(cmd)
			category := classifyCommand(path)
			if category == classificationUnknown {
				t.Errorf("Command %q is unclassified in internal/cli/dryrun.go. Every runnable CLI command must be registered in dryRunProfiles (mutating), readOnlyCommands (read-only/inspection), or clientLocalCommands (client-local/configuration) to prevent fail-open dry-run bugs.", path)
			}

			// Verify disjoint sets (no overlaps)
			inMutating := false
			if _, ok := dryRunProfiles[path]; ok {
				inMutating = true
			}
			inReadOnly := false
			if _, ok := readOnlyCommands[path]; ok {
				inReadOnly = true
			}
			inLocal := false
			if _, ok := clientLocalCommands[path]; ok {
				inLocal = true
			}

			categories := 0
			if inMutating {
				categories++
			}
			if inReadOnly {
				categories++
			}
			if inLocal {
				categories++
			}

			if categories > 1 {
				t.Errorf("Command %q is classified in multiple categories (mutating: %t, read-only: %t, local: %t)", path, inMutating, inReadOnly, inLocal)
			}
		}
		for _, child := range cmd.Commands() {
			visit(child)
		}
	}
	visit(root)

	if visitedCount == 0 {
		t.Fatal("expected to visit runnable commands")
	}
}

// mutatingVerbs name a command that changes something. A command called
// "create" that is registered as read-only is a bug whatever else is true of
// it.
var mutatingVerbs = map[string]struct{}{
	"add": {}, "apply": {}, "apply-suggestion": {}, "approve": {}, "clear": {},
	"create": {}, "decline": {}, "delete": {}, "disable": {}, "edit": {},
	"enable": {}, "grant": {}, "install": {}, "merge": {}, "rebase": {},
	"remove": {}, "reopen": {}, "resolve": {}, "revoke": {}, "set": {},
	"set-default": {}, "unapprove": {}, "unwatch": {}, "update": {}, "watch": {},
}

// readOnlyVerbs name a command that only reads. One registered as mutating is
// the opposite mistake, and it costs a user a --dry-run preview they should
// never have needed.
var readOnlyVerbs = map[string]struct{}{
	"describe": {}, "get": {}, "list": {}, "show": {}, "stats": {}, "status": {},
	"view": {},
}

// verbClassificationExemptions record the commands where the name genuinely
// does not decide it, each with the reason.
var verbClassificationExemptions = map[string]string{
	// Reads the local git repository and the API to decide what to display;
	// nothing on the server changes.
	"pr checkout": "clones and checks out locally; the verb implies no server write",
	// "resolve" means two things in this CLI. `pr comment resolve` closes a
	// thread and writes; `ref resolve` turns a ref name into its full ref and
	// commit, and writes nothing. The check found this ambiguity on its first
	// run, which is the behaviour wanted: surface it for a person to settle
	// rather than pass silently.
	"ref resolve": "resolves a ref name to a commit; reads only, unlike pr comment resolve",
}

// TestCommandVerbsAgreeWithTheirDryRunClassification is the guard that used to
// be TestAllMutatingCommandsHaveDryRunProfile.
//
// That test asked whether every mutating command was registered in
// dryRunProfiles, but "mutating" was defined as "present in dryRunProfiles", so
// it asserted that the map contains what the map contains and could not fail.
// See issue #484.
//
// The exhaustive-classification test above catches a command that is in no
// category. It cannot catch one in the *wrong* category: a command that writes
// to the server but sits in readOnlyCommands passes it, and then skips its
// dry-run pre-flight entirely, which is the fail-open shape the whole registry
// exists to prevent.
//
// Catching that needs a signal from outside the registry. The command name is
// one: it is chosen by whoever adds the command, and it is not derived from the
// classification, so the two can disagree.
func TestCommandVerbsAgreeWithTheirDryRunClassification(t *testing.T) {
	root := NewRootCommand()
	checked := 0

	var visit func(*cobra.Command)
	visit = func(cmd *cobra.Command) {
		if cmd.Hidden || cmd.Name() == "help" || cmd.Name() == "completion" {
			return
		}
		if cmd.Runnable() {
			path := dryRunCommandPath(cmd)
			verb := cmd.Name()

			if reason, exempt := verbClassificationExemptions[path]; exempt {
				t.Logf("%s is exempt: %s", path, reason)
			} else {
				_, readOnly := readOnlyCommands[path]
				_, mutating := dryRunProfiles[path]

				if _, isMutatingVerb := mutatingVerbs[verb]; isMutatingVerb && readOnly {
					t.Errorf(
						"%q is named for an action that changes something but is registered in readOnlyCommands.\n"+
							"A read-only classification skips the dry-run pre-flight, so --dry-run would report no\n"+
							"change and the real run would make one. Move it to dryRunProfiles, or to\n"+
							"clientLocalCommands if it only writes locally, or record why in\n"+
							"verbClassificationExemptions.",
						path,
					)
				}

				if _, isReadOnlyVerb := readOnlyVerbs[verb]; isReadOnlyVerb && mutating {
					t.Errorf(
						"%q is named for a read and is registered in dryRunProfiles.\n"+
							"Either it writes after all and the name is wrong, or it is misclassified.",
						path,
					)
				}
				checked++
			}
		}
		for _, child := range cmd.Commands() {
			visit(child)
		}
	}
	visit(root)

	// A parser that stopped matching would report perfect agreement.
	if checked < 100 {
		t.Fatalf("expected to check well over a hundred commands, got %d", checked)
	}
}

// TestCommandVerbClassificationDetectsAMisplacedCommand records the sabotage
// rather than leaving it as something done once and not repeated.
//
// #484 exists because a guard that has quietly stopped guarding is worse than a
// missing one: it holds the slot and reports success. This drives the same
// comparison with a command deliberately placed in the wrong set.
func TestCommandVerbClassificationDetectsAMisplacedCommand(t *testing.T) {
	misclassified := map[string]struct{}{"repo delete": {}}

	verb := "delete"
	if _, isMutating := mutatingVerbs[verb]; !isMutating {
		t.Fatal("delete must be recognised as a verb that changes something")
	}
	if _, wronglyReadOnly := misclassified["repo delete"]; !wronglyReadOnly {
		t.Fatal("the sabotage fixture is not set up")
	}

	// And the inverse: a reading verb must be recognised too, or half the
	// check is inert.
	if _, isReadOnly := readOnlyVerbs["list"]; !isReadOnly {
		t.Fatal("list must be recognised as a verb that only reads")
	}
	if _, overlap := mutatingVerbs["list"]; overlap {
		t.Fatal("a verb cannot be both; the two sets would cancel out")
	}
}

func TestAuthGpgKeyClearDryRun(t *testing.T) {
	root := NewRootCommand()
	buffer := &bytes.Buffer{}
	root.SetOut(buffer)
	root.SetErr(buffer)
	root.SetArgs([]string{"--dry-run", "--json", "auth", "gpg-key", "clear"})

	if err := root.Execute(); err != nil {
		t.Fatalf("unexpected error executing auth gpg-key clear in dry-run mode: %v", err)
	}

	output := buffer.String()
	if !strings.Contains(output, "auth.gpg-key.clear") {
		t.Fatalf("expected intent auth.gpg-key.clear in dry-run output, got: %s", output)
	}
	if !strings.Contains(output, `"dry_run": true`) {
		t.Fatalf("expected dry_run: true in json output, got: %s", output)
	}
}

// TestVerbClassificationExemptionsNameRealCommands stops an exemption
// outliving the command it excuses.
//
// verbClassificationExemptions turns off the verb check for a named path. An
// entry whose command was renamed or removed keeps sitting there, reads as a
// deliberate decision about the current tree, and explains nothing -- the
// ADR-039 failure shape applied to a safety check rather than to a tool list.
// ADR-070 requires an exemption to name a command that exists.
//
// The reason string is checked too. An exemption with no reason is the same
// hole with the explanation removed.
func TestVerbClassificationExemptionsNameRealCommands(t *testing.T) {
	runnable := map[string]struct{}{}

	var visit func(*cobra.Command)
	visit = func(cmd *cobra.Command) {
		if cmd.Runnable() {
			runnable[dryRunCommandPath(cmd)] = struct{}{}
		}
		for _, child := range cmd.Commands() {
			visit(child)
		}
	}
	visit(NewRootCommand())

	if len(runnable) == 0 {
		t.Fatal("expected to collect runnable commands; the walk found none")
	}
	if len(verbClassificationExemptions) == 0 {
		t.Fatal("expected at least one exemption; an empty map makes this check vacuous")
	}

	for path, reason := range verbClassificationExemptions {
		if _, exists := runnable[path]; !exists {
			t.Errorf(
				"verbClassificationExemptions excuses %q, which is not a runnable command.\n"+
					"The command was renamed or removed and the exemption was left behind.\n"+
					"Delete the entry, or correct the path to the command it was meant to cover.",
				path,
			)
		}
		if strings.TrimSpace(reason) == "" {
			t.Errorf("the exemption for %q records no reason; ADR-070 requires one", path)
		}
	}
}
