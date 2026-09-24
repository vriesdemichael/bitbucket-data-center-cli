package cli

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/enumflag"
)

// echoAliases are values a flag accepts and its command normalises before
// writing them back, so the published enum rightly leaves them out. Each says
// what it becomes.
var echoAliases = map[string]string{
	"pr comment list --state unresolved": "a synonym for open, and echoed as open",
}

// TestAnEchoedFlagPublishesTheValuesItAccepts is #577.
//
// --describe is the surface agents are told to rely on, and on pr list it named
// a --state vocabulary the flag rejects: two of the four values it advertised
// were refused, and the one that works was not mentioned. The issue asked for
// the documented vocabulary to come from the same enum the flag validates
// against, so the two cannot drift.
//
// So this walks every enum flag rather than a table of the one that was
// reported, and wherever a command's payload echoes the flag -- a property of
// the same name at the top level or under filters -- the schema has to publish
// an enum, and that enum has to be the flag's set. A vocabulary written as prose
// in a description is exactly what drifted, and a substring check on prose found
// "open" inside "openapi.PullRequestStateFilters".
func TestAnEchoedFlagPublishesTheValuesItAccepts(t *testing.T) {
	t.Parallel()

	root := NewRootCommand()

	var problems []string
	echoes := 0

	var visit func(*cobra.Command)
	visit = func(cmd *cobra.Command) {
		for _, child := range cmd.Commands() {
			visit(child)
		}
		if !cmd.Runnable() {
			return
		}

		path := commandPathWithoutRoot(cmd)
		declared, _, described := DataSchema(path)
		if !described {
			return
		}

		encoded, err := json.Marshal(declared)
		if err != nil {
			t.Fatalf("%s: encode schema: %v", path, err)
		}
		var schema map[string]any
		if err := json.Unmarshal(encoded, &schema); err != nil {
			t.Fatalf("%s: decode schema: %v", path, err)
		}

		cmd.LocalFlags().VisitAll(func(flag *pflag.Flag) {
			accepted, isEnum := enumflag.Allowed(flag)
			if !isEnum {
				return
			}

			name := echoedPropertyName(flag.Name)
			for _, location := range [][]string{{name}, {"filters", name}} {
				property := schemaPropertyAt(schema, location)
				if property == nil {
					continue
				}
				echoes++
				where := path + " --" + flag.Name + " (" + strings.Join(location, ".") + ")"

				published, _ := property["enum"].([]any)
				if len(published) == 0 {
					problems = append(problems, where+": echoes the flag with no enum, so its vocabulary is prose that can drift")
					continue
				}

				publishedSet := map[string]bool{}
				for _, value := range published {
					text, _ := value.(string)
					publishedSet[strings.ToLower(text)] = true
					if !containsFold(accepted, text) {
						problems = append(problems, where+": publishes "+text+", which the flag rejects")
					}
				}
				for _, value := range accepted {
					if publishedSet[strings.ToLower(value)] || echoAliases[path+" --"+flag.Name+" "+value] != "" {
						continue
					}
					problems = append(problems, where+": accepts "+value+", which the schema does not publish")
				}
			}
		})
	}
	visit(root)

	// A walk that stopped finding echoes would report perfect compliance
	// (ADR-067). Seven commands echo an enum flag today.
	if echoes < 5 {
		t.Fatalf("found only %d echoed enum flags, expected seven.\nThe walk is probably broken, not the commands.", echoes)
	}

	if len(problems) > 0 {
		sort.Strings(problems)
		t.Fatalf("%d echoed flag(s) publish a vocabulary that is not the flag's:\n  %s\n\n"+
			"Declare the property's enum from the slice the flag is registered with, so the two are one list.",
			len(problems), strings.Join(problems, "\n  "))
	}
}

// echoedPropertyName is the camelCase property a kebab-case flag is written
// back as (ADR-076).
func echoedPropertyName(flag string) string {
	parts := strings.Split(flag, "-")
	for index := 1; index < len(parts); index++ {
		if parts[index] != "" {
			parts[index] = strings.ToUpper(parts[index][:1]) + parts[index][1:]
		}
	}

	return strings.Join(parts, "")
}

// schemaPropertyAt walks object properties, and never into array items: a
// pull request's own state is not the filter that selected it.
func schemaPropertyAt(schema map[string]any, location []string) map[string]any {
	node := schema
	for _, step := range location {
		properties, ok := node["properties"].(map[string]any)
		if !ok {
			return nil
		}
		next, ok := properties[step].(map[string]any)
		if !ok {
			return nil
		}
		node = next
	}

	return node
}

func containsFold(values []string, want string) bool {
	for _, value := range values {
		if strings.EqualFold(value, want) {
			return true
		}
	}

	return false
}
