package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// doctorInputs builds a diagnosis's inputs from file contents and an
// environment given as maps, so a test states everything the diagnosis sees and
// runs in parallel with the rest.
func doctorInputs(t *testing.T, files map[string]string, environment map[string]string) diagnosisInputs {
	t.Helper()

	directory := t.TempDir()
	inputs := make([]fileInput, 0, 3)
	for _, tier := range []string{TierStored, TierWorkspace, TierSystem} {
		input := fileInput{tier: tier, path: filepath.Join(directory, tier+".yaml"), pathFrom: "default"}
		if content, ok := files[tier]; ok {
			input.exists, input.raw = true, []byte(content)
		}
		inputs = append(inputs, input)
	}

	return diagnosisInputs{
		getenv:       func(name string) string { return environment[name] },
		ambient:      func(name string) bool { _, ok := environment[name]; return ok },
		files:        inputs,
		secrets:      func(string, string) (string, string) { return "", "" },
		probeKeyring: func() error { return nil },
	}
}

// TestDoctorReportsTheRefusalsAnUpdateWouldMeet is ADR-086's rule that anything
// a command would refuse fails the doctor.
//
// The allow_http_update row read policy only, so two refusals were invisible: a
// variable bb update cannot parse, which fails the run before it starts, and a
// variable asking for plain HTTP where policy refuses it, which fails every run
// even against an https mirror.
func TestDoctorReportsTheRefusalsAnUpdateWouldMeet(t *testing.T) {
	t.Parallel()

	t.Run("a variable that is not a boolean", func(t *testing.T) {
		t.Parallel()

		setting := diagnosedSetting(t, diagnose(doctorInputs(t, nil, map[string]string{
			"BB_ALLOW_HTTP_UPDATE": "yes",
		})), "allow_http_update")

		if !strings.Contains(setting.Problem, "must be true or false") {
			t.Fatalf("bb update exits 2 on this, and the diagnosis says %q", setting.Problem)
		}
	})

	t.Run("an opt-in against a policy that refuses", func(t *testing.T) {
		t.Parallel()

		setting := diagnosedSetting(t, diagnose(doctorInputs(t,
			map[string]string{TierSystem: "policies:\n  allow_http_update: false\n"},
			map[string]string{"BB_ALLOW_HTTP_UPDATE": "1"},
		)), "allow_http_update")

		if !strings.Contains(setting.Problem, "refused") {
			t.Fatalf("every bb update exits 3 here, and the diagnosis says %q", setting.Problem)
		}
	})

	t.Run("nothing to report when the two agree", func(t *testing.T) {
		t.Parallel()

		setting := diagnosedSetting(t, diagnose(doctorInputs(t, nil, map[string]string{
			"BB_ALLOW_HTTP_UPDATE": "1",
		})), "allow_http_update")

		if setting.Problem != "" {
			t.Fatalf("a permitted opt-in was reported as a problem: %q", setting.Problem)
		}
	})
}

