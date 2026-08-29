//go:build !windows

package config

func loadPlatformPolicy() PolicyConfig {
	return PolicyConfig{}
}

// machineConfigPath is the fixed location of the administrative policy file.
func machineConfigPath() string {
	return "/etc/bb/config.yaml"
}
