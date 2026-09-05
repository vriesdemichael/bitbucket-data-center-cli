package repocmd

import (
	"encoding/json"
	"testing"
)

// TestRequiredApproversIn covers reading the current required-approver count out
// of a repository's pull-request settings.
//
// The shape matters more than the parsing. `update-approvers --dry-run` used to
// look for {"enabled": bool, "count": string}; Bitbucket sends a plain number at
// the top level, so the read failed on every real response and the preview could
// never say a change was a no-op. The payloads here were taken from a running
// instance, and TestLiveGovernanceDryRunPredictionsReadRealState drives the
// preview against one.
func TestRequiredApproversIn(t *testing.T) {
	cases := []struct {
		name     string
		settings string
		want     int
	}{
		{
			// What a running Bitbucket answers.
			name:     "a plain number at the top level",
			settings: `{"requiredApprovers":2,"requiredAllTasksComplete":true,"requiredSuccessfulBuilds":0}`,
			want:     2,
		},
		{
			name:     "no requirement set",
			settings: `{"requiredAllTasksComplete":true}`,
			want:     -1,
		},
		{
			name:     "zero approvers is a requirement of none, not an absent one",
			settings: `{"requiredApprovers":0}`,
			want:     0,
		},
		{
			// The bundled hook's own settings, which is where the nested shape
			// came from -- spelled "enable", and under a namespaced key.
			name:     "the hook's nested object",
			settings: `{"requiredApprovers":{"enable":true,"count":3}}`,
			want:     3,
		},
		{
			name:     "a disabled nested object requires none",
			settings: `{"requiredApprovers":{"enable":false,"count":3}}`,
			want:     0,
		},
		{
			name:     "a count sent as a string",
			settings: `{"requiredApprovers":"4"}`,
			want:     4,
		},
		{
			name:     "something else entirely",
			settings: `{"requiredApprovers":[1,2]}`,
			want:     -1,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var settings map[string]any
			if err := json.Unmarshal([]byte(testCase.settings), &settings); err != nil {
				t.Fatalf("decode settings: %v", err)
			}

			if got := requiredApproversIn(settings); got != testCase.want {
				t.Errorf("requiredApproversIn(%s) = %d, want %d", testCase.settings, got, testCase.want)
			}
		})
	}
}
