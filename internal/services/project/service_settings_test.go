package project

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
)

func TestProjectWebhookService(t *testing.T) {
	t.Run("ListWebhooks", func(t *testing.T) {
		service := newProjectTestService(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/PRJ/webhooks" {
				_, _ = w.Write([]byte(`[{"id":123,"name":"wh","url":"http://url","active":true,"events":["repo:refs_changed"]}]`))
				return
			}
			http.NotFound(w, r)
		})

		res, err := service.ListProjectWebhooks(context.Background(), "PRJ")
		if err != nil {
			t.Fatalf("unexpected list error: %v", err)
		}
		if res == nil {
			t.Fatal("expected non-nil webhooks list")
		}

		// Validation error
		if _, err := service.ListProjectWebhooks(context.Background(), ""); err == nil {
			t.Fatal("expected validation error for empty project key")
		}
	})

	t.Run("CreateWebhook", func(t *testing.T) {
		service := newProjectTestService(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.Method == http.MethodPost && r.URL.Path == "/rest/api/latest/projects/PRJ/webhooks" {
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"id":123,"name":"wh","url":"http://url","active":true}`))
				return
			}
			http.NotFound(w, r)
		})

		res, err := service.CreateProjectWebhook(context.Background(), "PRJ", "wh", "http://url", []string{"repo:refs_changed"}, true)
		if err != nil {
			t.Fatalf("unexpected create error: %v", err)
		}
		if res == nil {
			t.Fatal("expected non-nil created webhook")
		}

		// Validation error
		if _, err := service.CreateProjectWebhook(context.Background(), "", "wh", "http://url", nil, true); err == nil {
			t.Fatal("expected validation error for empty project key")
		}
		if _, err := service.CreateProjectWebhook(context.Background(), "PRJ", "", "http://url", nil, true); err == nil {
			t.Fatal("expected validation error for empty name")
		}
		if _, err := service.CreateProjectWebhook(context.Background(), "PRJ", "wh", "", nil, true); err == nil {
			t.Fatal("expected validation error for empty url")
		}
	})

	t.Run("GetWebhook", func(t *testing.T) {
		service := newProjectTestService(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/PRJ/webhooks/123" {
				_, _ = w.Write([]byte(`{"id":123,"name":"wh","url":"http://url","active":true}`))
				return
			}
			http.NotFound(w, r)
		})

		res, err := service.GetProjectWebhook(context.Background(), "PRJ", "123")
		if err != nil {
			t.Fatalf("unexpected get error: %v", err)
		}
		if res == nil {
			t.Fatal("expected non-nil webhook")
		}

		// Validation error
		if _, err := service.GetProjectWebhook(context.Background(), "", "123"); err == nil {
			t.Fatal("expected validation error for empty project key")
		}
		if _, err := service.GetProjectWebhook(context.Background(), "PRJ", ""); err == nil {
			t.Fatal("expected validation error for empty id")
		}
	})

	t.Run("UpdateWebhook", func(t *testing.T) {
		service := newProjectTestService(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.Method == http.MethodPut && r.URL.Path == "/rest/api/latest/projects/PRJ/webhooks/123" {
				_, _ = w.Write([]byte(`{"id":123,"name":"wh-new","url":"http://url-new","active":false}`))
				return
			}
			http.NotFound(w, r)
		})

		active := false
		res, err := service.UpdateProjectWebhook(context.Background(), "PRJ", "123", "wh-new", "http://url-new", []string{"repo:refs_changed"}, &active)
		if err != nil {
			t.Fatalf("unexpected update error: %v", err)
		}
		if res == nil {
			t.Fatal("expected non-nil updated webhook")
		}

		// Validation error
		if _, err := service.UpdateProjectWebhook(context.Background(), "", "123", "", "", nil, nil); err == nil {
			t.Fatal("expected validation error for empty project key")
		}
		if _, err := service.UpdateProjectWebhook(context.Background(), "PRJ", "", "", "", nil, nil); err == nil {
			t.Fatal("expected validation error for empty id")
		}
	})

	t.Run("DeleteWebhook", func(t *testing.T) {
		service := newProjectTestService(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodDelete && r.URL.Path == "/rest/api/latest/projects/PRJ/webhooks/123" {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			http.NotFound(w, r)
		})

		err := service.DeleteProjectWebhook(context.Background(), "PRJ", "123")
		if err != nil {
			t.Fatalf("unexpected delete error: %v", err)
		}

		// Validation error
		if err := service.DeleteProjectWebhook(context.Background(), "", "123"); err == nil {
			t.Fatal("expected validation error for empty project key")
		}
		if err := service.DeleteProjectWebhook(context.Background(), "PRJ", ""); err == nil {
			t.Fatal("expected validation error for empty id")
		}
	})

	t.Run("TestWebhook", func(t *testing.T) {
		service := newProjectTestService(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.Method == http.MethodPost && r.URL.Path == "/rest/api/latest/projects/PRJ/webhooks/test" {
				_, _ = w.Write([]byte(`{"status":"ok"}`))
				return
			}
			http.NotFound(w, r)
		})

		res, err := service.TestProjectWebhook(context.Background(), "PRJ", "123")
		if err != nil {
			t.Fatalf("unexpected test error: %v", err)
		}
		if res == nil {
			t.Fatal("expected non-nil test response")
		}

		// Validation error
		if _, err := service.TestProjectWebhook(context.Background(), "", "123"); err == nil {
			t.Fatal("expected validation error for empty project key")
		}
		if _, err := service.TestProjectWebhook(context.Background(), "PRJ", ""); err == nil {
			t.Fatal("expected validation error for empty id")
		}
		if _, err := service.TestProjectWebhook(context.Background(), "PRJ", "abc"); err == nil {
			t.Fatal("expected validation error for non-integer id")
		}
	})

	t.Run("Statistics", func(t *testing.T) {
		service := newProjectTestService(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch {
			case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/PRJ/webhooks/123/statistics":
				_, _ = w.Write([]byte(`{"invocations":[]}`))
			case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/PRJ/webhooks/123/statistics/summary":
				_, _ = w.Write([]byte(`{"successCount":5}`))
			default:
				http.NotFound(w, r)
			}
		})

		stats, err := service.GetProjectWebhookStatistics(context.Background(), "PRJ", "123")
		if err != nil {
			t.Fatalf("unexpected statistics error: %v", err)
		}
		if stats == nil {
			t.Fatal("expected stats")
		}

		summary, err := service.GetProjectWebhookStatisticsSummary(context.Background(), "PRJ", "123")
		if err != nil {
			t.Fatalf("unexpected summary error: %v", err)
		}
		if summary == nil {
			t.Fatal("expected summary")
		}

		// Validation error
		if _, err := service.GetProjectWebhookStatistics(context.Background(), "", "123"); err == nil {
			t.Fatal("expected validation error for empty project key")
		}
		if _, err := service.GetProjectWebhookStatistics(context.Background(), "PRJ", ""); err == nil {
			t.Fatal("expected validation error for empty id")
		}
		if _, err := service.GetProjectWebhookStatisticsSummary(context.Background(), "", "123"); err == nil {
			t.Fatal("expected validation error for empty project key")
		}
		if _, err := service.GetProjectWebhookStatisticsSummary(context.Background(), "PRJ", ""); err == nil {
			t.Fatal("expected validation error for empty id")
		}
	})
}

func TestProjectRestrictionService(t *testing.T) {
	t.Run("ListRestrictions", func(t *testing.T) {
		service := newProjectTestService(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.Method == http.MethodGet && r.URL.Path == "/rest/branch-permissions/latest/projects/PRJ/restrictions" {
				_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":123,"type":"read-only"}]}`))
				return
			}
			http.NotFound(w, r)
		})

		res, err := service.ListRestrictions(context.Background(), "PRJ", RestrictionListOptions{
			Type:        "read-only",
			MatcherType: "BRANCH",
			MatcherID:   "refs/heads/master",
		})
		if err != nil {
			t.Fatalf("unexpected list error: %v", err)
		}
		if len(res) != 1 {
			t.Fatalf("expected 1 restriction, got %d", len(res))
		}

		// Validation error
		if _, err := service.ListRestrictions(context.Background(), "", RestrictionListOptions{}); err == nil {
			t.Fatal("expected validation error for empty project key")
		}
		if _, err := service.ListRestrictions(context.Background(), "PRJ", RestrictionListOptions{Type: "invalid"}); err == nil {
			t.Fatal("expected validation error for invalid restriction type")
		}
		if _, err := service.ListRestrictions(context.Background(), "PRJ", RestrictionListOptions{MatcherType: "invalid"}); err == nil {
			t.Fatal("expected validation error for invalid matcher type")
		}
	})

	t.Run("GetRestriction", func(t *testing.T) {
		service := newProjectTestService(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.Method == http.MethodGet && r.URL.Path == "/rest/branch-permissions/latest/projects/PRJ/restrictions/123" {
				_, _ = w.Write([]byte(`{"id":123,"type":"read-only"}`))
				return
			}
			http.NotFound(w, r)
		})

		res, err := service.GetRestriction(context.Background(), "PRJ", "123")
		if err != nil {
			t.Fatalf("unexpected get error: %v", err)
		}
		if res.Id == nil || *res.Id != 123 {
			t.Fatalf("expected restriction id 123, got %v", res.Id)
		}

		// Validation error
		if _, err := service.GetRestriction(context.Background(), "", "123"); err == nil {
			t.Fatal("expected validation error for empty project key")
		}
		if _, err := service.GetRestriction(context.Background(), "PRJ", ""); err == nil {
			t.Fatal("expected validation error for empty id")
		}
	})

	t.Run("CreateRestriction", func(t *testing.T) {
		service := newProjectTestService(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.Method == http.MethodPost && r.URL.Path == "/rest/branch-permissions/latest/projects/PRJ/restrictions" {
				_, _ = w.Write([]byte(`[{"id":123,"type":"read-only"}]`))
				return
			}
			http.NotFound(w, r)
		})

		res, err := service.CreateRestriction(context.Background(), "PRJ", RestrictionUpsertInput{
			Type:           "read-only",
			MatcherID:      "refs/heads/master",
			MatcherType:    "BRANCH",
			MatcherDisplay: "master",
			Users:          []string{"user1"},
			Groups:         []string{"group1"},
			AccessKeyIDs:   []int32{456},
		})
		if err != nil {
			t.Fatalf("unexpected create error: %v", err)
		}
		if res.Id == nil || *res.Id != 123 {
			t.Fatalf("expected restriction id 123, got %v", res.Id)
		}

		// Validation error
		if _, err := service.CreateRestriction(context.Background(), "", RestrictionUpsertInput{}); err == nil {
			t.Fatal("expected validation error for empty project key")
		}
		if _, err := service.CreateRestriction(context.Background(), "PRJ", RestrictionUpsertInput{Type: ""}); err == nil {
			t.Fatal("expected validation error for empty type")
		}
		if _, err := service.CreateRestriction(context.Background(), "PRJ", RestrictionUpsertInput{Type: "read-only", MatcherID: ""}); err == nil {
			t.Fatal("expected validation error for empty matcher ID")
		}
		if _, err := service.CreateRestriction(context.Background(), "PRJ", RestrictionUpsertInput{Type: "read-only", MatcherID: "a", MatcherType: "invalid"}); err == nil {
			t.Fatal("expected validation error for invalid matcher type")
		}
	})

	t.Run("UpdateRestriction", func(t *testing.T) {
		service := newProjectTestService(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch {
			case r.Method == http.MethodDelete && r.URL.Path == "/rest/branch-permissions/latest/projects/PRJ/restrictions/123":
				w.WriteHeader(http.StatusNoContent)
			case r.Method == http.MethodPost && r.URL.Path == "/rest/branch-permissions/latest/projects/PRJ/restrictions":
				_, _ = w.Write([]byte(`[{"id":124,"type":"read-only"}]`))
			default:
				http.NotFound(w, r)
			}
		})

		res, err := service.UpdateRestriction(context.Background(), "PRJ", "123", RestrictionUpsertInput{
			Type:         "read-only",
			MatcherID:    "refs/heads/master",
			MatcherType:  "BRANCH",
			Users:        []string{"user1"},
			Groups:       []string{"group1"},
			AccessKeyIDs: []int32{456},
		})
		if err != nil {
			t.Fatalf("unexpected update error: %v", err)
		}
		if res.Id == nil || *res.Id != 124 {
			t.Fatalf("expected restriction id 124, got %v", res.Id)
		}
	})

	t.Run("DeleteRestriction", func(t *testing.T) {
		service := newProjectTestService(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodDelete && r.URL.Path == "/rest/branch-permissions/latest/projects/PRJ/restrictions/123" {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			http.NotFound(w, r)
		})

		err := service.DeleteRestriction(context.Background(), "PRJ", "123")
		if err != nil {
			t.Fatalf("unexpected delete error: %v", err)
		}

		// Validation error
		if err := service.DeleteRestriction(context.Background(), "", "123"); err == nil {
			t.Fatal("expected validation error for empty project key")
		}
		if err := service.DeleteRestriction(context.Background(), "PRJ", ""); err == nil {
			t.Fatal("expected validation error for empty id")
		}
	})
}

