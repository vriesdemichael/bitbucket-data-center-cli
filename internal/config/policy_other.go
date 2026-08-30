//go:build !windows

package config

func loadPlatformPolicy() PolicyConfig {
	return PolicyConfig{}
}

// machineConfigPath is the fixed location of the administrative policy file.
func machineConfigPath() string {
	return "/etc/bb/config.yaml"
}

// platformPolicyDescription names the platform-native policy store. Only
// Windows has one, so this reports that there is nothing to name and the
// caller falls back to the system configuration file.
func platformPolicyDescription() string {
	return ""
}
