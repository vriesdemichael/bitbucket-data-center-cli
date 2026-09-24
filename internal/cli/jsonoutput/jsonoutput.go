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

// Mode is the question an invocation asks, and so the member of the document
// that answers it (ADR-096). The flags decide it, and nothing that happens during
// the run changes it: a caller knows what it holds from the key alone.
type Mode string

const (
	// ModeRun answers with data, or with error when the run fails.
	ModeRun Mode = ""
	// ModeDryRun answers with preview, a verdict on the real run.
	ModeDryRun Mode = "dry-run"
	// ModeDescribe answers with description: what a command returns (ADR-097).
	ModeDescribe Mode = "describe"
)

// Settings is what the flags decided about machine output for one invocation.
type Settings struct {
	// Machine is whether a document was asked for at all, in either format.
	Machine bool
	Format  Format
	Mode    Mode
	// Command is the command writing the document, by its canonical path, or ""
	// when no command resolved. It is reported as meta.command.
	Command string
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

// SettingsOf returns the settings writer carries, and whether it carries any.
//
// A command that renders text reads the command path from here, and main reads
// the settings the invocation ran with when it has a failure to report.
func SettingsOf(writer io.Writer) (Settings, bool) {
	if bound, ok := writer.(*boundWriter); ok {
		return bound.settings, true
	}

	return Settings{Format: FormatJSON}, false
}

// settingsOf returns the settings writer carries, or JSON for a run for a
// writer that carries none.
func settingsOf(writer io.Writer) Settings {
	settings, _ := SettingsOf(writer)
	return settings
}

// Tier says how a verdict was reached (ADR-078), strongest first.
type Tier string

const (
	// TierServerValidated means Bitbucket answered the exact question, through
	// its own dry-run endpoint or an equivalent authoritative call -- or the
	// command only reads, and ran.
	TierServerValidated Tier = "server-validated"

	// TierPreconditionsChecked means the caller's permission and the current
	// state were both fetched, and the preconditions for the operation were
	// evaluated against them.
	TierPreconditionsChecked Tier = "preconditions-checked"

	// TierPredicted means the answer was derived from partial state.
	TierPredicted Tier = "predicted"
)

// Weaker reports whether tier claims less than other. A tier nobody named
// claims least of all.
func (tier Tier) Weaker(other Tier) bool {
	return tierRank(tier) < tierRank(other)
}

func tierRank(tier Tier) int {
	switch tier {
	case TierServerValidated:
		return 2
	case TierPreconditionsChecked:
		return 1
	default:
		return 0
	}
}

// Outcome is what one effect of the real run would come to.
type Outcome string

const (
	OutcomeWouldApply Outcome = "would-apply"
	OutcomeNoOp       Outcome = "no-op"
	OutcomeWouldFail  Outcome = "would-fail"
)

// Preview is the answer under --dry-run: a verdict on the real run (ADR-096).
//
// Error is present exactly when the verdict is that the run would fail, so a
// caller asks one question of it, the same one it asks of a run.
type Preview struct {
	// Tier is the weakest of the checks behind the verdict.
	Tier Tier `json:"tier" jsonschema:"How far to trust the verdict: the weakest check behind it."`
	// Effects are what the run would change, each with its outcome and why.
	// Empty for a command that only reads.
	Effects []Effect `json:"effects" jsonschema:"What the run would change, each with its outcome. Empty for a command that only reads."`
	// Data is what a command that only reads returned: reading changes
	// nothing, so it runs for real under --dry-run.
	Data any `json:"data,omitempty" jsonschema:"What a command that only reads returned: it runs for real under --dry-run."`
	// Error is what the real run would fail with.
	Error *EnvelopeError `json:"error,omitempty" jsonschema:"What the real run would fail with. Present exactly when it would fail."`
}

// Effect is one change the real run would make.
type Effect struct {
	// Action is create, update or delete.
	Action string `json:"action" jsonschema:"The kind of change: create, update or delete."`
	// Target names what the change is made to. Its keys depend on the command.
	Target  map[string]any `json:"target" jsonschema:"What the change is made to. Its keys depend on the command."`
	Outcome Outcome        `json:"outcome" jsonschema:"What the change would come to."`
	// Reasons say why the outcome is what it is; for would-fail, what stops it.
	Reasons []string `json:"reasons" jsonschema:"Why the outcome is what it is; for would-fail, what stops it."`
}

// PreviewEnvelope is the document written under --dry-run.
type PreviewEnvelope struct {
	Preview Preview      `json:"preview"`
	Meta    EnvelopeMeta `json:"meta"`
}

// DescriptionEnvelope is the document written under --describe.
type DescriptionEnvelope struct {
	Description any          `json:"description"`
	Meta        EnvelopeMeta `json:"meta"`
}

// WriteDescription emits what --describe answers.
func WriteDescription(writer io.Writer, description any) error {
	meta := metaFor(settingsOf(writer), EnvelopeMeta{})

	return writeDocument(writer, DescriptionEnvelope{Description: description, Meta: meta}, "output")
}

// IsVerdict reports whether err, met under --dry-run, is an answer about the
// real run rather than a failure to reach one (ADR-096).
//
// Bitbucket not answering, an interrupt, a bug in bb and an outcome bb could not
// tell say nothing about what the real run would do, so they are reported as
// themselves. Everything else -- an invalid invocation, a 404, a conflict, a
// veto -- is what the real run would meet too, and is the verdict.
func IsVerdict(err error) bool {
	if err == nil {
		return false
	}

	switch apperrors.KindOf(err) {
	case apperrors.KindTransient, apperrors.KindCancelled, apperrors.KindInternal, apperrors.KindUnknownOutcome:
		return false
	default:
		return true
	}
}

// TierOfFailure is how a verdict found as a failure was reached: from
// Bitbucket's own answer, or from what bb checked before asking.
func TierOfFailure(err error) Tier {
	if _, answered := apperrors.DetailsOf(err)["upstreamStatus"]; answered {
		return TierServerValidated
	}

	return TierPreconditionsChecked
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
	// Command is the command that wrote the document, by its canonical path:
	// bb pr view reports pr get. With it a document held on its own says which
	// --describe describes it. Absent when no command resolved.
	Command string `json:"command,omitempty" jsonschema:"The command that wrote it, by its canonical path."`
	// LimitReached reports that the result set came back at --limit, so there
	// may be more behind it. Omitted for commands that do not list, so its
	// presence is itself the signal that a result set is bounded.
	//
	// Without it a consumer cannot tell a complete result set from the first
	// --limit of an unknown number — the difference between finishing and
	// needing to ask again with a higher --limit or --all.
	LimitReached *bool `json:"limitReached,omitempty" jsonschema:"On a listing: true when it stopped at --limit, so there may be more."`
	// Encoding is present when data is a body that is not text, carried as a
	// string in this encoding: base64. A JSON string cannot hold arbitrary
	// bytes, and a wrapper object inside data could not be told from a body
	// that is such an object; meta is bb's own, so the encoding goes here.
	Encoding string `json:"encoding,omitempty" jsonschema:"Set when data is bytes carried as a string: base64."`
	// ContentType is the media type of the body Encoding carries.
	ContentType string `json:"contentType,omitempty" jsonschema:"Set with encoding: the media type of the bytes."`
	// BBVersion is the version of the binary that produced the document.
	//
	// Provenance, for an operator auditing stored output -- not a compatibility
	// switch. Nothing in bb branches on it and nothing outside bb should: the
	// way to pin a contract is to pin the binary (ADR-064).
	BBVersion string `json:"bbVersion" jsonschema:"The bb version that wrote it."`
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
	Kind     string `json:"kind" jsonschema:"What kind of failure it is."`
	Message  string `json:"message" jsonschema:"What went wrong, for a person."`
	ExitCode int    `json:"exitCode" jsonschema:"The exit code the kind maps to."`
	// Details carries handles the caller needs to act on the failure, keyed by
	// name so nobody has to scrape them out of the message: upstreamStatus and
	// upstreamException on a failure Bitbucket answered, one entry per issue
	// from bb doctor.
	//
	// Omitted when there is nothing to carry, so its absence means the message
	// is all there is.
	Details map[string]string `json:"details,omitempty" jsonschema:"Handles the caller needs to act on the failure, by name."`
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

	settings := settingsOf(writer)
	failure := EnvelopeErrorOf(err)
	meta := metaFor(settings, EnvelopeMeta{})

	// Under --dry-run a failure that is an answer about the real run is the
	// verdict, inside the preview; only a failure to reach one is a top-level
	// error (ADR-096).
	if settings.Mode == ModeDryRun && IsVerdict(err) {
		preview := Preview{Tier: TierOfFailure(err), Effects: []Effect{}, Error: &failure}
		return writeDocument(writer, PreviewEnvelope{Preview: preview, Meta: meta}, "output")
	}

	// Under --describe, a path that names no command is the answer: there is
	// no such command to describe (ADR-097).
	if settings.Mode == ModeDescribe && apperrors.IsKind(err, apperrors.KindValidation) {
		description := map[string]any{"error": failure}
		return writeDocument(writer, DescriptionEnvelope{Description: description, Meta: meta}, "output")
	}

	return writeDocument(writer, ErrorEnvelope{Error: failure, Meta: meta}, "error output")
}

// EnvelopeErrorOf is err as the document reports it.
func EnvelopeErrorOf(err error) EnvelopeError {
	return EnvelopeError{
		Kind:     string(apperrors.KindOf(err)),
		Message:  apperrors.MessageOf(err),
		ExitCode: apperrors.ExitCode(err),
		Details:  apperrors.DetailsOf(err),
	}
}

// Write emits what a command returned.
func Write(writer io.Writer, payload any) error {
	return writeResult(writer, payload, EnvelopeMeta{})
}

// WritePreview emits the verdict of a dry run.
func WritePreview(writer io.Writer, preview Preview) error {
	if preview.Effects == nil {
		preview.Effects = []Effect{}
	}

	meta := metaFor(settingsOf(writer), EnvelopeMeta{})

	return writeDocument(writer, PreviewEnvelope{Preview: preview, Meta: meta}, "output")
}

// writeResult writes what a command returned: as data for a run, and inside
// the preview for a dry run of a command that only reads -- reading changes
// nothing, so it ran for real, and the member is still the one --dry-run asks
// for.
func writeResult(writer io.Writer, payload any, meta EnvelopeMeta) error {
	settings := settingsOf(writer)
	meta = metaFor(settings, meta)

	if settings.Mode == ModeDryRun {
		preview := Preview{Tier: TierServerValidated, Effects: []Effect{}, Data: payload}
		return writeDocument(writer, PreviewEnvelope{Preview: preview, Meta: meta}, "output")
	}

	return writeDocument(writer, Envelope{Data: payload, Meta: meta}, "output")
}

// metaFor completes a command's meta with what every document carries.
func metaFor(settings Settings, meta EnvelopeMeta) EnvelopeMeta {
	meta.Command = settings.Command
	meta.BBVersion = releaseVersion

	return meta
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
	return writeResult(writer, base64.StdEncoding.EncodeToString(body), EnvelopeMeta{Encoding: "base64", ContentType: contentType})
}

// WriteList emits a list payload, recording whether --limit cut it short.
func WriteList(writer io.Writer, payload any, limitReached bool) error {
	return writeResult(writer, payload, EnvelopeMeta{LimitReached: &limitReached})
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