func TestProjectDefaultTaskService(t *testing.T) {
	t.Run("ListDefaultTasks", func(t *testing.T) {
		service := newProjectTestService(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.Method == http.MethodGet && r.URL.Path == "/rest/default-tasks/latest/projects/PRJ/tasks" {
				_, _ = w.Write([]byte(`{"values":[{"id":123,"description":"task1"}]}`))
				return
			}
			http.NotFound(w, r)
		})

		res, err := service.ListDefaultTasks(context.Background(), "PRJ")
		if err != nil {
			t.Fatalf("unexpected list error: %v", err)
		}
		if len(res) != 1 {
			t.Fatalf("expected 1 task, got %d", len(res))
		}

		// Validation error
		if _, err := service.ListDefaultTasks(context.Background(), ""); err == nil {
			t.Fatal("expected validation error for empty project key")
		}
	})

	t.Run("AddDefaultTask", func(t *testing.T) {
		service := newProjectTestService(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.Method == http.MethodPost && r.URL.Path == "/rest/default-tasks/latest/projects/PRJ/tasks" {
				var body openapigenerated.AddDefaultTaskJSONRequestBody
				_ = json.NewDecoder(r.Body).Decode(&body)
				if body.Description == nil || *body.Description != "task1" {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				_, _ = w.Write([]byte(`{"id":123,"description":"task1"}`))
				return
			}
			http.NotFound(w, r)
		})

		src := "refs/heads/feature/*"
		tgt := "refs/heads/master"
		res, err := service.AddDefaultTask(context.Background(), "PRJ", "task1", &src, &tgt)
		if err != nil {
			t.Fatalf("unexpected add error: %v", err)
		}
		if res.Id == nil || *res.Id != 123 {
			t.Fatalf("expected task id 123, got %v", res.Id)
		}

		// Validation error
		if _, err := service.AddDefaultTask(context.Background(), "", "task1", nil, nil); err == nil {
			t.Fatal("expected validation error for empty project key")
		}
		if _, err := service.AddDefaultTask(context.Background(), "PRJ", "", nil, nil); err == nil {
			t.Fatal("expected validation error for empty description")
		}
	})

	t.Run("UpdateDefaultTask", func(t *testing.T) {
		service := newProjectTestService(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.Method == http.MethodPut && r.URL.Path == "/rest/default-tasks/latest/projects/PRJ/tasks/123" {
				_, _ = w.Write([]byte(`{"id":123,"description":"task1-new"}`))
				return
			}
			http.NotFound(w, r)
		})

		src := "refs/heads/feature/*"
		tgt := "refs/heads/master"
		res, err := service.UpdateDefaultTask(context.Background(), "PRJ", "123", "task1-new", &src, &tgt)
		if err != nil {
			t.Fatalf("unexpected update error: %v", err)
		}
		if res.Description == nil || *res.Description != "task1-new" {
			t.Fatalf("expected updated description task1-new, got %v", res.Description)
		}

		// Validation error
		if _, err := service.UpdateDefaultTask(context.Background(), "", "123", "task1-new", nil, nil); err == nil {
			t.Fatal("expected validation error for empty project key")
		}
		if _, err := service.UpdateDefaultTask(context.Background(), "PRJ", "", "task1-new", nil, nil); err == nil {
			t.Fatal("expected validation error for empty id")
		}
		if _, err := service.UpdateDefaultTask(context.Background(), "PRJ", "123", "", nil, nil); err == nil {
			t.Fatal("expected validation error for empty description")
		}
	})

	t.Run("DeleteDefaultTask", func(t *testing.T) {
		service := newProjectTestService(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodDelete && r.URL.Path == "/rest/default-tasks/latest/projects/PRJ/tasks/123" {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			http.NotFound(w, r)
		})

		err := service.DeleteDefaultTask(context.Background(), "PRJ", "123")
		if err != nil {
			t.Fatalf("unexpected delete error: %v", err)
		}

		// Validation error
		if err := service.DeleteDefaultTask(context.Background(), "", "123"); err == nil {
			t.Fatal("expected validation error for empty project key")
		}
		if err := service.DeleteDefaultTask(context.Background(), "PRJ", ""); err == nil {
			t.Fatal("expected validation error for empty id")
		}
	})
}

