package mcp

import (
	"sort"
	"strings"

	pullrequestactivityservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequestactivity"
)

// A pull request's activity, as Bitbucket's overview lists it under the
// description: newest first, who opened, reviewed, updated and merged it, and
// each comment thread, one on a line with the lines of the diff Bitbucket
// shows it among. The comments of a pull request are there and in its diff,
// as in Bitbucket, and nowhere else.

// The activity a card carries. Its counts come from Bitbucket's summary of the
// whole timeline, so they hold however many items there are; the items past
// these are left out, and the view says it lists fewer.
const (
	maxViewActivityItems = 200
	maxViewActivityBytes = 384 << 10
)

// viewActivity is a pull request's activity, newest first.
type viewActivity struct {
	Items []viewActivityItem `json:"items"`
	// Total is how many items the pull request's activity has; a view that
	// carries fewer says so.
	Total int `json:"total"`
}

// viewActivityItem is one thing someone did, in Bitbucket's words for it:
// OPENED, APPROVED, UNAPPROVED, REVIEWED (changes requested), COMMENTED,
// RESCOPED (commits pushed), UPDATED (reviewers changed), MERGED, DECLINED or
// REOPENED.
type viewActivityItem struct {
	Action string `json:"action"`
	Date   int64  `json:"date,omitempty"`
	// User is who did it, and Username the key of their avatar.
	User     string `json:"user,omitempty"`
	Username string `json:"username,omitempty"`
	// Thread is the comment thread a COMMENTED item opened, with its replies,
	// and for a comment on a line the lines of the diff it is among.
	Thread *viewThread `json:"thread,omitempty"`
	// Added and Removed count the commits a push added and took away.
	Added   int `json:"added,omitempty"`
	Removed int `json:"removed,omitempty"`
	// Commit is the commit a merge made, as Bitbucket shows it.
	Commit string `json:"commit,omitempty"`
	// Reviewers are the reviewers an update added and took away.
	AddedReviewers   []string `json:"added_reviewers,omitempty"`
	RemovedReviewers []string `json:"removed_reviewers,omitempty"`
}

// activityActions are the actions an overview lists. The rest, such as a
// change of title, Bitbucket's overview lists in words a view has no data for.
var activityActions = map[string]bool{
	"OPENED": true, "APPROVED": true, "UNAPPROVED": true, "REVIEWED": true, "COMMENTED": true,
	"RESCOPED": true, "UPDATED": true, "MERGED": true, "DECLINED": true, "REOPENED": true,
}

// activityForView lists a pull request's activity as its overview draws it,
// and the people whose avatars it draws, from the timeline the review summary
// counts. threads are the pull request's comment threads, read from the same
// timeline, by the ID of their first comment.
func activityForView(activities []pullrequestactivityservice.Activity, threads map[int64]pullrequestactivityservice.Thread) (viewActivity, map[string]string) {
	ordered := make([]pullrequestactivityservice.Activity, 0, len(activities))
	for _, activity := range activities {
		if listedActivity(activity, threads) {
			ordered = append(ordered, activity)
		}
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].CreatedDate != ordered[j].CreatedDate {
			return ordered[i].CreatedDate > ordered[j].CreatedDate
		}
		return ordered[i].ID > ordered[j].ID
	})

	out := viewActivity{Items: []viewActivityItem{}, Total: len(ordered)}
	people := map[string]string{}
	add := func(account pullrequestactivityservice.Account) {
		if account.Username != "" && account.Slug != "" && len(people) < maxViewPeople {
			people[account.Username] = account.Slug
		}
	}
	budget := maxViewActivityBytes
	for _, activity := range ordered {
		if len(out.Items) >= maxViewActivityItems {
			break
		}
		item, account := activityItemOf(activity)
		size := 120 + len(item.User)
		if activity.Action == "COMMENTED" {
			thread := threads[commentIDOf(activity)]
			view := viewThreadOf(thread)
			view.Context = contextFromActivity(activity.Raw)
			item.Thread = &view
			size += view.size()
		}
		if size > budget {
			break
		}
		budget -= size
		out.Items = append(out.Items, item)
		add(account)
		if item.Thread != nil {
			thread := threads[item.Thread.ID]
			add(thread.AuthorAccount)
			for _, reply := range thread.Replies {
				add(reply.AuthorAccount)
			}
		}
	}
	return out, people
}

// listedActivity is whether an overview lists the activity: one of the
// actions it draws, and for a comment, the first of a thread; a reply is
// drawn in its thread.
func listedActivity(activity pullrequestactivityservice.Activity, threads map[int64]pullrequestactivityservice.Thread) bool {
	if !activityActions[activity.Action] {
		return false
	}
	switch activity.Action {
	case "COMMENTED":
		_, root := threads[commentIDOf(activity)]
		return root && !strings.EqualFold(stringOf(activity.Raw["commentAction"]), "DELETED")
	case "UPDATED":
		return len(namesOf(activity.Raw["addedReviewers"]))+len(namesOf(activity.Raw["removedReviewers"])) > 0
	}
	return true
}

