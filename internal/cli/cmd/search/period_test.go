package searchcmd

import (
	"strings"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	pullrequestservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequest"
)

// A day is read where the person is, and --until runs to the end of the day it
// names, so naming one day twice is that day. A moment is what it says.
func TestAPeriodIsReadInTheCallersDays(t *testing.T) {
	t.Parallel()

	amsterdam := time.FixedZone("CEST", 2*60*60)
	at := func(value string) int64 {
		t.Helper()
		moment, err := time.Parse(time.RFC3339Nano, value)
		if err != nil {
			t.Fatalf("parse %s: %v", value, err)
		}
		return moment.UnixMilli()
	}

	period, err := periodFrom("2026-07-20", "2026-07-20", pullrequestservice.DateClosed, amsterdam)
	if err != nil {
		t.Fatalf("a period of one day was refused: %v", err)
	}
	if period.Since != at("2026-07-20T00:00:00+02:00") || period.Until != at("2026-07-20T23:59:59.999+02:00") {
		t.Errorf("one day in Amsterdam runs from %d to %d", period.Since, period.Until)
	}
	if period.Field != pullrequestservice.DateClosed {
		t.Errorf("the date field is %q", period.Field)
	}

	period, err = periodFrom("2026-07-20T09:30:00Z", "", "", amsterdam)
	if err != nil || period.Since != at("2026-07-20T09:30:00Z") || period.Until != 0 {
		t.Errorf("a moment was read as %d (until %d): %v", period.Since, period.Until, err)
	}

	if period, err := periodFrom("", "", "", amsterdam); err != nil || period.Since != 0 || period.Until != 0 {
		t.Errorf("no bounds were read as %+v: %v", period, err)
	}

	for _, bad := range [][2]string{{"yesterday", ""}, {"", "20-07-2026"}, {"2026-07-21", "2026-07-20"}} {
		_, err := periodFrom(bad[0], bad[1], "", amsterdam)
		if !apperrors.IsKind(err, apperrors.KindValidation) {
			t.Errorf("--since %q --until %q was not refused as invalid: %v", bad[0], bad[1], err)
		}
	}
	if _, err := periodFrom("yesterday", "", "", amsterdam); err == nil || !strings.Contains(err.Error(), "--since") {
		t.Errorf("the refusal does not name the flag: %v", err)
	}
}

// Weeks are named by their Monday and listed newest first, repositories by
// name, and within a group the listing's own order stands. A pull request with
// no date on the chosen field comes last, under its own heading.
func TestPullRequestsAreGroupedByWeekOrRepository(t *testing.T) {
	t.Parallel()

	utc := time.UTC
	day := func(value string) int64 {
		t.Helper()
		moment, err := time.ParseInLocation(dayLayout, value, utc)
		if err != nil {
			t.Fatalf("parse %s: %v", value, err)
		}
		return moment.UnixMilli()
	}
	in := func(project, slug string) result.Repository {
		return result.Repository{ProjectKey: project, Slug: slug}
	}

	listing := []result.PullRequest{
		// 2026-07-26 is a Sunday, the last day of the week of Monday the 20th.
		{ID: 1, Repository: in("WEB", "site"), CreatedDate: day("2026-07-26"), ClosedDate: day("2026-07-27")},
		{ID: 2, Repository: in("API", "core"), CreatedDate: day("2026-07-27")},
		{ID: 3, Repository: in("WEB", "site"), CreatedDate: day("2026-07-20")},
	}
	describe := func(groups []pullRequestGroup) string {
		parts := []string{}
		for _, group := range groups {
			ids := []string{}
			for _, pullRequest := range group.pullRequests {
				ids = append(ids, string(rune('0'+pullRequest.ID)))
			}
			parts = append(parts, group.heading+"="+strings.Join(ids, ","))
		}
		return strings.Join(parts, "; ")
	}

	for _, testCase := range []struct {
		by, field, want string
	}{
		{"", "created", "=1,2,3"},
		{"week", "created", "Week of 2026-07-27=2; Week of 2026-07-20=1,3"},
		{"week", "closed", "Week of 2026-07-27=1; No date=2,3"},
		{"repo", "created", "API/core=2; WEB/site=1,3"},
	} {
		if got := describe(groupPullRequests(listing, testCase.by, testCase.field, utc)); got != testCase.want {
			t.Errorf("grouped by %q on %s: %s, want %s", testCase.by, testCase.field, got, testCase.want)
		}
	}

	if got := dayOf(day("2026-07-26"), utc); got != "2026-07-26" {
		t.Errorf("a date is shown as %q", got)
	}
	if got := dayOf(0, utc); got != "-" {
		t.Errorf("no date is shown as %q", got)
	}
}