func TestRestrictionNormalizationHelpers(t *testing.T) {
	// normalizeProjectRestrictionType
	for _, val := range []string{"read-only", "no-deletes", "fast-forward-only", "pull-request-only", "no-creates"} {
		res, err := normalizeProjectRestrictionType(val)
		if err != nil || string(res) != val {
			t.Fatalf("expected valid type for %s, got: %v, %v", val, res, err)
		}
	}
	if _, err := normalizeProjectRestrictionType("invalid"); err == nil {
		t.Fatal("expected error for invalid type")
	}

	// normalizeProjectRestrictionMatcherType
	for _, val := range []string{"BRANCH", "MODEL_BRANCH", "MODEL_CATEGORY", "PATTERN"} {
		res, err := normalizeProjectRestrictionMatcherType(val)
		if err != nil || string(res) != val {
			t.Fatalf("expected valid matcher for %s, got: %v, %v", val, res, err)
		}
	}
	if _, err := normalizeProjectRestrictionMatcherType("invalid"); err == nil {
		t.Fatal("expected error for invalid matcher")
	}

	// normalizeProjectRestrictionRequestMatcherType
	for _, val := range []string{"BRANCH", "MODEL_BRANCH", "MODEL_CATEGORY", "PATTERN"} {
		res, err := normalizeProjectRestrictionRequestMatcherType(val)
		if err != nil || string(res) != val {
			t.Fatalf("expected valid req matcher for %s, got: %v, %v", val, res, err)
		}
	}
	res, err := normalizeProjectRestrictionRequestMatcherType("")
	if err != nil || string(res) != "BRANCH" {
		t.Fatalf("expected default BRANCH for empty, got: %v, %v", res, err)
	}
	if _, err := normalizeProjectRestrictionRequestMatcherType("invalid"); err == nil {
		t.Fatal("expected error for invalid req matcher")
	}
}

