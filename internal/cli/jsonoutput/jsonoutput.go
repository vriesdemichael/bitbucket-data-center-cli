package jsonoutput

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"

	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
)

const ContractName = "bb.machine"

// releaseVersion is the binary's own version, reported as meta.bb_version.
//
// A package-level value because it is a property of the process rather than of
// any payload, and Write is called from roughly 250 sites that have no reason
// to know it. Set once from cmd/bb before any command runs; a binary built
// without the ldflags stamp reports "dev", which is true.
var releaseVersion = "dev"

// SetReleaseVersion records the version this binary reports in meta.bb_version.
//
// Call it once, at startup, before any command executes. It is provenance for
// an operator reading stored output, not a compatibility switch: nothing in bb
// branches on it, and nothing outside bb should either (ADR-064).
func SetReleaseVersion(version string) {
	if trimmed := strings.TrimSpace(version); trimmed != "" {
		releaseVersion = trimmed
	}
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
	Contract string `json:"contract"`
	// LimitReached reports that the result set came back at --limit, so there
	// may be more behind it. Omitted for commands that do not list, so its
	// presence is itself the signal that a result set is bounded.
	//
	// Without it a consumer cannot tell a complete result set from the first
	// --limit of an unknown number — the difference between finishing and
	// needing to ask again with a higher --limit or --all.
	LimitReached *bool `json:"limit_reached,omitempty"`
	// BBVersion is the version of the binary that produced the document.
	//
	// Provenance, for an operator auditing stored output -- not a compatibility
	// switch. Nothing in bb branches on it and nothing outside bb should: the
	// way to pin a contract is to pin the binary (ADR-064).
	BBVersion string `json:"bb_version"`
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
	ExitCode int    `json:"exit_code"`
	// Details carries handles the caller needs to act on the failure, keyed by
	// name -- bb bulk apply puts operation_id here, so the artifact of a failed
	// or cancelled run can be fetched without scraping the message.
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
			Contract:  ContractName,
			BBVersion: releaseVersion,
		},
	}

	encoded, marshalErr := marshalEnvelope(envelope)
	if marshalErr != nil {
		return apperrors.New(apperrors.KindInternal, "failed to encode JSON error output", marshalErr)
	}

	if _, writeErr := writer.Write(encoded); writeErr != nil {
		return apperrors.New(apperrors.KindInternal, "failed to write JSON error output", writeErr)
	}

	return nil
}

func Write(writer io.Writer, payload any) error {
	envelope := Envelope{
		Data: payload,
		Meta: EnvelopeMeta{
			Contract:  ContractName,
			BBVersion: releaseVersion,
		},
	}

	encoded, marshalErr := marshalEnvelope(envelope)
	if marshalErr != nil {
		return apperrors.New(apperrors.KindInternal, "failed to encode JSON output", marshalErr)
	}

	if _, writeErr := writer.Write(encoded); writeErr != nil {
		return apperrors.New(apperrors.KindInternal, "failed to write JSON output", writeErr)
	}

	return nil
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

// WriteList emits a list payload, recording whether --limit cut it short.
func WriteList(writer io.Writer, payload any, limitReached bool) error {
	envelope := Envelope{
		Data: payload,
		Meta: EnvelopeMeta{Contract: ContractName, LimitReached: &limitReached, BBVersion: releaseVersion},
	}

	encoded, marshalErr := marshalEnvelope(envelope)
	if marshalErr != nil {
		return apperrors.New(apperrors.KindInternal, "failed to encode JSON output", marshalErr)
	}

	if _, writeErr := writer.Write(encoded); writeErr != nil {
		return apperrors.New(apperrors.KindInternal, "failed to write JSON output", writeErr)
	}

	return nil
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
