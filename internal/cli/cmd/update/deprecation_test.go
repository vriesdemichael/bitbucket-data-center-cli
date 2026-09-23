package updatecmd

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/deprecation"
)

// An output field warns nobody at runtime, so its schema is where a consumer
// learns it is going (ADR-084). Every bb update field the registry deprecates
// says so in the schema --describe prints, and every field the schema calls
// deprecated is in the registry, which is what sets the major it goes in.
func TestDeprecatedFieldsAreTheOnesTheSchemaCallsDeprecated(t *testing.T) {
	t.Parallel()

	const namePrefix = "bb update --json field "
	registered := map[string]bool{}
	for _, entry := range deprecation.Entries {
		if field, ok := strings.CutPrefix(entry.Name, namePrefix); ok {
			registered[field] = true
		}
	}

	schema, ok := result.SchemaFor("update")
	if !ok {
		t.Fatal("bb update declares no result schema")
	}
	described := map[string]bool{}
	var walk func(prefix string, schema *jsonschema.Schema)
	walk = func(prefix string, schema *jsonschema.Schema) {
		for name, property := range schema.Properties {
			if strings.HasPrefix(property.Description, "Deprecated") {
				described[prefix+name] = true
			}
			walk(prefix+name+".", property)
		}
	}
	walk("", schema)

	if !maps.Equal(registered, described) {
		t.Fatalf("the registry deprecates %v, and the schema calls %v deprecated",
			slices.Sorted(maps.Keys(registered)), slices.Sorted(maps.Keys(described)))
	}
	for field := range described {
		if property := propertyAt(schema, field); !strings.Contains(property.Description, "Read applied instead.") {
			t.Errorf("%s does not say what to read instead: %q", field, property.Description)
		}
	}
}

func propertyAt(schema *jsonschema.Schema, path string) *jsonschema.Schema {
	for _, name := range strings.Split(path, ".") {
		schema = schema.Properties[name]
	}

	return schema
}
