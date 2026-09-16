//go:build windows

package config

import (
	"errors"
	"strings"
	"testing"
)

type mockRegistryReader struct {
	integers map[string]uint64
	strings  map[string]string
	strLists map[string][]string
}

func (m *mockRegistryReader) GetIntegerValue(name string) (uint64, uint32, error) {
	if val, ok := m.integers[name]; ok {
		return val, 0, nil
	}
	return 0, 0, errors.New("value not found")
}

func (m *mockRegistryReader) GetStringValue(name string) (string, uint32, error) {
	if val, ok := m.strings[name]; ok {
		return val, 0, nil
	}
	return "", 0, errors.New("value not found")
}

func (m *mockRegistryReader) GetStringsValue(name string) ([]string, uint32, error) {
	if val, ok := m.strLists[name]; ok {
		return val, 0, nil
	}
	return nil, 0, errors.New("value not found")
}

func TestParseRegistryPolicy(t *testing.T) {
	t.Parallel()

	readerInt := &mockRegistryReader{
		integers: map[string]uint64{
			"RequireKeyring":          1,
			"AllowInsecureSkipVerify": 0,
			"DisableUpdate":           1,
		},
		strings: map[string]string{
			"CAFile":        `C:\ProgramData\bb\ca.crt`,
			"UpdateBaseURL": "https://releases.internal/bb",
		},
		strLists: map[string][]string{
			"AllowedHosts": {"https://bitbucket1.internal", "https://bitbucket2.internal"},
		},
	}

	p1, _ := parseRegistryPolicy(readerInt)
	if p1.RequireKeyring == nil || !*p1.RequireKeyring {
		t.Errorf("expected RequireKeyring=true, got %v", p1.RequireKeyring)
	}
	if p1.AllowInsecureSkipVerify == nil || *p1.AllowInsecureSkipVerify {
		t.Errorf("expected AllowInsecureSkipVerify=false, got %v", p1.AllowInsecureSkipVerify)
	}
	if p1.DisableUpdate == nil || !*p1.DisableUpdate {
		t.Errorf("expected DisableUpdate=true, got %v", p1.DisableUpdate)
	}
	if p1.CAFile != `C:\ProgramData\bb\ca.crt` {
		t.Errorf("expected CAFile, got %s", p1.CAFile)
	}
	if p1.UpdateBaseURL != "https://releases.internal/bb" {
		t.Errorf("expected UpdateBaseURL, got %s", p1.UpdateBaseURL)
	}
	if len(p1.AllowedHosts) != 2 {
		t.Errorf("expected 2 AllowedHosts, got %v", p1.AllowedHosts)
	}

	readerStr := &mockRegistryReader{
		integers: map[string]uint64{},
		strings: map[string]string{
			"RequireKeyring":          "true",
			"AllowInsecureSkipVerify": "false",
			"DisableUpdate":           "1",
			"AllowedHosts":            "https://hostA.internal, https://hostB.internal",
		},
	}

	p2, _ := parseRegistryPolicy(readerStr)
	if p2.RequireKeyring == nil || !*p2.RequireKeyring {
		t.Errorf("expected RequireKeyring=true from string, got %v", p2.RequireKeyring)
	}
	if p2.AllowInsecureSkipVerify == nil || *p2.AllowInsecureSkipVerify {
		t.Errorf("expected AllowInsecureSkipVerify=false from string, got %v", p2.AllowInsecureSkipVerify)
	}
	if p2.DisableUpdate == nil || !*p2.DisableUpdate {
		t.Errorf("expected DisableUpdate=true from string, got %v", p2.DisableUpdate)
	}
	if len(p2.AllowedHosts) != 2 || p2.AllowedHosts[0] != "https://hostA.internal" || p2.AllowedHosts[1] != "https://hostB.internal" {
		t.Errorf("expected parsed comma-separated hosts, got %v", p2.AllowedHosts)
	}
}

