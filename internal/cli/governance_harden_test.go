package cli

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReviewerConditionCreateMutualExclusionCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", "http://localhost")
	command := NewRootCommand()
	command.SetArgs([]string{"reviewer", "condition", "create", "{}", "--config-file", "some.json", "--project", "PRJ"})
	err := command.Execute()
	if err == nil {
		t.Fatal("expected error for mutual exclusion")
	}
	if !strings.Contains(err.Error(), "cannot provide condition config as both an argument and via --config-file") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReviewerConditionUpdateMutualExclusionCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", "http://localhost")
	command := NewRootCommand()
	command.SetArgs([]string{"reviewer", "condition", "update", "1", "{}", "--config-file", "some.json", "--project", "PRJ"})
	err := command.Execute()
	if err == nil {
		t.Fatal("expected error for mutual exclusion")
	}
	if !strings.Contains(err.Error(), "cannot provide condition config as both an argument and via --config-file") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReviewerConditionCreateFileAndStdinCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.Method == http.MethodPost && strings.Contains(request.URL.Path, "condition") {
			writer.WriteHeader(http.StatusCreated)
			_, _ = writer.Write([]byte(`{"id":10}`))
			return
		}
		http.NotFound(writer, request)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	// Test --config-file
	tmpFile := filepath.Join(t.TempDir(), "cond.json")
	_ = os.WriteFile(tmpFile, []byte(`{"requiredApprovals":1}`), 0644)

	command := NewRootCommand()
	command.SetArgs([]string{"--json", "reviewer", "condition", "create", "--config-file", tmpFile, "--project", "PRJ"})
	if err := command.Execute(); err != nil {
		t.Fatalf("reviewer condition create file failed: %v", err)
	}

	// Test stdin
	command = NewRootCommand()
	stdinBuffer := bytes.NewBufferString(`{"requiredApprovals":1}`)
	command.SetIn(stdinBuffer)
	command.SetArgs([]string{"--json", "reviewer", "condition", "create", "-", "--project", "PRJ"})
	if err := command.Execute(); err != nil {
		t.Fatalf("reviewer condition create stdin failed: %v", err)
	}
}

func TestReviewerConditionUpdateFileAndStdinCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.Method == http.MethodPut && strings.Contains(request.URL.Path, "condition/1") {
			_, _ = writer.Write([]byte(`{"id":1}`))
			return
		}
		http.NotFound(writer, request)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	// Test --config-file
	tmpFile := filepath.Join(t.TempDir(), "cond-up.json")
	_ = os.WriteFile(tmpFile, []byte(`{"requiredApprovals":2}`), 0644)

	command := NewRootCommand()
	command.SetArgs([]string{"--json", "reviewer", "condition", "update", "1", "--config-file", tmpFile, "--project", "PRJ"})
	if err := command.Execute(); err != nil {
		t.Fatalf("reviewer condition update file failed: %v", err)
	}

	// Test stdin
	command = NewRootCommand()
	stdinBuffer := bytes.NewBufferString(`{"requiredApprovals":2}`)
	command.SetIn(stdinBuffer)
	command.SetArgs([]string{"--json", "reviewer", "condition", "update", "1", "-", "--project", "PRJ"})
	if err := command.Execute(); err != nil {
		t.Fatalf("reviewer condition update stdin failed: %v", err)
	}
}
