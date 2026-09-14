package doctorcmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// leakedSecret stands for a secret value that reached a setting. The config
// layer never puts one there; the command must not print one if it did.
const leakedSecret = "leaked-secret-7c21"

func brokenDiagnosis() config.Diagnosis {
	stored := "/home/alice/.config/bb/config.yaml"
	system := "/etc/bb/config.yaml"

	return config.Diagnosis{
		Files: []config.DiagnosedFile{
			{
				Tier: config.TierStored, Path: stored, PathFrom: "BB_CONFIG_PATH", Read: true, Exists: true, Parses: true,
				Violations: []config.SchemaViolation{
					{Key: "policies.require_keyrng", Line: 3, Problem: "unknown key"},
					{Key: "", Problem: "got array, want object"},
				},
				Ignored: []config.IgnoredKey{{Key: "require_keyring", ReadFrom: []string{config.TierSystem}}},
				Secrets: []config.SecretPresence{{Host: "https://bitbucket.example.com", Token: true, Password: true}},
			},
			{Tier: config.TierWorkspace, PathFrom: "search", Read: true},
			{Tier: config.TierSystem, Path: system, PathFrom: "machine", Read: true, Exists: true, Problem: "invalid YAML configuration (yaml: line 2)"},
		},
		Settings: []config.DiagnosedSetting{
			{
				Name: "host", Value: "https://bitbucket.example.com", Configured: true,
				Source:   config.SettingSource{Kind: config.SourceEnvironment, Name: "BITBUCKET_URL"},
				Shadowed: []config.SettingSource{{Kind: config.TierStored, Name: "default_host", Path: stored}},
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
			RequiredBy: config.SettingSource{Kind: config.TierSystem, Name: "require_keyring", Path: system},
		},
	}
}

func healthyDiagnosis() config.Diagnosis {
	return config.Diagnosis{
		Files: []config.DiagnosedFile{
			{Tier: config.TierStored, PathFrom: "default", NotRead: "its location could not be worked out: no home"},
			{Tier: config.TierWorkspace, Path: "/repo/.bb/config.yaml", PathFrom: "search", Read: true, Exists: true, Parses: true, MatchesSchema: true},
			{Tier: config.TierSystem, Path: "/etc/bb/config.yaml", PathFrom: "machine", Read: true},
		},
		Settings: []config.DiagnosedSetting{{Name: "host", Source: config.SettingSource{Kind: config.SourceDefault}}},
		Keyring: config.KeyringDiagnosis{
			Required: true, Checked: true, Reachable: true,
			RequiredBy: config.SettingSource{Kind: config.SourceEnvironment, Name: "BB_REQUIRE_KEYRING"},
		},
	}
}

func runDoctor(t *testing.T, diagnosis config.Diagnosis, asJSON bool, args ...string) (string, config.DiagnoseInput, error) {
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
	}))

	output := &bytes.Buffer{}
	root.SetOut(output)
	root.SetErr(output)
	root.SetArgs(append([]string{"doctor"}, args...))

	err := root.Execute()

	return output.String(), received, err
}

