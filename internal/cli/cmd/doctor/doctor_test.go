package doctorcmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/completionsetup"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// leakedSecret stands for a secret value that reached a setting. The config
// layer never puts one there; the command must not print one if it did.
const leakedSecret = "leaked-secret-7c21"

const (
	storedPath = "/home/alice/.config/bb/config.yaml"
	systemPath = "/etc/bb/config.yaml"
)

func brokenDiagnosis() config.Diagnosis {
	return config.Diagnosis{
		Files: []config.DiagnosedFile{
			{
				Tier: config.TierStored, Path: storedPath, PathFrom: "BB_CONFIG_PATH", Read: true, Exists: true, Parses: true,
				Violations: []config.SchemaViolation{
					{Key: "policies.require_keyrng", Path: []string{"policies", "require_keyrng"}, Line: 3, Problem: "unknown key"},
					{Key: "", Problem: "got array, want object"},
				},
				Ignored: []config.IgnoredKey{{Key: "require_keyring", Line: 5, ReadFrom: []string{config.TierSystem}}},
				Secrets: []config.SecretPresence{{Host: "https://bitbucket.example.com", Token: true, Password: true}},
			},
			{Tier: config.TierWorkspace, PathFrom: "search", Read: true},
			{Tier: config.TierSystem, Path: systemPath, PathFrom: "machine", Read: true, Exists: true, Problem: "invalid YAML configuration (yaml: line 2)"},
		},
		Settings: []config.DiagnosedSetting{
			{
				Name: "host", Value: "https://bitbucket.example.com", Configured: true,
				Source:   config.SettingSource{Kind: config.SourceEnvironment, Name: "BITBUCKET_URL"},
				Shadowed: []config.SettingSource{{Kind: config.TierStored, Name: "default_host", Path: storedPath}},
			},
			{Name: "token", Value: leakedSecret, Secret: true, Configured: true, Source: config.SettingSource{Kind: config.SourceKeyring, Name: "https://bitbucket.example.com"}},
			{Name: "password", Secret: true, Source: config.SettingSource{Kind: config.SourceDefault}},
			{Name: "retry_count", Value: "-3", Configured: true, Source: config.SettingSource{Kind: config.SourceFlag, Name: "--retry-count"}, Problem: "must be greater than or equal to 0"},
			{Name: "request_timeout", Value: "20s", Source: config.SettingSource{Kind: config.SourceDefault}},
			{Name: "log_level", Value: "debug", Configured: true, Source: config.SettingSource{Kind: config.SourceDotenv, Name: "BB_LOG_LEVEL", Path: "/repo/.env"}},
			{Name: "project_key", Value: "PRJ", Configured: true, Source: config.SettingSource{Kind: config.SourceOverride, Name: "project_key"}},
			{Name: "update_base_url", Value: "https://mirror.example.com", Configured: true, Source: config.SettingSource{Kind: config.SourceRegistry, Name: "UpdateBaseURL", Path: `HKEY_LOCAL_MACHINE\Software\Policies\bb`}},
			{Name: "ca_file", Configured: true, Source: config.SettingSource{Kind: config.SourceFlag, Name: "--ca-file"}},
		},
		Keyring: config.KeyringDiagnosis{
			Required: true, Checked: true, Problem: "the OS keyring could not be reached: no bus",
			RequiredBy: config.SettingSource{Kind: config.TierSystem, Name: "require_keyring", Path: systemPath},
		},
	}
}

// brokenDetails is error.details for brokenDiagnosis: one entry for each of the
// six issues its report shows.
var brokenDetails = map[string]string{
	"violation/stored/policies/require_keyrng": storedPath + ":3: policies.require_keyrng: unknown key",
	"violation/stored":                         storedPath + ": (the file): got array, want object",
	"ignored/stored/require_keyring":           storedPath + ":5: require_keyring is read only from the system configuration",
	"file/system":                              systemPath + ": invalid YAML configuration (yaml: line 2)",
	"setting/retry_count":                      "retry_count from the --retry-count flag: must be greater than or equal to 0",
	"keyring":                                  "keyring required by require_keyring in the system configuration " + systemPath + ": the OS keyring could not be reached: no bus",
}

const brokenSummary = "6 issues to fix: 3 in " + storedPath + ", 1 in " + systemPath + ", retry_count, the keyring"

