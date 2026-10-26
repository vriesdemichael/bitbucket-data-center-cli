//go:build windows

package config

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

type registryReader interface {
	GetIntegerValue(name string) (uint64, uint32, error)
	GetStringValue(name string) (string, uint32, error)
	GetStringsValue(name string) ([]string, uint32, error)
}

// machineConfigPath is the location of the administrative policy file, with the
// ProgramData directory taken from Windows itself.
//
// os.Getenv("ProgramData") answers the same on a healthy system, but the
// environment belongs to whoever launched the process, and this directory
// decides where policy is read from: a caller who can set ProgramData could
// otherwise point bb at a policy file of their own making. KnownFolderPath asks
// the OS instead.
func machineConfigPath() string {
	programData := `C:\ProgramData`
	if resolved, err := windows.KnownFolderPath(windows.FOLDERID_ProgramData, 0); err == nil {
		if trimmed := strings.TrimSpace(resolved); trimmed != "" {
			programData = trimmed
		}
	}
	return filepath.Join(programData, "bb", "config.yaml")
}

// platformPolicyDescription names the registry key policy is read from, so a
// message about a setting can say where to go and change it.
func platformPolicyDescription() string {
	return "Windows registry policy " + registryPolicyLocation
}

func loadPlatformPolicy() PolicyConfig {
	policy, _ := loadPlatformPolicyWithProblems()

	return policy
}

func loadPlatformPolicyWithProblems() (PolicyConfig, []PolicyProblem) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, RegistryPolicyKey, registry.QUERY_VALUE)
	if err != nil {
		return PolicyConfig{}, nil
	}
	defer func() { _ = k.Close() }()

	return parseRegistryPolicy(k)
}

// registryBool reads a boolean policy value.
//
// A value that is present but unreadable is not the same as one that is
// absent: an administrator set it, and treating it as unset switches the
// control off without a word. DWORD or a string spelling is accepted, and
// anything else takes the restrictive side and is reported.
func registryBool(k registryReader, name string, restrictive bool) (*bool, *PolicyProblem) {
	if value, _, err := k.GetIntegerValue(name); err == nil {
		parsed := value != 0

		return &parsed, nil
	}

	raw, _, err := k.GetStringValue(name)
	if err != nil {
		return nil, nil
	}
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}

	if parsed, ok := parsePolicyBool(raw); ok {
		return &parsed, nil
	}

	inForce := restrictive

	return &inForce, &PolicyProblem{
		Name: name,
		Message: fmt.Sprintf(
			"%s is set to %q in %s, which is not true or false; %s is in force until it is corrected",
			name, strings.TrimSpace(raw), platformPolicyDescription(), strconv.FormatBool(restrictive),
		),
	}
}

func parseRegistryPolicy(k registryReader) (PolicyConfig, []PolicyProblem) {
	var policy PolicyConfig
	var problems []PolicyProblem

	for _, value := range RegistryValues {
		switch value.Kind {
		case RegistryBoolean:
			parsed, problem := registryBool(k, value.Name, value.restrictive)
			if parsed != nil {
				*value.boolean(&policy) = parsed
			}
			if problem != nil {
				problems = append(problems, *problem)
			}
		case RegistryString:
			if val, _, err := k.GetStringValue(value.Name); err == nil && strings.TrimSpace(val) != "" {
				*value.text(&policy) = strings.TrimSpace(val)
			}
		case RegistryList:
			if list := registryList(k, value.Name); len(list) > 0 {
				*value.list(&policy) = list
			}
		}
	}

	return policy, problems
}

// registryList reads a list policy value: a REG_MULTI_SZ, or a REG_SZ of
// comma-separated entries.
func registryList(k registryReader, name string) []string {
	if vals, _, err := k.GetStringsValue(name); err == nil && len(vals) > 0 {
		return vals
	}

	strVal, _, err := k.GetStringValue(name)
	if err != nil || strings.TrimSpace(strVal) == "" {
		return nil
	}
	parts := strings.Split(strVal, ",")
	cleaned := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			cleaned = append(cleaned, t)
		}
	}

	return cleaned
}
