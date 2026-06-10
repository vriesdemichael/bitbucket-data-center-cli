package hook

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
)

func TestHookService(t *testing.T) {
	var projectHookCount atomic.Int32
	var repoHookCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/PRJ/settings/hooks":
			if projectHookCount.Add(1) == 1 {
				_, _ = w.Write([]byte(`{"values":[{"enabled":true,"details":{"key":"hook1","name":"Hook 1"}}],"isLastPage":false,"nextPageStart":1}`))
			} else {
				_, _ = w.Write([]byte(`{"values":[],"isLastPage":true}`))
			}
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/demo/settings/hooks":
			if repoHookCount.Add(1) == 1 {
				_, _ = w.Write([]byte(`{"values":[{"enabled":false,"details":{"key":"hook2","name":"Hook 2"}}],"isLastPage":false,"nextPageStart":1}`))
			} else {
				_, _ = w.Write([]byte(`{"values":[],"isLastPage":true}`))
			}
		case r.Method == http.MethodPut && r.URL.Path == "/rest/api/latest/projects/PRJ/settings/hooks/hook1/enabled":
			_, _ = w.Write([]byte(`{"enabled":true}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/api/latest/projects/PRJ/settings/hooks/hook1/enabled":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPut && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/demo/settings/hooks/hook2/enabled":
			_, _ = w.Write([]byte(`{"enabled":true}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/demo/settings/hooks/hook2/enabled":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/PRJ/settings/hooks/hook1/settings":
			_, _ = w.Write([]byte(`{"foo":"bar"}`))
		case r.Method == http.MethodPut && r.URL.Path == "/rest/api/latest/projects/PRJ/settings/hooks/hook1/settings":
			_, _ = w.Write([]byte(`{"foo":"bar"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/demo/settings/hooks/hook2/settings":
			_, _ = w.Write([]byte(`{"baz":123}`))
		case r.Method == http.MethodPut && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/demo/settings/hooks/hook2/settings":
			_, _ = w.Write([]byte(`{"baz":123}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, _ := openapigenerated.NewClientWithResponses(server.URL + "/rest")
	service := NewService(client)

	projHooks, err := service.ListProjectHooks(context.Background(), "PRJ", 1)
	if err != nil || len(projHooks) != 1 {
		t.Fatalf("list project hooks failed: %v", err)
	}

	repoHooks, err := service.ListRepositoryHooks(context.Background(), "PRJ", "demo", 1)
	if err != nil || len(repoHooks) != 1 {
		t.Fatalf("list repository hooks failed: %v", err)
	}

	if _, err := service.EnableProjectHook(context.Background(), "PRJ", "hook1"); err != nil {
		t.Fatalf("enable project hook failed: %v", err)
	}

	if err := service.DisableProjectHook(context.Background(), "PRJ", "hook1"); err != nil {
		t.Fatalf("disable project hook failed: %v", err)
	}

	if _, err := service.EnableRepositoryHook(context.Background(), "PRJ", "demo", "hook2"); err != nil {
		t.Fatalf("enable repository hook failed: %v", err)
	}

	if err := service.DisableRepositoryHook(context.Background(), "PRJ", "demo", "hook2"); err != nil {
		t.Fatalf("disable repository hook failed: %v", err)
	}

	if _, err := service.GetProjectHookSettings(context.Background(), "PRJ", "hook1"); err != nil {
		t.Fatalf("get project hook settings failed: %v", err)
	}

	if _, err := service.SetProjectHookSettings(context.Background(), "PRJ", "hook1", map[string]any{"foo": "bar"}); err != nil {
		t.Fatalf("set project hook settings failed: %v", err)
	}

	if _, err := service.GetRepositoryHookSettings(context.Background(), "PRJ", "demo", "hook2"); err != nil {
		t.Fatalf("get repository hook settings failed: %v", err)
	}

	if _, err := service.SetRepositoryHookSettings(context.Background(), "PRJ", "demo", "hook2", map[string]any{"baz": 123}); err != nil {
		t.Fatalf("set repository hook settings failed: %v", err)
	}
}

func TestHookServiceNetworkErrors(t *testing.T) {
	client, _ := openapigenerated.NewClientWithResponses("http://invalid-url-that-does-not-exist.local")
	service := NewService(client)
	ctx := context.Background()

	_, _ = service.ListProjectHooks(ctx, "P", 100)
	_, _ = service.ListRepositoryHooks(ctx, "P", "S", 100)
	_, _ = service.EnableProjectHook(ctx, "P", "h1")
	_ = service.DisableProjectHook(ctx, "P", "h1")
	_, _ = service.EnableRepositoryHook(ctx, "P", "S", "h1")
	_ = service.DisableRepositoryHook(ctx, "P", "S", "h1")
	_, _ = service.GetProjectHookSettings(ctx, "P", "h1")
	_, _ = service.GetRepositoryHookSettings(ctx, "P", "S", "h1")
	_, _ = service.SetProjectHookSettings(ctx, "P", "h1", map[string]any{"a": 1})
	_, _ = service.SetRepositoryHookSettings(ctx, "P", "S", "h1", map[string]any{"a": 1})
}
func TestHookServiceValidation(t *testing.T) {
	service := NewService(nil)
	ctx := context.Background()
	if _, err := service.ListProjectHooks(ctx, "", 100); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.ListRepositoryHooks(ctx, "", "", 100); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.EnableProjectHook(ctx, "", ""); err == nil {
		t.Fatal("expected error")
	}
	if err := service.DisableProjectHook(ctx, "", ""); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.EnableRepositoryHook(ctx, "", "", ""); err == nil {
		t.Fatal("expected error")
	}
	if err := service.DisableRepositoryHook(ctx, "", "", ""); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.GetProjectHookSettings(ctx, "", ""); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.GetRepositoryHookSettings(ctx, "", "", ""); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.SetProjectHookSettings(ctx, "", "", nil); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.SetRepositoryHookSettings(ctx, "", "", "", nil); err == nil {
		t.Fatal("expected error")
	}
}

func TestHookServiceErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client, _ := openapigenerated.NewClientWithResponses(server.URL + "/rest")
	service := NewService(client)
	ctx := context.Background()

	if _, err := service.ListProjectHooks(ctx, "P", 100); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.EnableProjectHook(ctx, "P", "h1"); err == nil {
		t.Fatal("expected error")
	}
	if err := service.DisableProjectHook(ctx, "P", "h1"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.GetProjectHookSettings(ctx, "P", "h1"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.SetProjectHookSettings(ctx, "P", "h1", map[string]any{"a": 1}); err == nil {
		t.Fatal("expected error")
	}

	if _, err := service.ListRepositoryHooks(ctx, "P", "S", 100); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.EnableRepositoryHook(ctx, "P", "S", "h1"); err == nil {
		t.Fatal("expected error")
	}
	if err := service.DisableRepositoryHook(ctx, "P", "S", "h1"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.GetRepositoryHookSettings(ctx, "P", "S", "h1"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.SetRepositoryHookSettings(ctx, "P", "S", "h1", map[string]any{"a": 1}); err == nil {
		t.Fatal("expected error")
	}
}

func TestHookServiceScripts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/demo/hook-scripts":
			_, _ = w.Write([]byte(`{"values":[{"script":{"id":123,"name":"my-script"}},{"script":{"id":456,"name":"another-script"}}],"isLastPage":true}`))
		case r.Method == http.MethodPut && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/demo/hook-scripts/123":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/demo/hook-scripts/123":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, _ := openapigenerated.NewClientWithResponses(server.URL + "/rest")
	service := NewService(client)
	ctx := context.Background()

	t.Run("list scripts success", func(t *testing.T) {
		scripts, err := service.ListHookScripts(ctx, "PRJ", "demo", 100)
		if err != nil || len(scripts) != 2 {
			t.Fatalf("expected 2 scripts, got %v: %v", scripts, err)
		}
		if *scripts[0].Script.Id != 123 {
			t.Fatalf("expected id 123, got %d", *scripts[0].Script.Id)
		}
	})

	t.Run("set script success", func(t *testing.T) {
		err := service.SetHookScript(ctx, "PRJ", "demo", "123", []string{"trigger1"})
		if err != nil {
			t.Fatalf("expected set script success, got %v", err)
		}
	})

	t.Run("remove script success", func(t *testing.T) {
		err := service.RemoveHookScript(ctx, "PRJ", "demo", "123")
		if err != nil {
			t.Fatalf("expected remove script success, got %v", err)
		}
	})

	t.Run("validation error", func(t *testing.T) {
		if _, err := service.ListHookScripts(ctx, "", "demo", 10); err == nil {
			t.Fatal("expected validation error")
		}
		if err := service.SetHookScript(ctx, "PRJ", "", "123", nil); err == nil {
			t.Fatal("expected validation error")
		}
		if err := service.RemoveHookScript(ctx, "PRJ", "demo", ""); err == nil {
			t.Fatal("expected validation error")
		}
	})

	t.Run("transient errors", func(t *testing.T) {
		badClient, _ := openapigenerated.NewClientWithResponses("http://invalid-url-that-does-not-exist.local")
		badService := NewService(badClient)

		if _, err := badService.ListHookScripts(ctx, "PRJ", "demo", 100); err == nil {
			t.Fatal("expected transient error")
		}
		if err := badService.SetHookScript(ctx, "PRJ", "demo", "123", nil); err == nil {
			t.Fatal("expected transient error")
		}
		if err := badService.RemoveHookScript(ctx, "PRJ", "demo", "123"); err == nil {
			t.Fatal("expected transient error")
		}
	})

	newHookTestService := func(t *testing.T, handler http.HandlerFunc) *Service {
		srv := httptest.NewServer(handler)
		t.Cleanup(srv.Close)
		cl, _ := openapigenerated.NewClientWithResponses(srv.URL + "/rest")
		return NewService(cl)
	}

	t.Run("list scripts coverage edge cases", func(t *testing.T) {
		// Test limit <= 0 defaults limit
		_, err := service.ListHookScripts(ctx, "PRJ", "demo", 0)
		if err != nil {
			t.Fatalf("expected list success with limit <= 0, got %v", err)
		}

		// Test status error
		errService := newHookTestService(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"errors":[]}`))
		})
		_, err = errService.ListHookScripts(ctx, "PRJ", "demo", 100)
		if err == nil || apperrors.ExitCode(err) != 4 {
			t.Fatalf("expected not found, got %v", err)
		}

		// Test empty response body
		nilBodyService := newHookTestService(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
		})
		res, err := nilBodyService.ListHookScripts(ctx, "PRJ", "demo", 100)
		if err != nil || len(res) != 0 {
			t.Fatalf("expected empty response, got %v, err %v", res, err)
		}

		// Test pagination next page start processing
		pageCalls := 0
		paginatedService := newHookTestService(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			pageCalls++
			if pageCalls == 1 {
				_, _ = w.Write([]byte(`{"isLastPage":false,"nextPageStart":1,"values":[{"script":{"id":1}}]}`))
			} else {
				_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"script":{"id":2}}]}`))
			}
		})
		res, err = paginatedService.ListHookScripts(ctx, "PRJ", "demo", 100)
		if err != nil || len(res) != 2 {
			t.Fatalf("expected 2 page items, got %v, err %v", res, err)
		}
	})

	t.Run("set and remove script errors", func(t *testing.T) {
		errService := newHookTestService(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusConflict)
		})
		err := errService.SetHookScript(ctx, "PRJ", "demo", "123", nil)
		if err == nil || apperrors.ExitCode(err) != 5 {
			t.Fatalf("expected conflict on set, got %v", err)
		}

		err = errService.RemoveHookScript(ctx, "PRJ", "demo", "123")
		if err == nil || apperrors.ExitCode(err) != 5 {
			t.Fatalf("expected conflict on remove, got %v", err)
		}
	})
}

