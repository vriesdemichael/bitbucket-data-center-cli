package searchcmd

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	pullrequestservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequest"
)

// dateFields are the dates a period can apply to, and groupings the ways the
// text output can be grouped.
var (
	dateFields = []string{pullrequestservice.DateCreated, pullrequestservice.DateUpdated, pullrequestservice.DateClosed}
	groupings  = []string{"week", "repo"}
)

const dayLayout = "2006-01-02"

// periodFrom reads --since, --until and --date-field.
//
// A bound is a day, as a person names one, or a moment. A day is read in
// location -- the day somebody worked, not the UTC one -- and the day given to
// --until runs to its end, so that --since and --until naming the same day is
// that day rather than nothing.
func periodFrom(since, until, field string, location *time.Location) (pullrequestservice.Period, error) {
	period := pullrequestservice.Period{Field: field}

	var err error
	if period.Since, err = boundOf("--since", since, false, location); err != nil {
		return pullrequestservice.Period{}, err
	}
	if period.Until, err = boundOf("--until", until, true, location); err != nil {
		return pullrequestservice.Period{}, err
	}
	if period.Since > 0 && period.Until > 0 && period.Until < period.Since {
		return pullrequestservice.Period{}, apperrors.New(apperrors.KindValidation,
			fmt.Sprintf("--until %s is before --since %s", until, since), nil)
	}

	return period, nil
}

func boundOf(flag, value string, endOfDay bool, location *time.Location) (int64, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, nil
	}

	if day, err := time.ParseInLocation(dayLayout, trimmed, location); err == nil {
		if endOfDay {
			day = day.AddDate(0, 0, 1).Add(-time.Millisecond)
		}
		return day.UnixMilli(), nil
	}
	if moment, err := time.Parse(time.RFC3339, trimmed); err == nil {
		return moment.UnixMilli(), nil
	}

	return 0, apperrors.New(apperrors.KindValidation,
		fmt.Sprintf("%s %q is neither a day (YYYY-MM-DD) nor a moment (RFC 3339, as 2026-07-20T09:00:00+02:00)", flag, value), nil)
}

// dateOf is the date of a pull request that field names, in milliseconds since
// the epoch, and zero when it has none.
func dateOf(pullRequest result.PullRequest, field string) int64 {
	switch field {
	case pullrequestservice.DateUpdated:
		return pullRequest.UpdatedDate
	case pullrequestservice.DateClosed:
		return pullRequest.ClosedDate
	default:
		return pullRequest.CreatedDate
	}
}

// dayOf is a date as the day it falls on in location, and a dash for none.
func dayOf(millis int64, location *time.Location) string {
	if millis == 0 {
		return "-"
	}

	return time.UnixMilli(millis).In(location).Format(dayLayout)
}

// pullRequestGroup is one heading of the text output and what is under it.
type pullRequestGroup struct {
	heading      string
	pullRequests []result.PullRequest
}

// groupPullRequests arranges a listing for the text output: by the week the
// date falls in, newest week first, or by repository, by name. Within a group
// the listing's own order stands. No grouping is one group with no heading.
func groupPullRequests(pullRequests []result.PullRequest, by, field string, location *time.Location) []pullRequestGroup {
	if by == "" {
		return []pullRequestGroup{{pullRequests: pullRequests}}
	}

	headings := []string{}
	grouped := map[string][]result.PullRequest{}
	for _, pullRequest := range pullRequests {
		heading := ""
		switch by {
		case "week":
			heading = weekOf(dateOf(pullRequest, field), location)
		default:
			heading = pullRequest.Repository.ProjectKey + "/" + pullRequest.Repository.Slug
		}
		if _, seen := grouped[heading]; !seen {
			headings = append(headings, heading)
		}
		grouped[heading] = append(grouped[heading], pullRequest)
	}

	slices.Sort(headings)
	if by == "week" {
		// Newest first, as the listing is. "No date" sorts after every week.
		slices.Reverse(headings)
		if index := slices.Index(headings, noDate); index >= 0 {
			headings = append(slices.Delete(headings, index, index+1), noDate)
		}
	}

	groups := make([]pullRequestGroup, 0, len(headings))
	for _, heading := range headings {
		groups = append(groups, pullRequestGroup{heading: heading, pullRequests: grouped[heading]})
	}

	return groups
}

const noDate = "No date"

// weekOf is the heading of the week a date falls in, named by its Monday.
func weekOf(millis int64, location *time.Location) string {
	if millis == 0 {
		return noDate
	}

	day := time.UnixMilli(millis).In(location)
	monday := day.AddDate(0, 0, -((int(day.Weekday()) + 6) % 7))

	return "Week of " + monday.Format(dayLayout)
}
