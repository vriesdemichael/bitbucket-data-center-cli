package gpgkey

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
)

func newGpgKeyTestService(t *testing.T, handler http.HandlerFunc) *Service {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client, err := openapigenerated.NewClientWithResponses(server.URL + "/rest")
	if err != nil {
		t.Fatalf("create client: %v", err)
	}

	return NewService(client)
}

func TestGpgKeyServiceCRUD(t *testing.T) {
	service := newGpgKeyTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/gpg/latest/keys":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":"426","emailAddress":"user@example.com","fingerprint":"FINGERPRINT1","text":"gpg-key-text"}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/rest/gpg/latest/keys":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"426","emailAddress":"user@example.com","fingerprint":"FINGERPRINT1","text":"gpg-key-text"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/gpg/latest/keys/426":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/gpg/latest/keys":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	})

	ctx := context.Background()

	// List
	list, err := service.ListGpgKeys(ctx, 10)
	if err != nil || len(list) != 1 || *list[0].Id != "426" {
		t.Fatalf("expected gpg key list success, got len=%d err=%v", len(list), err)
	}

	// Add
	added, err := service.AddGpgKey(ctx, "gpg-key-text")
	if err != nil || *added.Id != "426" {
		t.Fatalf("expected gpg key add success, got %#v err=%v", added, err)
	}

	// Remove
	if err := service.RemoveGpgKey(ctx, "426"); err != nil {
		t.Fatalf("expected gpg key remove success, got %v", err)
	}

	// Clear
	if err := service.ClearGpgKeys(ctx); err != nil {
		t.Fatalf("expected gpg key clear success, got %v", err)
	}
}

func TestGpgKeyServiceValidation(t *testing.T) {
	service := newGpgKeyTestService(t, func(w http.ResponseWriter, r *http.Request) {})
	ctx := context.Background()

	// Add validation error
	_, err := service.AddGpgKey(ctx, "   ")
	if err == nil || apperrors.ExitCode(err) != 2 { // Validation error has exit code 2
		t.Fatalf("expected validation error adding empty key, got %v", err)
	}

	// Remove validation error
	err = service.RemoveGpgKey(ctx, "")
	if err == nil || apperrors.ExitCode(err) != 2 {
		t.Fatalf("expected validation error removing empty key, got %v", err)
	}
}

func TestGpgKeyServicePagination(t *testing.T) {
	callCount := 0
	service := newGpgKeyTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && r.URL.Path == "/rest/gpg/latest/keys" {
			if callCount == 0 {
				callCount++
				_, _ = w.Write([]byte(`{"isLastPage":false,"nextPageStart":1,"values":[{"id":"1","emailAddress":"user1@example.com","fingerprint":"FP1","text":"key1"}]}`))
			} else {
				_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":"2","emailAddress":"user2@example.com","fingerprint":"FP2","text":"key2"}]}`))
			}
		} else {
			http.NotFound(w, r)
		}
	})

	ctx := context.Background()
	list, err := service.ListGpgKeys(ctx, 5)
	if err != nil || len(list) != 2 {
		t.Fatalf("expected gpg key list success with pagination, got len=%d err=%v", len(list), err)
	}
}

func TestGpgKeyServiceErrors(t *testing.T) {
	// 1. Client transport errors (e.g. invalid URL)
	badClient, _ := openapigenerated.NewClientWithResponses("http://127.0.0.1:0/rest")
	badService := NewService(badClient)
	ctx := context.Background()

	if _, err := badService.ListGpgKeys(ctx, 10); err == nil {
		t.Fatal("expected error listing with bad client")
	}
	if _, err := badService.AddGpgKey(ctx, "text"); err == nil {
		t.Fatal("expected error adding with bad client")
	}
	if err := badService.RemoveGpgKey(ctx, "key-id"); err == nil {
		t.Fatal("expected error removing with bad client")
	}
	if err := badService.ClearGpgKeys(ctx); err == nil {
		t.Fatal("expected error clearing with bad client")
	}

	// 2. HTTP Status errors (e.g. 500 Internal Server Error)
	errorService := newGpgKeyTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	if _, err := errorService.ListGpgKeys(ctx, 10); err == nil {
		t.Fatal("expected error listing on 500")
	}
	if _, err := errorService.AddGpgKey(ctx, "text"); err == nil {
		t.Fatal("expected error adding on 500")
	}
	if err := errorService.RemoveGpgKey(ctx, "key-id"); err == nil {
		t.Fatal("expected error removing on 500")
	}
	if err := errorService.ClearGpgKeys(ctx); err == nil {
		t.Fatal("expected error clearing on 500")
	}

	// 3. Nil JSON body cases
	nilBodyService := newGpgKeyTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`null`))
		} else {
			_, _ = w.Write([]byte(`invalid`))
		}
	})

	// ListGpgKeys with nil body
	list, err := nilBodyService.ListGpgKeys(ctx, 10)
	if err != nil || len(list) != 0 {
		t.Fatalf("expected empty list on nil body, got len=%d err=%v", len(list), err)
	}

	// AddGpgKey with nil body (returns empty JSON200)
	_, err = nilBodyService.AddGpgKey(ctx, "text")
	if err == nil {
		t.Fatal("expected error adding with nil JSON200 body")
	}
}

func TestGpgKeyServiceLimitAndEmpty(t *testing.T) {
	ctx := context.Background()

	// 1. Limit <= 0 and list truncation
	service := newGpgKeyTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"isLastPage":true,"values":[
			{"id":"1","emailAddress":"user1@example.com","fingerprint":"FP1"},
			{"id":"2","emailAddress":"user2@example.com","fingerprint":"FP2"},
			{"id":"3","emailAddress":"user3@example.com","fingerprint":"FP3"}
		]}`))
	})

	// test limit <= 0 fallback to default 25
	list, err := service.ListGpgKeys(ctx, 0)
	if err != nil || len(list) != 3 {
		t.Fatalf("expected 3 keys, got len=%d err=%v", len(list), err)
	}

	// test list truncation when exceeding limit
	list, err = service.ListGpgKeys(ctx, 2)
	if err != nil || len(list) != 2 {
		t.Fatalf("expected 2 keys due to limit, got len=%d err=%v", len(list), err)
	}

	// 2. Empty response body (non-json 200) for AddGpgKey
	emptyResponseService := newGpgKeyTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	_, err = emptyResponseService.AddGpgKey(ctx, "text")
	if err == nil {
		t.Fatal("expected error adding with empty body (non-json 200)")
	}
}

