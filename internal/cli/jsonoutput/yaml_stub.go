package jsonoutput

import "gopkg.in/yaml.v3"

// encodeYAML is a stand-in until the faithful encoder lands: JSON is YAML, so
// the node tree keeps key order, and plain styles let yaml.v3 quote what it must.
func encodeYAML(jsonDocument []byte) ([]byte, error) {
	var node yaml.Node
	if err := yaml.Unmarshal(jsonDocument, &node); err != nil {
		return nil, err
	}

	var plain func(*yaml.Node)
	plain = func(current *yaml.Node) {
		current.Style = 0
		for _, child := range current.Content {
			plain(child)
		}
	}
	plain(&node)

	return yaml.Marshal(&node)
}