func writeDoctorFile(t *testing.T, directory, name, content string) string {
	t.Helper()

	path := filepath.Join(directory, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

func diagnosedFile(t *testing.T, diagnosis Diagnosis, tier string) DiagnosedFile {
	t.Helper()

	for _, file := range diagnosis.Files {
		if file.Tier == tier {
			return file
		}
	}
	t.Fatalf("the diagnosis has no %s file", tier)

	return DiagnosedFile{}
}

func diagnosedSetting(t *testing.T, diagnosis Diagnosis, name string) DiagnosedSetting {
	t.Helper()

	for _, setting := range diagnosis.Settings {
		if setting.Name == name {
			return setting
		}
	}
	t.Fatalf("the diagnosis has no %s setting", name)

	return DiagnosedSetting{}
}

func shadowedNames(setting DiagnosedSetting) []string {
	names := []string{}
	for _, source := range setting.Shadowed {
		names = append(names, source.Name)
	}

	return names
}

// TestDiagnoseReportsEveryViolationInEveryFile is the command's reason to
// exist. A load stops at the first file it cannot use, and the schema check
// behind it stops at the answer "no"; repairing a file one error per run is the
// round trip bb doctor removes.
func TestDiagnoseReportsEveryViolationInEveryFile(t *testing.T) {
	t.Parallel()

	diagnosis := diagnose(doctorInputs(t, map[string]string{
		TierStored: strings.Join([]string{
			"default_host: https://bitbucket.example.com",
			"policies:",
			"  require_keyrng: true",
			"hosts:",
			"  corp:",
			"    url: 42",
			"    auth_mode: tokn",
			"    aliases:",
			"      - 7",
			"  other:",
			"    username: alice",
			"unknown_top: x",
		}, "\n"),
		TierWorkspace: "project_key: PRJ\n",
		TierSystem:    "allowed_hosts: nope\ndisable_updat: true\n",
	}, nil))

	want := map[string][]string{
		TierStored: {
			"3 policies.require_keyrng: unknown key",
			"6 hosts.corp.url: got number, want string",
			"7 hosts.corp.auth_mode: value must be one of 'token', 'basic'",
			"9 hosts.corp.aliases.0: got number, want string",
			"10 hosts.other.url: required key is missing",
			"12 unknown_top: unknown key",
		},
		TierWorkspace: {},
		TierSystem: {
			"1 allowed_hosts: got string, want array",
			"2 disable_updat: unknown key",
		},
	}

	for tier, expected := range want {
		file := diagnosedFile(t, diagnosis, tier)

		got := []string{}
		for _, violation := range file.Violations {
			got = append(got, fmt.Sprintf("%d %s: %s", violation.Line, violation.Key, violation.Problem))
		}
		if !slices.Equal(got, expected) {
			t.Errorf("%s violations:\n got %q\nwant %q", tier, got, expected)
		}
		if file.Valid() != (len(expected) == 0) || !file.Parses || file.MatchesSchema != (len(expected) == 0) {
			t.Errorf("%s: valid=%v parses=%v matchesSchema=%v, with %d violations", tier, file.Valid(), file.Parses, file.MatchesSchema, len(expected))
		}
	}
}

func TestDiagnoseReportsAFileThatDoesNotParseAndStillChecksTheOthers(t *testing.T) {
	t.Parallel()

	diagnosis := diagnose(doctorInputs(t, map[string]string{
		TierStored: "hosts:\n  corp: {url: https://a.example.com}\n\tbad: {url: https://b.example.com}\n",
		TierSystem: "requre_keyring: true\n",
	}, nil))

	stored := diagnosedFile(t, diagnosis, TierStored)
	if stored.Valid() || stored.Parses || !strings.Contains(stored.Problem, "line 3") {
		t.Errorf("a file with a tab in its indentation: valid=%v parses=%v problem=%q", stored.Valid(), stored.Parses, stored.Problem)
	}

	if system := diagnosedFile(t, diagnosis, TierSystem); len(system.Violations) != 1 {
		t.Errorf("the system file was not checked after the stored file failed: %+v", system)
	}

	// A file that is not there is no obstacle to a load.
	if workspace := diagnosedFile(t, diagnosis, TierWorkspace); !workspace.Valid() || workspace.Exists || workspace.Parses {
		t.Errorf("an absent workspace file: %+v", workspace)
	}
}

// TestDiagnoseNeverCarriesASecretValue checks the diagnosis itself rather than
// what renders it: a value that never enters the structure cannot be printed by
// any output built on it.
func TestDiagnoseNeverCarriesASecretValue(t *testing.T) {
	t.Parallel()

	const (
		storedToken      = "stored-token-3f9a"
		storedPassword   = "stored-password-3f9a"
		environmentToken = "environment-token-3f9a"
		adminPassword    = "admin-password-3f9a"
		keyringPassword  = "keyring-password-3f9a"
	)

	inputs := doctorInputs(t, map[string]string{
		TierStored: strings.Join([]string{
			"default_host: https://bitbucket.example.com",
			"hosts:",
			"  https://bitbucket.example.com:",
			"    url: https://bitbucket.example.com",
			"    username: alice",
			"insecure_secrets:",
			"  https://bitbucket.example.com:",
			"    token: " + storedToken,
			"    password: " + storedPassword,
		}, "\n"),
	}, map[string]string{"BITBUCKET_TOKEN": environmentToken, "ADMIN_PASSWORD": adminPassword})
	inputs.secrets = func(string, string) (string, string) { return "", keyringPassword }

	diagnosis := diagnose(inputs)

	encoded, err := json.Marshal(diagnosis)
	if err != nil {
		t.Fatal(err)
	}
	rendered := string(encoded) + fmt.Sprintf("%+v", diagnosis)
	for _, secret := range []string{storedToken, storedPassword, environmentToken, adminPassword, keyringPassword} {
		if strings.Contains(rendered, secret) {
			t.Errorf("the diagnosis carries the secret %q", secret)
		}
	}

	token := diagnosedSetting(t, diagnosis, "token")
	if !token.Secret || !token.Configured || token.Source.Name != "BITBUCKET_TOKEN" ||
		!slices.Equal(shadowedNames(token), []string{"insecure_secrets.https://bitbucket.example.com.token"}) {
		t.Errorf("token: %+v", token)
	}

	password := diagnosedSetting(t, diagnosis, "password")
	if !password.Secret || password.Source.Name != "ADMIN_PASSWORD" || len(password.Shadowed) != 1 || password.Shadowed[0].Kind != SourceKeyring {
		t.Errorf("password: %+v", password)
	}

	stored := diagnosedFile(t, diagnosis, TierStored)
	if want := []SecretPresence{{Host: "https://bitbucket.example.com", Token: true, Password: true}}; !reflect.DeepEqual(stored.Secrets, want) {
		t.Errorf("secrets = %+v, want %+v", stored.Secrets, want)
	}
}

func TestDiagnoseTakesTheHostFromTheStrongestSource(t *testing.T) {
	t.Parallel()

	files := map[string]string{
		TierWorkspace: "default_host: https://workspace.example.com\n",
		TierStored:    "default_host: corp\nhosts:\n  corp:\n    url: https://stored.example.com\n",
		TierSystem:    "default_host: https://system.example.com\nallowed_hosts: [stored.example.com]\n",
	}

	cases := []struct {
		name        string
		environment map[string]string
		drop        []string
		want        string
		kind        string
		shadowed    int
		permitted   bool
	}{
		{"the environment outranks every file", map[string]string{"BITBUCKET_URL": "https://env.example.com"}, nil, "https://env.example.com", SourceEnvironment, 3, false},
		{"the workspace outranks the stored and system files", nil, nil, "https://workspace.example.com", TierWorkspace, 2, false},
		{"the stored file resolves a name through its hosts", nil, []string{TierWorkspace}, "https://stored.example.com", TierStored, 1, true},
		{"the system file comes last", nil, []string{TierWorkspace, TierStored}, "https://system.example.com", TierSystem, 0, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			present := map[string]string{}
			for tier, content := range files {
				if !slices.Contains(tc.drop, tier) {
					present[tier] = content
				}
			}

			host := diagnosedSetting(t, diagnose(doctorInputs(t, present, tc.environment)), "host")
			if host.Value != tc.want || host.Source.Kind != tc.kind || len(host.Shadowed) != tc.shadowed {
				t.Errorf("host = %+v, want %s from %s over %d others", host, tc.want, tc.kind, tc.shadowed)
			}
			if permitted := host.Problem == ""; permitted != tc.permitted {
				t.Errorf("problem = %q; allowed_hosts permits only stored.example.com", host.Problem)
			}
		})
	}

	unresolved := diagnosedSetting(t, diagnose(doctorInputs(t, map[string]string{TierStored: "default_host: nowhere\n"}, nil)), "host")
	if !unresolved.Configured || unresolved.Value != "" || !strings.Contains(unresolved.Problem, "default_host") {
		t.Errorf("a default_host naming no host: %+v", unresolved)
	}
}

// TestUpdateBaseURLInTheUsersOwnFileOverridesTheSystemMirror is the precedence
// the issue calls out: every other policy key is a mandate, and this one is a
// default that the user's own configuration replaces.
func TestUpdateBaseURLInTheUsersOwnFileOverridesTheSystemMirror(t *testing.T) {
	t.Parallel()

	inputs := doctorInputs(t, map[string]string{
		TierStored: "update_base_url: https://user-mirror.example.com\n",
		TierSystem: "policies:\n  update_base_url: https://system-mirror.example.com\n",
	}, nil)
	inputs.platformPolicy = PolicyConfig{UpdateBaseURL: "https://registry-mirror.example.com"}
	// bb update never loads .env, so a value only a .env file carries takes no part.
	inputs.getenv = func(name string) string {
		if name == "BB_UPDATE_BASE_URL" {
			return "https://dotenv-mirror.example.com"
		}
		return ""
	}

	setting := diagnosedSetting(t, diagnose(inputs), "update_base_url")
	if setting.Value != "https://user-mirror.example.com" || setting.Source.Kind != TierStored {
		t.Errorf("update_base_url = %q from %+v, want the user's mirror from the stored file", setting.Value, setting.Source)
	}
	// Inside the system tier the registry outranks the file, the way it does
	// for every other policy key: the two orderings are listed strongest first.
	if want := []string{"UpdateBaseURL", "policies.update_base_url"}; !slices.Equal(shadowedNames(setting), want) {
		t.Errorf("shadowed = %q, want %q", shadowedNames(setting), want)
	}
}

func TestPolicyComesFromTheStrongestOfItsSources(t *testing.T) {
	t.Parallel()

	inputs := doctorInputs(t, map[string]string{
		TierSystem: strings.Join([]string{
			"allowed_hosts: [top.example.com]",
			"policies:",
			"  allowed_hosts: [policies.example.com]",
			"  mcp_audit_file: /var/log/policies.jsonl",
			"policy:",
			"  allowed_hosts: [policy.example.com]",
			"mcp_audit_file: /var/log/top.jsonl",
		}, "\n"),
	}, nil)
	inputs.platformPolicy = PolicyConfig{AllowedHosts: []string{"registry.example.com", "second.example.com"}}

	diagnosis := diagnose(inputs)

	hosts := diagnosedSetting(t, diagnosis, "allowed_hosts")
	if hosts.Value != "registry.example.com, second.example.com" || hosts.Source.Kind != SourceRegistry {
		t.Errorf("allowed_hosts = %+v, want the registry's", hosts)
	}
	if want := []string{"policy.allowed_hosts", "policies.allowed_hosts", "allowed_hosts"}; !slices.Equal(shadowedNames(hosts), want) {
		t.Errorf("allowed_hosts shadowed = %q, want %q", shadowedNames(hosts), want)
	}

	audit := diagnosedSetting(t, diagnosis, "mcp_audit_file")
	if audit.Value != "/var/log/policies.jsonl" || audit.Source.Name != "policies.mcp_audit_file" ||
		!slices.Equal(shadowedNames(audit), []string{"mcp_audit_file"}) {
		t.Errorf("mcp_audit_file = %+v, want the policies block over the top level", audit)
	}
}

func TestAMandatingPolicyOutranksItsVariableAndOneThatDoesNotIsOutranked(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		system      string
		environment map[string]string
		setting     string
		want        string
		kind        string
		shadowed    int
		problem     string
	}{
		{"require_keyring true outranks BB_REQUIRE_KEYRING=0", "require_keyring: true\n", map[string]string{"BB_REQUIRE_KEYRING": "0"}, "require_keyring", "true", TierSystem, 1, ""},
		{"BB_REQUIRE_KEYRING outranks require_keyring false", "policies:\n  require_keyring: false\n", map[string]string{"BB_REQUIRE_KEYRING": "1"}, "require_keyring", "true", SourceEnvironment, 1, ""},
		{"BB_REQUIRE_KEYRING that is not a boolean", "", map[string]string{"BB_REQUIRE_KEYRING": "maybe"}, "require_keyring", "maybe", SourceEnvironment, 0, "must be a boolean"},
		{"disable_update true outranks BB_DISABLE_UPDATE", "disable_update: true\n", map[string]string{"BB_DISABLE_UPDATE": "0"}, "disable_update", "true", TierSystem, 1, ""},
		{"BB_DISABLE_UPDATE outranks disable_update false", "disable_update: false\n", map[string]string{"BB_DISABLE_UPDATE": "TRUE"}, "disable_update", "true", SourceEnvironment, 1, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			setting := diagnosedSetting(t, diagnose(doctorInputs(t, map[string]string{TierSystem: tc.system}, tc.environment)), tc.setting)
			if setting.Value != tc.want || setting.Source.Kind != tc.kind || len(setting.Shadowed) != tc.shadowed {
				t.Errorf("%s = %+v, want %s from %s over %d", tc.setting, setting, tc.want, tc.kind, tc.shadowed)
			}
			if !strings.Contains(setting.Problem, tc.problem) || (tc.problem == "" && setting.Problem != "") {
				t.Errorf("problem = %q, want %q", setting.Problem, tc.problem)
			}
		})
	}
}

