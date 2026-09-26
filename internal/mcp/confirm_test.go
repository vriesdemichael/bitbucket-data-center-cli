package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// confirmationEras are the two ways a client answers a confirmation. A
// 2026-07-28 client calls the tool again with the answer; a handshake-era
// client is sent elicitation/create by go-sdk while its one call is open. The
// server's code is the same for both, and go-sdk's bridge decides which path a
// call takes, so the tests that matter run against both.
var confirmationEras = []struct {
	name     string
	protocol string
}{
	{name: "2026-07-28", protocol: ""},
	{name: "2025-11-25", protocol: "2025-11-25"},
}

// The clients point at a closed port (testClients), so a tool that runs fails
// at the transport, and its handler's own error says so. That failure is the
// evidence a call reached the point of calling Bitbucket; a confirmation that
// stops a call stops it before then.
const createTagRan = "create_tag failed"

var tagArguments = map[string]any{"project": "PROJ", "repo": "payments", "name": "v1.2.3", "start_point": "main"}

// answering is a client's side of a confirmation: it records what it was asked
// and answers with answer.
type answering struct {
	mu     sync.Mutex
	asked  []*mcp.ElicitParams
	answer func(*mcp.ElicitParams) *mcp.ElicitResult
}

func (a *answering) handle(_ context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.asked = append(a.asked, req.Params)

	return a.answer(req.Params), nil
}

func (a *answering) questions() []*mcp.ElicitParams {
	a.mu.Lock()
	defer a.mu.Unlock()

	return append([]*mcp.ElicitParams(nil), a.asked...)
}

func accept(*mcp.ElicitParams) *mcp.ElicitResult {
	return &mcp.ElicitResult{Action: "accept", Content: map[string]any{confirmKey: true}}
}

func decline(*mcp.ElicitParams) *mcp.ElicitResult {
	return &mcp.ElicitResult{Action: "decline"}
}

func testServer(t *testing.T) ServerOptions {
	t.Helper()

	return ServerOptions{Name: "bb", Version: "test", Clients: testClients(t)}
}

// connectAsking connects a client that can show a confirmation and answers it
// with answer, speaking protocol ("" for the newest).
func connectAsking(t *testing.T, opts ServerOptions, protocol string, answer func(*mcp.ElicitParams) *mcp.ElicitResult) (*mcp.ClientSession, *answering) {
	t.Helper()
	answers := &answering{answer: answer}

	return connectClient(t, opts, &mcp.ClientOptions{ElicitationHandler: answers.handle}, protocol), answers
}

// connectClient is connectWith for a client with options of its own.
func connectClient(t *testing.T, opts ServerOptions, clientOptions *mcp.ClientOptions, protocol string) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := NewServer(opts).Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Wait() })

	client := mcp.NewClient(&mcp.Implementation{Name: "bb-test", Version: "test"}, clientOptions)
	session, err := client.Connect(ctx, clientTransport, &mcp.ClientSessionOptions{ProtocolVersion: protocol})
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	return session
}

// connectSelfAnswering connects a 2026-07-28 client that declares it can show
// a confirmation and hands the question back rather than answering it, so the
// test can answer it as it likes.
func connectSelfAnswering(t *testing.T, opts ServerOptions) *mcp.ClientSession {
	t.Helper()

	return connectClient(t, opts, &mcp.ClientOptions{
		ElicitationHandler: func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
			return nil, errors.New("the test answers confirmations itself")
		},
		MultiRoundTrip: &mcp.MultiRoundTripOptions{Disabled: true},
	}, "")
}

// protocolError returns err as a JSON-RPC error, failing the test when it is
// not one.
func protocolError(t *testing.T, err error) *jsonrpc.Error {
	t.Helper()
	var wire *jsonrpc.Error
	if !errors.As(err, &wire) {
		t.Fatalf("want a JSON-RPC error, got %v", err)
	}

	return wire
}

