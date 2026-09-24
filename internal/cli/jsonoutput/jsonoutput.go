package jsonoutput

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// releaseVersion is the binary's own version, reported as meta.bbVersion.
//
// A package-level value because it is a property of the process rather than of
// any payload, and Write is called from roughly 250 sites that have no reason
// to know it. Set once from cmd/bb before any command runs; a binary built
// without the ldflags stamp reports "dev", which is true.
var releaseVersion = "dev"

// SetReleaseVersion records the version this binary reports in meta.bbVersion.
//
// Call it once, at startup, before any command executes. It is provenance for
// an operator reading stored output, not a compatibility switch: nothing in bb
// branches on it, and nothing outside bb should either (ADR-064).
func SetReleaseVersion(version string) {
	if trimmed := strings.TrimSpace(version); trimmed != "" {
		releaseVersion = trimmed
	}
}

// Format is the encoding a machine document is written in (ADR-095).
//
// The document is the same in every format: it is built once, encoded as JSON,
// and only then re-encoded when another format was asked for. So --yaml cannot
// carry a field --json does not, and a command never has to know which of the
// two it is writing.
type Format string

const (
	FormatJSON Format = "json"
	FormatYAML Format = "yaml"
)

// Settings is what the flags decided about machine output for one invocation.
type Settings struct {
	Format Format
}

// boundWriter carries Settings to every document written through it.
type boundWriter struct {
	io.Writer
	settings Settings
}

// Bind returns a writer that writes to writer and carries settings to every
// document written through it.
//
// Settings travel with the writer rather than as an argument because roughly 250
// sites write a document, all of them through the command's own output writer,
// and none has reason to know which format the flags chose. They are not a
// package variable because tests run commands side by side in one process, each
// with its own flags.
func Bind(writer io.Writer, settings Settings) io.Writer {
	if bound, ok := writer.(*boundWriter); ok {
		writer = bound.Writer
	}

	return &boundWriter{Writer: writer, settings: settings}
}

// settingsOf returns the settings writer carries, or JSON for a writer that
// carries none.
func settingsOf(writer io.Writer) Settings {
	if bound, ok := writer.(*boundWriter); ok {
		return bound.settings
	}

	return Settings{Format: FormatJSON}
}

// Envelope is the bb.machine document written to stdout on success.
//
// It carries no contract version. A payload version exists so a server can tell
// clients which shape they are getting, because those clients cannot choose the
// server's code; a CLI inverts that, since the consumer installs the binary. So
// the binary version is the contract version, and breaking payload changes ride
// the release major -- see ADR-064, which supersedes ADR-014 and is the record
// to read before changing this shape.
type Envelope struct {
	Data any          `json:"data"`
	Meta EnvelopeMeta `json:"meta"`
}

type EnvelopeMeta struct {
	// LimitReached reports that the result set came back at --limit, so there
	// may be more behind it. Omitted for commands that do not list, so its
	// presence is itself the signal that a result set is bounded.
	//
	// Without it a consumer cannot tell a complete result set from the first
	// --limit of an unknown number — the difference between finishing and
	// needing to ask again with a higher --limit or --all.
	LimitReached *bool `json:"limitReached,omitempty"`
	// Encoding is present when data is a body that is not text, carried as a
	// string in this encoding: base64. A JSON string cannot hold arbitrary
	// bytes, and a wrapper object inside data could not be told from a body
	// that is such an object; meta is bb's own, so the encoding goes here.
	Encoding string `json:"encoding,omitempty"`
	// ContentType is the media type of the body Encoding carries.
	ContentType string `json:"contentType,omitempty"`
	// BBVersion is the version of the binary that produced the document.
	//
	// Provenance, for an operator auditing stored output -- not a compatibility
	// switch. Nothing in bb branches on it and nothing outside bb should: the
	// way to pin a contract is to pin the binary (ADR-064).
	BBVersion string `json:"bbVersion"`
}

// ErrorEnvelope is the bb.machine document written to stdout when a command
// fails under --json.
//
// It carries error where a successful run carries data, rather than adding
// error alongside a null data. A consumer decides success or failure by which
// key is present, which stays unambiguous for a command whose successful data
// is legitimately null.
type ErrorEnvelope struct {
	Error EnvelopeError `json:"error"`
	Meta  EnvelopeMeta  `json:"meta"`
}

