package repocmd

import (
	"testing"

	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
)

func TestCommentOwnedByUser(t *testing.T) {
	username := "alice"
	commentWithName := openapigenerated.RestComment{Author: &struct {
		Active       *bool                                  `json:"active,omitempty"`
		AvatarUrl    *string                                `json:"avatarUrl,omitempty"`
		DisplayName  string                                 `json:"displayName"`
		EmailAddress *string                                `json:"emailAddress,omitempty"`
		Id           *int32                                 `json:"id,omitempty"`
		Links        *map[string]interface{}                `json:"links,omitempty"`
		Name         string                                 `json:"name"`
		Slug         string                                 `json:"slug"`
		Type         openapigenerated.RestCommentAuthorType `json:"type"`
	}{Name: username}}
	if !commentOwnedByUser(commentWithName, " alice ") {
		t.Fatal("expected comment ownership match by name")
	}

	slug := "alice"
	commentWithSlug := openapigenerated.RestComment{Author: &struct {
		Active       *bool                                  `json:"active,omitempty"`
		AvatarUrl    *string                                `json:"avatarUrl,omitempty"`
		DisplayName  string                                 `json:"displayName"`
		EmailAddress *string                                `json:"emailAddress,omitempty"`
		Id           *int32                                 `json:"id,omitempty"`
		Links        *map[string]interface{}                `json:"links,omitempty"`
		Name         string                                 `json:"name"`
		Slug         string                                 `json:"slug"`
		Type         openapigenerated.RestCommentAuthorType `json:"type"`
	}{Slug: slug}}
	if !commentOwnedByUser(commentWithSlug, "alice") {
		t.Fatal("expected comment ownership match by slug")
	}

	if commentOwnedByUser(openapigenerated.RestComment{}, "alice") {
		t.Fatal("expected missing author to fail ownership check")
	}
	if commentOwnedByUser(commentWithName, "") {
		t.Fatal("expected blank username to fail ownership check")
	}
	if commentOwnedByUser(commentWithName, "bob") {
		t.Fatal("expected mismatched username to fail ownership check")
	}
}
