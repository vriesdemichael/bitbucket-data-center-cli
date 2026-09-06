package branch

import (
	"context"
	"testing"
)

func TestMapRestrictionInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   RestrictionUpsertInput
		wantErr bool
	}{
		{
			name: "valid full input",
			input: RestrictionUpsertInput{
				Type:           "read-only",
				MatcherType:    "BRANCH",
				MatcherID:      "refs/heads/main",
				MatcherDisplay: "main",
				Users:          []string{"user1"},
				Groups:         []string{"group1"},
				AccessKeyIDs:   []int32{123},
			},
			wantErr: false,
		},
		{
			name: "missing type",
			input: RestrictionUpsertInput{
				MatcherType: "BRANCH",
				MatcherID:   "refs/heads/main",
			},
			wantErr: true,
		},
		{
			name: "missing matcher id",
			input: RestrictionUpsertInput{
				Type:        "read-only",
				MatcherType: "BRANCH",
			},
			wantErr: true,
		},
		{
			name: "invalid matcher type",
			input: RestrictionUpsertInput{
				Type:        "read-only",
				MatcherType: "INVALID",
				MatcherID:   "refs/heads/main",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := mapRestrictionInput(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("mapRestrictionInput() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestBranchServiceValidationErrorsAdditional(t *testing.T) {
	t.Parallel()

	service := NewService(nil)
	repo := RepositoryRef{ProjectKey: " ", Slug: " "}

	if _, err := service.List(context.Background(), repo, ListOptions{}); err == nil {
		t.Error("expected error for empty repo")
	}
	if _, err := service.Create(context.Background(), repo, "", ""); err == nil {
		t.Error("expected error for empty name/startpoint")
	}
	if err := service.Delete(context.Background(), repo, "", "", false); err == nil {
		t.Error("expected error for empty delete name")
	}
	if _, err := service.GetDefault(context.Background(), repo); err == nil {
		t.Error("expected error for get default")
	}
	if err := service.SetDefault(context.Background(), repo, ""); err == nil {
		t.Error("expected error for set default empty")
	}
	if _, err := service.FindByCommit(context.Background(), repo, "", 0); err == nil {
		t.Error("expected error for find by commit empty")
	}
	if _, err := service.ListRestrictions(context.Background(), repo, RestrictionListOptions{}); err == nil {
		t.Error("expected error for list restrictions empty")
	}
	if _, err := service.GetRestriction(context.Background(), repo, ""); err == nil {
		t.Error("expected error for get restriction empty")
	}
}

func TestParseRestrictionID(t *testing.T) {
	t.Parallel()

	if _, err := parseRestrictionID("abc"); err == nil {
		t.Error("expected error for non-numeric id")
	}
	if _, err := parseRestrictionID("0"); err == nil {
		t.Error("expected error for id 0")
	}
	if id, err := parseRestrictionID("123"); err != nil || id != 123 {
		t.Errorf("expected 123, got %d err=%v", id, err)
	}
}