func TestTheKeyringIsProbedOnlyWhenItIsRequired(t *testing.T) {
	t.Parallel()

	unreachable := errors.New("no secret service on this bus")
	cases := []struct {
		name        string
		environment map[string]string
		probe       error
		checked     bool
		reachable   bool
	}{
		{"not required", nil, unreachable, false, false},
		{"required and reachable", map[string]string{"BB_REQUIRE_KEYRING": "true"}, nil, true, true},
		{"required and unreachable", map[string]string{"BB_REQUIRE_KEYRING": "true"}, unreachable, true, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			inputs := doctorInputs(t, nil, tc.environment)
			probed := false
			inputs.probeKeyring = func() error {
				probed = true
				return tc.probe
			}

			keyring := diagnose(inputs).Keyring
			if probed != tc.checked || keyring.Checked != tc.checked || keyring.Reachable != tc.reachable {
				t.Errorf("probed=%v keyring=%+v, want checked=%v reachable=%v", probed, keyring, tc.checked, tc.reachable)
			}
			if unreached := tc.checked && !tc.reachable; unreached != strings.Contains(keyring.Problem, unreachable.Error()) {
				t.Errorf("problem = %q", keyring.Problem)
			}
			if tc.checked && keyring.RequiredBy.Name != "BB_REQUIRE_KEYRING" {
				t.Errorf("requiredBy = %+v, want BB_REQUIRE_KEYRING", keyring.RequiredBy)
			}
		})
	}
}