func healthyDiagnosis() config.Diagnosis {
	return config.Diagnosis{
		Files: []config.DiagnosedFile{
			{Tier: config.TierStored, PathFrom: "default", NotRead: "its location could not be worked out: no home"},
			{Tier: config.TierWorkspace, Path: "/repo/.bb/config.yaml", PathFrom: "search", Read: true, Exists: true, Parses: true, MatchesSchema: true},
			{Tier: config.TierSystem, Path: systemPath, PathFrom: "machine", Read: true},
		},
		Settings: []config.DiagnosedSetting{
			{Name: "host", Source: config.SettingSource{Kind: config.SourceDefault}},
			{Name: "token", Value: leakedSecret, Secret: true, Configured: true, Source: config.SettingSource{Kind: config.SourceEnvironment, Name: "BITBUCKET_TOKEN"}},
		},
		Keyring: config.KeyringDiagnosis{
			Required: true, Checked: true, Reachable: true,
			RequiredBy: config.SettingSource{Kind: config.SourceEnvironment, Name: "BB_REQUIRE_KEYRING"},
		},
	}
}

// runDoctor runs bb doctor over diagnosis on a machine with no shell installed
// and nothing in its directories, so only the configuration has anything to
// say.
func runDoctor(t *testing.T, diagnosis config.Diagnosis, asJSON bool, args ...string) (string, config.DiagnoseInput, error) {
	t.Helper()

	return runDoctorOn(t, emptyMachine(t), diagnosis, asJSON, args...)
}

func runDoctorOn(t *testing.T, machine Machine, diagnosis config.Diagnosis, asJSON bool, args ...string) (string, config.DiagnoseInput, error) {
	t.Helper()

	var received config.DiagnoseInput
	root := &cobra.Command{Use: "bb", SilenceErrors: true, SilenceUsage: true}
	root.PersistentFlags().String("log-level", "", "")
	root.PersistentFlags().String("log-format", "", "")
	root.AddCommand(New(Dependencies{
		JSONEnabled:      func() bool { return asJSON },
		RuntimeOverrides: func() config.Overrides { return config.Overrides{Host: "https://override.example.com"} },
		Diagnose: func(input config.DiagnoseInput) config.Diagnosis {
			received = input
			return diagnosis
		},
		Version:          func() string { return testVersion },
		CompletionScript: generatedScript,
		Machine:          machine,
	}))

	output := &bytes.Buffer{}
	root.SetOut(output)
	root.SetErr(output)
	root.SetArgs(append([]string{"doctor"}, args...))

	err := root.Execute()

	return output.String(), received, err
}

func TestDoctorFailsNamingEveryIssueWhenThereIsSomethingToFix(t *testing.T) {
	t.Parallel()

	output, _, err := runDoctor(t, brokenDiagnosis(), false)

	if apperrors.KindOf(err) != apperrors.KindPermanent || apperrors.ExitCode(err) != 1 {
		t.Fatalf("issues must exit 1 as permanent, got %v (exit %d)", err, apperrors.ExitCode(err))
	}
	if apperrors.MessageOf(err) != brokenSummary {
		t.Errorf("message = %q, want %q", apperrors.MessageOf(err), brokenSummary)
	}
	if details := apperrors.DetailsOf(err); !reflect.DeepEqual(details, brokenDetails) {
		t.Errorf("details:\n got %#v\nwant %#v", details, brokenDetails)
	}

	for _, line := range []string{
		"  stored     " + storedPath + " (BB_CONFIG_PATH)",
		"invalid: the schema rejects 2 keys",
		"line 3: policies.require_keyrng: unknown key",
		"(the file): got array, want object",
		"ignored: require_keyring is read only from the system configuration",
		"plaintext: a token and a password for https://bitbucket.example.com",
		"  workspace  none found above the working directory",
		"invalid: invalid YAML configuration (yaml: line 2)",
		"https://bitbucket.example.com, from BITBUCKET_URL in the environment",
		"overrides default_host in the stored configuration " + storedPath,
		"configured, from the OS keyring",
		"password         not set",
		"-3, from the --retry-count flag",
		"problem: must be greater than or equal to 0",
		"20s (default)",
		"debug, from BB_LOG_LEVEL in /repo/.env",
		"PRJ, from the program running bb",
		`https://mirror.example.com, from UpdateBaseURL in HKEY_LOCAL_MACHINE\Software\Policies\bb`,
		"empty, from the --ca-file flag",
		"required by require_keyring in the system configuration " + systemPath + "; the OS keyring could not be reached: no bus",
		"6 issues to fix.",
	} {
		if !strings.Contains(output, line) {
			t.Errorf("the report lacks %q:\n%s", line, output)
		}
	}
	if strings.Contains(output, leakedSecret) {
		t.Errorf("the report printed a secret:\n%s", output)
	}
}

