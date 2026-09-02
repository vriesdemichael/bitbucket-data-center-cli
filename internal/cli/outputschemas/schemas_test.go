package outputschemas_test

import (
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/outputschemas"
)

func TestSchemasReturnNonEmpty(t *testing.T) {
	schemas := outputschemas.Schemas()

	if len(schemas) == 0 {
		t.Fatal("Schemas() returned no schemas")
	}

	for name, schema := range schemas {
		if schema == nil {
			t.Errorf("schema %s is nil", name)
		}

		if _, ok := schema["$schema"]; !ok {
			t.Errorf("schema %s missing $schema field", name)
		}

		if _, ok := schema["$id"]; !ok {
			t.Errorf("schema %s missing $id field", name)
		}

		id, _ := schema["$id"].(string)
		if !strings.Contains(id, name) {
			t.Errorf("schema %s $id %q does not contain the file name", name, id)
		}

		props, ok := schema["properties"].(map[string]any)
		if !ok {
			t.Errorf("schema %s missing or invalid properties", name)
			continue
		}

		for _, field := range []string{"meta"} {
			if _, ok := props[field]; !ok {
				t.Errorf("schema %s missing envelope property %q", name, field)
			}
		}

		// A bb.machine envelope carries data or error, never both: which key is
		// present is how a consumer tells success from failure, so a schema
		// permitting both would make the distinction meaningless.
		_, hasData := props["data"]
		_, hasError := props["error"]

		switch {
		case hasData && hasError:
			t.Errorf("schema %s declares both data and error", name)
		case !hasData && !hasError:
			t.Errorf("schema %s declares neither data nor error", name)
		}

		if name == outputschemas.ErrorSchemaFileName && !hasError {
			t.Errorf("schema %s is the failure envelope but declares data", name)
		}
	}
}

// Every release publishes a full copy of the schemas, and mike also serves the
// newest copy under the "latest" alias.  Identifying them all against the alias
// makes three distinct documents claim one canonical identity, which is what a
// validator resolves $ref against and caches by.
func TestSchemasForIdentifiesEverySchemaAgainstTheGivenSiteVersion(t *testing.T) {
	schemas := outputschemas.SchemasFor("v4.0.0")
	if len(schemas) == 0 {
		t.Fatal("SchemasFor returned no schemas")
	}

	for name, schema := range schemas {
		id, _ := schema["$id"].(string)
		want := "https://vriesdemichael.github.io/bitbucket-data-center-cli/v4.0.0/reference/schemas/output/" + name
		if id != want {
			t.Errorf("schema %s: $id = %q, want %q", name, id, want)
		}
	}
}

func TestSchemasIdentifyAgainstTheLatestAlias(t *testing.T) {
	for name, schema := range outputschemas.Schemas() {
		id, _ := schema["$id"].(string)
		want := "https://vriesdemichael.github.io/bitbucket-data-center-cli/latest/reference/schemas/output/" + name
		if id != want {
			t.Errorf("schema %s: $id = %q, want %q", name, id, want)
		}
	}
}
