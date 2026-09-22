package completion

import (
	"encoding/json"
	"testing"

	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

// TestGroupSpellingDecidesWhatAReviewerWordNames covers the one place a user
// slot answers with something that is not a user.
//
// The spelling has to come back on the candidate, because a shell replaces the
// whole word: returning "backend" for "@back" would leave the line reading
// --reviewers backend, which names a user who does not exist.
func TestGroupSpellingDecidesWhatAReviewerWordNames(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name     string
		word     string
		spelling string
		isGroup  bool
	}{
		{name: "an empty word is a user", word: "", spelling: "", isGroup: false},
		{name: "a bare word is a user", word: "ali", spelling: "", isGroup: false},
		{name: "an at-sign alone opens the groups", word: "@", spelling: "@", isGroup: true},
		{name: "a named group keeps its at-sign", word: "@backend", spelling: "@", isGroup: true},
		{
			name:     "the code owners spelling is kept once it is typed",
			word:     "@reviewer-group/back",
			spelling: "@reviewer-group/",
			isGroup:  true,
		},
		{
			name:     "the code owners spelling is matched regardless of case",
			word:     "@Reviewer-Group/back",
			spelling: "@Reviewer-Group/",
			isGroup:  true,
		},
		{
			name:     "a half-typed code owners prefix is still a bare group",
			word:     "@reviewer-gro",
			spelling: "@",
			isGroup:  true,
		},
		{
			name:     "an at-sign that is not leading names a user",
			word:     "alice@example.com",
			spelling: "",
			isGroup:  false,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			spelling, isGroup := groupSpelling(testCase.word)
			if isGroup != testCase.isGroup {
				t.Fatalf("groupSpelling(%q) reported group=%v, want %v", testCase.word, isGroup, testCase.isGroup)
			}
			if spelling != testCase.spelling {
				t.Errorf("groupSpelling(%q) = %q, want %q", testCase.word, spelling, testCase.spelling)
			}
		})
	}
}

// TestGroupSpellingIsAPrefixOfTheWord is the property the shell depends on.
//
// format drops any candidate that does not start with the word being
// completed, so a spelling that is not itself a prefix of that word would
// answer every press with nothing.
func TestGroupSpellingIsAPrefixOfTheWord(t *testing.T) {
	t.Parallel()

	for _, word := range []string{"@", "@b", "@backend", "@reviewer-group/", "@reviewer-group/qa", "@REVIEWER-GROUP/qa"} {
		spelling, isGroup := groupSpelling(word)
		if !isGroup {
			t.Fatalf("groupSpelling(%q) did not report a group", word)
		}
		if len(spelling) > len(word) || word[:len(spelling)] != spelling {
			t.Errorf("groupSpelling(%q) = %q, which is not a prefix of the word", word, spelling)
		}
	}
}

func TestDescribeUser(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name        string
		displayName string
		email       string
		want        string
	}{
		{
			name:        "both, because the address tells two Jan de Vrieses apart",
			displayName: "Jan de Vries",
			email:       "jan@example.com",
			want:        "Jan de Vries <jan@example.com>",
		},
		{
			name:        "the display name alone when the directory published no address",
			displayName: "Jan de Vries",
			want:        "Jan de Vries",
		},
		{
			name:  "the address alone when there is no display name",
			email: "jan@example.com",
			want:  "jan@example.com",
		},
		{name: "nothing at all rather than punctuation around nothing"},
		{
			name:        "whitespace is not a value",
			displayName: "   ",
			email:       "\t",
			want:        "",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := describeUser(testCase.displayName, testCase.email); got != testCase.want {
				t.Errorf("describeUser(%q, %q) = %q, want %q", testCase.displayName, testCase.email, got, testCase.want)
			}
		})
	}
}

