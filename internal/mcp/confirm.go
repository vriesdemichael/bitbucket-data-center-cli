package mcp

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Asking says when a tool asks the person to confirm a call before it runs.
//
// A tool asks when it merges, changes whether or when a pull request merges,
// or feeds a check that decides whether one may, and when it creates a tag,
// which release pipelines commonly act on. The values are part of the --json
// contract of bb ai mcp tools.
type Asking string

const (
	// AsksNever: the tool runs when called.
	AsksNever Asking = "never"
	// AsksAlways: every call asks.
	AsksAlways Asking = "always"
	// AsksWhenDraftChanges: update_pull_request asks only for a call that
	// changes the draft flag. A draft cannot be merged, and marking a pull
	// request a draft cancels its auto-merge, so the flag decides whether and
	// when it merges. A title or description does not.
	AsksWhenDraftChanges Asking = "when-draft-changes"
)

// confirmKey names the one field of the confirmation form, and the answer's
// key among a retried call's inputResponses.
const confirmKey = "confirm"

// confirmationTTL bounds how long an unanswered confirmation stays valid. It
// covers a person reading the dialog, not a call parked for later.
const confirmationTTL = 10 * time.Minute

// Outcomes of a confirmation, as the audit record carries them (ADR-062).
const (
	confirmationAccepted    = "accepted"
	confirmationDeclined    = "declined"
	confirmationCancelled   = "cancelled"
	confirmationUnavailable = "unavailable"
)

// ask describes how a tool that asks builds its confirmation. In is the tool's
// input type.
type ask[In any] struct {
	// when reports whether a call asks. Nil means every call does.
	when func(In) bool

	// confirm reads what the person needs to see and returns the confirmation.
	// It may call Bitbucket; an error ends the call before anyone is asked.
	confirm func(ctx context.Context, c Clients, in In) (confirmation, error)

	// hold applies the confirmation's pin to the input before the tool runs.
	// Nil when the tool pins nothing.
	hold func(in *In, pin string) error
}

// confirmation is what the person is asked, and what the call is then held to.
type confirmation struct {
	// Message says what will happen, as the client's dialog shows it. It
	// carries no link: clients should not render one clickable in a form.
	Message string

	// Label is the checkbox's title. It names the target, so a click made out
	// of habit still passes over it.
	Label string

	// Pin is a value the accepted call is held to, such as the pull request
	// version the person saw. A merge then fails if the pull request changed
	// in between, rather than merging something nobody was shown.
	Pin string
}

// confirmed wraps a tool's handler so that a call runs only once the person
// has accepted it through the client.
//
// It lives in the handler rather than in the governance middleware because of
// where go-sdk bridges clients on the handshake-era revisions: its bridge sits
// between the middleware and the handler, so an input request the middleware
// returned would never reach a client that must be sent elicitation/create
// itself. Every tool is registered through toolSpec, so this is still one
// place, and it runs after the SDK has validated the arguments.
//
// A 2026-07-28 client sees two calls: the first answers input_required with
// the form and a sealed request state, and the retry carries the answer and
// the state back. A handshake-era client sees one call, during which go-sdk
// sends the form itself and runs this handler again with the answer.
func confirmed[In, Out any](tool string, policy ask[In], clients Clients, handler mcp.ToolHandlerFor[In, Out]) mcp.ToolHandlerFor[In, Out] {
	return func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
		var none Out

		if policy.when != nil && !policy.when(in) {
			return handler(ctx, req, in)
		}

		if !canConfirm(req) {
			recordConfirmation(ctx, confirmationUnavailable)
			return nil, none, missingElicitation(tool)
		}

		digest, err := inputDigest(in)
		if err != nil {
			return nil, none, fmt.Errorf("%s: %w", tool, err)
		}

		answer, answered := req.Params.InputResponses[confirmKey]
		state := req.Params.RequestState

		switch {
		case state == "" && answered:
			// An answer with no question behind it. Accepting it would let a
			// client hold a call to a confirmation bb never issued.
			return nil, none, invalidConfirmation("the answer came without the request state its confirmation carried")
		case state == "":
			return askFor[In, Out](ctx, tool, digest, policy, clients, in)
		}

		pin, err := confirmations.open(state, tool, digest)
		if err != nil {
			return nil, none, err
		}
		if !answered {
			// The client retried without the answer. The spec asks the server
			// to ask again rather than fail.
			return askFor[In, Out](ctx, tool, digest, policy, clients, in)
		}

		result, ok := answer.(*mcp.ElicitResult)
		if !ok {
			return nil, none, invalidConfirmation("the answer is not an elicitation result")
		}
		if err := confirmations.consume(state); err != nil {
			return nil, none, err
		}

		switch {
		case result.Action == "accept" && result.Content[confirmKey] == true:
			recordConfirmation(ctx, confirmationAccepted)
		case result.Action == "cancel":
			recordConfirmation(ctx, confirmationCancelled)
			return nil, none, fmt.Errorf("%s did not run: the person closed the confirmation without answering. Do not call it again unless they ask", tool)
		default:
			// Declined, or accepted with the box left unticked.
			recordConfirmation(ctx, confirmationDeclined)
			return nil, none, fmt.Errorf("%s did not run: the person declined. Do not call it again unless they ask", tool)
		}

		if policy.hold != nil {
			if err := policy.hold(&in, pin); err != nil {
				return nil, none, fmt.Errorf("%s: %w", tool, err)
			}
		}

		return handler(ctx, req, in)
	}
}