// TestProbeKeyringTellsNotFoundFromUnreachable swaps the package's keyring, so
// it does not run in parallel.
func TestProbeKeyringTellsNotFoundFromUnreachable(t *testing.T) {
	if err := probeKeyring(); err != nil {
		t.Fatalf("a store that answers not found is reachable, got %v", err)
	}

	withUnavailableKeyring(t)
	if err := probeKeyring(); err == nil {
		t.Fatal("a store that cannot be reached reported no error")
	}
}

// TestKeysValidOnlyInAnotherFileAreReportedAsIgnored covers the silent case the
// schema cannot: require_keyring is a valid key in the user's own file, where
// nothing reads it.
func TestKeysValidOnlyInAnotherFileAreReportedAsIgnored(t *testing.T) {
	t.Parallel()

	diagnosis := diagnose(doctorInputs(t, map[string]string{
		TierStored:    "$schema: https://example.com/config.schema.json\nrequire_keyring: true\nproject_key: PRJ\n",
		TierWorkspace: "insecure_secrets: {}\n",
		TierSystem:    "project_key: PRJ\n",
	}, nil))

	want := map[string][]IgnoredKey{
		TierStored:    {{Key: "require_keyring", Line: 2, ReadFrom: []string{TierSystem}}, {Key: "project_key", Line: 3, ReadFrom: []string{TierWorkspace}}},
		TierWorkspace: {{Key: "insecure_secrets", Line: 1, ReadFrom: []string{TierStored, TierSystem}}},
		TierSystem:    {{Key: "project_key", Line: 1, ReadFrom: []string{TierWorkspace}}},
	}
	for tier, expected := range want {
		file := diagnosedFile(t, diagnosis, tier)
		if !reflect.DeepEqual(file.Ignored, expected) {
			t.Errorf("%s ignored = %+v, want %+v", tier, file.Ignored, expected)
		}
		if !file.Valid() {
			t.Errorf("%s: a key valid in another file does not make this one invalid: %+v", tier, file)
		}
	}

	if setting := diagnosedSetting(t, diagnosis, "require_keyring"); setting.Configured {
		t.Errorf("require_keyring in the user's file mandated something: %+v", setting)
	}
}

