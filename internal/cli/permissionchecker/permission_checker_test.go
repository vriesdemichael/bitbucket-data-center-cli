package permissionchecker

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

// mock-inventory: transport-fault — a server failing every request is injected; the subject is how the checker classifies it.
func TestPermissionCheckerInspect500Error(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"errors":[{"message":"Internal Server Error"}]}`))
	}))
	defer server.Close()

	client, err := openapigenerated.NewClientWithResponses(server.URL + "/rest")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	checker := New(client)
	ctx := context.Background()

	// InspectRepoPermissions returns error on 500
	if _, err := checker.InspectRepoPermissions(ctx, "PRJ", "repo1"); err == nil {
		t.Fatal("expected error from InspectRepoPermissions on 500")
	}

	// InspectProjectPermissions returns error on 500
	if _, err := checker.InspectProjectPermissions(ctx, "PRJ"); err == nil {
		t.Fatal("expected error from InspectProjectPermissions on 500")
	}
}

// TestAnUnreachableServerIsTransientNotInternal pins the classification the
// dry-run path depends on.
//
// The pre-flight is the only request a --dry-run makes, so an unclassified
// transport error here made `--dry-run` report kind=internal where the real run
// reported transient, for the same command against the same dead host. A
// consumer branching on kind was told to report a bug in bb when the network
// was down (#478).
func TestAnUnreachableServerIsTransientNotInternal(t *testing.T) {
	t.Parallel()

	// Reserve a port and close it, so connecting is refused rather than hanging.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	address := listener.Addr().String()
	_ = listener.Close()

	client, err := openapi.NewClientWithResponsesFromConfig(config.AppConfig{BitbucketURL: "http://" + address})
	if err != nil {
		t.Fatalf("client: %v", err)
	}

	checker := New(client)
	ctx := context.Background()

	for _, testCase := range []struct {
		name string
		call func() error
	}{
		{"repo permission", func() error {
			return checker.CheckRepoPermission(ctx, "PRJ", "repo", openapigenerated.GetRepositories1ParamsPermissionREPOWRITE)
		}},
		{"project write", func() error { return checker.CheckProjectWrite(ctx, "PRJ") }},
		{"project admin", func() error { return checker.CheckProjectAdmin(ctx, "PRJ") }},
		{"project create", func() error { return checker.CheckProjectCreate(ctx) }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			err := testCase.call()
			if err == nil {
				t.Fatal("expected the unreachable host to fail")
			}
			if kind := apperrors.KindOf(err); kind != apperrors.KindTransient {
				t.Errorf("kind = %v, want transient so --dry-run agrees with the real run (error: %v)", kind, err)
			}
		})
	}
}

// TestACancelledContextIsNotReportedAsTransient keeps the caller stopping
// distinct from the network failing.
func TestACancelledContextIsNotReportedAsTransient(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	address := listener.Addr().String()
	_ = listener.Close()

	client, err := openapi.NewClientWithResponsesFromConfig(config.AppConfig{BitbucketURL: "http://" + address})
	if err != nil {
		t.Fatalf("client: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = New(client).CheckProjectCreate(ctx)
	if err == nil {
		t.Fatal("expected a cancelled context to fail")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("cancellation was reclassified and no longer unwraps to context.Canceled: %v", err)
	}
}

// TestTransportFailureLeavesClassifiedErrorsAlone covers the branches that
// decide what is a network failure and what is not.
func TestTransportFailureLeavesClassifiedErrorsAlone(t *testing.T) {
	t.Parallel()

	if err := transportFailure(nil); err != nil {
		t.Errorf("nil became %v", err)
	}

	// Already classified: reclassifying would turn a refusal into a retry.
	denied := apperrors.New(apperrors.KindAuthorization, "insufficient permission", nil)
	if got := transportFailure(denied); !errors.Is(got, denied) {
		t.Errorf("an authorization error was rewritten to %v", got)
	}
	notFound := apperrors.New(apperrors.KindNotFound, "no such project", nil)
	if kind := apperrors.KindOf(transportFailure(notFound)); kind != apperrors.KindNotFound {
		t.Errorf("kind = %v, want not_found preserved", kind)
	}

	// The caller stopping, in both spellings.
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		wrapped := fmt.Errorf("get projects: %w", cause)
		got := transportFailure(wrapped)
		if !errors.Is(got, cause) {
			t.Errorf("%v no longer unwraps to itself: %v", cause, got)
		}
		if apperrors.KindOf(got) == apperrors.KindTransient {
			t.Errorf("%v was reported as a network failure", cause)
		}
	}

	// An unclassified transport error is the case this exists for.
	if kind := apperrors.KindOf(transportFailure(errors.New("dial tcp: connection refused"))); kind != apperrors.KindTransient {
		t.Errorf("kind = %v, want transient", kind)
	}
}

// TestEveryCheckerMethodClassifiesAnUnreachableServer covers the transport
// branch of the methods the shorter test above does not reach.
func TestEveryCheckerMethodClassifiesAnUnreachableServer(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	address := listener.Addr().String()
	_ = listener.Close()

	client, err := openapi.NewClientWithResponsesFromConfig(config.AppConfig{BitbucketURL: "http://" + address})
	if err != nil {
		t.Fatalf("client: %v", err)
	}

	checker := New(client)
	ctx := context.Background()

	for name, call := range map[string]func() error{
		"inspect repo permissions": func() error {
			_, err := checker.InspectRepoPermissions(ctx, "PRJ", "repo")

			return err
		},
		"inspect project permissions": func() error {
			_, err := checker.InspectProjectPermissions(ctx, "PRJ")

			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			err := call()
			if err == nil {
				t.Fatal("expected the unreachable host to fail")
			}
			if kind := apperrors.KindOf(err); kind != apperrors.KindTransient {
				t.Errorf("kind = %v, want transient (error: %v)", kind, err)
			}
		})
	}
}

// TestASecondRequestFailingIsAlsoTransient covers the transport branches that
// only run after an earlier request succeeded -- CheckProjectWrite's permission
// lookup, and CheckProjectRead's.
func TestASecondRequestFailingIsAlsoTransient(t *testing.T) {
	t.Parallel()

	// Answers the project fetch and then closes the connection on anything
	// else, so the follow-up fails at the transport rather than with a status.
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if strings.HasSuffix(request.URL.Path, "/projects/PRJ") {
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"key":"PRJ","id":1,"name":"Project PRJ"}`))

			return
		}

		hijacker, ok := writer.(http.Hijacker)
		if !ok {
			t.Errorf("server does not support hijacking")

			return
		}
		conn, _, err := hijacker.Hijack()
		if err != nil {
			t.Errorf("hijack: %v", err)

			return
		}
		_ = conn.Close()
	}))
	defer server.Close()

	client, err := openapi.NewClientWithResponsesFromConfig(config.AppConfig{BitbucketURL: server.URL})
	if err != nil {
		t.Fatalf("client: %v", err)
	}

	ctx := context.Background()

	for name, call := range map[string]func() error{
		"project write": func() error { return New(client).CheckProjectWrite(ctx, "PRJ") },
		"project read":  func() error { return New(client).CheckProjectRead(ctx, "PRJ") },
	} {
		t.Run(name, func(t *testing.T) {
			err := call()
			if err == nil {
				t.Fatal("expected the second request to fail")
			}
			if kind := apperrors.KindOf(err); kind != apperrors.KindTransient {
				t.Errorf("kind = %v, want transient (error: %v)", kind, err)
			}
		})
	}
}
