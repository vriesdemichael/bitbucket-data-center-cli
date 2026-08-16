package jsonoutput

import (
	"bytes"
	"encoding/json"
	"io"

	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
)

const ContractVersion = "v2"
const ContractName = "bb.machine"

type Envelope struct {
	Version string       `json:"version"`
	Data    any          `json:"data"`
	Meta    EnvelopeMeta `json:"meta"`
}

type EnvelopeMeta struct {
	Contract string `json:"contract"`
}

// ErrorEnvelope is the bb.machine v2 document written to stdout when a command
// fails under --json.
//
// It carries error where a successful run carries data, rather than adding
// error alongside a null data. A consumer decides success or failure by which
// key is present, which stays unambiguous for a command whose successful data
// is legitimately null.
type ErrorEnvelope struct {
	Version string        `json:"version"`
	Error   EnvelopeError `json:"error"`
	Meta    EnvelopeMeta  `json:"meta"`
}

// EnvelopeError is the classified failure. Kind and ExitCode come from the
// ADR-011 taxonomy, so a script can branch on either without parsing the
// message.
type EnvelopeError struct {
	Kind     string `json:"kind"`
	Message  string `json:"message"`
	ExitCode int    `json:"exit_code"`
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
		Version: ContractVersion,
		Error: EnvelopeError{
			Kind:     string(apperrors.KindOf(err)),
			Message:  apperrors.MessageOf(err),
			ExitCode: apperrors.ExitCode(err),
		},
		Meta: EnvelopeMeta{
			Contract: ContractName,
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
		Version: ContractVersion,
		Data:    payload,
		Meta: EnvelopeMeta{
			Contract: ContractName,
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
