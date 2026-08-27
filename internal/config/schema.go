package config

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v6"
	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	"gopkg.in/yaml.v3"
)

const ConfigSchemaID = "https://raw.githubusercontent.com/vriesdemichael/bitbucket-data-center-cli/main/docs/reference/schemas/config.schema.json"
const jsonSchemaVersion = "https://json-schema.org/draft/2020-12/schema"

// ConfigJSONSchema returns the JSON Schema definition for bb configuration files.
func ConfigJSONSchema() map[string]any {
	policyProps := map[string]any{
		"require_keyring": map[string]any{
			"type":        "boolean",
			"description": "Mandate OS keyring storage for credentials and prohibit plaintext config file fallback.",
		},
		"ca_file": map[string]any{
			"type":        "string",
			"description": "Absolute or relative path to PEM Root CA certificate bundle.",
		},
		"allowed_hosts": map[string]any{
			"type":        "array",
			"description": "Whitelist of permitted Bitbucket Server / Data Center instance URLs or hostnames.",
			"items": map[string]any{
				"type": "string",
			},
		},
		"allow_insecure_skip_verify": map[string]any{
			"type":        "boolean",
			"description": "Control whether insecure TLS verification (--insecure-skip-verify) is permitted. When false, insecure TLS cannot be enabled.",
		},
		"disable_update": map[string]any{
			"type":        "boolean",
			"description": "Disable CLI self-update command (bb update) machine-wide.",
		},
		"update_base_url": map[string]any{
			"type":        "string",
			"description": "Base URL of internal release manifest and asset mirror.",
		},
	}

	hostProfileSchema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"url"},
		"properties": map[string]any{
			"url": map[string]any{
				"type":        "string",
				"description": "Bitbucket Server or Data Center base URL.",
			},
			"auth_mode": map[string]any{
				"type":        "string",
				"enum":        []string{"token", "basic"},
				"description": "Authentication mechanism for the host.",
			},
			"username": map[string]any{
				"type":        "string",
				"description": "Username when using HTTP basic authentication.",
			},
			"aliases": map[string]any{
				"type":        "array",
				"description": "Host aliases for remote URL matching.",
				"items": map[string]any{
					"type": "string",
				},
			},
		},
	}

	insecureSecretSchema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"token": map[string]any{
				"type":        "string",
				"description": "Plaintext token storage fallback.",
			},
			"password": map[string]any{
				"type":        "string",
				"description": "Plaintext password storage fallback.",
			},
		},
	}

	return map[string]any{
		"$schema":              jsonSchemaVersion,
		"$id":                  ConfigSchemaID,
		"title":                "Bitbucket Server CLI Configuration",
		"description":          "Schema for bb system, workspace, and user configuration files (/etc/bb/config.yaml, %ProgramData%\\bb\\config.yaml, .bb/config.yaml, and ~/.config/bb/config.yaml).",
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"$schema": map[string]any{
				"type":        "string",
				"description": "JSON schema reference URI.",
			},
			"default_host": map[string]any{
				"type":        "string",
				"description": "Default Bitbucket Server / Data Center instance URL or alias.",
			},
			"project_key": map[string]any{
				"type":        "string",
				"description": "Default project key for repository operations.",
			},
			"require_keyring":            policyProps["require_keyring"],
			"ca_file":                    policyProps["ca_file"],
			"allowed_hosts":              policyProps["allowed_hosts"],
			"allow_insecure_skip_verify": policyProps["allow_insecure_skip_verify"],
			"disable_update":             policyProps["disable_update"],
			"update_base_url":            policyProps["update_base_url"],
			"policies": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"description":          "Administrative policy block.",
				"properties":           policyProps,
			},
			"policy": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"description":          "Administrative policy block (alias).",
				"properties":           policyProps,
			},
			"hosts": map[string]any{
				"type":                 "object",
				"description":          "Configured Bitbucket host profiles.",
				"additionalProperties": hostProfileSchema,
			},
			"insecure_secrets": map[string]any{
				"type":                 "object",
				"description":          "Plaintext fallback secrets store.",
				"additionalProperties": insecureSecretSchema,
			},
		},
	}
}

// ValidateConfigYAML validates YAML bytes against the configuration JSON schema.
func ValidateConfigYAML(rawYAML []byte) error {
	trimmed := bytes.TrimSpace(rawYAML)
	if len(trimmed) == 0 {
		return nil
	}

	var rawMap any
	if err := yaml.Unmarshal(rawYAML, &rawMap); err != nil {
		return apperrors.New(apperrors.KindValidation, "invalid YAML configuration", err)
	}
	if rawMap == nil {
		return nil
	}

	// Round-trip through JSON to convert map[any]any to map[string]any for jsonschema
	jsonBytes, err := json.Marshal(rawMap)
	if err != nil {
		return apperrors.New(apperrors.KindValidation, "failed to serialize configuration for schema validation", err)
	}

	var data any
	if err := json.Unmarshal(jsonBytes, &data); err != nil {
		return apperrors.New(apperrors.KindValidation, "failed to decode configuration for schema validation", err)
	}

	var schemaDoc any
	schemaBytes, err := json.Marshal(ConfigJSONSchema())
	if err != nil {
		return apperrors.New(apperrors.KindInternal, "failed to serialize configuration schema", err)
	}
	if err := json.Unmarshal(schemaBytes, &schemaDoc); err != nil {
		return apperrors.New(apperrors.KindInternal, "failed to parse configuration schema", err)
	}

	compiler := jsonschema.NewCompiler()
	schemaURL := "config.schema.json"
	if err := compiler.AddResource(schemaURL, schemaDoc); err != nil {
		return apperrors.New(apperrors.KindInternal, "failed to register configuration schema", err)
	}

	compiled, err := compiler.Compile(schemaURL)
	if err != nil {
		return apperrors.New(apperrors.KindInternal, "failed to compile configuration schema", err)
	}

	if err := compiled.Validate(data); err != nil {
		return apperrors.New(apperrors.KindValidation, fmt.Sprintf("configuration does not match schema: %v", err), err)
	}

	return nil
}
