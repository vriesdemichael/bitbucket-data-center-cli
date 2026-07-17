package auth

import (
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/jsonoutput"
)

// authStatusDataSchema is the schema for the data field of bb auth status --json.
func authStatusDataSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"bitbucket_url":            map[string]any{"type": "string", "description": "The configured Bitbucket Data Center base URL."},
			"bitbucket_version_target": map[string]any{"type": "string", "description": "Expected Bitbucket version string."},
			"auth_mode":                map[string]any{"type": "string", "description": "Active authentication mode (token, basic, none)."},
			"auth_source":              map[string]any{"type": "string", "description": "Source of the auth configuration (env, keyring, config)."},
		},
		"required": []any{"bitbucket_url", "bitbucket_version_target", "auth_mode", "auth_source"},
	}
}

// authLoginDataSchema is the schema for the data field of bb auth login --json.
func authLoginDataSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"host":                  map[string]any{"type": "string", "description": "The Bitbucket Data Center host that credentials were stored for."},
			"aliases":               map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Discovered or configured host aliases stored for this server context."},
			"auth_mode":             map[string]any{"type": "string", "description": "Stored authentication mode (token or basic)."},
			"used_insecure_storage": map[string]any{"type": "boolean", "description": "True when the system keyring was unavailable and the config fallback was used."},
		},
		"required": []any{"host", "aliases", "auth_mode", "used_insecure_storage"},
	}
}

// authIdentityUserSchema is the schema for the nested user object in bb auth identity --json.
func authIdentityUserSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": true,
		"properties": map[string]any{
			"name":         map[string]any{"type": "string"},
			"slug":         map[string]any{"type": "string"},
			"display_name": map[string]any{"type": "string"},
			"email":        map[string]any{"type": "string"},
			"id":           map[string]any{"type": "integer"},
			"type":         map[string]any{"type": "string"},
			"active":       map[string]any{"type": "boolean"},
		},
	}
}

// authIdentityDataSchema is the schema for the data field of bb auth identity --json.
func authIdentityDataSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"bitbucket_url": map[string]any{"type": "string", "description": "Bitbucket Data Center base URL used for the identity lookup."},
			"user":          authIdentityUserSchema(),
		},
		"required": []any{"bitbucket_url", "user"},
	}
}

// authTokenURLDataSchema is the schema for the data field of bb auth token-url --json.
func authTokenURLDataSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"bitbucket_url": map[string]any{"type": "string", "description": "Bitbucket Data Center base URL."},
			"token_url":     map[string]any{"type": "string", "format": "uri", "description": "URL to the personal access token creation page."},
		},
		"required": []any{"bitbucket_url", "token_url"},
	}
}

// authLogoutDataSchema is the schema for the data field of bb auth logout --json.
func authLogoutDataSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"status": map[string]any{"const": "ok"},
		},
		"required": []any{"status"},
	}
}

// authServerContextSchema is the schema for a single server context entry.
func authServerContextSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": true,
		"properties": map[string]any{
			"Host":      map[string]any{"type": "string"},
			"Aliases":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"AuthMode":  map[string]any{"type": "string"},
			"Username":  map[string]any{"type": "string"},
			"IsDefault": map[string]any{"type": "boolean"},
		},
		"required": []any{"Host", "Aliases", "AuthMode", "IsDefault"},
	}
}

func authAliasDataSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"host":    map[string]any{"type": "string"},
			"aliases": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"status":  map[string]any{"type": "string"},
		},
	}
}

// authServerListDataSchema is the schema for the data field of bb auth server list --json.
func authServerListDataSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"servers": map[string]any{
				"type":  "array",
				"items": authServerContextSchema(),
			},
		},
		"required": []any{"servers"},
	}
}

// authServerUseDataSchema is the schema for the data field of bb auth server use --json.
func authServerUseDataSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"status":       map[string]any{"const": "ok"},
			"default_host": map[string]any{"type": "string", "description": "The configured default Bitbucket Data Center host."},
		},
		"required": []any{"status", "default_host"},
	}
}

// Schemas returns all auth command output schemas keyed by file name.  The
// schemas describe the full bb.machine v2 envelope emitted by each command.
func Schemas() map[string]map[string]any {
	return map[string]map[string]any{
		"output.auth.status.schema.json": jsonoutput.EnvelopeSchemaFor(
			"output.auth.status.schema.json",
			"bb auth status output",
			"JSON output schema for `bb auth status --json`. Data contains the currently configured Bitbucket connection and auth settings.",
			authStatusDataSchema(),
		),
		"output.auth.login.schema.json": jsonoutput.EnvelopeSchemaFor(
			"output.auth.login.schema.json",
			"bb auth login output",
			"JSON output schema for `bb auth login --json`. Data describes the stored credential result.",
			authLoginDataSchema(),
		),
		"output.auth.identity.schema.json": jsonoutput.EnvelopeSchemaFor(
			"output.auth.identity.schema.json",
			"bb auth identity output",
			"JSON output schema for `bb auth identity --json` (alias: bb auth whoami). Data contains the authenticated user identity.",
			authIdentityDataSchema(),
		),
		"output.auth.token-url.schema.json": jsonoutput.EnvelopeSchemaFor(
			"output.auth.token-url.schema.json",
			"bb auth token-url output",
			"JSON output schema for `bb auth token-url --json`. Data contains the personal access token creation URL.",
			authTokenURLDataSchema(),
		),
		"output.auth.logout.schema.json": jsonoutput.EnvelopeSchemaFor(
			"output.auth.logout.schema.json",
			"bb auth logout output",
			"JSON output schema for `bb auth logout --json`. Data confirms the logout operation succeeded.",
			authLogoutDataSchema(),
		),
		"output.auth.server.list.schema.json": jsonoutput.EnvelopeSchemaFor(
			"output.auth.server.list.schema.json",
			"bb auth server list output",
			"JSON output schema for `bb auth server list --json`. Data lists all stored Bitbucket server contexts.",
			authServerListDataSchema(),
		),
		"output.auth.server.use.schema.json": jsonoutput.EnvelopeSchemaFor(
			"output.auth.server.use.schema.json",
			"bb auth server use output",
			"JSON output schema for `bb auth server use --json`. Data confirms the new default server.",
			authServerUseDataSchema(),
		),
		"output.auth.alias.list.schema.json": jsonoutput.EnvelopeSchemaFor(
			"output.auth.alias.list.schema.json",
			"bb auth alias list output",
			"JSON output schema for `bb auth alias list --json`. Data lists aliases for a stored server context.",
			authAliasDataSchema(),
		),
		"output.auth.alias.add.schema.json": jsonoutput.EnvelopeSchemaFor(
			"output.auth.alias.add.schema.json",
			"bb auth alias add output",
			"JSON output schema for `bb auth alias add --json`. Data confirms the updated alias set.",
			authAliasDataSchema(),
		),
		"output.auth.alias.remove.schema.json": jsonoutput.EnvelopeSchemaFor(
			"output.auth.alias.remove.schema.json",
			"bb auth alias remove output",
			"JSON output schema for `bb auth alias remove --json`. Data confirms the updated alias set.",
			authAliasDataSchema(),
		),
		"output.auth.alias.discover.schema.json": jsonoutput.EnvelopeSchemaFor(
			"output.auth.alias.discover.schema.json",
			"bb auth alias discover output",
			"JSON output schema for `bb auth alias discover --json`. Data contains the discovered alias set.",
			authAliasDataSchema(),
		),
	}
}