func TestAToolThatAsksRunsOnceTheConfirmationIsAccepted(t *testing.T) {
	t.Parallel()

	for _, era := range confirmationEras {
		t.Run(era.name, func(t *testing.T) {
			t.Parallel()

			session, answers := connectAsking(t, testServer(t), era.protocol, accept)
			result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "create_tag", Arguments: tagArguments})
			if err != nil {
				t.Fatalf("create_tag: %v", err)
			}
			if text := resultText(result); !result.IsError || !strings.Contains(text, createTagRan) {
				t.Errorf("an accepted create_tag should have run, and failed at the transport; got %q", text)
			}
			if asked := answers.questions(); len(asked) != 1 || !strings.Contains(asked[0].Message, `"v1.2.3"`) {
				t.Errorf("the person should have been asked once, about v1.2.3: %+v", asked)
			}
		})
	}
}

func TestAToolThatAsksDoesNotRunWithoutAnAcceptance(t *testing.T) {
	t.Parallel()

	answers := []struct {
		name   string
		answer *mcp.ElicitResult
		want   string
	}{
		{"declined", &mcp.ElicitResult{Action: "decline"}, "the person declined"},
		{"cancelled", &mcp.ElicitResult{Action: "cancel"}, "closed the confirmation"},
		{"accepted with the box unticked", &mcp.ElicitResult{Action: "accept", Content: map[string]any{confirmKey: false}}, "the person declined"},
	}

	for _, era := range confirmationEras {
		for _, answer := range answers {
			t.Run(era.name+"/"+answer.name, func(t *testing.T) {
				t.Parallel()

				session, _ := connectAsking(t, testServer(t), era.protocol, func(*mcp.ElicitParams) *mcp.ElicitResult { return answer.answer })
				result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "create_tag", Arguments: tagArguments})
				if err != nil {
					t.Fatalf("create_tag: %v", err)
				}
				text := resultText(result)
				if !result.IsError || !strings.Contains(text, answer.want) || strings.Contains(text, createTagRan) {
					t.Errorf("want a refusal saying %q, with nothing sent to Bitbucket; got %q", answer.want, text)
				}
				if !strings.Contains(text, "Do not call it again unless they ask") {
					t.Errorf("the refusal does not tell the model to leave it: %q", text)
				}
			})
		}
	}
}

// A client that cannot show a confirmation is refused with the 2026-07-28
// revision's MissingRequiredClientCapability, naming form elicitation as a
// ClientCapabilities object. Clients on handshake-era revisions get it too.
func TestAClientThatCannotAskGetsMissingRequiredClientCapability(t *testing.T) {
	t.Parallel()

	for _, era := range confirmationEras {
		t.Run(era.name, func(t *testing.T) {
			t.Parallel()

			session := connectClient(t, testServer(t), nil, era.protocol)
			result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "create_tag", Arguments: tagArguments})
			if err == nil {
				t.Fatalf("want error -32021, got the result %q", resultText(result))
			}

			wire := protocolError(t, err)
			if wire.Code != mcp.CodeMissingRequiredClientCapabilities {
				t.Errorf("code = %d, want %d", wire.Code, mcp.CodeMissingRequiredClientCapabilities)
			}
			var data struct {
				RequiredCapabilities map[string]json.RawMessage `json:"requiredCapabilities"`
			}
			if err := json.Unmarshal(wire.Data, &data); err != nil {
				t.Fatalf("data %s: %v", wire.Data, err)
			}
			if len(data.RequiredCapabilities) != 1 || string(data.RequiredCapabilities["elicitation"]) != `{"form":{}}` {
				t.Errorf(`requiredCapabilities = %s, want exactly {"elicitation":{"form":{}}}`, wire.Data)
			}
		})
	}
}

// Only the draft flag decides whether and when a pull request merges, so only
// a call that sets it asks.
func TestUpdatePullRequestAsksOnlyWhenItSetsTheDraftFlag(t *testing.T) {
	t.Parallel()

	arguments := map[string]any{"project": "PROJ", "repo": "payments", "pr_id": "7", "version": 3, "title": "A new title"}

	session, answers := connectAsking(t, testServer(t), "", accept)
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "update_pull_request", Arguments: arguments})
	if err != nil {
		t.Fatalf("update_pull_request: %v", err)
	}
	if text := resultText(result); !strings.Contains(text, "update_pull_request failed") {
		t.Errorf("a title change should have run without asking; got %q", text)
	}
	if asked := answers.questions(); len(asked) != 0 {
		t.Errorf("a title change asked: %q", asked[0].Message)
	}

	arguments["draft"] = false
	result, err = session.CallTool(context.Background(), &mcp.CallToolParams{Name: "update_pull_request", Arguments: arguments})
	if err != nil {
		t.Fatalf("update_pull_request with draft: %v", err)
	}
	if text := resultText(result); !strings.Contains(text, "update_pull_request failed") {
		t.Errorf("an accepted call setting draft should have run; got %q", text)
	}
	asked := answers.questions()
	if len(asked) != 1 {
		t.Fatalf("a call setting draft asked %d times, want once", len(asked))
	}
	if !strings.Contains(asked[0].Message, "ready for review") || !strings.Contains(asked[0].Message, "also changes its title") {
		t.Errorf("the question does not say what the call does: %q", asked[0].Message)
	}
}