// TestDoctorUnderJSONWritesOnlyTheFailureEnvelope follows ADR-075: a run that
// fails writes one document, and it is the error.
func TestDoctorUnderJSONWritesOnlyTheFailureEnvelope(t *testing.T) {
	t.Parallel()

	output, _, err := runDoctor(t, brokenDiagnosis(), true)
	if output != "" {
		t.Errorf("a run with issues wrote a report beside its failure:\n%s", output)
	}

	written := &bytes.Buffer{}
	if writeErr := jsonoutput.WriteError(written, err); writeErr != nil {
		t.Fatal(writeErr)
	}

	var envelope jsonoutput.ErrorEnvelope
	if decodeErr := json.Unmarshal(written.Bytes(), &envelope); decodeErr != nil {
		t.Fatalf("not one JSON document: %v\n%s", decodeErr, written)
	}
	if envelope.Error.Kind != string(apperrors.KindPermanent) || envelope.Error.ExitCode != 1 || envelope.Error.Message != brokenSummary {
		t.Errorf("error = %+v", envelope.Error)
	}
	if !reflect.DeepEqual(envelope.Error.Details, brokenDetails) {
		t.Errorf("details:\n got %#v\nwant %#v", envelope.Error.Details, brokenDetails)
	}
	if strings.Contains(written.String(), leakedSecret) {
		t.Errorf("the failure envelope carries a secret:\n%s", written)
	}
}

func TestEveryKindOfIssueFailsTheRun(t *testing.T) {
	t.Parallel()

	file := func(change func(*config.DiagnosedFile)) config.Diagnosis {
		stored := config.DiagnosedFile{Tier: config.TierStored, Path: storedPath, Read: true, Exists: true, Parses: true, MatchesSchema: true}
		change(&stored)
		return config.Diagnosis{Files: []config.DiagnosedFile{stored}}
	}

	cases := []struct {
		name      string
		diagnosis config.Diagnosis
		key       string
	}{
		{"a file that could not be read", file(func(f *config.DiagnosedFile) { f.Problem = "could not be read: denied" }), "file/stored"},
		{"a key the schema rejects", file(func(f *config.DiagnosedFile) {
			f.Violations = []config.SchemaViolation{{Key: "unknown", Path: []string{"unknown"}, Line: 1, Problem: "unknown key"}}
		}), "violation/stored/unknown"},
		{"a key its file never reads", file(func(f *config.DiagnosedFile) {
			f.Ignored = []config.IgnoredKey{{Key: "require_keyring", Line: 1, ReadFrom: []string{config.TierSystem}}}
		}), "ignored/stored/require_keyring"},
		{"a setting a command would refuse", config.Diagnosis{Settings: []config.DiagnosedSetting{
			{Name: "retry_count", Value: "-1", Configured: true, Source: config.SettingSource{Kind: config.SourceEnvironment, Name: "BB_RETRY_COUNT"}, Problem: "must be greater than or equal to 0"},
		}}, "setting/retry_count"},
		{"a required keyring that cannot be reached", config.Diagnosis{Keyring: config.KeyringDiagnosis{
			Required: true, Checked: true, Problem: "the OS keyring could not be reached: no bus",
			RequiredBy: config.SettingSource{Kind: config.SourceEnvironment, Name: "BB_REQUIRE_KEYRING"},
		}}, "keyring"},
	}

	for _, tc := range cases {
		for _, asJSON := range []bool{false, true} {
			output, _, err := runDoctor(t, tc.diagnosis, asJSON)

			details := apperrors.DetailsOf(err)
			if apperrors.ExitCode(err) != 1 || apperrors.KindOf(err) != apperrors.KindPermanent || len(details) != 1 || details[tc.key] == "" {
				t.Errorf("%s (json=%v): err=%v details=%v, want exit 1 naming %s", tc.name, asJSON, err, details, tc.key)
			}
			if asJSON && output != "" {
				t.Errorf("%s: under --json a run with issues wrote a report:\n%s", tc.name, output)
			}
		}
	}
}