func TestAFlagOutranksItsVariableEvenWhenPassedEmpty(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	caFile := writeDoctorFile(t, directory, "flag-ca.pem", "pem")
	cert := writeDoctorFile(t, directory, "client.pem", "pem")
	key := writeDoctorFile(t, directory, "client.key", "key")

	inputs := doctorInputs(t, map[string]string{
		TierStored: strings.Join([]string{
			"default_host: https://bitbucket.example.com",
			"hosts:",
			"  https://bitbucket.example.com:",
			"    url: https://bitbucket.example.com",
			"    client_cert: " + cert,
			"    client_key: " + key,
		}, "\n"),
	}, map[string]string{
		"BB_CA_FILE":              filepath.Join(directory, "elsewhere.pem"),
		"BB_CLIENT_CERT":          filepath.Join(directory, "elsewhere-client.pem"),
		"BB_RETRY_COUNT":          "9",
		"BB_INSECURE_SKIP_VERIFY": "true",
		"BB_LOG_LEVEL":            "debug",
	})
	empty, retries, insecure := "", 4, false
	inputs.flags = Overrides{CAFile: &caFile, ClientCert: &empty, RetryCount: &retries, InsecureSkipVerify: &insecure}
	inputs.changedFlags = map[string]bool{"log-level": true}

	diagnosis := diagnose(inputs)

	cases := []struct {
		setting, value, kind, name, shadowed string
	}{
		{"ca_file", caFile, SourceFlag, "--ca-file", "BB_CA_FILE"},
		// Passed empty, the flag still silences the variable, and the host
		// profile supplies the value.
		{"client_cert", cert, TierStored, "hosts.https://bitbucket.example.com.client_cert", "BB_CLIENT_CERT"},
		{"retry_count", "4", SourceFlag, "--retry-count", "BB_RETRY_COUNT"},
		{"insecure_skip_verify", "false", SourceFlag, "--insecure-skip-verify", "BB_INSECURE_SKIP_VERIFY"},
	}
	for _, tc := range cases {
		setting := diagnosedSetting(t, diagnosis, tc.setting)
		if setting.Value != tc.value || setting.Source.Kind != tc.kind || setting.Source.Name != tc.name ||
			!slices.Equal(shadowedNames(setting), []string{tc.shadowed}) || setting.Problem != "" {
			t.Errorf("%s = %+v, want %s from %s %s over %s", tc.setting, setting, tc.value, tc.kind, tc.name, tc.shadowed)
		}
	}

	if level := diagnosedSetting(t, diagnosis, "log_level"); level.Source.Kind != SourceFlag || level.Value != "debug" {
		t.Errorf("log_level = %+v, want debug from the --log-level flag that wrote it", level)
	}
}