func TestProjectSettingsServiceAPIErrors(t *testing.T) {
	service := newProjectTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errors":[{"message":"not found"}]}`))
	})

	ctx := context.Background()

	// Webhooks
	if _, err := service.ListProjectWebhooks(ctx, "PRJ"); err == nil {
		t.Fatal("expected error on ListProjectWebhooks")
	}
	if _, err := service.CreateProjectWebhook(ctx, "PRJ", "wh", "http://url", nil, true); err == nil {
		t.Fatal("expected error on CreateProjectWebhook")
	}
	if _, err := service.GetProjectWebhook(ctx, "PRJ", "123"); err == nil {
		t.Fatal("expected error on GetProjectWebhook")
	}
	active := true
	if _, err := service.UpdateProjectWebhook(ctx, "PRJ", "123", "wh", "http://url", nil, &active); err == nil {
		t.Fatal("expected error on UpdateProjectWebhook")
	}
	if err := service.DeleteProjectWebhook(ctx, "PRJ", "123"); err == nil {
		t.Fatal("expected error on DeleteProjectWebhook")
	}
	if _, err := service.TestProjectWebhook(ctx, "PRJ", "123"); err == nil {
		t.Fatal("expected error on TestProjectWebhook")
	}
	if _, err := service.GetProjectWebhookStatistics(ctx, "PRJ", "123"); err == nil {
		t.Fatal("expected error on GetProjectWebhookStatistics")
	}
	if _, err := service.GetProjectWebhookStatisticsSummary(ctx, "PRJ", "123"); err == nil {
		t.Fatal("expected error on GetProjectWebhookStatisticsSummary")
	}

	// Restrictions
	if _, err := service.ListRestrictions(ctx, "PRJ", RestrictionListOptions{Type: "read-only"}); err == nil {
		t.Fatal("expected error on ListRestrictions")
	}
	if _, err := service.GetRestriction(ctx, "PRJ", "123"); err == nil {
		t.Fatal("expected error on GetRestriction")
	}
	if _, err := service.CreateRestriction(ctx, "PRJ", RestrictionUpsertInput{Type: "read-only", MatcherID: "a"}); err == nil {
		t.Fatal("expected error on CreateRestriction")
	}
	if _, err := service.UpdateRestriction(ctx, "PRJ", "123", RestrictionUpsertInput{Type: "read-only", MatcherID: "a"}); err == nil {
		t.Fatal("expected error on UpdateRestriction")
	}
	if err := service.DeleteRestriction(ctx, "PRJ", "123"); err == nil {
		t.Fatal("expected error on DeleteRestriction")
	}

	// Default Tasks
	if _, err := service.ListDefaultTasks(ctx, "PRJ"); err == nil {
		t.Fatal("expected error on ListDefaultTasks")
	}
	if _, err := service.AddDefaultTask(ctx, "PRJ", "desc", nil, nil); err == nil {
		t.Fatal("expected error on AddDefaultTask")
	}
	if _, err := service.UpdateDefaultTask(ctx, "PRJ", "123", "desc", nil, nil); err == nil {
		t.Fatal("expected error on UpdateDefaultTask")
	}
	if err := service.DeleteDefaultTask(ctx, "PRJ", "123"); err == nil {
		t.Fatal("expected error on DeleteDefaultTask")
	}
}

