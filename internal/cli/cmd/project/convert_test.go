package projectcmd

import (
	"testing"

	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/result"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
)

// TestPageOfNarrowsBothRenderingsTheSameWay guards the flag defect: --start and
// --limit used to narrow the table while --json returned every webhook, so one
// command answered two ways to the same flags.
func TestPageOfNarrowsBothRenderingsTheSameWay(t *testing.T) {
	t.Parallel()

	all := []result.Webhook{{ID: 1}, {ID: 2}, {ID: 3}, {ID: 4}}

	if page := pageOf(all, 1, 2); len(page) != 2 || page[0].ID != 2 || page[1].ID != 3 {
		t.Fatalf("page = %+v, want the second and third", page)
	}
	if page := pageOf(all, -5, 2); len(page) != 2 || page[0].ID != 1 {
		t.Fatalf("a negative start produced %+v, want the beginning", page)
	}
	if page := pageOf(all, 10, 2); len(page) != 0 {
		t.Fatalf("a start past the end produced %+v, want nothing", page)
	}
	if page := pageOf(all, 2, 100); len(page) != 2 {
		t.Fatalf("a limit past the end produced %+v, want the remainder", page)
	}
	if page := pageOf(all, 0, 0); len(page) != 4 {
		t.Fatalf("no limit produced %+v, want everything", page)
	}
}

func TestProjectFromDropsTheAvatarAndKeepsTheKey(t *testing.T) {
	t.Parallel()

	// The avatar arrives as a base64 data URI running to tens of kilobytes,
	// larger than every other field put together and carrying nothing a caller
	// acts on.
	projectType := openapigenerated.RestProjectType("NORMAL")
	id := int32(3)
	key, name, description, scope := "PRJ", "Project", "the description", "PROJECT"
	public := true
	avatar := "data:image/png;base64,AAAA"

	converted := projectFrom(openapigenerated.RestProject{
		Id:          &id,
		Key:         &key,
		Name:        &name,
		Description: &description,
		Public:      &public,
		Type:        &projectType,
		Scope:       &scope,
		Avatar:      &avatar,
	})

	want := Project{ID: 3, Key: "PRJ", Name: "Project", Description: "the description", Public: true, Type: "NORMAL", Scope: "PROJECT"}
	if converted != want {
		t.Fatalf("project = %+v, want %+v", converted, want)
	}

	if list := projectsFrom(nil); list == nil || len(list) != 0 {
		t.Fatalf("projectsFrom(nil) = %v, want an empty slice rather than nil", list)
	}
}

func TestEffectivePermissionsFromIsOrderedRatherThanAMap(t *testing.T) {
	t.Parallel()

	converted := effectivePermissionsFrom(map[string]bool{"PROJECT_READ": true, "PROJECT_ADMIN": true})

	if len(converted) != 3 {
		t.Fatalf("expected one entry per level, got %+v", converted)
	}
	for index, want := range []string{"PROJECT_READ", "PROJECT_WRITE", "PROJECT_ADMIN"} {
		if converted[index].Permission != want {
			t.Fatalf("entry %d = %q, want %q -- increasing privilege", index, converted[index].Permission, want)
		}
	}
	if !converted[0].Granted || converted[1].Granted || !converted[2].Granted {
		t.Fatalf("granted flags wrong: %+v", converted)
	}
}

func TestDefaultTaskValueHandlesTheAbsentPointer(t *testing.T) {
	t.Parallel()

	if value := defaultTaskValue(nil); value.ID != 0 || value.Description != "" {
		t.Fatalf("defaultTaskValue(nil) = %+v, want the zero value", value)
	}
	if list := defaultTasksFrom(nil); list == nil || len(list) != 0 {
		t.Fatalf("defaultTasksFrom(nil) = %v, want an empty slice rather than nil", list)
	}
	if list := permissionEntriesFrom(nil); list == nil || len(list) != 0 {
		t.Fatalf("permissionEntriesFrom(nil) = %v, want an empty slice rather than nil", list)
	}
}
