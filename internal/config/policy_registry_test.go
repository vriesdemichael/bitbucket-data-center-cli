package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

// TestEveryPolicyKeyHasARegistryValue holds RegistryValues to the policy keys:
// each one but mcp_audit_file has a value, under a name of its own, and the
// field a value sets is the one its key names.
func TestEveryPolicyKeyHasARegistryValue(t *testing.T) {
	t.Parallel()

	want := policyKeys()
	delete(want, "mcp_audit_file")

	names := map[string]bool{}
	got := map[string]bool{}
	for _, value := range RegistryValues {
		if names[value.Name] || got[value.Key] {
			t.Errorf("%s (%s) is in RegistryValues twice", value.Name, value.Key)
		}
		names[value.Name] = true
		got[value.Key] = true

		if value.Title == "" {
			t.Errorf("%s has no title for Group Policy", value.Name)
		}
		if value.Kind != RegistryBoolean && value.restrictive {
			t.Errorf("%s is a %s, and only a boolean has a restrictive side", value.Name, value.Kind)
		}

		set := setThroughRegistryValue(t, value)
		encoded, err := yaml.Marshal(set)
		if err != nil {
			t.Fatalf("encode the policy %s sets: %v", value.Name, err)
		}
		var keys map[string]any
		if err := yaml.Unmarshal(encoded, &keys); err != nil {
			t.Fatalf("decode the policy %s sets: %v", value.Name, err)
		}
		if _, ok := keys[value.Key]; !ok || len(keys) != 1 {
			t.Errorf("%s sets %v, want %s alone", value.Name, keys, value.Key)
		}
	}

	assertSameKeys(t, "RegistryValues", got, want)
}

// setThroughRegistryValue is a policy with value's field set, through the
// accessor for its kind, which must be its only one.
func setThroughRegistryValue(t *testing.T, value RegistryValue) PolicyConfig {
	t.Helper()

	accessors := 0
	for _, present := range []bool{value.boolean != nil, value.text != nil, value.list != nil} {
		if present {
			accessors++
		}
	}
	if accessors != 1 {
		t.Fatalf("%s has %d fields to set, want 1", value.Name, accessors)
	}

	var policy PolicyConfig
	switch value.Kind {
	case RegistryBoolean:
		if value.boolean == nil {
			t.Fatalf("%s is a boolean with no boolean field", value.Name)
		}
		set := true
		*value.boolean(&policy) = &set
	case RegistryString:
		if value.text == nil {
			t.Fatalf("%s is a string with no string field", value.Name)
		}
		*value.text(&policy) = "set"
	case RegistryList:
		if value.list == nil {
			t.Fatalf("%s is a list with no list field", value.Name)
		}
		*value.list(&policy) = []string{"set"}
	default:
		t.Fatalf("%s is of kind %q, which nothing reads", value.Name, value.Kind)
	}

	return policy
}