func TestProjectSettingsServiceEmptyAndInvalidJSON(t *testing.T) {
	// Webhooks
	t.Run("ListWebhooksEmptyAndInvalidJSON", func(t *testing.T) {
		serviceEmpty := newProjectTestService(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		res, err := serviceEmpty.ListProjectWebhooks(context.Background(), "PRJ")
		if err != nil || res != nil {
			t.Fatalf("expected nil webhook list on empty response, got: %v, %v", res, err)
		}

		serviceInvalid := newProjectTestService(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("{invalid-json"))
		})
		_, err = serviceInvalid.ListProjectWebhooks(context.Background(), "PRJ")
		if err == nil {
			t.Fatal("expected error on invalid JSON")
		}
	})

	// Restrictions
	t.Run("ListRestrictionsEmptyResponse", func(t *testing.T) {
		service := newProjectTestService(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
		})
		res, err := service.ListRestrictions(context.Background(), "PRJ", RestrictionListOptions{})
		if err != nil || len(res) != 0 {
			t.Fatalf("expected empty restriction list on empty response, got: %v, %v", res, err)
		}
	})

	// Default Tasks
	t.Run("ListDefaultTasksEmptyAndInvalidJSON", func(t *testing.T) {
		serviceEmpty := newProjectTestService(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		res, err := serviceEmpty.ListDefaultTasks(context.Background(), "PRJ")
		if err != nil || len(res) != 0 {
			t.Fatalf("expected empty default task list on empty response, got: %v, %v", res, err)
		}

		serviceInvalid := newProjectTestService(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("{invalid-json"))
		})
		_, err = serviceInvalid.ListDefaultTasks(context.Background(), "PRJ")
		if err == nil {
			t.Fatal("expected error on invalid JSON")
		}
	})

	t.Run("ListRestrictionsPagination", func(t *testing.T) {
		service := newProjectTestService(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.Method == http.MethodGet && r.URL.Path == "/rest/branch-permissions/latest/projects/PRJ/restrictions" {
				start := r.URL.Query().Get("start")
				if start == "" || start == "0" {
					_, _ = w.Write([]byte(`{"isLastPage":false,"nextPageStart":1,"values":[{"id":123,"type":"read-only"}]}`))
				} else {
					_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":124,"type":"read-only"}]}`))
				}
				return
			}
			http.NotFound(w, r)
		})

		res, err := service.ListRestrictions(context.Background(), "PRJ", RestrictionListOptions{Limit: 10})
		if err != nil {
			t.Fatalf("unexpected list error: %v", err)
		}
		if len(res) != 2 {
			t.Fatalf("expected 2 restrictions, got %d", len(res))
		}
	})

	t.Run("CreateRestrictionClientCastFail", func(t *testing.T) {
		service := &Service{
			client: &openapigenerated.ClientWithResponses{
				ClientInterface: nil, // not *openapigenerated.Client
			},
		}
		_, err := service.CreateRestriction(context.Background(), "PRJ", RestrictionUpsertInput{Type: "read-only", MatcherID: "a"})
		if err == nil || !strings.Contains(err.Error(), "failed to initialize project branch restriction request client") {
			t.Fatalf("expected client initialization error, got: %v", err)
		}
	})

	t.Run("CreateRestrictionEmptyResults", func(t *testing.T) {
		service := newProjectTestService(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
		})
		res, err := service.CreateRestriction(context.Background(), "PRJ", RestrictionUpsertInput{Type: "read-only", MatcherID: "a"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Id != nil {
			t.Fatalf("expected nil restriction ID, got: %v", res.Id)
		}
	})

	t.Run("UpdateRestrictionDeleteFail", func(t *testing.T) {
		service := newProjectTestService(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		})
		_, err := service.UpdateRestriction(context.Background(), "PRJ", "123", RestrictionUpsertInput{Type: "read-only", MatcherID: "a"})
		if err == nil || !strings.Contains(err.Error(), "failed to delete existing restriction for update") {
			t.Fatalf("expected delete fail error, got: %v", err)
		}
	})

	t.Run("CreateRestrictionInvalidJSON", func(t *testing.T) {
		service := newProjectTestService(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("{invalid-json"))
		})
		_, err := service.CreateRestriction(context.Background(), "PRJ", RestrictionUpsertInput{Type: "read-only", MatcherID: "a"})
		if err == nil || !strings.Contains(err.Error(), "failed to decode project branch restriction response") {
			t.Fatalf("expected json decode error, got: %v", err)
		}
	})

	t.Run("AddDefaultTaskInvalidJSON", func(t *testing.T) {
		service := newProjectTestService(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("{invalid-json"))
		})
		_, err := service.AddDefaultTask(context.Background(), "PRJ", "desc", nil, nil)
		if err == nil || !strings.Contains(err.Error(), "failed to decode default task response") {
			t.Fatalf("expected json decode error, got: %v", err)
		}
	})

	t.Run("UpdateDefaultTaskInvalidJSON", func(t *testing.T) {
		service := newProjectTestService(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("{invalid-json"))
		})
		_, err := service.UpdateDefaultTask(context.Background(), "PRJ", "123", "desc", nil, nil)
		if err == nil || !strings.Contains(err.Error(), "failed to decode default task response") {
			t.Fatalf("expected json decode error, got: %v", err)
		}
	})

	t.Run("UpdateProjectWebhookInvalidJSON", func(t *testing.T) {
		service := newProjectTestService(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("{invalid-json"))
		})
		active := true
		_, err := service.UpdateProjectWebhook(context.Background(), "PRJ", "123", "wh", "http://url", nil, &active)
		if err == nil || !strings.Contains(err.Error(), "failed to decode project webhook payload") {
			t.Fatalf("expected json decode error, got: %v", err)
		}
	})
}