// The confirmation is one required checkbox with no default, so accepting
// takes a tick, and a client that accepts forms with no fields by itself has
// something it cannot fill in. The message carries no link.
func TestTheConfirmationIsOneRequiredCheckbox(t *testing.T) {
	t.Parallel()

	session, answers := connectAsking(t, testServer(t), "", accept)
	if _, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "create_tag", Arguments: tagArguments}); err != nil {
		t.Fatalf("create_tag: %v", err)
	}
	asked := answers.questions()
	if len(asked) != 1 {
		t.Fatalf("asked %d times, want once", len(asked))
	}

	encoded, err := json.Marshal(asked[0].RequestedSchema)
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Type       string                    `json:"type"`
		Properties map[string]map[string]any `json:"properties"`
		Required   []string                  `json:"required"`
	}
	if err := json.Unmarshal(encoded, &schema); err != nil {
		t.Fatalf("schema %s: %v", encoded, err)
	}

	checkbox, ok := schema.Properties[confirmKey]
	switch {
	case schema.Type != "object" || len(schema.Properties) != 1 || !ok:
		t.Errorf("want an object with one property %q, got %s", confirmKey, encoded)
	case checkbox["type"] != "boolean":
		t.Errorf("the property is %v, want a boolean", checkbox["type"])
	case checkbox["default"] != nil:
		t.Errorf("the box starts at %v; it has to start unticked", checkbox["default"])
	case len(schema.Required) != 1 || schema.Required[0] != confirmKey:
		t.Errorf("required = %v, want [%s]", schema.Required, confirmKey)
	}
	if title, _ := checkbox["title"].(string); !strings.Contains(title, "v1.2.3") {
		t.Errorf("the box's label %q does not name the tag", title)
	}
	if strings.Contains(asked[0].Message, "http") {
		t.Errorf("the message carries a link: %q", asked[0].Message)
	}
}