func TestParseRegistryPolicyUpdateTrust(t *testing.T) {
	t.Parallel()

	reader := &mockRegistryReader{
		integers: map[string]uint64{
			"AllowUnverifiedUpdate": 1,
			"AllowHTTPUpdate":       0,
		},
		strings: map[string]string{
			"UpdateTrustedRoot":       `C:\ProgramData\bb\trusted_root.json`,
			"UpdateTUFURL":            "https://artifactory.internal/tuf",
			"UpdateSignatureIdentity": "https://github.com/corp/bb/.github/workflows/mirror.yml@refs/heads/main",
			"UpdateSignatureIssuer":   "https://fulcio.internal",
		},
	}

	policy, _ := parseRegistryPolicy(reader)
	if policy.UpdateTrustedRoot != `C:\ProgramData\bb\trusted_root.json` {
		t.Errorf("expected UpdateTrustedRoot, got %s", policy.UpdateTrustedRoot)
	}
	if policy.UpdateTUFURL != "https://artifactory.internal/tuf" {
		t.Errorf("expected UpdateTUFURL, got %s", policy.UpdateTUFURL)
	}
	if policy.UpdateSignatureIdentity == "" || policy.UpdateSignatureIssuer != "https://fulcio.internal" {
		t.Errorf("expected signer overrides, got %s / %s", policy.UpdateSignatureIdentity, policy.UpdateSignatureIssuer)
	}
	if policy.AllowUnverifiedUpdate == nil || !*policy.AllowUnverifiedUpdate {
		t.Errorf("expected AllowUnverifiedUpdate=true, got %v", policy.AllowUnverifiedUpdate)
	}
	if policy.AllowHTTPUpdate == nil || *policy.AllowHTTPUpdate {
		t.Errorf("expected AllowHTTPUpdate=false, got %v", policy.AllowHTTPUpdate)
	}

	stringForm := &mockRegistryReader{
		strings: map[string]string{
			"AllowUnverifiedUpdate": "false",
			"AllowHTTPUpdate":       "true",
		},
	}
	stringPolicy, _ := parseRegistryPolicy(stringForm)
	if stringPolicy.AllowUnverifiedUpdate == nil || *stringPolicy.AllowUnverifiedUpdate {
		t.Errorf("expected AllowUnverifiedUpdate=false from string value, got %v", stringPolicy.AllowUnverifiedUpdate)
	}
	if stringPolicy.AllowHTTPUpdate == nil || !*stringPolicy.AllowHTTPUpdate {
		t.Errorf("expected AllowHTTPUpdate=true from string value, got %v", stringPolicy.AllowHTTPUpdate)
	}
}

// TestRegistryPolicyDoesNotFailOpen is an administrator's control disappearing
// without a word.
//
// A REG_SZ that strconv.ParseBool rejected was dropped, so DisableUpdate=yes
// left self-update enabled and AllowHTTPUpdate=no left plain HTTP permitted,
// with the policy page saying the opposite. A damaged policy file fails closed;
// the registry has to behave the same way.
func TestRegistryPolicyDoesNotFailOpen(t *testing.T) {
	t.Parallel()

	t.Run("the spellings a policy is written in", func(t *testing.T) {
		t.Parallel()

		policy, problems := parseRegistryPolicy(&mockRegistryReader{
			strings: map[string]string{
				"RequireKeyring":          "yes",
				"AllowInsecureSkipVerify": "off",
				"DisableUpdate":           "Enabled",
				"AllowHTTPUpdate":         "No",
				"AllowUnverifiedUpdate":   "disabled",
			},
		})

		if len(problems) != 0 {
			t.Fatalf("a readable policy reported problems: %+v", problems)
		}
		for name, got := range map[string]*bool{
			"RequireKeyring":          policy.RequireKeyring,
			"AllowInsecureSkipVerify": policy.AllowInsecureSkipVerify,
			"DisableUpdate":           policy.DisableUpdate,
			"AllowHTTPUpdate":         policy.AllowHTTPUpdate,
			"AllowUnverifiedUpdate":   policy.AllowUnverifiedUpdate,
		} {
			if got == nil {
				t.Errorf("%s was dropped", name)
			}
		}
		if policy.RequireKeyring == nil || !*policy.RequireKeyring {
			t.Error("RequireKeyring=yes did not read as true")
		}
		if policy.DisableUpdate == nil || !*policy.DisableUpdate {
			t.Error("DisableUpdate=Enabled did not read as true")
		}
		if policy.AllowHTTPUpdate == nil || *policy.AllowHTTPUpdate {
			t.Error("AllowHTTPUpdate=No did not read as false")
		}
	})

	t.Run("a value that cannot be read at all", func(t *testing.T) {
		t.Parallel()

		policy, problems := parseRegistryPolicy(&mockRegistryReader{
			strings: map[string]string{
				"DisableUpdate":   "sometimes",
				"AllowHTTPUpdate": "maybe",
			},
		})

		if policy.DisableUpdate == nil || !*policy.DisableUpdate {
			t.Error("an unreadable DisableUpdate left self-update enabled")
		}
		if policy.AllowHTTPUpdate == nil || *policy.AllowHTTPUpdate {
			t.Error("an unreadable AllowHTTPUpdate left plain HTTP permitted")
		}

		if len(problems) != 2 {
			t.Fatalf("expected both values reported, got %+v", problems)
		}
		for _, problem := range problems {
			if problem.Name != "DisableUpdate" && problem.Name != "AllowHTTPUpdate" {
				t.Errorf("unexpected problem: %+v", problem)
			}
			for _, want := range []string{problem.Name, "is in force"} {
				if !strings.Contains(problem.Message, want) {
					t.Errorf("the message does not say %q: %s", want, problem.Message)
				}
			}
		}
	})

	t.Run("a DWORD is still read as a number", func(t *testing.T) {
		t.Parallel()

		policy, problems := parseRegistryPolicy(&mockRegistryReader{
			integers: map[string]uint64{"DisableUpdate": 0, "RequireKeyring": 1},
		})

		if len(problems) != 0 {
			t.Fatalf("a DWORD policy reported problems: %+v", problems)
		}
		if policy.DisableUpdate == nil || *policy.DisableUpdate {
			t.Error("DisableUpdate=0 did not read as false")
		}
		if policy.RequireKeyring == nil || !*policy.RequireKeyring {
			t.Error("RequireKeyring=1 did not read as true")
		}
	})
}