func TestDoctorExitsPermanentWhenAFileIsInvalid(t *testing.T) {
	t.Parallel()

	output, _, err := runDoctor(t, brokenDiagnosis(), false)

	if apperrors.KindOf(err) != apperrors.KindPermanent || apperrors.ExitCode(err) != 1 {
		t.Fatalf("an invalid file must exit 1 as permanent, got %v (exit %d)", err, apperrors.ExitCode(err))
	}
	if want := "2 configuration files are invalid: /home/alice/.config/bb/config.yaml, /etc/bb/config.yaml"; apperrors.MessageOf(err) != want {
		t.Errorf("message = %q, want %q", apperrors.MessageOf(err), want)
	}

	for _, line := range []string{
		"  stored     /home/alice/.config/bb/config.yaml (BB_CONFIG_PATH)",
		"invalid: the schema rejects 2 keys",
		"line 3: policies.require_keyrng: unknown key",
		"(the file): got array, want object",
		"ignored: require_keyring is read only from the system configuration",
		"plaintext: a token and a password for https://bitbucket.example.com",
		"  workspace  none found above the working directory",
		"invalid: invalid YAML configuration (yaml: line 2)",
		"https://bitbucket.example.com, from BITBUCKET_URL in the environment",
		"overrides default_host in the stored configuration /home/alice/.config/bb/config.yaml",
		"configured, from the OS keyring",
		"password         not set",
		"-3, from the --retry-count flag",
		"problem: must be greater than or equal to 0",
		"20s (default)",
		"debug, from BB_LOG_LEVEL in /repo/.env",
		"PRJ, from the program running bb",
		`https://mirror.example.com, from UpdateBaseURL in HKEY_LOCAL_MACHINE\Software\Policies\bb`,
		"empty, from the --ca-file flag",
		"required by require_keyring in the system configuration /etc/bb/config.yaml; the OS keyring could not be reached: no bus",
		"A configuration file bb reads is invalid.",
	} {
		if !strings.Contains(output, line) {
			t.Errorf("the report lacks %q:\n%s", line, output)
		}
	}
	if strings.Contains(output, leakedSecret) {
		t.Errorf("the report printed a secret:\n%s", output)
	}
}

// TestDoctorUnderJSONReportsTheVerdictAndExitsZero follows ADR-075: a failing
// exit would replace the report with an error envelope.
func TestDoctorUnderJSONReportsTheVerdictAndExitsZero(t *testing.T) {
	t.Parallel()

	output, _, err := runDoctor(t, brokenDiagnosis(), true)
	if err != nil {
		t.Fatalf("under --json the verdict is ok, not the exit status: %v", err)
	}
	if strings.Contains(output, leakedSecret) {
		t.Errorf("the JSON report carries a secret:\n%s", output)
	}

	var envelope struct {
		Data Report `json:"data"`
	}
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		t.Fatalf("not one JSON document: %v\n%s", err, output)
	}

	report := envelope.Data
	if report.OK || report.Files[0].Valid || !report.Files[1].Valid || report.Files[2].Valid {
		t.Errorf("verdicts: ok=%v files=%+v", report.OK, report.Files)
	}
	if len(report.Files[0].Violations) != 2 || report.Settings[1].Value != "" || !report.Settings[1].Secret {
		t.Errorf("report = %+v", report)
	}
}

func TestDoctorPassesTheInvocationToTheDiagnosis(t *testing.T) {
	t.Parallel()

	output, received, err := runDoctor(t, healthyDiagnosis(), false, "--log-level", "debug")
	if err != nil {
		t.Fatalf("a healthy configuration exits zero: %v", err)
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
		"  system     /etc/bb/config.yaml\n             not present",
		"host  not set",
		"required by BB_REQUIRE_KEYRING in the environment; reachable",
		"Every configuration file bb reads is valid.",
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

func TestVerdictNamesAFileWithoutAPathByItsTier(t *testing.T) {
	t.Parallel()

	err := verdict(Report{Files: []File{{Tier: config.TierWorkspace}}})
	if want := "1 configuration file is invalid: the workspace configuration"; apperrors.MessageOf(err) != want {
		t.Errorf("message = %q, want %q", apperrors.MessageOf(err), want)
	}
}

func TestDoctorBuildsWithItsDefaults(t *testing.T) {
	t.Parallel()

	command := New(Dependencies{})
	output := &bytes.Buffer{}
	command.SetOut(output)
	command.SetErr(output)
	command.SetArgs([]string{})

	// Whatever this machine's configuration holds, the command reports on it.
	_ = command.Execute()
	if !strings.Contains(output.String(), "Configuration files") {
		t.Errorf("the default diagnosis produced no report:\n%s", output)
	}
}
