package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

var pathParameterPattern = regexp.MustCompile(`\{([^{}]+)\}`)
var nonAlnumPattern = regexp.MustCompile(`[^a-zA-Z0-9]+`)

func main() {
	inputPath := flag.String("in", "", "input OpenAPI JSON path")
	outputPath := flag.String("out", "", "output OpenAPI JSON path")
	flag.Parse()

	if *inputPath == "" || *outputPath == "" {
		exitWithErr(errors.New("both -in and -out are required"))
	}

	if err := sanitize(*inputPath, *outputPath); err != nil {
		exitWithErr(err)
	}
}

func sanitize(inputPath, outputPath string) error {
	payload, err := os.ReadFile(inputPath)
	if err != nil {
		return fmt.Errorf("read input: %w", err)
	}

	var spec map[string]any
	if err := json.Unmarshal(payload, &spec); err != nil {
		return fmt.Errorf("unmarshal spec: %w", err)
	}

	paths, ok := spec["paths"].(map[string]any)
	if !ok {
		return errors.New("spec paths missing or invalid")
	}

	fixedOperations := 0
	renamedOperationIDs := 0
	seenOperationIDs := map[string]int{}

	// Iterate paths in sorted order. Collision suffixes are assigned in
	// iteration order, so ranging over the map directly made the generated
	// client nondeterministic: the same spec produced different
	// operationId-to-endpoint mappings on every run, silently rewiring which
	// endpoint a suffixed method such as Get3WithResponse actually calls.
	// Nothing caught it because models:verify and client:verify are not run by
	// CI.
	sortedPaths := make([]string, 0, len(paths))
	for rawPath := range paths {
		sortedPaths = append(sortedPaths, rawPath)
	}
	sort.Strings(sortedPaths)

	for _, rawPath := range sortedPaths {
		rawPathItem := paths[rawPath]
		pathItem, ok := rawPathItem.(map[string]any)
		if !ok {
			continue
		}

		requiredParams := requiredPathParams(rawPath)

		for _, method := range []string{"get", "put", "post", "delete", "options", "head", "patch", "trace"} {
			rawOp, exists := pathItem[method]
			if !exists {
				continue
			}

			op, ok := rawOp.(map[string]any)
			if !ok {
				continue
			}

			operationID, _ := op["operationId"].(string)
			if operationID == "" {
				operationID = operationIDFromPath(method, rawPath)
				op["operationId"] = operationID
			}

			canonicalID := canonicalOperationID(operationID)
			if seenOperationIDs[canonicalID] > 0 {
				renamedOperationIDs++
				operationID = uniqueOperationID(operationID, seenOperationIDs)
				op["operationId"] = operationID
				canonicalID = canonicalOperationID(operationID)
			}
			seenOperationIDs[canonicalID]++

			declared := declaredPathParamNames(op["parameters"])
			added := false
			for _, name := range requiredParams {
				if _, present := declared[name]; present {
					continue
				}
				op["parameters"] = appendParameter(op["parameters"], name)
				declared[name] = struct{}{}
				added = true
			}
			if added {
				fixedOperations++
			}
		}
	}

	fixedSchemaFields := fixEpochMillisFields(spec)
	fixedPathParams := fixContentEncodedPathParams(spec)
	renamedProperties := fixSchemaPropertyNames(spec)
	fixedArrayProperties := fixArrayTypedProperties(spec)
	fixedArrayResponses := fixArrayTypedResponses(spec)

	output, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal sanitized spec: %w", err)
	}

	if err := os.WriteFile(outputPath, output, 0o644); err != nil {
		return fmt.Errorf("write output: %w", err)
	}

	fmt.Printf(
		"sanitized OpenAPI spec; fixed operations=%d renamed operationIds=%d fixed schema fields=%d fixed path params=%d renamed properties=%d fixed array properties=%d fixed array responses=%d\n",
		fixedOperations, renamedOperationIDs, fixedSchemaFields, fixedPathParams, renamedProperties, fixedArrayProperties, fixedArrayResponses,
	)
	return nil
}

