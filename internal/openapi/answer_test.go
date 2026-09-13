package openapi

import (
	"context"
	"net/http"
	"testing"
)

// serialiserRaceBody is what Bitbucket answered a webhook create with in CI,
// verbatim.
const serialiserRaceBody = `{"errors":[{"message":"(was java.util.ConcurrentModificationException) (through reference chain: com.atlassian.webhooks.internal.rest.RestWebhook[\"configuration\"])","exceptionName":"com.fasterxml.jackson.databind.JsonMappingException"}]}`

func TestFailedWritingAnswerIsTheSerialiserRaceAndNothingElse(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{"the race", http.StatusBadRequest, serialiserRaceBody, true},
		{"the same exception for a body that could not be read", http.StatusBadRequest,
			`{"errors":[{"message":"Unrecognized field \"nme\"","exceptionName":"com.fasterxml.jackson.databind.JsonMappingException"}]}`, false},
		{"the race's words under another status", http.StatusInternalServerError, serialiserRaceBody, false},
		{"another exception", http.StatusBadRequest,
			`{"errors":[{"message":"Branch 'x' already exists","exceptionName":"com.atlassian.bitbucket.repository.DuplicateRefException"}]}`, false},
		{"a body that is not an error envelope", http.StatusBadRequest, "<html>bad request</html>", false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := FailedWritingAnswer(testCase.status, []byte(testCase.body)); got != testCase.want {
				t.Fatalf("FailedWritingAnswer = %v, want %v", got, testCase.want)
			}
		})
	}
}

func TestPageQueryAsksForOnePageAndKeepsTheRest(t *testing.T) {
	t.Parallel()

	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet,
		"http://bitbucket.example/rest/api/latest/projects/P/webhooks?event=repo%3Arefs_changed", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}

	if err := PageQuery(50, 25)(context.Background(), request); err != nil {
		t.Fatalf("PageQuery: %v", err)
	}

	query := request.URL.Query()
	if query.Get("start") != "50" || query.Get("limit") != "25" || query.Get("event") != "repo:refs_changed" {
		t.Fatalf("query = %q, want start=50, limit=25 and the event filter kept", request.URL.RawQuery)
	}
}
