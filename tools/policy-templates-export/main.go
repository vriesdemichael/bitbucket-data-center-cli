// Command policy-templates-export writes the Group Policy template for the
// registry values bb reads: bb.admx, and its English strings in en-US/bb.adml.
//
// Each setting comes from config.RegistryValues, the table the Windows reader
// reads by, and its explanation from the configuration schema's description of
// the policy key it sets, so the template cannot offer a value bb ignores or
// describe one differently from the schema.
package main

import (
	"encoding/xml"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
)

func main() {
	outputDir := flag.String("out", "docs/reference/policy-templates", "directory for bb.admx and en-US/bb.adml")
	flag.Parse()

	if err := export(*outputDir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func export(outputDir string) error {
	descriptions, err := policyDescriptions(config.ConfigJSONSchema())
	if err != nil {
		return err
	}
	admx, adml, err := templates(config.RegistryValues, descriptions)
	if err != nil {
		return err
	}

	for path, content := range map[string][]byte{
		filepath.Join(outputDir, "bb.admx"):          admx,
		filepath.Join(outputDir, "en-US", "bb.adml"): adml,
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, content, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
		fmt.Printf("wrote %s\n", path)
	}

	return nil
}

// policyDescriptions are the schema's descriptions of the policy keys.
func policyDescriptions(schema map[string]any) (map[string]string, error) {
	properties, ok := dig(schema, "properties", "policy", "properties")
	if !ok {
		return nil, fmt.Errorf("the configuration schema has no properties.policy.properties")
	}

	descriptions := map[string]string{}
	for key, property := range properties {
		if fields, ok := property.(map[string]any); ok {
			if description, ok := fields["description"].(string); ok {
				descriptions[key] = description
			}
		}
	}

	return descriptions, nil
}

func dig(node map[string]any, path ...string) (map[string]any, bool) {
	for _, name := range path {
		next, ok := node[name].(map[string]any)
		if !ok {
			return nil, false
		}
		node = next
	}

	return node, true
}

const (
	definitionsNamespace = "http://schemas.microsoft.com/GroupPolicy/2006/07/PolicyDefinitions"
	// targetNamespace is the template's own namespace, which Group Policy
	// keeps apart from every other template's. Administrators load it into
	// their policy store, so it does not change.
	targetNamespace = "io.github.vriesdemichael.bb"
	category        = "bb"
	supported       = "SUPPORTED_bb"
	configFile      = `%ProgramData%\bb\config.yaml`
)

// templates are the ADMX and the en-US ADML for values.
func templates(values []config.RegistryValue, descriptions map[string]string) (admx, adml []byte, err error) {
	definitions := policyDefinitions{
		Namespace:     definitionsNamespace,
		Revision:      "1.0",
		SchemaVersion: "1.0",
		Target:        target{Prefix: "bb", Namespace: targetNamespace},
		Resources:     resourcesReference{MinRequiredRevision: "1.0"},
		SupportedOn:   []definition{{Name: supported, DisplayName: stringRef(supported)}},
		Categories:    []definition{{Name: category, DisplayName: stringRef(category)}},
	}
	resources := policyDefinitionResources{
		Namespace:     definitionsNamespace,
		Revision:      "1.0",
		SchemaVersion: "1.0",
		DisplayName:   "bb",
		Description:   "Group Policy settings for bb, the Bitbucket Data Center CLI.",
		Strings: []stringEntry{
			{ID: category, Text: "bb, the Bitbucket Data Center CLI"},
			{ID: supported, Text: "bb, the Bitbucket Data Center CLI"},
		},
	}

	for _, value := range values {
		description, ok := descriptions[value.Key]
		if !ok {
			return nil, nil, fmt.Errorf("the configuration schema does not describe %s, which %s sets", value.Key, value.Name)
		}

		entry := policy{
			Name:           value.Name,
			Class:          "Machine",
			DisplayName:    stringRef(value.Name),
			ExplainText:    stringRef(value.Name + "_Help"),
			Key:            config.RegistryPolicyKey,
			ParentCategory: reference{Ref: category},
			SupportedOn:    reference{Ref: supported},
		}
		switch value.Kind {
		case config.RegistryBoolean:
			entry.ValueName = value.Name
			entry.EnabledValue = &decimalValue{Decimal: decimal{Value: 1}}
			entry.DisabledValue = &decimalValue{Decimal: decimal{Value: 0}}
		case config.RegistryString:
			entry.Presentation = presentationRef(value.Name)
			entry.Elements = &elements{Text: []element{{ID: value.Name, ValueName: value.Name, Required: "true"}}}
			resources.Presentations = append(resources.Presentations, presentation{ID: value.Name, TextBox: &textBox{RefID: value.Name, Label: value.Title}})
		case config.RegistryList:
			entry.Presentation = presentationRef(value.Name)
			entry.Elements = &elements{MultiText: []element{{ID: value.Name, ValueName: value.Name, Required: "true"}}}
			resources.Presentations = append(resources.Presentations, presentation{ID: value.Name, MultiTextBox: &multiTextBox{RefID: value.Name, Text: value.Title}})
		default:
			return nil, nil, fmt.Errorf("%s is of kind %q, which the template cannot offer", value.Name, value.Kind)
		}

		definitions.Policies = append(definitions.Policies, entry)
		resources.Strings = append(resources.Strings,
			stringEntry{ID: value.Name, Text: value.Title},
			stringEntry{ID: value.Name + "_Help", Text: description + "\n\n" + whatEachStateDoes(value)},
		)
	}

	if admx, err = encode(definitions); err != nil {
		return nil, nil, err
	}
	if adml, err = encode(resources); err != nil {
		return nil, nil, err
	}

	return admx, adml, nil
}

// whatEachStateDoes says what Enabled, Disabled and Not Configured write. A
// value under the policy key takes precedence over the same key in the system
// configuration file.
func whatEachStateDoes(value config.RegistryValue) string {
	switch value.Kind {
	case config.RegistryBoolean:
		return fmt.Sprintf("Enabled sets %s to 1 (true) and Disabled sets it to 0 (false), either one taking precedence over %s in %s. Not configured, the setting comes from that file, or bb's default.", value.Name, value.Key, configFile)
	case config.RegistryList:
		return fmt.Sprintf("Enabled sets %s to the entries given, one per line, taking precedence over %s in %s. Disabled or not configured, the setting comes from that file, or bb's default.", value.Name, value.Key, configFile)
	default:
		return fmt.Sprintf("Enabled sets %s to the text given, taking precedence over %s in %s. Disabled or not configured, the setting comes from that file, or bb's default.", value.Name, value.Key, configFile)
	}
}

func stringRef(id string) string       { return "$(string." + id + ")" }
func presentationRef(id string) string { return "$(presentation." + id + ")" }

func encode(document any) ([]byte, error) {
	encoded, err := xml.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode the policy template: %w", err)
	}

	return append(append([]byte(xml.Header), encoded...), '\n'), nil
}

type policyDefinitions struct {
	XMLName       xml.Name           `xml:"policyDefinitions"`
	Namespace     string             `xml:"xmlns,attr"`
	Revision      string             `xml:"revision,attr"`
	SchemaVersion string             `xml:"schemaVersion,attr"`
	Target        target             `xml:"policyNamespaces>target"`
	Resources     resourcesReference `xml:"resources"`
	SupportedOn   []definition       `xml:"supportedOn>definitions>definition"`
	Categories    []definition       `xml:"categories>category"`
	Policies      []policy           `xml:"policies>policy"`
}

type target struct {
	Prefix    string `xml:"prefix,attr"`
	Namespace string `xml:"namespace,attr"`
}

type resourcesReference struct {
	MinRequiredRevision string `xml:"minRequiredRevision,attr"`
}

type definition struct {
	Name        string `xml:"name,attr"`
	DisplayName string `xml:"displayName,attr"`
}

type reference struct {
	Ref string `xml:"ref,attr"`
}

type policy struct {
	Name           string        `xml:"name,attr"`
	Class          string        `xml:"class,attr"`
	DisplayName    string        `xml:"displayName,attr"`
	ExplainText    string        `xml:"explainText,attr"`
	Presentation   string        `xml:"presentation,attr,omitempty"`
	Key            string        `xml:"key,attr"`
	ValueName      string        `xml:"valueName,attr,omitempty"`
	ParentCategory reference     `xml:"parentCategory"`
	SupportedOn    reference     `xml:"supportedOn"`
	EnabledValue   *decimalValue `xml:"enabledValue"`
	DisabledValue  *decimalValue `xml:"disabledValue"`
	Elements       *elements     `xml:"elements"`
}

type decimalValue struct {
	Decimal decimal `xml:"decimal"`
}

type decimal struct {
	Value int `xml:"value,attr"`
}

type elements struct {
	Text      []element `xml:"text"`
	MultiText []element `xml:"multiText"`
}

type element struct {
	ID        string `xml:"id,attr"`
	ValueName string `xml:"valueName,attr"`
	Required  string `xml:"required,attr"`
}

type policyDefinitionResources struct {
	XMLName       xml.Name       `xml:"policyDefinitionResources"`
	Namespace     string         `xml:"xmlns,attr"`
	Revision      string         `xml:"revision,attr"`
	SchemaVersion string         `xml:"schemaVersion,attr"`
	DisplayName   string         `xml:"displayName"`
	Description   string         `xml:"description"`
	Strings       []stringEntry  `xml:"resources>stringTable>string"`
	Presentations []presentation `xml:"resources>presentationTable>presentation"`
}

type stringEntry struct {
	ID   string `xml:"id,attr"`
	Text string `xml:",chardata"`
}

type presentation struct {
	ID           string        `xml:"id,attr"`
	TextBox      *textBox      `xml:"textBox"`
	MultiTextBox *multiTextBox `xml:"multiTextBox"`
}

type textBox struct {
	RefID string `xml:"refId,attr"`
	Label string `xml:"label"`
}

type multiTextBox struct {
	RefID string `xml:"refId,attr"`
	Text  string `xml:",chardata"`
}
