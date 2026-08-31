package config

import (
	"strings"
	"testing"
	"time"
)

// TestAFlagIsBlamedForItsOwnValue is the behaviour issue #458's comment asked
// for, and the half that threading values alone does not deliver.
//
// A flag used to reach the config layer by being written into BB_*, so the
// origin was lost and validation reported a variable the user never set. Every
// case here was produced by passing a flag.
func TestAFlagIsBlamedForItsOwnValue(t *testing.T) {
	t.Parallel()

	timeout := "0s"
	badTimeout := "not-a-duration"
	negative := -5
	missing := "/nonexistent.pem"
	backoff := "0s"

	cases := []struct {
		name      string
		overrides Overrides
		wants     string
	}{
		{
			name:      "a timeout that is not positive",
			overrides: Overrides{RequestTimeout: &timeout},
			wants:     "--request-timeout",
		},
		{
			name:      "a timeout that does not parse",
			overrides: Overrides{RequestTimeout: &badTimeout},
			wants:     "--request-timeout",
		},
		{
			name:      "a negative retry count",
			overrides: Overrides{RetryCount: &negative},
			wants:     "--retry-count",
		},
		{
			name:      "a backoff that is not positive",
			overrides: Overrides{RetryBackoff: &backoff},
			wants:     "--retry-backoff",
		},
		{
			name:      "a CA bundle that is not there",
			overrides: Overrides{CAFile: &missing},
			wants:     "--ca-file",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			overrides := testCase.overrides
			overrides.Host = "https://bitbucket.example.com"

			_, err := LoadWithOverrides(overrides)
			if err == nil {
				t.Fatalf("expected a validation failure naming %s", testCase.wants)
			}
			if !strings.Contains(err.Error(), testCase.wants) {
				t.Errorf("error = %q, want it to name %q", err.Error(), testCase.wants)
			}
			// The variable is what the user did not touch. Naming it is the
			// defect this test exists for.
			environment := strings.ToUpper(strings.TrimPrefix(testCase.wants, "--"))
			if strings.Contains(err.Error(), "BB_"+strings.ReplaceAll(environment, "-", "_")) {
				t.Errorf("error = %q, still blames the environment variable", err.Error())
			}
		})
	}
}

// TestOneMissingHalfNamesEachSourceSeparately is the case worth having a test
// for on its own.
//
// Passing --client-cert without --client-key means the key would come from the
// environment, so the message names one of each. Reporting both as variables,
// or both as flags, would be wrong in opposite directions.
func TestOneMissingHalfNamesEachSourceSeparately(t *testing.T) {
	t.Parallel()

	cert := "/nonexistent-cert.pem"
	_, err := LoadWithOverrides(Overrides{
		Host:       "https://bitbucket.example.com",
		ClientCert: &cert,
	})
	if err == nil {
		t.Fatal("expected a validation failure for a cert without a key")
	}

	message := err.Error()
	if !strings.Contains(message, "--client-cert") {
		t.Errorf("error = %q, want it to name the flag that was passed", message)
	}
	if !strings.Contains(message, "BB_CLIENT_KEY") {
		t.Errorf("error = %q, want it to name the variable the key would come from", message)
	}
}

// TestResolutionPrefersTheOverrideAndRecordsIt covers the helpers directly.
func TestResolutionPrefersTheOverrideAndRecordsIt(t *testing.T) {
	t.Parallel()

	t.Run("a nil override leaves the environment its slot", func(t *testing.T) {
		t.Parallel()

		sourced := map[string]bool{}
		if got := resolveString(sourced, settingCAFile, nil); got != "" {
			t.Errorf("value = %q, want the environment's (empty here)", got)
		}
		if sourced[settingCAFile.environment] {
			t.Error("a nil override was recorded as flag-sourced")
		}
		if name := nameOf(sourced, settingCAFile); name != "BB_CA_FILE" {
			t.Errorf("name = %q, want the variable", name)
		}
	})

	t.Run("an override wins and is recorded", func(t *testing.T) {
		t.Parallel()

		sourced := map[string]bool{}
		value := "  /tmp/ca.pem  "
		if got := resolveString(sourced, settingCAFile, &value); got != "/tmp/ca.pem" {
			t.Errorf("value = %q, want it trimmed", got)
		}
		if !sourced[settingCAFile.environment] {
			t.Error("an override was not recorded as flag-sourced")
		}
		if name := nameOf(sourced, settingCAFile); name != "--ca-file" {
			t.Errorf("name = %q, want the flag", name)
		}
	})

	t.Run("an empty override is a decision, not an absence", func(t *testing.T) {
		t.Parallel()

		sourced := map[string]bool{}
		empty := ""
		got, err := resolveDuration(sourced, settingRequestTimeout, &empty, 7*time.Second)
		if err != nil {
			t.Fatalf("resolving an empty override failed: %v", err)
		}
		if got != 7*time.Second {
			t.Errorf("duration = %v, want the fallback", got)
		}
		if !sourced[settingRequestTimeout.environment] {
			t.Error("an empty override was not recorded; a later failure would blame the variable")
		}
	})

	t.Run("integers and booleans record too", func(t *testing.T) {
		t.Parallel()

		sourced := map[string]bool{}
		count := 3
		if got, err := resolveInt(sourced, settingRetryCount, &count, 9); err != nil || got != 3 {
			t.Errorf("resolveInt = %v, %v; want 3", got, err)
		}
		skip := true
		if got, err := resolveBool(sourced, settingInsecureSkipVerify, &skip, false); err != nil || !got {
			t.Errorf("resolveBool = %v, %v; want true", got, err)
		}
		for _, setting := range []runtimeSetting{settingRetryCount, settingInsecureSkipVerify} {
			if !sourced[setting.environment] {
				t.Errorf("%s was not recorded as flag-sourced", setting.environment)
			}
		}
	})
}

// TestAnInferredContextReachesTheResolvedConfiguration closes a gap a field
// assertion alone would not.
//
// Overrides.ProjectKey and RepoSlug were written by the inference path and
// merged into every load, and nothing read them: LoadWithOverrides still
// resolved the project from BITBUCKET_PROJECT_KEY alone. A test asserting the
// field would have passed with the whole path deleted, which is why this
// asserts the configuration that comes out.
func TestAnInferredContextReachesTheResolvedConfiguration(t *testing.T) {
	t.Parallel()

	cfg, err := LoadWithOverrides(Overrides{
		Host:       "https://bitbucket.example.com",
		ProjectKey: "INFERRED",
		RepoSlug:   "from-remote",
	})
	if err != nil {
		t.Fatalf("loading with an inferred context failed: %v", err)
	}

	if cfg.ProjectKey != "INFERRED" {
		t.Errorf("project key = %q, want the inferred value to outrank the environment", cfg.ProjectKey)
	}
	if cfg.RepoSlug != "from-remote" {
		t.Errorf("repo slug = %q, want the inferred value", cfg.RepoSlug)
	}
}