// TestIssueKeysCannotCollide covers the keys a script reads. A host key is a
// URL, so the dotted path is ambiguous, and a key may hold the characters a
// pointer escapes.
func TestIssueKeysCannotCollide(t *testing.T) {
	t.Parallel()

	diagnosis := config.Diagnosis{Files: []config.DiagnosedFile{{
		Tier: config.TierStored, Path: "/c.yaml", Read: true, Exists: true, Parses: true,
		Violations: []config.SchemaViolation{
			{Key: "hosts.a.b.x", Path: []string{"hosts", "a.b", "x"}, Line: 3, Problem: "unknown key"},
			{Key: "hosts.a.b.x", Path: []string{"hosts", "a", "b.x"}, Line: 6, Problem: "unknown key"},
			{Key: "hosts.a/b~c.url", Path: []string{"hosts", "a/b~c", "url"}, Line: 8, Problem: "got number, want string"},
			{Key: "hosts.a/b~c.url", Path: []string{"hosts", "a/b~c", "url"}, Line: 8, Problem: "value must be one of 'token', 'basic'"},
		},
	}}}

	want := map[string]string{
		"violation/stored/hosts/a.b/x":       "/c.yaml:3: hosts.a.b.x: unknown key",
		"violation/stored/hosts/a/b.x":       "/c.yaml:6: hosts.a.b.x: unknown key",
		"violation/stored/hosts/a~1b~0c/url": "/c.yaml:8: hosts.a/b~c.url: got number, want string; /c.yaml:8: hosts.a/b~c.url: value must be one of 'token', 'basic'",
	}
	if details := apperrors.DetailsOf(failureFor(diagnosis, issuesIn(diagnosis))); !reflect.DeepEqual(details, want) {
		t.Errorf("details:\n got %#v\nwant %#v", details, want)
	}
}

