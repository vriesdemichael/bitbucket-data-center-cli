package main

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
)

// TestTheTemplateOffersEveryValueBBReads: each registry value is one machine
// policy writing that value under bb's key, in the shape its kind is read in,
// and explained with the schema's description of its key.
func TestTheTemplateOffersEveryValueBBReads(t *testing.T) {
	t.Parallel()

	descriptions, err := policyDescriptions(config.ConfigJSONSchema())
	if err != nil {
		t.Fatal(err)
	}
	admx, adml, err := templates(config.RegistryValues, descriptions)
	if err != nil {
		t.Fatal(err)
	}
	definitions, resources := decode(t, admx, adml)

	policies := map[string]policy{}
	for _, entry := range definitions.Policies {
		policies[entry.Name] = entry
	}
	if len(policies) != len(config.RegistryValues) {
		t.Errorf("the template has %d policies, want one for each of the %d registry values", len(policies), len(config.RegistryValues))
	}
	texts := stringsByID(resources)

	for _, value := range config.RegistryValues {
		entry, ok := policies[value.Name]
		if !ok {
			t.Errorf("the template has no policy for %s", value.Name)
			continue
		}
		if entry.Class != "Machine" || entry.Key != config.RegistryPolicyKey {
			t.Errorf("%s is a %s policy under %s, want Machine under %s", value.Name, entry.Class, entry.Key, config.RegistryPolicyKey)
		}
		if written := writtenValue(entry, value.Kind); written != value.Name {
			t.Errorf("%s, a %s, writes %q", value.Name, value.Kind, written)
		}
		if help := texts[value.Name+"_Help"]; !containsAll(help, descriptions[value.Key], value.Name, value.Key) {
			t.Errorf("%s is explained as %q", value.Name, help)
		}
	}
}

// TestEveryReferenceInTheTemplateResolves: Group Policy refuses a template
// whose strings, presentations or categories point at nothing.
func TestEveryReferenceInTheTemplateResolves(t *testing.T) {
	t.Parallel()

	descriptions, err := policyDescriptions(config.ConfigJSONSchema())
	if err != nil {
		t.Fatal(err)
	}
	admx, adml, err := templates(config.RegistryValues, descriptions)
	if err != nil {
		t.Fatal(err)
	}
	definitions, resources := decode(t, admx, adml)

	texts := stringsByID(resources)
	for _, match := range regexp.MustCompile(`\$\(string\.([^)]+)\)`).FindAllStringSubmatch(string(admx), -1) {
		if _, ok := texts[match[1]]; !ok {
			t.Errorf("the ADMX names string %s, which the ADML does not define", match[1])
		}
	}

	presentations := map[string]presentation{}
	for _, entry := range resources.Presentations {
		presentations[entry.ID] = entry
	}
	for _, entry := range definitions.Policies {
		if entry.ParentCategory.Ref != category || entry.SupportedOn.Ref != supported {
			t.Errorf("%s is in category %q, supported on %q", entry.Name, entry.ParentCategory.Ref, entry.SupportedOn.Ref)
		}
		if entry.Elements == nil {
			continue
		}
		shown, ok := presentations[presentationID(entry.Presentation)]
		if !ok {
			t.Errorf("%s shows presentation %q, which the ADML does not define", entry.Name, entry.Presentation)
			continue
		}
		var ref string
		switch {
		case shown.TextBox != nil:
			ref = shown.TextBox.RefID
		case shown.MultiTextBox != nil:
			ref = shown.MultiTextBox.RefID
		}
		if ref != writtenValue(entry, kindOf(entry)) {
			t.Errorf("%s's presentation shows element %q, not its own", entry.Name, ref)
		}
	}
}

func TestATemplateNeedsADescriptionOfEachKey(t *testing.T) {
	t.Parallel()

	values := []config.RegistryValue{{Name: "Undescribed", Key: "undescribed", Kind: config.RegistryBoolean, Title: "Undescribed"}}
	if _, _, err := templates(values, map[string]string{}); err == nil || !strings.Contains(err.Error(), "undescribed") {
		t.Errorf("a value the schema does not describe gave %v", err)
	}
}

