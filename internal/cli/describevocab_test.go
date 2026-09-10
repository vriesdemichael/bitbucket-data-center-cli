package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestAPublishedVocabularyIsOneTheFlagAccepts is #577.
//
// --describe is the surface this project tells agents to rely on, and on
// pr list it named a --state vocabulary the flag rejects: two of the four
// values it advertised were refused, and the one that works was not mentioned.
// A human recovers instantly, because the rejection names the valid set. An
// agent consulted --describe precisely so it would not have to guess.
//
// The flag is the authority here, not a second list: every value is put to the
// flag itself, and the schema has to agree with the answer.
func TestAPublishedVocabularyIsOneTheFlagAccepts(t *testing.T) {
	t.Parallel()

	for name, testCase := range map[string]struct {
		command []string
		flag    string
		values  []string
		// field is where the schema documents that flag. Scoped, because the
		// same words appear legitimately elsewhere: a pull request really can
		// be declined, which is not a value --state takes.
		field []string
	}{
		"pr list --state": {
			command: []string{"pr", "list"},
			flag:    "state",
			// Every value the issue named, accepted or rejected. Which is
			// which is decided by the flag below, not here.
			values: []string{"open", "closed", "all", "merged", "declined"},
			field:  []string{"properties", "filters", "properties", "state"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			root := NewRootCommand()
			cmd, _, err := root.Find(testCase.command)
			if err != nil {
				t.Fatalf("find %v: %v", testCase.command, err)
			}

			flag := cmd.Flags().Lookup(testCase.flag)
			if flag == nil {
				t.Fatalf("%v has no --%s", testCase.command, testCase.flag)
			}

			published := describedField(t, describeCommand(commandPathWithoutRoot(cmd)), testCase.field)

			for _, value := range testCase.values {
				accepted := flag.Value.Set(value) == nil
				documented := strings.Contains(published, value)

				switch {
				case accepted && !documented:
					t.Errorf("--%s accepts %q and --describe does not publish it:\n%s", testCase.flag, value, published)
				case !accepted && documented:
					t.Errorf("--describe publishes %q and --%s rejects it:\n%s", value, testCase.flag, published)
				}
			}
		})
	}
}

// describedField returns the description the schema publishes at one path.
func describedField(t *testing.T, described DescribeResult, path []string) string {
	t.Helper()

	encoded, err := json.Marshal(described.Schema)
	if err != nil {
		t.Fatalf("encode the schema: %v", err)
	}

	var node any
	if err := json.Unmarshal(encoded, &node); err != nil {
		t.Fatalf("decode the schema: %v", err)
	}

	for _, step := range path {
		object, ok := node.(map[string]any)
		if !ok {
			t.Fatalf("the schema has no %s", strings.Join(path, "."))
		}
		node, ok = object[step]
		if !ok {
			t.Fatalf("the schema has no %s", strings.Join(path, "."))
		}
	}

	object, ok := node.(map[string]any)
	if !ok {
		t.Fatalf("%s is not an object", strings.Join(path, "."))
	}

	description, _ := object["description"].(string)
	if strings.TrimSpace(description) == "" {
		t.Fatalf("%s publishes no description", strings.Join(path, "."))
	}

	return description
}