func TestDiagnoseNamesWhatACommandWouldRefuse(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	mandatedCA := writeDoctorFile(t, directory, "mandated-ca.pem", "pem")
	otherCA := writeDoctorFile(t, directory, "other-ca.pem", "pem")

	first := diagnose(doctorInputs(t, map[string]string{
		TierStored: strings.Join([]string{
			"default_host: https://bitbucket.example.com",
			"hosts:",
			"  https://bitbucket.example.com:",
			"    url: https://bitbucket.example.com",
			"    client_key: " + otherCA,
			"insecure_secrets:",
			"  https://bitbucket.example.com:",
			"    token: plaintext",
		}, "\n"),
		TierSystem: strings.Join([]string{
			"require_keyring: true",
			"allow_insecure_skip_verify: false",
			"ca_file: " + mandatedCA,
			"update_trusted_root: " + mandatedCA,
			"update_tuf_url: https://tuf.example.com",
		}, "\n"),
	}, map[string]string{
		"BB_CA_FILE":              otherCA,
		"BB_INSECURE_SKIP_VERIFY": "true",
		"BB_REQUEST_TIMEOUT":      "soon",
		"BB_RETRY_BACKOFF":        "0s",
		"BB_RETRY_COUNT":          "many",
		"BB_LOG_FORMAT":           "xml",
	}))

	second := diagnose(doctorInputs(t, map[string]string{
		TierStored: "default_host: https://bitbucket.example.com\n",
		TierSystem: "update_tuf_url: http://tuf.example.com\n",
	}, map[string]string{
		"BB_CA_FILE":              filepath.Join(directory, "absent.pem"),
		"BB_CLIENT_CERT":          directory,
		"BB_CLIENT_KEY":           otherCA,
		"BB_INSECURE_SKIP_VERIFY": "yes",
		"BB_REQUEST_TIMEOUT":      "-1s",
		"BB_RETRY_COUNT":          "-1",
		"BB_LOG_LEVEL":            "loud",
	}))

	for _, tc := range []struct {
		diagnosis Diagnosis
		problems  map[string]string
	}{
		{first, map[string]string{
			"ca_file":              "differs from the CA file policy mandates",
			"insecure_skip_verify": "allow_insecure_skip_verify",
			"client_key":           "must be set together",
			"token":                "plaintext",
			"update_trusted_root":  "mutually exclusive",
			"update_tuf_url":       "mutually exclusive",
			"request_timeout":      "valid duration",
			"retry_backoff":        "greater than 0",
			"retry_count":          "non-negative integer",
			"log_format":           "must be one of",
		}},
		{second, map[string]string{
			"ca_file":              "cannot be opened",
			"client_cert":          "is a directory",
			"client_key":           "",
			"insecure_skip_verify": "must be a boolean",
			"request_timeout":      "greater than 0",
			"retry_count":          "greater than or equal to 0",
			"log_level":            "must be one of",
			"update_trusted_root":  "",
			"update_tuf_url":       "absolute https URL",
		}},
	} {
		for name, want := range tc.problems {
			problem := diagnosedSetting(t, tc.diagnosis, name).Problem
			if !strings.Contains(problem, want) || (want == "" && problem != "") {
				t.Errorf("%s problem = %q, want %q", name, problem, want)
			}
		}
	}
}