// A 2026-07-28 client returns the answer with the request state the question
// carried. These are the retries an honest client never sends, each of which
// would otherwise let a client accept a call nobody was shown.
func TestAConfirmationAcceptsOnlyTheCallItWasAskedAbout(t *testing.T) {
	t.Parallel()

	session := connectSelfAnswering(t, testServer(t))
	ctx := context.Background()

	ask := func(t *testing.T) string {
		t.Helper()
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "create_tag", Arguments: tagArguments})
		if err != nil {
			t.Fatalf("create_tag: %v", err)
		}
		if result.InputRequests[confirmKey] == nil || result.RequestState == "" {
			t.Fatalf("want a confirmation to answer, got %q", resultText(result))
		}
		return result.RequestState
	}
	accepted := mcp.InputResponseMap{confirmKey: &mcp.ElicitResult{Action: "accept", Content: map[string]any{confirmKey: true}}}
	answer := func(arguments map[string]any, state string, responses mcp.InputResponseMap) (*mcp.CallToolResult, error) {
		return session.CallTool(ctx, &mcp.CallToolParams{
			Name: "create_tag", Arguments: arguments, InputResponses: responses, RequestState: state,
		})
	}
	refused := func(t *testing.T, err error, why string) {
		t.Helper()
		wire := protocolError(t, err)
		if wire.Code != jsonrpc.CodeInvalidParams || !strings.Contains(wire.Message, why) {
			t.Errorf("want invalid params saying %q, got %d %q", why, wire.Code, wire.Message)
		}
	}

	state := ask(t)

	t.Run("an answer without the state", func(t *testing.T) {
		_, err := answer(tagArguments, "", accepted)
		refused(t, err, "without the request state")
	})
	t.Run("a state that was altered", func(t *testing.T) {
		payload, mac, _ := strings.Cut(state, ".")
		middle := len(payload) / 2
		replacement := map[bool]string{true: "A", false: "B"}[payload[middle] != 'A']
		altered := payload[:middle] + replacement + payload[middle+1:] + "." + mac
		_, err := answer(tagArguments, altered, accepted)
		refused(t, err, "not issued by this server")
	})
	t.Run("the state spelled another way", func(t *testing.T) {
		// The MAC's 32 bytes end in a character with two unused bits, which
		// the encoder leaves clear. Setting one spells the same bytes again.
		const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
		last := strings.IndexByte(alphabet, state[len(state)-1])
		respelled := state[:len(state)-1] + string(alphabet[last|1])
		_, err := answer(tagArguments, respelled, accepted)
		refused(t, err, "malformed")
	})
	t.Run("an answer for other arguments", func(t *testing.T) {
		other := map[string]any{"project": "PROJ", "repo": "payments", "name": "v9.9.9", "start_point": "main"}
		_, err := answer(other, state, accepted)
		refused(t, err, "the call changed")
	})
	t.Run("the answer to the call asked about runs it, once", func(t *testing.T) {
		result, err := answer(tagArguments, state, accepted)
		if err != nil {
			t.Fatalf("the genuine answer was refused: %v", err)
		}
		if text := resultText(result); !strings.Contains(text, createTagRan) {
			t.Fatalf("the genuine answer did not run the call: %q", text)
		}
		_, err = answer(tagArguments, state, accepted)
		refused(t, err, "answered already")
	})
	t.Run("a retry without the answer asks again", func(t *testing.T) {
		result, err := answer(tagArguments, ask(t), nil)
		if err != nil {
			t.Fatalf("create_tag: %v", err)
		}
		if result.InputRequests[confirmKey] == nil {
			t.Errorf("want the question again, got %q", resultText(result))
		}
	})
}

// A person may answer after the confirmation expired. A refusal still stands,
// and the model is told to leave the call. Only an acceptance has to be on
// time, since what the person was shown may have changed since; the model can
// ask again. Nothing was forged, so each is a tool error.
func TestALateAnswerStandsUnlessItAccepts(t *testing.T) {
	t.Parallel()

	session := connectSelfAnswering(t, testServer(t))
	digest, err := inputDigest(CreateTagInput{Project: "PROJ", Repo: "payments", Name: "v1.2.3", StartPoint: "main"})
	if err != nil {
		t.Fatal(err)
	}
	// This process's key with a clock a minute more than the lifetime behind
	// issues states that expired a minute ago.
	late := &confirmationSeal{key: confirmations.key, answered: map[string]int64{}, now: func() time.Time {
		return time.Now().Add(-confirmationTTL - time.Minute)
	}}

	for _, tc := range []struct {
		name   string
		answer *mcp.ElicitResult
		want   string
	}{
		{"declined", &mcp.ElicitResult{Action: "decline"}, "the person declined. Do not call it again"},
		{"cancelled", &mcp.ElicitResult{Action: "cancel"}, "without answering. Do not call it again"},
		{"accepted", &mcp.ElicitResult{Action: "accept", Content: map[string]any{confirmKey: true}}, "accepted after its confirmation expired. Call it again to ask again"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state, err := late.seal("create_tag", digest, "")
			if err != nil {
				t.Fatal(err)
			}
			result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
				Name: "create_tag", Arguments: tagArguments,
				InputResponses: mcp.InputResponseMap{confirmKey: tc.answer}, RequestState: state,
			})
			if err != nil {
				t.Fatalf("want a tool error, got %v", err)
			}
			if text := resultText(result); !result.IsError || !strings.Contains(text, tc.want) || strings.Contains(text, createTagRan) {
				t.Errorf("want a refusal saying %q, with nothing sent to Bitbucket; got %q", tc.want, text)
			}
		})
	}
}

