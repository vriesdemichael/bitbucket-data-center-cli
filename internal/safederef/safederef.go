// Package safederef reads a pointer the generated client handed back.
//
// oapi-codegen models almost every response field as a pointer, because almost
// every field in Atlassian's spec is optional. Reading one means a nil check,
// and the codebase had thirty-four copies of that nil check across eighteen
// packages under five different names -- safeString, stringValue, safeInt32,
// safeInt64, safeStringSlice.
//
// Individually trivial, which is why nobody consolidated them. Collectively
// they were eighteen places the next one would be added, and they had already
// stopped agreeing: safeStringSlice returned an empty slice in three packages
// and nil in two. That difference is invisible to a caller that ranges or
// counts, and decides whether a required JSON field marshals as [] or null.
//
// These are for reading a decoded response. They are deliberately not generic
// over any pointer type: a caller reaching for safederef.Value[T] is usually
// about to publish a zero where absent was the answer, and the model layer has
// pointers precisely so it can tell those apart (ADR-076).
package safederef

// String reads a string pointer, treating absent as empty.
func String(value *string) string {
	if value == nil {
		return ""
	}

	return *value
}

// Int32 reads an int32 pointer, treating absent as zero.
func Int32(value *int32) int32 {
	if value == nil {
		return 0
	}

	return *value
}

// Int64 reads an int64 pointer, treating absent as zero.
func Int64(value *int64) int64 {
	if value == nil {
		return 0
	}

	return *value
}

// StringSlice reads a slice pointer, treating absent as empty rather than nil.
//
// Empty, not nil, because that is what three of the five copies did and what
// the payload layer wants: a required list marshals as [] rather than null, so
// a caller iterating the result does not have to handle a null the command
// never means to send. The two copies that returned nil had three callers
// between them, every one of which ranged or counted -- where the two are the
// same -- so nothing observes the change.
func StringSlice(values *[]string) []string {
	if values == nil {
		return []string{}
	}

	return *values
}
