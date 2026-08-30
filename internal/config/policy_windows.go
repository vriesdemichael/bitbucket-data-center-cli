//go:build windows

package config

import (
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const registryPolicyKey = `Software\Policies\bb`

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
	return "Windows registry policy HKEY_LOCAL_MACHINE\\" + registryPolicyKey
}

func loadPlatformPolicy() PolicyConfig {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, registryPolicyKey, registry.QUERY_VALUE)
	if err != nil {
		return PolicyConfig{}
	}
	defer k.Close()

	return parseRegistryPolicy(k)
}

func parseRegistryPolicy(k registryReader) PolicyConfig {
	var policy PolicyConfig

	if val, _, err := k.GetIntegerValue("RequireKeyring"); err == nil {
		b := val != 0
		policy.RequireKeyring = &b
	} else if strVal, _, err := k.GetStringValue("RequireKeyring"); err == nil {
		if b, parseErr := strconv.ParseBool(strings.TrimSpace(strVal)); parseErr == nil {
			policy.RequireKeyring = &b
		}
	}

	if val, _, err := k.GetStringValue("CAFile"); err == nil && strings.TrimSpace(val) != "" {
		policy.CAFile = strings.TrimSpace(val)
	}

	if vals, _, err := k.GetStringsValue("AllowedHosts"); err == nil && len(vals) > 0 {
		policy.AllowedHosts = vals
	} else if strVal, _, err := k.GetStringValue("AllowedHosts"); err == nil && strings.TrimSpace(strVal) != "" {
		parts := strings.Split(strVal, ",")
		cleaned := make([]string, 0, len(parts))
		for _, p := range parts {
			if t := strings.TrimSpace(p); t != "" {
				cleaned = append(cleaned, t)
			}
		}
		if len(cleaned) > 0 {
			policy.AllowedHosts = cleaned
		}
	}

	if val, _, err := k.GetIntegerValue("AllowInsecureSkipVerify"); err == nil {
		b := val != 0
		policy.AllowInsecureSkipVerify = &b
	} else if strVal, _, err := k.GetStringValue("AllowInsecureSkipVerify"); err == nil {
		if b, parseErr := strconv.ParseBool(strings.TrimSpace(strVal)); parseErr == nil {
			policy.AllowInsecureSkipVerify = &b
		}
	}

	if val, _, err := k.GetIntegerValue("DisableUpdate"); err == nil {
		b := val != 0
		policy.DisableUpdate = &b
	} else if strVal, _, err := k.GetStringValue("DisableUpdate"); err == nil {
		if b, parseErr := strconv.ParseBool(strings.TrimSpace(strVal)); parseErr == nil {
			policy.DisableUpdate = &b
		}
	}

	if val, _, err := k.GetStringValue("UpdateBaseURL"); err == nil && strings.TrimSpace(val) != "" {
		policy.UpdateBaseURL = strings.TrimSpace(val)
	}

	if val, _, err := k.GetStringValue("UpdateTrustedRoot"); err == nil && strings.TrimSpace(val) != "" {
		policy.UpdateTrustedRoot = strings.TrimSpace(val)
	}

	if val, _, err := k.GetStringValue("UpdateTUFURL"); err == nil && strings.TrimSpace(val) != "" {
		policy.UpdateTUFURL = strings.TrimSpace(val)
	}

	if val, _, err := k.GetStringValue("UpdateSignatureIdentity"); err == nil && strings.TrimSpace(val) != "" {
		policy.UpdateSignatureIdentity = strings.TrimSpace(val)
	}

	if val, _, err := k.GetStringValue("UpdateSignatureIssuer"); err == nil && strings.TrimSpace(val) != "" {
		policy.UpdateSignatureIssuer = strings.TrimSpace(val)
	}

	if val, _, err := k.GetIntegerValue("AllowUnverifiedUpdate"); err == nil {
		b := val != 0
		policy.AllowUnverifiedUpdate = &b
	} else if strVal, _, err := k.GetStringValue("AllowUnverifiedUpdate"); err == nil {
		if b, parseErr := strconv.ParseBool(strings.TrimSpace(strVal)); parseErr == nil {
			policy.AllowUnverifiedUpdate = &b
		}
	}

	return policy
}
