package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli"
	"gopkg.in/yaml.v3"
)

// TestEveryLeafCommandUnderYAMLWritesTheJSONDocument walks the command tree
// twice, under --json and under --yaml, and holds the two answers to being one
// document (ADR-095).
//
// --yaml is not a second output path: the document is built once and only its
// encoding differs. This is what keeps that true as commands are added -- a
// command that wrote JSON itself, or checked the --json flag rather than asking
// for machine output, would answer --yaml with text or with nothing, and this
// fails it. It reaches what the JSON walk reaches: the paths a command can take
// with no configuration and no server, the failure envelope for most of them.
func TestEveryLeafCommandUnderYAMLWritesTheJSONDocument(t *testing.T) {
	sealEnvironment(t)

	commandsThatDoNotEmitJSON := exemptCommands(t)

	reported := 0

	for _, path := range leafCommandPaths(t) {
		if commandsThatDoNotEmitJSON[path] {
			continue
		}

		t.Run(path, func(t *testing.T) {
			t.Parallel()

			asJSON := runForStdout(append([]string{"--json", "--no-input"}, strings.Fields(path)...))
			asYAML := runForStdout(append([]string{"--yaml", "--no-input"}, strings.Fields(path)...))

			var fromJSON any
			decoder := json.NewDecoder(bytes.NewReader(asJSON))
			decoder.UseNumber()
			if err := decoder.Decode(&fromJSON); err != nil {
				t.Fatalf("wrote non-JSON under --json: %v\n%s", err, asJSON)
			}

			yamlDecoder := yaml.NewDecoder(bytes.NewReader(asYAML))
			var fromYAML any
			if err := yamlDecoder.Decode(&fromYAML); err != nil {
				t.Fatalf("wrote non-YAML under --yaml: %v\n%s", err, asYAML)
			}
			var second any
			if err := yamlDecoder.Decode(&second); !errors.Is(err, io.EOF) {
				t.Errorf("wrote more than one YAML document under --yaml (ADR-075)\n%s", asYAML)
			}

			if difference := sameDocument(fromJSON, fromYAML, "$"); difference != "" {
				t.Errorf("--yaml is not the --json document: %s\n--json:\n%s\n--yaml:\n%s", difference, asJSON, asYAML)
			}
		})

		reported++
	}

	if reported < 200 {
		t.Fatalf("walked only %d commands; the tree walk has stopped reaching the tree", reported)
	}
}

// runForStdout runs bb with args through the real entry point and returns what
// it wrote to stdout.
func runForStdout(args []string) []byte {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	root := cli.NewRootCommand()
	root.SetArgs(args)
	root.SetErr(stderr)

	_ = executeRootCommand(root, args, stdout, stderr)

	return stdout.Bytes()
}

// sameDocument compares a JSON value decoded with UseNumber against a YAML
// value decoded by yaml.v3, and describes the first difference, or returns "".
//
// Numbers are compared by value: JSON keeps the literal, and YAML hands back an
// int, a uint64 or a float64 for the same number.
func sameDocument(fromJSON, fromYAML any, where string) string {
	switch expected := fromJSON.(type) {
	case map[string]any:
		actual, ok := fromYAML.(map[string]any)
		if !ok {
			return fmt.Sprintf("%s: an object in JSON is %T in YAML", where, fromYAML)
		}
		if len(actual) != len(expected) {
			return fmt.Sprintf("%s: %d keys in JSON, %d in YAML", where, len(expected), len(actual))
		}
		for key, value := range expected {
			other, present := actual[key]
			if !present {
				return fmt.Sprintf("%s: key %q is missing from YAML", where, key)
			}
			if difference := sameDocument(value, other, where+"."+key); difference != "" {
				return difference
			}
		}
		return ""
	case []any:
		actual, ok := fromYAML.([]any)
		if !ok || len(actual) != len(expected) {
			return fmt.Sprintf("%s: a list of %d in JSON is %#v in YAML", where, len(expected), fromYAML)
		}
		for index := range expected {
			if difference := sameDocument(expected[index], actual[index], fmt.Sprintf("%s[%d]", where, index)); difference != "" {
				return difference
			}
		}
		return ""
	case json.Number:
		want, ok := new(big.Rat).SetString(expected.String())
		got, gotOK := numberOf(fromYAML)
		if !ok || !gotOK || want.Cmp(got) != 0 {
			return fmt.Sprintf("%s: the number %s in JSON is %#v in YAML", where, expected, fromYAML)
		}
		return ""
	default:
		if fromJSON != fromYAML {
			return fmt.Sprintf("%s: %#v in JSON is %#v in YAML", where, fromJSON, fromYAML)
		}
		return ""
	}
}

func numberOf(value any) (*big.Rat, bool) {
	switch number := value.(type) {
	case int:
		return new(big.Rat).SetInt64(int64(number)), true
	case int64:
		return new(big.Rat).SetInt64(number), true
	case uint64:
		return new(big.Rat).SetUint64(number), true
	case float64:
		rat := new(big.Rat)
		if rat.SetFloat64(number) == nil {
			return nil, false
		}
		return rat, true
	default:
		return nil, false
	}
}