func TestProjectSettingsServiceTransientErrors(t *testing.T) {
	service := newProjectTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Webhooks
	if _, err := service.ListProjectWebhooks(ctx, "PRJ"); err == nil {
		t.Fatal("expected transient error")
	}
	if _, err := service.CreateProjectWebhook(ctx, "PRJ", "wh", "http://url", nil, true); err == nil {
		t.Fatal("expected transient error")
	}
	if _, err := service.GetProjectWebhook(ctx, "PRJ", "123"); err == nil {
		t.Fatal("expected transient error")
	}
	active := true
	if _, err := service.UpdateProjectWebhook(ctx, "PRJ", "123", "wh", "http://url", nil, &active); err == nil {
		t.Fatal("expected transient error")
	}
	if err := service.DeleteProjectWebhook(ctx, "PRJ", "123"); err == nil {
		t.Fatal("expected transient error")
	}
	if _, err := service.TestProjectWebhook(ctx, "PRJ", "123"); err == nil {
		t.Fatal("expected transient error")
	}
	if _, err := service.GetProjectWebhookStatistics(ctx, "PRJ", "123"); err == nil {
		t.Fatal("expected transient error")
	}
	if _, err := service.GetProjectWebhookStatisticsSummary(ctx, "PRJ", "123"); err == nil {
		t.Fatal("expected transient error")
	}

	// Restrictions
	if _, err := service.ListRestrictions(ctx, "PRJ", RestrictionListOptions{}); err == nil {
		t.Fatal("expected transient error")
	}
	if _, err := service.GetRestriction(ctx, "PRJ", "123"); err == nil {
		t.Fatal("expected transient error")
	}
	if _, err := service.CreateRestriction(ctx, "PRJ", RestrictionUpsertInput{Type: "read-only", MatcherID: "a"}); err == nil {
		t.Fatal("expected transient error")
	}
	if _, err := service.UpdateRestriction(ctx, "PRJ", "123", RestrictionUpsertInput{Type: "read-only", MatcherID: "a"}); err == nil {
		t.Fatal("expected transient error")
	}
	if err := service.DeleteRestriction(ctx, "PRJ", "123"); err == nil {
		t.Fatal("expected transient error")
	}

	// Default Tasks
	if _, err := service.ListDefaultTasks(ctx, "PRJ"); err == nil {
		t.Fatal("expected transient error")
	}
	if _, err := service.AddDefaultTask(ctx, "PRJ", "desc", nil, nil); err == nil {
		t.Fatal("expected transient error")
	}
	if _, err := service.UpdateDefaultTask(ctx, "PRJ", "123", "desc", nil, nil); err == nil {
		t.Fatal("expected transient error")
	}
	if err := service.DeleteDefaultTask(ctx, "PRJ", "123"); err == nil {
		t.Fatal("expected transient error")
	}
}