func TestPolicyDescriptionsComeFromThePolicyBlock(t *testing.T) {
	t.Parallel()

	schema := map[string]any{"properties": map[string]any{"policy": map[string]any{"properties": map[string]any{
		"read_only": map[string]any{"type": "boolean", "description": "Make bb read-only."},
		"untyped":   "not a property",
	}}}}
	descriptions, err := policyDescriptions(schema)
	if err != nil {
		t.Fatal(err)
	}
	if len(descriptions) != 1 || descriptions["read_only"] != "Make bb read-only." {
		t.Errorf("descriptions are %v", descriptions)
	}

	if _, err := policyDescriptions(map[string]any{"properties": map[string]any{}}); err == nil {
		t.Error("a schema with no policy block gave no error")
	}
}

func TestExportWritesTheADMXAndItsEnglishStrings(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	if err := export(directory); err != nil {
		t.Fatal(err)
	}

	admx, err := os.ReadFile(filepath.Join(directory, "bb.admx"))
	if err != nil {
		t.Fatal(err)
	}
	adml, err := os.ReadFile(filepath.Join(directory, "en-US", "bb.adml"))
	if err != nil {
		t.Fatal(err)
	}
	definitions, _ := decode(t, admx, adml)
	if len(definitions.Policies) != len(config.RegistryValues) {
		t.Errorf("the exported ADMX has %d policies, want %d", len(definitions.Policies), len(config.RegistryValues))
	}
}

func decode(t *testing.T, admx, adml []byte) (policyDefinitions, policyDefinitionResources) {
	t.Helper()

	var definitions policyDefinitions
	if err := xml.Unmarshal(admx, &definitions); err != nil {
		t.Fatalf("the ADMX is not well-formed: %v", err)
	}
	var resources policyDefinitionResources
	if err := xml.Unmarshal(adml, &resources); err != nil {
		t.Fatalf("the ADML is not well-formed: %v", err)
	}

	return definitions, resources
}

func stringsByID(resources policyDefinitionResources) map[string]string {
	byID := map[string]string{}
	for _, entry := range resources.Strings {
		byID[entry.ID] = entry.Text
	}

	return byID
}

// writtenValue is the registry value entry writes, in the shape kind is read
// in: a value of the policy's own for a boolean, a text element for a string
// and a multi-text element for a list.
func writtenValue(entry policy, kind config.RegistryKind) string {
	switch kind {
	case config.RegistryBoolean:
		if entry.Elements != nil || entry.EnabledValue == nil || entry.DisabledValue == nil ||
			entry.EnabledValue.Decimal.Value != 1 || entry.DisabledValue.Decimal.Value != 0 {
			return ""
		}
		return entry.ValueName
	case config.RegistryString:
		if entry.ValueName != "" || entry.Elements == nil || len(entry.Elements.Text) != 1 || len(entry.Elements.MultiText) != 0 {
			return ""
		}
		return entry.Elements.Text[0].ValueName
	case config.RegistryList:
		if entry.ValueName != "" || entry.Elements == nil || len(entry.Elements.MultiText) != 1 || len(entry.Elements.Text) != 0 {
			return ""
		}
		return entry.Elements.MultiText[0].ValueName
	}

	return ""
}

func kindOf(entry policy) config.RegistryKind {
	switch {
	case entry.Elements == nil:
		return config.RegistryBoolean
	case len(entry.Elements.MultiText) > 0:
		return config.RegistryList
	default:
		return config.RegistryString
	}
}

func presentationID(ref string) string {
	return strings.TrimSuffix(strings.TrimPrefix(ref, "$(presentation."), ")")
}

func containsAll(text string, parts ...string) bool {
	for _, part := range parts {
		if part == "" || !strings.Contains(text, part) {
			return false
		}
	}

	return true
}