// EnvelopeError is the classified failure. Kind and ExitCode come from the
// ADR-011 taxonomy, so a script can branch on either without parsing the
// message.
type EnvelopeError struct {
	Kind     string `json:"kind"`
	Message  string `json:"message"`
	ExitCode int    `json:"exitCode"`
	// Details carries handles the caller needs to act on the failure, keyed by
	// name so nobody has to scrape them out of the message: upstreamStatus and
	// upstreamException on a failure Bitbucket answered, one entry per issue
	// from bb doctor.
	//
	// Omitted when there is nothing to carry, so its absence means the message
	// is all there is.
	Details map[string]string `json:"details,omitempty"`
}

// WriteError emits the failure envelope for err.
//
// The caller has already decided to exit non-zero, so a write failure here must
// not change the exit code the taxonomy dictates; it is returned for the caller
// to report, not to act on.
func WriteError(writer io.Writer, err error) error {
	if err == nil {
		return nil
	}

	envelope := ErrorEnvelope{
		Error: EnvelopeError{
			Kind:     string(apperrors.KindOf(err)),
			Message:  apperrors.MessageOf(err),
			ExitCode: apperrors.ExitCode(err),
			Details:  apperrors.DetailsOf(err),
		},
		Meta: EnvelopeMeta{
			BBVersion: releaseVersion,
		},
	}

	return writeDocument(writer, envelope, "error output")
}

func Write(writer io.Writer, payload any) error {
	envelope := Envelope{
		Data: payload,
		Meta: EnvelopeMeta{
			BBVersion: releaseVersion,
		},
	}

	return writeDocument(writer, envelope, "output")
}

// writeDocument encodes one document in the format writer carries and writes
// it. what names the document in an error: "output" or "error output".
func writeDocument(writer io.Writer, document any, what string) error {
	format := settingsOf(writer).Format

	encoded, marshalErr := marshalEnvelope(document)
	if marshalErr == nil && format == FormatYAML {
		encoded, marshalErr = encodeYAML(encoded)
	}
	if marshalErr != nil {
		return apperrors.New(apperrors.KindInternal, fmt.Sprintf("failed to encode %s %s", formatName(format), what), marshalErr)
	}

	if _, writeErr := writer.Write(encoded); writeErr != nil {
		return apperrors.New(apperrors.KindInternal, fmt.Sprintf("failed to write %s %s", formatName(format), what), writeErr)
	}

	return nil
}

// formatName is the format as an error message names it.
func formatName(format Format) string {
	if format == FormatYAML {
		return "YAML"
	}

	return "JSON"
}

// marshalEnvelope renders envelope as indented JSON with HTML escaping off.
//
// encoding/json escapes <, > and & by default, which is meant for embedding
// JSON in HTML. Nothing here does that, and the escaping turns readable strings
// into noise: angle brackets around a placeholder such as <host> arrive as
// unicode escapes in the very message an operator or agent has to read.
//
// Only json.Encoder can turn that off, so it renders into a buffer rather than
// straight to the writer — that keeps encoding failures distinguishable from
// write failures, which report different causes to the user.
func marshalEnvelope(envelope any) ([]byte, error) {
	buffer := &bytes.Buffer{}

	encoder := json.NewEncoder(buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")

	if err := encoder.Encode(envelope); err != nil {
		return nil, err
	}

	// Encode already terminates the document with a newline.
	return buffer.Bytes(), nil
}

// WriteBytes emits a body that is not text: its bytes base64-encoded as the
// payload, with meta saying how they are encoded and what they are.
func WriteBytes(writer io.Writer, body []byte, contentType string) error {
	envelope := Envelope{
		Data: base64.StdEncoding.EncodeToString(body),
		Meta: EnvelopeMeta{Encoding: "base64", ContentType: contentType, BBVersion: releaseVersion},
	}

	return writeDocument(writer, envelope, "output")
}

// WriteList emits a list payload, recording whether --limit cut it short.
func WriteList(writer io.Writer, payload any, limitReached bool) error {
	envelope := Envelope{
		Data: payload,
		Meta: EnvelopeMeta{LimitReached: &limitReached, BBVersion: releaseVersion},
	}

	return writeDocument(writer, envelope, "output")
}

// MarshalIndent renders v the way the envelope is rendered: indented, with HTML
// escaping off.
//
// Exported so a document printed outside an envelope -- a JSON Schema from
// --describe, for instance -- looks the same as everything else bb prints,
// rather than arriving with < where a < was.
func MarshalIndent(value any) ([]byte, error) {
	encoded, err := marshalEnvelope(value)
	if err != nil {
		return nil, apperrors.New(apperrors.KindInternal, "failed to encode JSON output", err)
	}

	return encoded, nil
}
