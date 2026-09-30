package pullrequest

import (
	"testing"
)

// A period keeps the pull requests whose chosen date falls inside it, both
// bounds included. One with no such date -- an open one has never closed --
// is outside any period on that date.
func TestPeriodIncludesByItsDateField(t *testing.T) {
	t.Parallel()

	pullRequest := PullRequest{CreatedDate: 100, UpdatedDate: 300, ClosedDate: 200}
	open := PullRequest{CreatedDate: 100, UpdatedDate: 300}

	for _, testCase := range []struct {
		name        string
		period      Period
		pullRequest PullRequest
		want        bool
	}{
		{"no bounds keeps everything", Period{}, open, true},
		{"created on the first moment", Period{Since: 100, Until: 150}, pullRequest, true},
		{"created on the last moment", Period{Since: 50, Until: 100}, pullRequest, true},
		{"created before the period", Period{Since: 101}, pullRequest, false},
		{"created after the period", Period{Until: 99}, pullRequest, false},
		{"updated in the period", Period{Since: 250, Until: 350, Field: DateUpdated}, pullRequest, true},
		{"closed in the period", Period{Since: 150, Until: 250, Field: DateClosed}, pullRequest, true},
		{"closed outside a period its creation is in", Period{Since: 50, Until: 150, Field: DateClosed}, pullRequest, false},
		{"never closed", Period{Since: 1, Field: DateClosed}, open, false},
	} {
		if got := testCase.period.includes(testCase.pullRequest); got != testCase.want {
			t.Errorf("%s: included = %v, want %v", testCase.name, got, testCase.want)
		}
	}
}

// Bitbucket lists pull requests newest first by when they were last updated,
// and one is never created or closed after that. So a page is cut at the first
// pull request updated before the period began, whichever date the period
// looks at, and the walk is told it has ended -- it would otherwise page back
// to the first pull request ever opened to find nothing more.
func TestPeriodEndsTheWalkAtTheFirstPullRequestUpdatedBeforeIt(t *testing.T) {
	t.Parallel()

	everything := func(PullRequest) bool { return true }
	page := []PullRequest{
		{ID: 4, CreatedDate: 500, UpdatedDate: 900},
		{ID: 3, CreatedDate: 100, UpdatedDate: 800, ClosedDate: 800},
		{ID: 2, CreatedDate: 600, UpdatedDate: 700},
		{ID: 1, CreatedDate: 100, UpdatedDate: 300},
		{ID: 0, CreatedDate: 50, UpdatedDate: 200},
	}

	for _, testCase := range []struct {
		name   string
		period Period
		want   []int64
		ended  bool
	}{
		// 3 was created before the period and is dropped, but it was updated
		// in it, so the walk goes on past it to 2.
		{"created", Period{Since: 400}, []int64{4, 2}, true},
		{"closed", Period{Since: 400, Field: DateClosed}, []int64{3}, true},
		{"updated", Period{Since: 750, Field: DateUpdated}, []int64{4, 3}, true},
		{"only an upper bound never ends it", Period{Until: 150}, []int64{3, 1, 0}, false},
		{"a period older than the whole page", Period{Since: 10}, []int64{4, 3, 2, 1, 0}, false},
	} {
		kept, ended := testCase.period.within(page, everything)

		ids := make([]int64, 0, len(kept))
		for _, pullRequest := range kept {
			ids = append(ids, pullRequest.ID)
		}
		if len(ids) != len(testCase.want) || ended != testCase.ended {
			t.Errorf("%s: kept %v (ended %v), want %v (ended %v)", testCase.name, ids, ended, testCase.want, testCase.ended)
			continue
		}
		for index, id := range ids {
			if id != testCase.want[index] {
				t.Errorf("%s: kept %v, want %v", testCase.name, ids, testCase.want)
				break
			}
		}
	}

	// The caller's own filter still applies inside the period.
	kept, _ := Period{Since: 400}.within(page, func(pullRequest PullRequest) bool { return pullRequest.ID != 4 })
	if len(kept) != 1 || kept[0].ID != 2 {
		t.Errorf("the caller's filter was not applied: kept %v", kept)
	}
}

// The states bb accepts, as each listing is asked for them. The dashboard has
// no ALL and answers 400 to one, so every state is asked for by naming none.
func TestStatesAsEachListingIsAskedForThem(t *testing.T) {
	t.Parallel()

	for state, want := range map[string][2]string{
		"open":     {"OPEN", "OPEN"},
		"merged":   {"MERGED", "MERGED"},
		"declined": {"DECLINED", "DECLINED"},
		"closed":   {"ALL", ""},
		"all":      {"ALL", ""},
	} {
		normalized, err := normalizeState(state)
		if err != nil {
			t.Fatalf("state %s was refused: %v", state, err)
		}
		if got := bitbucketState(normalized); got != want[0] {
			t.Errorf("a repository listing is asked for %q as %q, want %q", state, got, want[0])
		}
		if got := dashboardState(normalized); got != want[1] {
			t.Errorf("the dashboard is asked for %q as %q, want %q", state, got, want[1])
		}
	}

	if _, err := normalizeState("reopened"); err == nil {
		t.Error("a state that is not one was accepted")
	}

	// closed is merged and declined together, which neither listing can be
	// asked for, so the open ones are dropped as they arrive.
	open := PullRequest{State: "OPEN", Open: true}
	merged := PullRequest{State: "MERGED", Closed: true}
	declined := PullRequest{State: "DECLINED", Closed: true}
	for _, testCase := range []struct {
		state string
		keeps []PullRequest
		drops []PullRequest
	}{
		{"closed", []PullRequest{merged, declined}, []PullRequest{open}},
		{"merged", []PullRequest{merged}, []PullRequest{open, declined}},
		{"declined", []PullRequest{declined}, []PullRequest{open, merged}},
		{"all", []PullRequest{open, merged, declined}, nil},
	} {
		for _, pullRequest := range testCase.keeps {
			if !matchesFilters(pullRequest, testCase.state, "", "") {
				t.Errorf("state %s dropped a %s pull request", testCase.state, pullRequest.State)
			}
		}
		for _, pullRequest := range testCase.drops {
			if matchesFilters(pullRequest, testCase.state, "", "") {
				t.Errorf("state %s kept a %s pull request", testCase.state, pullRequest.State)
			}
		}
	}
}
