package doctorcmd

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/completionsetup"
)

// testVersion is the bb these tests run as.
const testVersion = "9.9.9"

// fakeMachine is a machine described rather than probed: which shells are
// installed, what each PowerShell answers when asked where its profile is, and
// the directories everything else is in. Every directory is a test's own.
//
// It is Windows, which gives bash, zsh and fish no place for every user. On
// Linux or macOS those places are fixed directories under /usr/local, and what
// is there on the machine running the test is not the test's.
type fakeMachine struct {
	home      string
	work      string
	env       map[string]string
	installed map[string]string
	// profiles are what each PowerShell executable answers: its profile and
	// execution policy for the user, and for every user.
	profiles map[string]profileAnswers
	failures map[string]error
	packaged map[completionsetup.Shell][]string
}

type profileAnswers struct {
	user     string
	allUsers string
}

func (fake *fakeMachine) machine() Machine {
	return Machine{
		System: completionsetup.System{
			GOOS:    "windows",
			Getenv:  func(name string) string { return fake.env[name] },
			HomeDir: func() (string, error) { return fake.home, nil },
			LookPath: func(name string) (string, error) {
				if executable, ok := fake.installed[name]; ok {
					return executable, nil
				}

				return "", exec.ErrNotFound
			},
			Run: func(_ context.Context, executable string, args ...string) (string, error) {
				if err := fake.failures[executable]; err != nil {
					return "", err
				}

				answers := fake.profiles[executable]
				if strings.Contains(args[len(args)-1], "AllUsersAllHosts") {
					return answers.allUsers, nil
				}

				return answers.user, nil
			},
		},
		WorkingDirectory: func() (string, error) { return fake.work, nil },
		PackagedScripts:  func(shell completionsetup.Shell) []string { return fake.packaged[shell] },
	}
}

// emptyMachine has no shell installed and nothing in its directories.
func emptyMachine(t *testing.T) Machine {
	t.Helper()

	return (&fakeMachine{home: t.TempDir(), work: t.TempDir()}).machine()
}

// generatedScript stands for what bb completion <shell> prints. It opens the
// way bb's scripts do, which is how a saved one is recognised.
func generatedScript(shell completionsetup.Shell, withDescriptions bool) (string, error) {
	heading := map[completionsetup.Shell]string{
		completionsetup.Bash: "# bash completion V2 for bb",
		completionsetup.Zsh:  "#compdef bb\ncompdef _bb bb\n\n# zsh completion for bb",
		completionsetup.Fish: "# fish completion for bb",
	}[shell]

	variant := "with descriptions"
	if !withDescriptions {
		variant = "without descriptions"
	}

	return heading + "                          -*- shell-script -*-\n# the script this bb prints, " + variant + "\n", nil
}

func put(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// install writes what bb completion install would to target.
func install(t *testing.T, target completionsetup.Target) {
	t.Helper()

	if outcome, err := completionsetup.Install(target); err != nil || outcome.Status != completionsetup.Installed {
		t.Fatalf("setting up %s: %+v, %v", target.Path, outcome, err)
	}
}

// decodeReport reads the report out of a --json run, and checks it against
// the schema bb doctor declares: every value a field takes must be one the
// schema allows.
func decodeReport(t *testing.T, output string) Report {
	t.Helper()

	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		t.Fatalf("not one JSON document: %v\n%s", err, output)
	}

	declared, ok := result.SchemaFor("doctor")
	if !ok {
		t.Fatal("bb doctor declares no result")
	}
	encoded, err := json.Marshal(declared)
	if err != nil {
		t.Fatal(err)
	}
	schemaDocument, err := jsonschema.UnmarshalJSON(strings.NewReader(string(encoded)))
	if err != nil {
		t.Fatal(err)
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("doctor.schema.json", schemaDocument); err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile("doctor.schema.json")
	if err != nil {
		t.Fatal(err)
	}

	payload, err := jsonschema.UnmarshalJSON(strings.NewReader(string(envelope.Data)))
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.Validate(payload); err != nil {
		t.Errorf("the report does not match the schema bb doctor declares: %v", err)
	}

	var report Report
	if err := json.Unmarshal(envelope.Data, &report); err != nil {
		t.Fatal(err)
	}

	return report
}