func TestDiagnoseReadsTheFilesALoadWould(t *testing.T) {
	directory := t.TempDir()
	stored := writeDoctorFile(t, directory, "stored.yaml", "hosts: [broken\n")
	system := writeDoctorFile(t, directory, "system.yaml", "requre_keyring: true\n")

	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BB_CONFIG_PATH", stored)
	t.Setenv("BB_WORKSPACE_CONFIG_PATH", directory)
	t.Setenv("BB_SYSTEM_CONFIG_PATH", system)

	diagnosis := Diagnose(DiagnoseInput{})

	// BB_DISABLE_STORED_CONFIG promises the file is not read, so its damage is
	// no obstacle.
	storedFile := diagnosedFile(t, diagnosis, TierStored)
	if storedFile.Read || !storedFile.Exists || !storedFile.Valid() || storedFile.PathFrom != "BB_CONFIG_PATH" ||
		!strings.Contains(storedFile.NotRead, "BB_DISABLE_STORED_CONFIG") {
		t.Errorf("a disabled stored file: %+v", storedFile)
	}

	workspaceFile := diagnosedFile(t, diagnosis, TierWorkspace)
	if workspaceFile.Valid() || !strings.Contains(workspaceFile.Problem, "could not be read") || workspaceFile.PathFrom != "BB_WORKSPACE_CONFIG_PATH" {
		t.Errorf("a workspace path naming a directory: %+v", workspaceFile)
	}

	systemFile := diagnosedFile(t, diagnosis, TierSystem)
	if systemFile.PathFrom != "BB_SYSTEM_CONFIG_PATH" || len(systemFile.Violations) != 1 {
		t.Errorf("the system file: %+v", systemFile)
	}
}

