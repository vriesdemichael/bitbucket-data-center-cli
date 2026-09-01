package auth

import (
	"testing"
)

func TestAuthSchemasKeys(t *testing.T) {
	schemas := Schemas()

	want := []string{
		"output.auth.status.schema.json",
		"output.auth.login.schema.json",
		"output.auth.identity.schema.json",
		"output.auth.token-url.schema.json",
		"output.auth.logout.schema.json",
		"output.auth.server.list.schema.json",
		"output.auth.server.use.schema.json",
		"output.auth.alias.list.schema.json",
		"output.auth.alias.add.schema.json",
		"output.auth.alias.remove.schema.json",
		"output.auth.alias.discover.schema.json",
	}

	for _, key := range want {
		schema, ok := schemas[key]
		if !ok {
			t.Errorf("Schemas() missing key %q", key)
			continue
		}
		if schema == nil {
			t.Errorf("Schemas() key %q is nil", key)
		}
	}

	if len(schemas) != len(want) {
		t.Errorf("expected %d schemas, got %d", len(want), len(schemas))
	}
}

func TestAuthSchemasEnvelopeShape(t *testing.T) {
	for name, schema := range Schemas() {
		props, ok := schema["properties"].(map[string]any)
		if !ok {
			t.Errorf("%s: missing envelope properties", name)
			continue
		}
		for _, field := range []string{"data", "meta"} {
			if _, ok := props[field]; !ok {
				t.Errorf("%s: missing envelope property %q", name, field)
			}
		}
	}
}