// askFor builds the confirmation and returns it as an input request.
func askFor[In, Out any](ctx context.Context, tool, digest string, policy ask[In], clients Clients, in In) (*mcp.CallToolResult, Out, error) {
	var none Out

	c, err := policy.confirm(ctx, clients, in)
	if err != nil {
		return nil, none, fmt.Errorf("%s: %w", tool, err)
	}

	state, err := confirmations.seal(tool, digest, c.Pin)
	if err != nil {
		return nil, none, fmt.Errorf("%s: %w", tool, err)
	}

	return &mcp.CallToolResult{
		InputRequests: mcp.InputRequestMap{confirmKey: confirmationForm(c)},
		RequestState:  state,
	}, none, nil
}

// confirmationForm is the elicitation a confirmation is shown as: the message,
// and one required checkbox with no default, so accepting takes a tick.
//
// A form with no fields would be simpler and is accepted without anyone
// seeing it by a client whose approvals are set to automatic; Codex does this.
func confirmationForm(c confirmation) *mcp.ElicitParams {
	return &mcp.ElicitParams{
		Message: c.Message,
		RequestedSchema: &jsonschema.Schema{
			Type: "object",
			Properties: map[string]*jsonschema.Schema{
				confirmKey: {Type: "boolean", Title: c.Label},
			},
			Required: []string{confirmKey},
		},
	}
}

// canConfirm reports whether this request's client declared form elicitation.
//
// The 2026-07-28 revision declares capabilities per request, so this is asked
// of the call itself, never of an earlier one. An empty elicitation object
// means form mode; only a client that declares URL mode alone cannot show one.
func canConfirm(req *mcp.CallToolRequest) bool {
	caps := req.ClientCapabilities()
	if caps == nil || caps.Elicitation == nil {
		return false
	}

	return caps.Elicitation.Form != nil || caps.Elicitation.URL == nil
}

// missingElicitation is the error a client that cannot show a confirmation
// gets: MissingRequiredClientCapability, naming the capability as a
// ClientCapabilities object, as the 2026-07-28 schema defines it.
//
// Every client gets it, including those on handshake-era revisions, which
// have no such code: a person who cannot be asked uses a client that can, or
// acts in Bitbucket. The data is written out by hand because marshalling the
// SDK's ClientCapabilities adds a roots entry nothing asked for.
func missingElicitation(tool string) error {
	return &jsonrpc.Error{
		Code:    mcp.CodeMissingRequiredClientCapabilities,
		Message: fmt.Sprintf("%s asks the person to confirm it, and this client cannot show a confirmation", tool),
		Data:    json.RawMessage(`{"requiredCapabilities":{"elicitation":{}}}`),
	}
}

// invalidConfirmation is the error for a retry that does not match the
// confirmation it claims to answer. It is a protocol error rather than a tool
// error: an honest client never sends one.
func invalidConfirmation(reason string) error {
	return &jsonrpc.Error{
		Code:    jsonrpc.CodeInvalidParams,
		Message: "the confirmation cannot be used: " + reason,
	}
}

// inputDigest fingerprints a call's input, so an answer can only accept the
// call it was asked about.
//
// The input is the decoded struct, after the governance middleware has bound
// it to the scope, so the fingerprint does not depend on key order or spacing.
func inputDigest(in any) (string, error) {
	encoded, err := json.Marshal(in)
	if err != nil {
		return "", fmt.Errorf("cannot fingerprint the call for its confirmation: %w", err)
	}
	sum := sha256.Sum256(encoded)

	return hex.EncodeToString(sum[:]), nil
}

// sealedConfirmation is what a request state holds.
type sealedConfirmation struct {
	Tool    string `json:"t"`
	Digest  string `json:"d"`
	Pin     string `json:"p,omitempty"`
	Nonce   string `json:"n"`
	Expires int64  `json:"e"`
}

// confirmationSeal signs request states and remembers which were answered.
//
// The spec treats a request state as controlled by an attacker: a client could
// otherwise answer "accept" for a call that was never shown to anyone. The key
// is drawn when the process starts and never leaves it; every round trip of a
// stdio session reaches the same process, and a state from an earlier one no
// longer verifies. One process serves one Bitbucket identity, so the key also
// binds a state to it.
type confirmationSeal struct {
	key []byte

	mu       sync.Mutex
	answered map[string]time.Time
	now      func() time.Time
}

