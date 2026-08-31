package reviewercmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
)

// conditionDeps builds a command against a stub that accepts any write.
func conditionDeps(t *testing.T) Dependencies {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":1,"requiredApprovals":1}`))
	}))
	t.Cleanup(server.Close)

	cfg := config.AppConfig{BitbucketURL: server.URL, ProjectKey: "PRJ"}
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