func TestDiagnoseNamesTheDotenvFileThatSuppliedAVariable(t *testing.T) {
	directory := t.TempDir()
	if err := os.Mkdir(filepath.Join(directory, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	dotenv := writeDoctorFile(t, directory, ".env", "BB_RETRY_BACKOFF=2s\n")
	workspace := writeDoctorFile(t, directory, filepath.Join(".bb", "config.yaml"), "project_key: DOT\n")

	// Unset, so the .env file is what supplies it; t.Setenv puts it back.
	t.Setenv("BB_RETRY_BACKOFF", "")
	if err := os.Unsetenv("BB_RETRY_BACKOFF"); err != nil {
		t.Fatal(err)
	}
	t.Chdir(directory)

	diagnosis := Diagnose(DiagnoseInput{})

	backoff := diagnosedSetting(t, diagnosis, "retry_backoff")
	if backoff.Value != "2s" || backoff.Source.Kind != SourceDotenv || !sameDoctorFile(t, backoff.Source.Path, dotenv) {
		t.Errorf("retry_backoff = %+v, want 2s from %s", backoff, dotenv)
	}

	workspaceFile := diagnosedFile(t, diagnosis, TierWorkspace)
	if workspaceFile.PathFrom != "search" || !sameDoctorFile(t, workspaceFile.Path, workspace) {
		t.Errorf("workspace file = %+v, want %s found by search", workspaceFile, workspace)
	}
	if key := diagnosedSetting(t, diagnosis, "project_key"); key.Value != "DOT" || key.Source.Kind != TierWorkspace {
		t.Errorf("project_key = %+v", key)
	}
}

func sameDoctorFile(t *testing.T, got, want string) bool {
	t.Helper()

	gotInfo, err := os.Stat(got)
	if err != nil {
		return false
	}
	wantInfo, err := os.Stat(want)
	if err != nil {
		t.Fatal(err)
	}

	return os.SameFile(gotInfo, wantInfo)
}

// TestDiagnoseAgreesWithTheLoader holds bb doctor to the code it describes.
//
// Its resolution is a second reading of precedence spread over
// LoadWithOverrides, ResolveUpdateBaseURL, LoadPolicy and RequireKeyring, and a
// second reading drifts. This loads the same configuration both ways and
// compares every value the loaders expose.
func TestDiagnoseAgreesWithTheLoader(t *testing.T) {
	directory := t.TempDir()
	caFile := writeDoctorFile(t, directory, "ca.pem", "pem")
	stored := writeDoctorFile(t, directory, "stored.yaml", strings.Join([]string{
		"default_host: corp",
		"hosts:",
		"  corp:",
		"    url: https://bitbucket.example.com",
		"    username: alice",
		"insecure_secrets:",
		"  corp:",
		"    password: plaintext-password",
		"update_base_url: https://user-mirror.example.com",
	}, "\n"))
	workspace := writeDoctorFile(t, directory, "workspace.yaml", "project_key: WSP\n")
	system := writeDoctorFile(t, directory, "system.yaml", strings.Join([]string{
		"require_keyring: false",
		"policies:",
		"  ca_file: " + caFile,
		"  allowed_hosts: [bitbucket.example.com]",
		"  update_base_url: https://system-mirror.example.com",
	}, "\n"))

	for name, value := range map[string]string{
		"BB_DISABLE_STORED_CONFIG": "",
		"BB_CONFIG_PATH":           stored,
		"BB_WORKSPACE_CONFIG_PATH": workspace,
		"BB_SYSTEM_CONFIG_PATH":    system,
		"BB_REQUEST_TIMEOUT":       "7s",
		"BB_RETRY_COUNT":           "5",
		"BB_RETRY_BACKOFF":         "1s",
		"BB_LOG_LEVEL":             "debug",
		"BB_LOG_FORMAT":            "",
		"BB_UPDATE_BASE_URL":       "",
		"BB_REQUIRE_KEYRING":       "",
		"BB_CA_FILE":               "",
		"BB_INSECURE_SKIP_VERIFY":  "",
		"BB_CLIENT_CERT":           "",
		"BB_CLIENT_KEY":            "",
	} {
		t.Setenv(name, value)
	}

	assertDiagnosisAgreesWithTheLoader(t)

	// The environment names the host, and the stored file is switched off.
	t.Setenv("BITBUCKET_URL", "https://bitbucket.example.com/")
	t.Setenv("BITBUCKET_TOKEN", "environment-token")
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	assertDiagnosisAgreesWithTheLoader(t)
}

func assertDiagnosisAgreesWithTheLoader(t *testing.T) {
	t.Helper()

	loaded, err := LoadWithOverrides(Overrides{})
	if err != nil {
		t.Fatalf("the configuration did not load: %v", err)
	}
	updateBaseURL, err := ResolveUpdateBaseURL("")
	if err != nil {
		t.Fatal(err)
	}
	policy, err := LoadPolicy()
	if err != nil {
		t.Fatal(err)
	}
	requireKeyring, err := RequireKeyring()
	if err != nil {
		t.Fatal(err)
	}

	diagnosis := Diagnose(DiagnoseInput{})

	for name, want := range map[string]string{
		"host":                 loaded.BitbucketURL,
		"project_key":          loaded.ProjectKey,
		"username":             loaded.BitbucketUsername,
		"ca_file":              loaded.CAFile,
		"insecure_skip_verify": strconv.FormatBool(loaded.InsecureSkipVerify),
		"request_timeout":      loaded.RequestTimeout.String(),
		"retry_count":          strconv.Itoa(loaded.RetryCount),
		"retry_backoff":        loaded.RetryBackoff.String(),
		"log_level":            loaded.LogLevel,
		"log_format":           loaded.LogFormat,
		"update_base_url":      updateBaseURL,
		"allowed_hosts":        strings.Join(policy.AllowedHosts, ", "),
		"require_keyring":      strconv.FormatBool(requireKeyring),
	} {
		if got := diagnosedSetting(t, diagnosis, name).Value; got != want {
			t.Errorf("%s: bb doctor says %q, the loader %q", name, got, want)
		}
	}

	for name, held := range map[string]string{"token": loaded.BitbucketToken, "password": loaded.BitbucketPassword} {
		if configured := diagnosedSetting(t, diagnosis, name).Configured; configured != (held != "") {
			t.Errorf("%s: bb doctor says configured=%v, the loader holds one=%v", name, configured, held != "")
		}
	}
	if plaintext := diagnosedSetting(t, diagnosis, "password").Source.Kind == TierStored; plaintext != loaded.UsedInsecureStorage {
		t.Errorf("password from the stored file=%v, the loader used plaintext storage=%v", plaintext, loaded.UsedInsecureStorage)
	}
}

func TestAnUnreadableConfigurationPointsAtBBDoctor(t *testing.T) {
	t.Parallel()

	for _, pathOf := range []func() (string, error){
		func() (string, error) { return "/home/alice/.config/bb/config.yaml", nil },
		func() (string, error) { return "", errors.New("no home directory") },
	} {
		message := apperrors.MessageOf(unreadableConfig(pathOf, "stored configuration", errors.New("yaml: line 5")))
		if !strings.Contains(message, "run 'bb doctor' to list every problem in it") {
			t.Errorf("the message does not point at bb doctor: %s", message)
		}
	}
}