// confirmations is the process's seal.
var confirmations = newConfirmationSeal()

func newConfirmationSeal() *confirmationSeal {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		panic(fmt.Sprintf("drawing the confirmation key: %v", err))
	}

	return &confirmationSeal{key: key, answered: map[string]time.Time{}, now: time.Now}
}

// seal issues the request state for a confirmation of tool with this input.
func (s *confirmationSeal) seal(tool, digest, pin string) (string, error) {
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("drawing a confirmation nonce: %w", err)
	}

	payload, err := json.Marshal(sealedConfirmation{
		Tool:    tool,
		Digest:  digest,
		Pin:     pin,
		Nonce:   hex.EncodeToString(nonce),
		Expires: s.now().Add(confirmationTTL).Unix(),
	})
	if err != nil {
		return "", fmt.Errorf("encoding the confirmation state: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(s.mac(payload)), nil
}

// open verifies a request state against the call it came back with and
// returns its pin.
//
// A state that does not verify, names another tool or another input, or was
// answered already is refused as a protocol error. One that merely expired is
// a tool error instead, since the model can do something about it: ask again.
func (s *confirmationSeal) open(state, tool, digest string) (string, error) {
	sealed, err := s.verify(state)
	if err != nil {
		return "", err
	}

	switch {
	case sealed.Tool != tool:
		return "", invalidConfirmation("it was issued for another tool")
	case sealed.Digest != digest:
		return "", invalidConfirmation("the call changed since the person was asked")
	case s.now().Unix() > sealed.Expires:
		return "", fmt.Errorf("%s did not run: its confirmation expired before it was answered. Call it again to ask again", tool)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, used := s.answered[sealed.Nonce]; used {
		return "", invalidConfirmation("it was answered already")
	}

	return sealed.Pin, nil
}

// consume marks a request state answered, so its answer cannot be sent again.
func (s *confirmationSeal) consume(state string) error {
	sealed, err := s.verify(state)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	for nonce, expires := range s.answered {
		if now.After(expires) {
			delete(s.answered, nonce)
		}
	}
	if _, used := s.answered[sealed.Nonce]; used {
		return invalidConfirmation("it was answered already")
	}
	// Kept until it would have expired anyway, when it can no longer verify.
	s.answered[sealed.Nonce] = time.Unix(sealed.Expires, 0)

	return nil
}

// verify checks a request state's signature and decodes it.
func (s *confirmationSeal) verify(state string) (sealedConfirmation, error) {
	encodedPayload, encodedMAC, found := strings.Cut(state, ".")
	if !found {
		return sealedConfirmation{}, invalidConfirmation("its request state is malformed")
	}

	payload, err := base64.RawURLEncoding.DecodeString(encodedPayload)
	if err != nil {
		return sealedConfirmation{}, invalidConfirmation("its request state is malformed")
	}
	mac, err := base64.RawURLEncoding.DecodeString(encodedMAC)
	if err != nil {
		return sealedConfirmation{}, invalidConfirmation("its request state is malformed")
	}
	if !hmac.Equal(mac, s.mac(payload)) {
		return sealedConfirmation{}, invalidConfirmation("its request state was not issued by this server")
	}

	var sealed sealedConfirmation
	if err := json.Unmarshal(payload, &sealed); err != nil {
		return sealedConfirmation{}, invalidConfirmation("its request state is malformed")
	}

	return sealed, nil
}

func (s *confirmationSeal) mac(payload []byte) []byte {
	h := hmac.New(sha256.New, s.key)
	h.Write(payload)

	return h.Sum(nil)
}

// confirmationRecorder carries a call's confirmation outcome from the handler
// to the governance middleware, which writes it into the audit record.
//
// The middleware cannot read the outcome off the request: go-sdk's bridge for
// handshake-era clients retries the handler with a copy of the parameters,
// which the middleware never sees.
type confirmationRecorder struct {
	mu      sync.Mutex
	outcome string
}

type confirmationRecorderKey struct{}

// withConfirmationRecorder returns a context that collects the outcome of any
// confirmation a call makes.
func withConfirmationRecorder(ctx context.Context) (context.Context, *confirmationRecorder) {
	recorder := &confirmationRecorder{}

	return context.WithValue(ctx, confirmationRecorderKey{}, recorder), recorder
}

// recordConfirmation notes a confirmation's outcome, when something collects it.
func recordConfirmation(ctx context.Context, outcome string) {
	recorder, ok := ctx.Value(confirmationRecorderKey{}).(*confirmationRecorder)
	if !ok {
		return
	}

	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	recorder.outcome = outcome
}

// get returns the confirmation outcome recorded, or "" when there was none.
func (r *confirmationRecorder) get() string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.outcome
}
