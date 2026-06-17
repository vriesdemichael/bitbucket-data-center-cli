package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProjectCLICommandsMock(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects":
			limit := r.URL.Query().Get("limit")
			start := r.URL.Query().Get("start")
			if limit == "1" && start == "2" {
				_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"key":"PRJ-paginated","name":"Project Paginated"}]}`))
			} else {
				_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"key":"PRJ","name":"Project"}]}`))
			}
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/PRJ":
			_, _ = w.Write([]byte(`{"key":"PRJ","name":"Project"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/rest/api/latest/projects":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"key":"PRJ2","name":"Project 2"}`))
		case r.Method == http.MethodPut && r.URL.Path == "/rest/api/latest/projects/PRJ":
			_, _ = w.Write([]byte(`{"key":"PRJ","name":"Project Updated"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/api/latest/projects/PRJ":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_TOKEN", "test-token")

	out, err := executeTestCLI(t, "project", "list")
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if !strings.Contains(out, "PRJ") || !strings.Contains(out, "Project") {
		t.Fatalf("unexpected list output: %s", out)
	}

	out, err = executeTestCLI(t, "project", "list", "--limit", "1", "--start", "2")
	if err != nil {
		t.Fatalf("list with pagination flags failed: %v", err)
	}
	if !strings.Contains(out, "PRJ-paginated") {
		t.Fatalf("unexpected list with pagination flags output: %s", out)
	}

	out, err = executeTestCLI(t, "--json", "project", "list")
	if err != nil {
		t.Fatalf("list json failed: %v", err)
	}
	if !strings.Contains(out, `"projects"`) {
		t.Fatalf("unexpected list json output: %s", out)
	}

	out, err = executeTestCLI(t, "project", "get", "PRJ")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if !strings.Contains(out, "Key: PRJ") {
		t.Fatalf("unexpected get output: %s", out)
	}

	out, err = executeTestCLI(t, "--json", "project", "get", "PRJ")
	if err != nil {
		t.Fatalf("get json failed: %v", err)
	}
	if !strings.Contains(out, `"project"`) {
		t.Fatalf("unexpected get json output: %s", out)
	}

	out, err = executeTestCLI(t, "project", "create", "PRJ2", "--name", "Project 2")
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if !strings.Contains(out, "Created project PRJ2") {
		t.Fatalf("unexpected create output: %s", out)
	}

	out, err = executeTestCLI(t, "--json", "project", "create", "PRJ2", "--name", "Project 2")
	if err != nil {
		t.Fatalf("create json failed: %v", err)
	}
	if !strings.Contains(out, `"project"`) {
		t.Fatalf("unexpected create json output: %s", out)
	}

	out, err = executeTestCLI(t, "project", "update", "PRJ", "--name", "Project Updated")
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}
	if !strings.Contains(out, "Updated project PRJ") {
		t.Fatalf("unexpected update output: %s", out)
	}

	out, err = executeTestCLI(t, "--json", "project", "update", "PRJ", "--name", "Project Updated")
	if err != nil {
		t.Fatalf("update json failed: %v", err)
	}
	if !strings.Contains(out, `"project"`) {
		t.Fatalf("unexpected update json output: %s", out)
	}

	out, err = executeTestCLI(t, "project", "delete", "PRJ")
	if err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	if !strings.Contains(out, "Deleted project PRJ") {
		t.Fatalf("unexpected delete output: %s", out)
	}

	out, err = executeTestCLI(t, "--json", "project", "delete", "PRJ")
	if err != nil {
		t.Fatalf("delete json failed: %v", err)
	}
	if !strings.Contains(out, `"status"`) {
		t.Fatalf("unexpected delete json output: %s", out)
	}
}

func TestProjectCLIValidation(t *testing.T) {
	_, err := executeTestCLI(t, "project", "get")
	if err == nil {
		t.Fatal("expected err missing arg")
	}
	_, err = executeTestCLI(t, "project", "create")
	if err == nil {
		t.Fatal("expected err missing arg")
	}
	_, err = executeTestCLI(t, "project", "update")
	if err == nil {
		t.Fatal("expected err missing arg")
	}
	_, err = executeTestCLI(t, "project", "delete")
	if err == nil {
		t.Fatal("expected err missing arg")
	}
}