func TestDescribeReviewerGroup(t *testing.T) {
	t.Parallel()

	describing := func(description *string, members int, withUsers bool) openapigenerated.RestReviewerGroup {
		group := openapigenerated.RestReviewerGroup{Description: description}
		if withUsers {
			users := make([]openapigenerated.ApplicationUser, members)
			group.Users = &users
		}

		return group
	}

	text := func(value string) *string { return &value }

	for _, testCase := range []struct {
		name  string
		group openapigenerated.RestReviewerGroup
		want  string
	}{
		{
			name:  "the description when the group carries one",
			group: describing(text("Owns the payment service"), 3, true),
			want:  "Owns the payment service",
		},
		{
			name:  "the size when it does not",
			group: describing(nil, 3, true),
			want:  "3 members",
		},
		{
			name:  "one member reads as one member",
			group: describing(nil, 1, true),
			want:  "1 member",
		},
		{
			name:  "an empty description is not a description",
			group: describing(text("  "), 2, true),
			want:  "2 members",
		},
		{
			name:  "nothing at all when the payload carries neither",
			group: describing(nil, 0, false),
			want:  "",
		},
		{
			name:  "an empty member list is not a size worth showing",
			group: describing(nil, 0, true),
			want:  "",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := describeReviewerGroup(testCase.group); got != testCase.want {
				t.Errorf("describeReviewerGroup() = %q, want %q", got, testCase.want)
			}
		})
	}
}

// TestGroupValueReadsBothListingShapes pins the decoding that survives the
// difference between the two group listings Bitbucket serves.
func TestGroupValueReadsBothListingShapes(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		body string
		want []string
	}{
		{
			name: "an array of names, which is what /groups answers with",
			body: `{"values":["stash-users"," padded "]}`,
			want: []string{"stash-users", "padded"},
		},
		{
			name: "an array of objects, which is what the administrative listing answers with",
			body: `{"values":[{"name":"stash-users","deletable":true}]}`,
			want: []string{"stash-users"},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var page struct {
				Values []groupValue `json:"values"`
			}
			if err := json.Unmarshal([]byte(testCase.body), &page); err != nil {
				t.Fatalf("decoding %s failed: %v", testCase.body, err)
			}

			if len(page.Values) != len(testCase.want) {
				t.Fatalf("decoded %d groups from %s, want %d", len(page.Values), testCase.body, len(testCase.want))
			}
			for index, want := range testCase.want {
				if page.Values[index].Name != want {
					t.Errorf("group %d decoded as %q, want %q", index, page.Values[index].Name, want)
				}
			}
		})
	}
}

// TestReviewerQueryNumbersItsPermissionFiltersFromOne pins the shape Bitbucket
// requires and silently ignores when it is wrong.
//
// The endpoint reads permission.1, permission.2 and so on, refuses to start
// anywhere but 1, and drops a query parameter it does not recognise instead of
// answering with an error -- so a misnumbered filter comes back as the whole
// instance rather than as a failure.
func TestReviewerQueryNumbersItsPermissionFiltersFromOne(t *testing.T) {
	t.Parallel()

	query := reviewerQuery("ali", "PROJ", "repo")

	for key, want := range map[string]string{
		"filter":                      "ali",
		"limit":                       "100",
		"permission.1":                "REPO_READ",
		"permission.1.projectKey":     "PROJ",
		"permission.1.repositorySlug": "repo",
		"permission.2":                "LICENSED_USER",
	} {
		if got := query[key]; got != want {
			t.Errorf("query[%q] = %q, want %q", key, got, want)
		}
	}

	if len(query) != 6 {
		t.Errorf("the query carries %d parameters, want exactly the six above: %v", len(query), query)
	}

	// The gap is the failure mode worth naming: Bitbucket drops a filter it
	// cannot number and answers with everybody, which looks like a working
	// completion.
	if _, numbered := query["permission.3"]; numbered {
		t.Error("a third filter appeared, leaving the numbering to be checked somewhere else")
	}
}

