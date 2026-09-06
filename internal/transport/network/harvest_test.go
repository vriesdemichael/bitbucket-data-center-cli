package network

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubTransport answers with a response written here rather than by a server.
//
// The subject is the recorder, not Bitbucket: what it writes down, and what it
// leaves behind for the caller. A server would add nothing and would be a
// server standing in for Bitbucket, which is the thing ADR-079 is about.
type stubTransport struct {
	status      int
	body        string
	contentType string
}

func (stub stubTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	length := int64(len(stub.body))
	return &http.Response{
		StatusCode:    stub.status,
		Body:          io.NopCloser(strings.NewReader(stub.body)),
		ContentLength: length,
		Request:       request,
		Header:        http.Header{"Content-Type": []string{stub.contentType}},
	}, nil
}

// harvestTo runs one request through the recorder and returns the file it
// wrote plus the body the caller was left with.
func harvestTo(t *testing.T, stub stubTransport) (string, string) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "harvest.jsonl")
	transport := &harvestTransport{base: stub, path: path}

	request, err := http.NewRequestWithContext(t.Context(), http.MethodDelete,
		"https://bitbucket.example/rest/api/latest/projects/PRJ/repos/demo", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}

	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatalf("round trip: %v", err)
	}
	served, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil {
		t.Fatalf("read the response the caller gets: %v", err)
	}

	return path, string(served)
}

func readRecords(t *testing.T, path string) []harvestRecord {
	t.Helper()

	raw, err := os.ReadFile(path) //nolint:gosec // a path this test just created
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	records := make([]harvestRecord, 0, 1)
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var record harvestRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("record is not JSON: %v (%s)", err, line)
		}
		records = append(records, record)
	}

	return records
}

// TestHarvestRecordsARefusalAndLeavesItReadable is the property that matters
// most: the recorder reads the body to write it down, and the caller still has
// to be able to read the same body afterwards.
//
// Getting that wrong would not fail quietly. Every command would see an empty
// response for as long as the recorder was on, and the harvest would have
// broken the runs it exists to observe.
func TestHarvestRecordsARefusalAndLeavesItReadable(t *testing.T) {
	const body = `{"errors":[{"message":"Repository does not exist.","exceptionName":"com.atlassian.bitbucket.repository.NoSuchRepositoryException"}]}`

	path, served := harvestTo(t, stubTransport{status: http.StatusNotFound, body: body, contentType: "application/json"})

	if served != body {
		t.Errorf("the caller received %q, want the body untouched", served)
	}

	records := readRecords(t, path)
	if len(records) != 1 {
		t.Fatalf("recorded %d responses, want 1", len(records))
	}

	record := records[0]
	if record.Status != http.StatusNotFound {
		t.Errorf("status = %d, want 404", record.Status)
	}
	if !strings.HasSuffix(record.Exception, "NoSuchRepositoryException") {
		t.Errorf("exception = %q, want the name Bitbucket sent", record.Exception)
	}
	if record.Message != "Repository does not exist." {
		t.Errorf("message = %q", record.Message)
	}
	if !record.BodyIsJSON || record.BodyBytes != len(body) {
		t.Errorf("bodyIsJSON = %v, bodyBytes = %d, want true and %d", record.BodyIsJSON, record.BodyBytes, len(body))
	}
	if record.Method != http.MethodDelete || !strings.HasSuffix(record.Path, "/repos/demo") {
		t.Errorf("recorded %s %s, want the request that was made", record.Method, record.Path)
	}
}

// TestHarvestRecordsA204AndIgnoresASuccessWithABody covers the filter.
//
// A 204 is the whole point of recording successes at all -- whether an endpoint
// answers one is what tells a deliberate no-op from a payload that failed to
// decode. A 200 carrying a body is the ordinary case and would bury that.
func TestHarvestRecordsA204AndIgnoresASuccessWithABody(t *testing.T) {
	t.Run("204 is recorded", func(t *testing.T) {
		path, _ := harvestTo(t, stubTransport{status: http.StatusNoContent})

		records := readRecords(t, path)
		if len(records) != 1 {
			t.Fatalf("recorded %d responses, want the 204", len(records))
		}
		if records[0].BodyBytes != 0 || records[0].Exception != "" {
			t.Errorf("a 204 recorded as %+v", records[0])
		}
	})

	t.Run("200 with a body is not", func(t *testing.T) {
		path, _ := harvestTo(t, stubTransport{status: http.StatusOK, body: `{"id":1}`})

		if records := readRecords(t, path); len(records) != 0 {
			t.Errorf("recorded %d ordinary successes, want none: %+v", len(records), records)
		}
	})
}

// TestHarvestRecordsABodyThatIsNotJSON covers the case the registry needs in
// order to tell "the server said nothing" from "the server said something the
// client could not read".
func TestHarvestRecordsABodyThatIsNotJSON(t *testing.T) {
	path, _ := harvestTo(t, stubTransport{status: http.StatusBadGateway, body: "<html>gateway</html>"})

	records := readRecords(t, path)
	if len(records) != 1 {
		t.Fatalf("recorded %d responses, want 1", len(records))
	}
	if records[0].BodyIsJSON {
		t.Error("an HTML error page was recorded as JSON")
	}
	if records[0].Exception != "" {
		t.Errorf("exception = %q, want none from a body with no errors array", records[0].Exception)
	}
}

// TestWithHarvestIsInertUnlessAsked is why this can live in the transport every
// run builds: with the variable unset the recorder is not wrapped at all, so a
// real run cannot open a file, read a body, or slow a request down.
func TestWithHarvestIsInertUnlessAsked(t *testing.T) {
	base := stubTransport{status: http.StatusOK}

	t.Setenv(HarvestEnvVar, "")
	if wrapped := withHarvest(base); wrapped != http.RoundTripper(base) {
		t.Errorf("with %s unset the transport was wrapped: %T", HarvestEnvVar, wrapped)
	}

	t.Setenv(HarvestEnvVar, filepath.Join(t.TempDir(), "harvest.jsonl"))
	if _, ok := withHarvest(base).(*harvestTransport); !ok {
		t.Errorf("with %s set the transport was not wrapped", HarvestEnvVar)
	}
}