// An answered state is remembered for as long as it can be accepted, to the
// second, so it cannot be answered again in the last second of its life.
func TestAnAnsweredStateStaysAnsweredUntilItExpires(t *testing.T) {
	t.Parallel()

	seal := newConfirmationSeal()
	issued := time.Unix(1_800_000_000, 0)
	now := issued
	seal.now = func() time.Time { return now }

	state, err := seal.seal("create_tag", "digest", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := seal.consume(state); err != nil {
		t.Fatalf("consume: %v", err)
	}

	// Half a second into the last second the state is valid. Answering another
	// state is what forgets the nonces of expired ones.
	now = issued.Add(confirmationTTL + 500*time.Millisecond)
	other, err := seal.seal("create_tag", "digest", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := seal.consume(other); err != nil {
		t.Fatalf("consume: %v", err)
	}

	_, expired, err := seal.open(state, "create_tag", "digest")
	if wire := protocolError(t, err); !strings.Contains(wire.Message, "answered already") {
		t.Errorf("the answered state opened again (expired %v): %q", expired, wire.Message)
	}
}

// The key is drawn per process, so a request state from another process, or
// from an earlier run of this one, does not verify.
func TestAStateFromAnotherProcessDoesNotVerify(t *testing.T) {
	t.Parallel()

	state, err := newConfirmationSeal().seal("create_tag", "digest", "")
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = newConfirmationSeal().open(state, "create_tag", "digest")
	if wire := protocolError(t, err); !strings.Contains(wire.Message, "not issued by this server") {
		t.Errorf("got %q", wire.Message)
	}
}

// The audit record says how the person answered, and a refused confirmation
// is also status denied. A 2026-07-28 client makes two calls for one decision;
// the trail holds one record for it.
func TestTheAuditRecordSaysHowTheConfirmationWent(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name             string
		protocol         string
		answer           func(*mcp.ElicitParams) *mcp.ElicitResult
		wantStatus       string
		wantConfirmation string
	}{
		// Accepted, then failed at the closed port.
		{"accepted", "", accept, auditStatusError, confirmationAccepted},
		{"accepted on a handshake-era client", "2025-11-25", accept, auditStatusError, confirmationAccepted},
		{"declined", "", decline, auditStatusDenied, confirmationDeclined},
		{"cancelled", "", func(*mcp.ElicitParams) *mcp.ElicitResult { return &mcp.ElicitResult{Action: "cancel"} }, auditStatusDenied, confirmationCancelled},
		{"unavailable", "", nil, auditStatusDenied, confirmationUnavailable},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "audit.jsonl")
			audit, err := NewAuditLogger(path)
			if err != nil {
				t.Fatalf("NewAuditLogger: %v", err)
			}
			opts := testServer(t)
			opts.Audit = audit

			var session *mcp.ClientSession
			if tc.answer == nil {
				session = connectClient(t, opts, nil, tc.protocol)
			} else {
				session, _ = connectAsking(t, opts, tc.protocol, tc.answer)
			}
			_, _ = session.CallTool(context.Background(), &mcp.CallToolParams{Name: "create_tag", Arguments: tagArguments})
			_ = audit.Close()

			records := readAuditRecords(t, path)
			if len(records) != 1 {
				t.Fatalf("want one record for one decision, got %d: %+v", len(records), records)
			}
			if records[0].Status != tc.wantStatus || records[0].Confirmation != tc.wantConfirmation {
				t.Errorf("record status %q confirmation %q, want %q and %q",
					records[0].Status, records[0].Confirmation, tc.wantStatus, tc.wantConfirmation)
			}
		})
	}
}

// An answer that cannot be used is refused before anything reaches Bitbucket,
// so the call is audited as denied. Nobody answered it, so the record names no
// outcome.
func TestARefusedAnswerIsAuditedAsDenied(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "audit.jsonl")
	audit, err := NewAuditLogger(path)
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}
	opts := testServer(t)
	opts.Audit = audit

	session := connectSelfAnswering(t, opts)
	_, err = session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "create_tag", Arguments: tagArguments,
		InputResponses: mcp.InputResponseMap{confirmKey: &mcp.ElicitResult{Action: "accept", Content: map[string]any{confirmKey: true}}},
	})
	protocolError(t, err)
	_ = audit.Close()

	records := readAuditRecords(t, path)
	if len(records) != 1 {
		t.Fatalf("want one record, got %d: %+v", len(records), records)
	}
	if records[0].Status != auditStatusDenied || records[0].Confirmation != "" || !strings.Contains(records[0].ErrorMessage, "without the request state") {
		t.Errorf("record status %q confirmation %q error %q, want denied, no outcome, and the reason",
			records[0].Status, records[0].Confirmation, records[0].ErrorMessage)
	}
}
