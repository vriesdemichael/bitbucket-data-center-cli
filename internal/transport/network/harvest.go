package network

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"sync"
)

// HarvestEnvVar names the file every non-2xx response is recorded to.
//
// Unset, which is every real run, nothing below is reached: the transport is
// returned unwrapped and no file is opened.
const HarvestEnvVar = "BB_ERROR_HARVEST"

// harvestRecord is one response Bitbucket refused, as observed rather than as
// documented.
//
// The published spec describes shapes and says nothing about which exception
// arrives with which status, which is the question #535 and #537 both turn on.
// Nothing here is written by hand: a record exists because a live test provoked
// the server into sending it.
type harvestRecord struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Status int    `json:"status"`
	// Exception is Bitbucket's own name for what went wrong. It is the field
	// the status cannot tell you -- a 400 is "your request failed" and
	// InvalidPullRequestRoleException is "you may not review your own".
	Exception string `json:"exception,omitempty"`
	Message   string `json:"message,omitempty"`
	// BodyBytes and BodyIsJSON are for #537: a 204, a 200 with nothing in it
	// and a 200 whose body the client could not decode are three different
	// situations that four services answer four different ways.
	BodyBytes  int  `json:"bodyBytes"`
	BodyIsJSON bool `json:"bodyIsJSON"`
}

// harvestTransport records what the server answered and changes nothing about
// it. It reads the body and puts it back, so the caller sees the response it
// would have seen.
type harvestTransport struct {
	base http.RoundTripper
	path string

	mu sync.Mutex
}

func (transport *harvestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := transport.base.RoundTrip(request)
	if err != nil || response == nil {
		return response, err
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 && response.StatusCode != http.StatusNoContent {
		// A 204 is recorded: whether an endpoint answers one is exactly what
		// the empty-body question needs to know.
		if response.ContentLength != 0 {
			return response, nil
		}
	}

	body, readErr := io.ReadAll(response.Body)
	_ = response.Body.Close()
	response.Body = io.NopCloser(bytes.NewReader(body))
	if readErr != nil {
		return response, nil
	}

	record := harvestRecord{
		Method:    request.Method,
		Path:      request.URL.Path,
		Status:    response.StatusCode,
		BodyBytes: len(body),
	}

	var decoded struct {
		Errors []struct {
			Message       string `json:"message"`
			ExceptionName string `json:"exceptionName"`
		} `json:"errors"`
	}
	if json.Unmarshal(body, &decoded) == nil {
		record.BodyIsJSON = true
		if len(decoded.Errors) > 0 {
			record.Exception = decoded.Errors[0].ExceptionName
			record.Message = decoded.Errors[0].Message
		}
	}

	transport.append(record)

	return response, nil
}

// append writes one line. Appending rather than accumulating means a run that
// crashes still leaves what it saw, and several processes can share the file --
// the live suite drives the CLI in-process, but not always in one process.
func (transport *harvestTransport) append(record harvestRecord) {
	line, err := json.Marshal(record)
	if err != nil {
		return
	}

	transport.mu.Lock()
	defer transport.mu.Unlock()

	file, err := os.OpenFile(transport.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer func() { _ = file.Close() }()

	_, _ = file.Write(append(line, '\n'))
}

// withHarvest wraps a transport when BB_ERROR_HARVEST names a file, and returns
// it untouched otherwise.
//
// The recorder is deliberately not a flag: it is for the live suite, which
// drives the CLI in-process and cannot pass one, and turning it on in a real
// run would write every refusal a person hit to a file they did not ask for.
func withHarvest(transport http.RoundTripper) http.RoundTripper {
	path := os.Getenv(HarvestEnvVar)
	if path == "" {
		return transport
	}

	return &harvestTransport{base: transport, path: path}
}