// activityItemOf is an activity as its item, and the account of who did it.
func activityItemOf(activity pullrequestactivityservice.Activity) (viewActivityItem, pullrequestactivityservice.Account) {
	item := viewActivityItem{Action: activity.Action, Date: activity.CreatedDate}
	var account pullrequestactivityservice.Account
	if user, ok := activity.Raw["user"].(map[string]any); ok {
		item.User = strings.TrimSpace(stringOf(user["displayName"]))
		account = pullrequestactivityservice.Account{Username: strings.TrimSpace(stringOf(user["name"])), Slug: strings.TrimSpace(stringOf(user["slug"]))}
		if item.User == "" {
			item.User = account.Username
		}
		item.Username = account.Username
	}
	switch activity.Action {
	case "RESCOPED":
		item.Added = commitCount(activity.Raw["added"])
		item.Removed = commitCount(activity.Raw["removed"])
	case "MERGED":
		if commit, ok := activity.Raw["commit"].(map[string]any); ok {
			item.Commit = stringOf(commit["displayId"])
		}
	case "UPDATED":
		item.AddedReviewers = namesOf(activity.Raw["addedReviewers"])
		item.RemovedReviewers = namesOf(activity.Raw["removedReviewers"])
	}
	return item, account
}

// commentIDOf is the ID of the comment a COMMENTED activity is about.
func commentIDOf(activity pullrequestactivityservice.Activity) int64 {
	if activity.Comment != nil && activity.Comment.Id != nil {
		return *activity.Comment.Id
	}
	return 0
}

// commitCount is how many commits a push's added or removed part says it has.
func commitCount(value any) int {
	part, ok := value.(map[string]any)
	if !ok {
		return 0
	}
	if total, ok := part["total"].(float64); ok {
		return int(total)
	}
	commits, _ := part["commits"].([]any)
	return len(commits)
}

// namesOf is the display names of a list of users, as an update lists the
// reviewers it added or took away.
func namesOf(value any) []string {
	users, _ := value.([]any)
	names := []string{}
	for _, entry := range users {
		user, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		name := strings.TrimSpace(stringOf(user["displayName"]))
		if name == "" {
			name = strings.TrimSpace(stringOf(user["name"]))
		}
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

func stringOf(value any) string {
	text, _ := value.(string)
	return text
}

// contextFromActivity is the lines of the diff Bitbucket's overview shows a
// comment on a line among, as the activity carries them, with the line the
// comment is on marked. It is nil for a comment on the pull request or a
// file, and where the activity carries no diff.
func contextFromActivity(raw map[string]any) []viewContextLine {
	diff, _ := raw["diff"].(map[string]any)
	anchor, _ := raw["commentAnchor"].(map[string]any)
	if diff == nil || anchor == nil {
		return nil
	}
	line, _ := anchor["line"].(float64)
	lineType := strings.ToUpper(stringOf(anchor["lineType"]))
	fromSide := strings.EqualFold(stringOf(anchor["fileType"]), "FROM")

	var lines []viewContextLine
	hunks, _ := diff["hunks"].([]any)
	for _, entry := range hunks {
		hunk, _ := entry.(map[string]any)
		segments, _ := hunk["segments"].([]any)
		for _, entry := range segments {
			segment, _ := entry.(map[string]any)
			kind := strings.ToUpper(stringOf(segment["type"]))
			rows, _ := segment["lines"].([]any)
			for _, entry := range rows {
				row, _ := entry.(map[string]any)
				source, _ := row["source"].(float64)
				destination, _ := row["destination"].(float64)
				context := viewContextLine{}
				switch kind {
				case "ADDED":
					context.Type, context.New = "add", int(destination)
				case "REMOVED":
					context.Type, context.Old = "del", int(source)
				default:
					context.Type, context.Old, context.New = "context", int(source), int(destination)
				}
				context.Text, context.More = cutLine(stringOf(row["line"]))
				context.Anchor = anchoredHere(context, int(line), lineType, fromSide)
				lines = append(lines, context)
			}
		}
	}
	return lines
}

// anchoredHere is whether a comment anchored to line, of a line type and on a
// side, is on this line of the diff. Bitbucket numbers a removed line as the
// file was and an added one as it is, and a line on both sides as the side
// the comment was made on.
func anchoredHere(line viewContextLine, number int, lineType string, fromSide bool) bool {
	switch lineType {
	case "REMOVED":
		return line.Type == "del" && line.Old == number
	case "ADDED":
		return line.Type == "add" && line.New == number
	default:
		if line.Type != "context" {
			return false
		}
		if fromSide {
			return line.Old == number
		}
		return line.New == number
	}
}
