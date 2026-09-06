package reviewercmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// conditionDeps builds a command against a URL that is not a server.
//
// Nothing here needs a reply. A case that must be refused is refused before a
// request exists, and a case that must get past that refusal only has to fail
// some other way.
func conditionDeps(t *testing.T) Dependencies {
	t.Helper()

	cfg := config.AppConfig{BitbucketURL: testsupport.ClosedListenerURL(t), ProjectKey: "PRJ"}
	return Dependencies{
		JSONEnabled:   func() bool { return false },
		DryRunEnabled: func() bool { return false },
		LoadConfig:    func() (config.AppConfig, error) { return cfg, nil },
		LoadConfigAndClient: func() (config.AppConfig, *openapigenerated.ClientWithResponses, error) {
			client, err := openapi.NewClientWithResponsesFromConfig(cfg)
			return cfg, client, err
		},
	}
}

// TestConditionRequiresStdinToBeAskedFor covers the fallback that used to
// block.
//
// Args is MaximumNArgs(1), so zero arguments was legal and fell through to an
// unconditional read of stdin. On a CI runner that blocked forever. The help
// already documented "or stdin (-)"; the code now requires it.
func TestConditionRequiresStdinToBeAskedFor(t *testing.T) {
	t.Parallel()

	deps := conditionDeps(t)

	cases := []struct {
		name  string
		args  []string
		typed string
		asked bool
	}{
		{
			name: "create with nothing given",
			args: []string{"condition", "create", "--repo", "PRJ/repo1"},
		},
		{
			name:  "create with a dash",
			args:  []string{"condition", "create", "-", "--repo", "PRJ/repo1"},
			typed: `{"requiredApprovals":1}`,
			asked: true,
		},
		{
			name: "update with nothing given",
			args: []string{"condition", "update", "1", "--repo", "PRJ/repo1"},
		},
		{
			name:  "update with a dash",
			args:  []string{"condition", "update", "1", "-", "--repo", "PRJ/repo1"},
			typed: `{"requiredApprovals":2}`,
			asked: true,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			command := New(deps)
			buf := new(bytes.Buffer)
			command.SetOut(buf)
			command.SetErr(buf)
			command.SetIn(strings.NewReader(testCase.typed))
			command.SetArgs(testCase.args)

			err := command.Execute()

			if testCase.asked {
				if err != nil && strings.Contains(err.Error(), "no condition given") {
					t.Errorf("a - argument was treated as no input: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("stdin was read without the caller asking for it")
			}
			if !strings.Contains(err.Error(), "no condition given") {
				t.Errorf("error = %q, want it to say no condition was given", err.Error())
			}
		})
	}
}
