//go:build !windows

package config

func loadPlatformPolicy() PolicyConfig {
	return PolicyConfig{}
}