// epochMillisSchemaFields lists schema properties that Atlassian's spec declares
// as string/date-time but which the server actually returns as epoch
// milliseconds.
//
// The mismatch is not cosmetic: oapi-codegen emits *time.Time for a date-time
// field, and time.Time.UnmarshalJSON only accepts an RFC 3339 string, so every
// response carrying the field fails to decode with
// "Time.UnmarshalJSON: input is not a JSON string". For RestAccessToken this
// made `bb auth token create` fail outright against a real server even though
// the request succeeded.
//
// Every other createdDate in the spec is already declared as an integer, so
// these entries are upstream inconsistencies rather than a deliberate encoding.
// Only schemas whose encoding has been confirmed against a running Bitbucket
// are listed. Roughly twenty other schemas declare string/date-time fields with
// the same suspect shape, but each needs verifying against a real response
// before being rewritten — see the follow-up issue referenced in
// docs/openapi/fixes.yaml.
// epochMillisFieldNames lists property names that Bitbucket always encodes as
// epoch milliseconds, applied wherever they appear in the spec.
//
// Scope is a field name rather than a schema path for two reasons. The spec
// declares createdDate as an integer in 16 places and as string/date-time in 11
// — the date-time declarations are the inconsistency, not a distinct encoding.
// And the same object shape appears inline in several request and response
// bodies, which oapi-codegen emits as anonymous structs; rewriting only some of
// them makes otherwise-identical shapes incompatible and the generated code
// stops compiling.
//
// Only names whose encoding has been observed against a running Bitbucket are
// listed. Other date fields in the spec (expiryDate, lastSeenDate,
// lockAcquireTime and similar) are left alone until someone confirms what the
// server actually sends for them.
var epochMillisFieldNames = map[string]struct{}{
	"createdDate": {},
}

// fixEpochMillisFields rewrites the listed properties to integer/int64 so the
// generated models match what the server sends.
func fixEpochMillisFields(node any) int {
	fixed := 0

	switch typed := node.(type) {
	case map[string]any:
		for key, value := range typed {
			property, isObject := value.(map[string]any)
			if isObject {
				if _, targeted := epochMillisFieldNames[key]; targeted {
					// Only rewrite the shapes this fix targets. If Atlassian
					// corrects the spec upstream, leave their declaration alone
					// rather than forcing it back to the workaround.
					switch {
					case property["type"] == "string" && property["format"] == "date-time":
						property["type"] = "integer"
						property["format"] = "int64"
						property["description"] = "Epoch milliseconds. Upstream spec declares string/date-time; the server returns a number."
						fixed++
					case property["type"] == "number":
						// A bare number becomes float32, whose 24-bit mantissa
						// cannot hold an epoch millisecond: the value is
						// rounded to the nearest ~4.4 minutes on decode, so
						// RestInsightReport.createdDate came back off by
						// minutes with nothing saying so.
						property["type"] = "integer"
						property["format"] = "int64"
						property["description"] = "Epoch milliseconds. Upstream spec declares a bare number, which does not survive a float32 round trip."
						fixed++
					}
				}
			}

			fixed += fixEpochMillisFields(value)
		}
	case []any:
		for _, item := range typed {
			fixed += fixEpochMillisFields(item)
		}
	}

	return fixed
}

func operationIDFromPath(method, path string) string {
	normalizedPath := nonAlnumPattern.ReplaceAllString(path, "_")
	normalizedPath = strings.Trim(normalizedPath, "_")
	if normalizedPath == "" {
		normalizedPath = "root"
	}
	return fmt.Sprintf("%s_%s", strings.ToLower(method), normalizedPath)
}

// uniqueOperationID picks a suffixed operationId that no earlier operation has
// claimed.
//
// Suffixing blindly is not enough: Bitbucket 10.2 ships an operation already
// named "get_2", so renaming a second "get" to "get_2" produced two operations
// with the same id and the generated client failed to compile with duplicate
// Get2 declarations. Keep incrementing until the canonical form is free.
func uniqueOperationID(operationID string, seen map[string]int) string {
	for suffix := 2; ; suffix++ {
		candidate := fmt.Sprintf("%s_%d", operationID, suffix)
		if seen[canonicalOperationID(candidate)] == 0 {
			return candidate
		}
	}
}

func canonicalOperationID(operationID string) string {
	normalized := nonAlnumPattern.ReplaceAllString(operationID, "")
	return strings.ToLower(normalized)
}

func requiredPathParams(path string) []string {
	matches := pathParameterPattern.FindAllStringSubmatch(path, -1)
	if len(matches) == 0 {
		return nil
	}

	params := make([]string, 0, len(matches))
	seen := map[string]struct{}{}
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		name := match[1]
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		params = append(params, name)
	}
	return params
}

