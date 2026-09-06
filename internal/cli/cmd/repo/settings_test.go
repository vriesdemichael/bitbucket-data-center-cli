package repocmd

import (
	"slices"
	"testing"
)

func TestWebhookHelperFunctions(t *testing.T) {
	t.Parallel()

	payload := map[string]any{"values": []any{map[string]any{"id": float64(42), "name": "ci", "url": "http://example.invalid/hook"}}}

	entries := webhookEntries(payload)
	if len(entries) != 1 {
		t.Fatalf("expected one webhook entry, got %d", len(entries))
	}
	if !webhookExistsByNameAndURL(payload, "CI", "http://example.invalid/hook") {
		t.Fatal("expected webhook to match by name+url case-insensitively")
	}
	if !webhookExistsByID(payload, "42") {
		t.Fatal("expected webhook to match by numeric id")
	}
	if webhookExistsByID(payload, "999") {
		t.Fatal("did not expect webhook id 999 to exist")
	}
}

// TestEnabledStrategiesWithHandlesTheShapesBitbucketSends covers the cases the
// end-to-end test above cannot reach, including the fresh-repository case the
// helper's own comment claims.
func TestEnabledStrategiesWithHandlesTheShapesBitbucketSends(t *testing.T) {
	t.Parallel()

	strategies := func(entries ...any) map[string]any {
		return map[string]any{"mergeConfig": map[string]any{"strategies": entries}}
	}
	entry := func(id string, enabled bool) any {
		return map[string]any{"id": id, "enabled": enabled}
	}

	testCases := []struct {
		name     string
		settings map[string]any
		want     []string
	}{
		{
			name:     "requested is already enabled, and is not repeated",
			settings: strategies(entry("no-ff", true), entry("squash", true)),
			want:     []string{"no-ff", "squash"},
		},
		{
			name:     "disabled ones are not switched on as a side effect",
			settings: strategies(entry("no-ff", true), entry("ff", false)),
			want:     []string{"no-ff", "squash"},
		},
		{
			// A repository with no strategies enabled still gets a usable
			// request, which is what lets set-strategy work on a fresh one.
			name:     "nothing enabled",
			settings: strategies(entry("no-ff", false)),
			want:     []string{"squash"},
		},
		{
			name:     "no strategies key at all",
			settings: map[string]any{"mergeConfig": map[string]any{}},
			want:     []string{"squash"},
		},
		{
			name:     "no mergeConfig at all",
			settings: map[string]any{},
			want:     []string{"squash"},
		},
		{
			name:     "an id repeated by the server is sent once",
			settings: strategies(entry("no-ff", true), entry("no-ff", true)),
			want:     []string{"no-ff", "squash"},
		},
		{
			// Every response observed carries "enabled"; absent is treated as
			// off rather than on, so an unrecognised shape cannot silently
			// enable strategies the caller never asked for.
			name:     "enabled field missing",
			settings: strategies(map[string]any{"id": "no-ff"}),
			want:     []string{"squash"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var got []string
			for _, strategy := range enabledStrategiesWith(testCase.settings, "squash") {
				got = append(got, strategy["id"].(string))
			}
			if !slices.Equal(got, testCase.want) {
				t.Errorf("got %v, want %v", got, testCase.want)
			}
		})
	}
}

// TestSetStrategySendsTheEnabledStrategiesWithTheDefault is live now, in
// TestLiveGovernanceCLI. Bitbucket refuses a default that is not among the
// enabled strategies, which is why the command has to send the set along with
// it; the unit version decoded the request body it had just been handed, so
// what it checked was that we send what we send. The live one sets the
// strategy and reads the settings back: the default took, no-ff stayed on,
// and ff-only did not come on by itself. Sabotage-checked by sending the
// default alone, which the live test catches.
//
// TestEnabledStrategiesWithHandlesTheShapesBitbucketSends stays. It calls the
// helper directly with no server at all, and covers the shapes the live test
// cannot arrange -- a repository with no mergeConfig, a strategies list that
// is not a list.

// TestRepoSettingsCommands and TestRepoSettingsJSONAndDryRunModes are gone
// rather than moved.
//
// Between them they drove every `repo settings` command -- webhooks,
// pull-requests, auto-merge, auto-decline -- against one handwritten
// Bitbucket, in human mode, JSON mode and dry run, and asserted the rendering
// of what that fixture answered. Command reach is 234/234, so each of those
// commands is asserted against a real instance: TestLiveRepoCLICoverage in
// both output modes, TestLiveGovernanceCLI for the pull-request settings, and
// the dry-run suites for the previews, which read the state they predict from
// rather than from a page written beside them.
