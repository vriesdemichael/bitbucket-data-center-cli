package config

import (
	"strings"
	"testing"
)

// TestDoctorReportsAnUpdateMirrorBBUpdateWouldRefuse: bb doctor says what the
// next bb update would refuse, from what it can see without --allow-http.
func TestDoctorReportsAnUpdateMirrorBBUpdateWouldRefuse(t *testing.T) {
	t.Parallel()

	plainMirror := map[string]string{TierStored: "update_base_url: http://mirror.example.com\n"}

	cases := map[string]struct {
		files       map[string]string
		environment map[string]string
		registry    PolicyConfig
		problem     string
	}{
		"an https mirror": {
			files: map[string]string{TierStored: "update_base_url: https://mirror.example.com\n"},
		},
		"a plain-HTTP mirror nobody opted into": {
			files:   plainMirror,
			problem: "pass --allow-http or set BB_ALLOW_HTTP_UPDATE=1",
		},
		"a plain-HTTP mirror with BB_ALLOW_HTTP_UPDATE": {
			files:       plainMirror,
			environment: map[string]string{"BB_ALLOW_HTTP_UPDATE": "1"},
		},
		"a plain-HTTP mirror policy permits": {
			files:    plainMirror,
			registry: PolicyConfig{AllowHTTPUpdate: httpOptIn(true)},
		},
		"a plain-HTTP mirror policy forbids, despite BB_ALLOW_HTTP_UPDATE": {
			files: map[string]string{
				TierStored: "update_base_url: http://mirror.example.com\n",
				TierSystem: "policies:\n  allow_http_update: false\n",
			},
			environment: map[string]string{"BB_ALLOW_HTTP_UPDATE": "1"},
			problem:     "which administrative policy forbids (allow_http_update)",
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			inputs := doctorInputs(t, testCase.files, testCase.environment)
			inputs.platformPolicy = testCase.registry

			setting := diagnosedSetting(t, diagnose(inputs), "update_base_url")
			switch {
			case testCase.problem == "" && setting.Problem != "":
				t.Errorf("update_base_url %q: unexpected problem %q", setting.Value, setting.Problem)
			case testCase.problem != "" && !strings.Contains(setting.Problem, testCase.problem):
				t.Errorf("update_base_url %q: problem = %q, want one containing %q", setting.Value, setting.Problem, testCase.problem)
			}
		})
	}
}

func TestDoctorShowsWhereAllowHTTPUpdateComesFrom(t *testing.T) {
	t.Parallel()

	inputs := doctorInputs(t, map[string]string{TierSystem: "policies:\n  allow_http_update: false\n"}, nil)
	inputs.platformPolicy = PolicyConfig{AllowHTTPUpdate: httpOptIn(true)}

	setting := diagnosedSetting(t, diagnose(inputs), "allow_http_update")
	if setting.Value != "true" || setting.Source.Kind != SourceRegistry || setting.Source.Name != "AllowHTTPUpdate" {
		t.Errorf("allow_http_update = %q from %+v, want true from the registry, which is merged last", setting.Value, setting.Source)
	}
}