func declaredPathParamNames(rawParameters any) map[string]struct{} {
	declared := map[string]struct{}{}

	parameters, ok := rawParameters.([]any)
	if !ok {
		return declared
	}

	for _, rawParameter := range parameters {
		parameter, ok := rawParameter.(map[string]any)
		if !ok {
			continue
		}
		if parameter["in"] != "path" {
			continue
		}
		name, ok := parameter["name"].(string)
		if !ok || name == "" {
			continue
		}
		declared[name] = struct{}{}
	}

	return declared
}

func appendParameter(rawParameters any, name string) []any {
	parameter := map[string]any{
		"name":     name,
		"in":       "path",
		"required": true,
		"schema": map[string]any{
			"type": "string",
		},
	}

	if rawParameters == nil {
		return []any{parameter}
	}

	parameters, ok := rawParameters.([]any)
	if !ok {
		return []any{parameter}
	}

	return append(parameters, parameter)
}

func exitWithErr(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

// fixContentEncodedPathParams rewrites path parameters declared with a `content`
// map into the equivalent plain `schema`.
//
// A parameter carrying `content: {application/json: {schema: ...}}` asks for the
// value to be serialised as JSON, which is what oapi-codegen emits: a
// json.Marshal of the argument rather than the usual style-based encoding. For a
// string parameter that yields a path segment wrapped in literal double quotes,
// and Bitbucket passes the quotes straight through to git:
//
//	git branch --contains "\"<sha>\"" ... -> malformed object name
//
// Bitbucket 10.2 declares exactly one parameter this way, commitId on the
// branches/info endpoint, and it is plainly unintentional -- the same commit id
// is a plain string schema on every other endpoint that takes one. The endpoint
// answers correctly when the id is sent unquoted.
func fixContentEncodedPathParams(spec map[string]any) int {
	paths, ok := spec["paths"].(map[string]any)
	if !ok {
		return 0
	}

	fixed := 0
	for _, rawPathItem := range paths {
		pathItem, ok := rawPathItem.(map[string]any)
		if !ok {
			continue
		}
		for _, rawOp := range pathItem {
			op, ok := rawOp.(map[string]any)
			if !ok {
				continue
			}
			rawParams, ok := op["parameters"].([]any)
			if !ok {
				continue
			}
			for _, rawParam := range rawParams {
				param, ok := rawParam.(map[string]any)
				if !ok {
					continue
				}
				if location, _ := param["in"].(string); location != "path" {
					continue
				}
				if _, alreadyHasSchema := param["schema"]; alreadyHasSchema {
					continue
				}
				content, ok := param["content"].(map[string]any)
				if !ok {
					continue
				}
				mediaType, ok := content["application/json"].(map[string]any)
				if !ok {
					continue
				}
				schema, ok := mediaType["schema"]
				if !ok {
					continue
				}
				param["schema"] = schema
				delete(param, "content")
				fixed++
			}
		}
	}

	return fixed
}

// schemaPropertyRename records a request property whose name in the spec is not
// the name the server reads.
type schemaPropertyRename struct {
	schema       string
	from         string
	to           string
	alsoRequired bool
}

// schemaPropertyRenames lists those renames.
//
// Only properties whose wire name has been confirmed against a running Bitbucket
// belong here. A rename is not a workaround for an awkward name: it exists only
// where sending the documented name fails and sending the other one works.
var schemaPropertyRenames = []schemaPropertyRename{
	{
		// The apply-suggestion endpoint reads the commit message from `message`.
		// Sending the documented `commitMessage` is rejected with
		// "'message' cannot be null or empty", the same answer as sending
		// nothing at all, so the field is also required rather than optional.
		schema:       "RestApplySuggestionRequest",
		from:         "commitMessage",
		to:           "message",
		alsoRequired: true,
	},
}

// fixSchemaPropertyNames applies the confirmed renames to the component schemas.
func fixSchemaPropertyNames(spec map[string]any) int {
	components, ok := spec["components"].(map[string]any)
	if !ok {
		return 0
	}
	schemas, ok := components["schemas"].(map[string]any)
	if !ok {
		return 0
	}

	renamed := 0
	for _, rename := range schemaPropertyRenames {
		schema, ok := schemas[rename.schema].(map[string]any)
		if !ok {
			continue
		}
		properties, ok := schema["properties"].(map[string]any)
		if !ok {
			continue
		}
		property, present := properties[rename.from]
		if !present {
			continue
		}
		if _, taken := properties[rename.to]; taken {
			continue
		}

		delete(properties, rename.from)
		properties[rename.to] = property
		renamed++

		if !rename.alsoRequired {
			continue
		}
		required, _ := schema["required"].([]any)
		alreadyRequired := false
		for _, name := range required {
			if text, ok := name.(string); ok && text == rename.to {
				alreadyRequired = true
				break
			}
		}
		if !alreadyRequired {
			schema["required"] = append(required, rename.to)
		}
	}

	return renamed
}

// schemaArrayProperty names a property the spec declares as a single object but
// the server returns as an array of that object.
type schemaArrayProperty struct {
	schema   string
	property string
}

// schemaArrayProperties lists those properties.
//
// The generated model for a bare object cannot decode a JSON array, so every
// response carrying one fails with "cannot unmarshal array into Go struct
// field". Only properties whose encoding has been confirmed against a running
// Bitbucket are listed here; a plural name on its own is a hint, not evidence.
var schemaArrayProperties = []schemaArrayProperty{
	// A fork's ref synchronization status reports three collections of refs. The
	// GET omits them entirely while synchronization is off, which is why only the
	// call that enables it -- and so gets a populated body back -- failed.
	{schema: "RestRefSyncStatus", property: "aheadRefs"},
	{schema: "RestRefSyncStatus", property: "divergedRefs"},
	{schema: "RestRefSyncStatus", property: "orphanedRefs"},
}

// fixArrayTypedProperties rewrites the listed properties into arrays of the
// object they already describe.
func fixArrayTypedProperties(spec map[string]any) int {
	components, ok := spec["components"].(map[string]any)
	if !ok {
		return 0
	}
	schemas, ok := components["schemas"].(map[string]any)
	if !ok {
		return 0
	}

	fixed := 0
	for _, entry := range schemaArrayProperties {
		schema, ok := schemas[entry.schema].(map[string]any)
		if !ok {
			continue
		}
		properties, ok := schema["properties"].(map[string]any)
		if !ok {
			continue
		}
		property, ok := properties[entry.property].(map[string]any)
		if !ok {
			continue
		}
		if propertyType, _ := property["type"].(string); propertyType != "object" {
			continue
		}

		// readOnly describes the field rather than its element type, so it moves
		// up to the array and does not stay on the item schema.
		wrapper := map[string]any{"type": "array", "items": property}
		if readOnly, present := property["readOnly"]; present {
			wrapper["readOnly"] = readOnly
			delete(property, "readOnly")
		}

		properties[entry.property] = wrapper
		fixed++
	}

	return fixed
}

// arrayTypedResponse names an operation response the spec describes as a single
// object but the server sends as an array of that object.
type arrayTypedResponse struct {
	operationID string
	status      string
}

// arrayTypedResponses lists those responses.
//
// Same rule as the property list: an entry belongs here only where the encoding
// has been seen coming back from a running Bitbucket.
var arrayTypedResponses = []arrayTypedResponse{
	// Adding a GPG key answers with every key the submitted block contained, so
	// even the ordinary one-key case comes back wrapped in an array. The
	// description says "the GPG key that was just created", singular, which is
	// what the schema was written from.
	{operationID: "addKey", status: "200"},
}

// fixArrayTypedResponses rewrites the listed responses into arrays.
func fixArrayTypedResponses(spec map[string]any) int {
	paths, ok := spec["paths"].(map[string]any)
	if !ok {
		return 0
	}

	wanted := map[string]map[string]struct{}{}
	for _, entry := range arrayTypedResponses {
		if wanted[entry.operationID] == nil {
			wanted[entry.operationID] = map[string]struct{}{}
		}
		wanted[entry.operationID][entry.status] = struct{}{}
	}

	fixed := 0
	for _, rawPathItem := range paths {
		pathItem, ok := rawPathItem.(map[string]any)
		if !ok {
			continue
		}
		for _, rawOp := range pathItem {
			op, ok := rawOp.(map[string]any)
			if !ok {
				continue
			}
			operationID, _ := op["operationId"].(string)
			statuses, present := wanted[operationID]
			if !present {
				continue
			}
			responses, ok := op["responses"].(map[string]any)
			if !ok {
				continue
			}
			for status := range statuses {
				response, ok := responses[status].(map[string]any)
				if !ok {
					continue
				}
				content, ok := response["content"].(map[string]any)
				if !ok {
					continue
				}
				mediaType, ok := content["application/json"].(map[string]any)
				if !ok {
					continue
				}
				schema, ok := mediaType["schema"].(map[string]any)
				if !ok {
					continue
				}
				if schemaType, _ := schema["type"].(string); schemaType == "array" {
					continue
				}
				mediaType["schema"] = map[string]any{"type": "array", "items": schema}
				fixed++
			}
		}
	}

	return fixed
}