func TestFilteredListingOnlyFiltersOnSomethingTyped(t *testing.T) {
	t.Parallel()

	if _, filtered := filteredListing("")["filter"]; filtered {
		t.Error("an empty word sent a filter, which would ask the server for the users called \"\"")
	}
	if _, filtered := filteredListing("   ")["filter"]; filtered {
		t.Error("whitespace sent a filter")
	}
	if got := filteredListing("ali")["filter"]; got != "ali" {
		t.Errorf("filter = %q, want %q", got, "ali")
	}
	if got := filteredListing("")["limit"]; got != "100" {
		t.Errorf("limit = %q, want the cap the shell applies anyway", got)
	}
}

// TestReviewerSlotNamesOnlyTheSlotsThatMakeAReviewer guards the narrowing
// against spreading.
//
// Every other user slot takes any user on the instance, and a licensed-reader
// filter there would hide names the command accepts: `reviewer-group create
// --users` and `repo permissions grant <username>` both take people who have
// never read the repository.
func TestReviewerSlotNamesOnlyTheSlotsThatMakeAReviewer(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		path string
		flag string
		want bool
	}{
		{path: "pr create", flag: "reviewers", want: true},
		{path: "pr update", flag: "reviewers", want: true},
		{path: "pr review reviewer add", flag: "user", want: true},
		{path: "pr review reviewer remove", flag: "user", want: false},
		{path: "pr create", flag: "title", want: false},
		{path: "reviewer-group create", flag: "users", want: false},
		{path: "reviewer-group update", flag: "users", want: false},
		{path: "branch restriction create", flag: "user", want: false},
		{path: "auth token create", flag: "user", want: false},
		{path: "repo permissions grant", flag: "", want: false},
	} {
		if got := reviewerSlot(testCase.path, testCase.flag); got != testCase.want {
			t.Errorf("reviewerSlot(%q, %q) = %v, want %v", testCase.path, testCase.flag, got, testCase.want)
		}
	}
}

// TestAlsoProjectGroupsFollowsWhatTheCommandWillResolve keeps the two scopes
// apart where the command keeps them apart.
func TestAlsoProjectGroupsFollowsWhatTheCommandWillResolve(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		path string
		want bool
	}{
		{path: "pr create", want: true},
		{path: "pr review reviewer add", want: true},
		{path: "reviewer-group delete", want: false},
		{path: "reviewer-group update", want: false},
		{path: "reviewer-group users", want: false},
	} {
		if got := alsoProjectGroups(testCase.path); got != testCase.want {
			t.Errorf("alsoProjectGroups(%q) = %v, want %v", testCase.path, got, testCase.want)
		}
	}
}

// TestReviewerGroupResultCarriesTheSpellingAndDropsDuplicates covers the
// merging of two scopes into one answer.
//
// A repository group and a project group can share a name, and the resolution
// looks in the repository first, so the repository's is the one a person would
// get and the only one worth offering.
func TestReviewerGroupResultCarriesTheSpellingAndDropsDuplicates(t *testing.T) {
	t.Parallel()

	text := func(value string) *string { return &value }
	blank := ""

	result := reviewerGroupResult([]openapigenerated.RestReviewerGroup{
		{Name: text("backend"), Description: text("repository scope")},
		{Name: text("Backend"), Description: text("project scope")},
		{Name: text("qa")},
		{Name: &blank},
		{Description: text("nameless")},
	}, "@")

	if len(result.Candidates) != 2 {
		t.Fatalf("expected two candidates, got %v", result.Candidates)
	}
	if result.Candidates[0].Value != "@backend" {
		t.Errorf("first candidate = %q, want %q", result.Candidates[0].Value, "@backend")
	}
	if result.Candidates[0].Description != "repository scope" {
		t.Errorf("the project's group won over the repository's: %q", result.Candidates[0].Description)
	}
	if result.Candidates[1].Value != "@qa" {
		t.Errorf("second candidate = %q, want %q", result.Candidates[1].Value, "@qa")
	}
}