// TestErrorDetailsNameEveryIssueTheReportShows runs the real diagnosis over
// broken files and checks that nothing the report shows is missing from the
// error, and that no two issues were folded into one entry. It sets the
// environment, so it does not run in parallel.
func TestErrorDetailsNameEveryIssueTheReportShows(t *testing.T) {
	directory := t.TempDir()
	write := func(name, content string) string {
		path := filepath.Join(directory, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	stored := write("stored.yaml", strings.Join([]string{
		"hosts:",
		"  corp:",
		"    url: 42",
		"  a.b:",
		"    x: 1",
		"  a:",
		"    url: https://a.example.com",
		"    b.x: 1",
		"require_keyring: true",
		"unknown: 1",
	}, "\n"))
	system := write("system.yaml", "project_key: X\npolicies:\n  allowed_hosts: nope\n")

	t.Setenv("BB_DISABLE_STORED_CONFIG", "")
	t.Setenv("BB_CONFIG_PATH", stored)
	t.Setenv("BB_SYSTEM_CONFIG_PATH", system)
	t.Setenv("BB_WORKSPACE_CONFIG_PATH", directory)
	t.Setenv("BB_RETRY_COUNT", "-1")

	diagnosis := config.Diagnose(config.DiagnoseInput{})
	details := apperrors.DetailsOf(failureFor(diagnosis, issuesIn(diagnosis)))

	shown := []string{}
	for _, file := range diagnosis.Files {
		if file.Problem != "" {
			shown = append(shown, file.Problem)
		}
		for _, violation := range file.Violations {
			shown = append(shown, violation.Key+": "+violation.Problem)
		}
		for _, ignored := range file.Ignored {
			shown = append(shown, ignored.Key+" is read only")
		}
	}
	for _, setting := range diagnosis.Settings {
		if setting.Problem != "" {
			shown = append(shown, setting.Problem)
		}
	}
	if diagnosis.Keyring.Problem != "" {
		shown = append(shown, diagnosis.Keyring.Problem)
	}
	if len(shown) != 10 {
		t.Fatalf("the fixture was written to show ten issues, and shows %d: %q", len(shown), shown)
	}

	if len(details) != len(shown) {
		t.Errorf("error.details has %d entries for %d issues: %#v", len(details), len(shown), details)
	}
	values := []string{}
	for _, value := range details {
		values = append(values, value)
	}
	joined := strings.Join(values, "\n")
	times := map[string]int{}
	for _, text := range shown {
		times[text]++
	}
	for text, want := range times {
		if got := strings.Count(joined, text); got < want {
			t.Errorf("error.details names %q %d times, the report shows it %d times:\n%s", text, got, want, joined)
		}
	}
}

func TestDoctorWritesTheReportWhenThereIsNothingToFix(t *testing.T) {
	t.Parallel()

	output, _, err := runDoctor(t, healthyDiagnosis(), true)
	if err != nil {
		t.Fatalf("nothing to fix exits zero: %v", err)
	}
	if strings.Contains(output, leakedSecret) {
		t.Errorf("the report carries a secret:\n%s", output)
	}

	var envelope struct {
		Data Report `json:"data"`
	}
	if decodeErr := json.Unmarshal([]byte(output), &envelope); decodeErr != nil {
		t.Fatalf("not one JSON document: %v\n%s", decodeErr, output)
	}
	report := envelope.Data
	if len(report.Files) != 3 || !report.Keyring.Reachable || report.Settings[1].Value != "" || !report.Settings[1].Secret {
		t.Errorf("report = %+v", report)
	}
}

func TestDoctorPassesTheInvocationToTheDiagnosis(t *testing.T) {
	t.Parallel()

	output, received, err := runDoctor(t, healthyDiagnosis(), false, "--log-level", "debug")
	if err != nil {
		t.Fatalf("nothing to fix exits zero: %v", err)
	}

	if !received.ChangedFlags["log-level"] || received.ChangedFlags["log-format"] {
		t.Errorf("changed flags = %v, want only log-level", received.ChangedFlags)
	}
	if received.Overrides.Host != "https://override.example.com" {
		t.Errorf("overrides = %+v, want the runtime overrides", received.Overrides)
	}

	for _, line := range []string{
		"  stored     no location",
		"not read: its location could not be worked out: no home",
		"  workspace  /repo/.bb/config.yaml\n             valid",
		"  system     " + systemPath + "\n             not present",
		"host   not set",
		"required by BB_REQUIRE_KEYRING in the environment; reachable",
		"No issues found.",
	} {
		if !strings.Contains(output, line) {
			t.Errorf("the report lacks %q:\n%s", line, output)
		}
	}
}

func TestDoctorSaysWhenNoKeyringIsRequired(t *testing.T) {
	t.Parallel()

	diagnosis := healthyDiagnosis()
	diagnosis.Keyring = config.KeyringDiagnosis{}

	output, _, err := runDoctor(t, diagnosis, false)
	if err != nil || !strings.Contains(output, "not required, so not checked") {
		t.Errorf("err=%v output:\n%s", err, output)
	}
}

func TestAFileWithoutAPathIsNamedByItsTier(t *testing.T) {
	t.Parallel()

	diagnosis := config.Diagnosis{Files: []config.DiagnosedFile{{Tier: config.TierWorkspace, Read: true, Problem: "could not be read: no working directory"}}}
	err := failureFor(diagnosis, issuesIn(diagnosis))

	if want := "1 issue to fix: 1 in the workspace configuration"; apperrors.MessageOf(err) != want {
		t.Errorf("message = %q, want %q", apperrors.MessageOf(err), want)
	}
	if want := map[string]string{"file/workspace": "the workspace configuration: could not be read: no working directory"}; !reflect.DeepEqual(apperrors.DetailsOf(err), want) {
		t.Errorf("details = %#v, want %#v", apperrors.DetailsOf(err), want)
	}
}

func TestDoctorBuildsWithItsDefaults(t *testing.T) {
	t.Parallel()

	// The configuration is this machine's; the shells and skills are a
	// described machine's, so the test reads no profile or home directory of
	// whoever runs it.
	command := New(Dependencies{Machine: emptyMachine(t)})
	output := &bytes.Buffer{}
	command.SetOut(output)
	command.SetErr(output)
	command.SetArgs([]string{})

	// Whatever this machine's configuration holds, the command reports on it.
	_ = command.Execute()
	for _, section := range []string{"Configuration files", "Shell completion", "Agent skills"} {
		if !strings.Contains(output.String(), section) {
			t.Errorf("the default diagnosis produced no %s section:\n%s", section, output)
		}
	}
}

// TestTheDefaultsLookAtThisMachine checks what a root that wires nothing gets,
// without looking: running it would read the profiles and home directory of
// whoever runs the test.
func TestTheDefaultsLookAtThisMachine(t *testing.T) {
	t.Parallel()

	defaults := Dependencies{}.withDefaults()

	if defaults.Machine.System.GOOS != runtime.GOOS || defaults.Machine.System.LookPath == nil || defaults.Machine.System.Run == nil {
		t.Errorf("the default machine is not this one: %+v", defaults.Machine.System)
	}
	if defaults.Machine.WorkingDirectory == nil || defaults.Machine.PackagedScripts == nil || defaults.Version == nil {
		t.Errorf("a default is missing: %+v", defaults)
	}
	if _, err := defaults.CompletionScript(completionsetup.Bash, true); apperrors.KindOf(err) != apperrors.KindInternal {
		t.Errorf("an unwired completion script generator gave %v, want an internal failure", err)
	}
}
