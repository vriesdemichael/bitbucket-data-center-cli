package openapi

import (
	"context"
	"io"
	"net"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

// A gateway giving up on a mutation, through the generated client. The listener
// answers every request with a 504 and no Bitbucket payload (ADR-079): it plays
// the proxy in front of Bitbucket, not Bitbucket.
func TestAGatewayTimeoutOnTheGeneratedClientHasAnUnknownOutcome(t *testing.T) {
	t.Parallel()

	baseURL, requests := hangUpAfterReading(t, func(conn net.Conn) {
		_, _ = io.WriteString(conn, "HTTP/1.1 504 Gateway Timeout\r\nContent-Type: text/html\r\nContent-Length: 21\r\n\r\n<html>timeout</html>\n")
	})

	_, err := generatedClient(t, baseURL).CreateProjectWithResponse(context.Background(), openapigenerated.RestProject{})
	err = apperrors.Transport("failed to create project", err)

	if !apperrors.IsKind(err, apperrors.KindUnknownOutcome) {
		t.Fatalf("got %v, want unknown_outcome", err)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("the POST was sent %d times", got)
	}
}
