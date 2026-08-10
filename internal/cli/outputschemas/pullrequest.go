package outputschemas

import (
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/jsonoutput"
)

// pullRequestRepositoryRefSchema describes the repository reference emitted by
// the pull request commands.  Unlike repositoryRefSchema, the pull request
// service tags its fields, so the keys are snake_case.
func pullRequestRepositoryRefSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": true,
		"properties": map[string]any{
			"project_key": map[string]any{"type": "string"},
			"slug":        map[string]any{"type": "string"},
		},
	}
}

// nullableCount describes a count that is absent when it was never measured.
// The distinction matters: a missing count means "not checked", which is not
// the same claim as zero.
func nullableCount(description string) map[string]any {
	return map[string]any{
		"type":        "integer",
		"minimum":     0,
		"description": description,
	}
}

// reviewSummarySchema describes the outstanding-review-feedback block attached
// to pull request output.
func reviewSummarySchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": true,
		"description": "Outstanding review feedback. Count fields are omitted when they could not be measured, " +
			"which is not the same as zero — check counts_source before treating a missing count as 'nothing outstanding'.",
		"properties": map[string]any{
			"action_required": map[string]any{
				"type": "boolean",
				"description": "True when the pull request is waiting on its author: an unresolved thread, " +
					"an open task, or a reviewer who requested changes.",
			},
			"unresolved_threads": nullableCount("Comment threads still open, tasks included."),
			"open_tasks":         nullableCount("Subset of unresolved_threads that blocks the merge. Do not add to unresolved_threads."),
			"resolved_threads":   nullableCount("Comment threads marked resolved."),
			"resolved_tasks":     nullableCount("Subset of resolved_threads that Bitbucket tracks as tasks."),
			"pending_comments":   nullableCount("The author's own unpublished draft comments."),
			"comment_count": nullableCount("Raw comment counter reported by Bitbucket, replies included. " +
				"A weaker signal than unresolved_threads."),
			"needs_work": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Reviewers who requested changes.",
			},
			"approvals": map[string]any{"type": "integer", "minimum": 0},
			"reviewers": map[string]any{"type": "integer", "minimum": 0},
			"counts_source": map[string]any{
				"enum": []any{"activities", "blocker_comments", "properties", "none"},
				"description": "Where the counts came from. 'activities' counts unresolved threads exactly; " +
					"'blocker_comments' and 'properties' carry task counts only; 'none' means nothing was measured.",
			},
		},
		"required": []any{"action_required", "counts_source", "approvals", "reviewers"},
	}
}

// commentThreadSchema describes one entry of the summarised thread view.
func commentThreadSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": true,
		"properties": map[string]any{
			"id":   map[string]any{"type": "integer", "description": "Root comment id. Use it to reply to or resolve the thread."},
			"kind": map[string]any{"enum": []any{"comment", "task"}, "description": "Bitbucket models a task as a blocker comment."},
			"state": map[string]any{
				"enum":        []any{"OPEN", "RESOLVED", "PENDING", ""},
				"description": "Resolution state as reported by Bitbucket. May be empty on servers that omit it.",
			},
			"resolved":     map[string]any{"type": "boolean"},
			"author":       map[string]any{"type": "string"},
			"version":      map[string]any{"type": "integer", "description": "Comment version, required for optimistic locking on update."},
			"created_date": map[string]any{"type": "integer"},
			"updated_date": map[string]any{"type": "integer"},
			"anchor": map[string]any{
				"type":                 "object",
				"additionalProperties": true,
				"description":          "Location in the diff. Absent for pull-request-level comments.",
				"properties": map[string]any{
					"path":      map[string]any{"type": "string"},
					"line":      map[string]any{"type": "integer"},
					"line_type": map[string]any{"type": "string"},
					"orphaned":  map[string]any{"type": "boolean", "description": "True when the anchored line no longer exists in the diff."},
				},
			},
			"text": map[string]any{"type": "string"},
			"has_suggestion": map[string]any{
				"type":        "boolean",
				"description": "True when the comment carries a fenced suggestion block that `bb pr comment apply-suggestion` can apply.",
			},
			"reply_count": map[string]any{"type": "integer", "minimum": 0},
			"last_reply": map[string]any{
				"type":                 "object",
				"additionalProperties": true,
				"description":          "Most recent reply, useful for telling whether the thread was already addressed.",
				"properties": map[string]any{
					"id":     map[string]any{"type": "integer"},
					"author": map[string]any{"type": "string"},
					"date":   map[string]any{"type": "integer"},
					"text":   map[string]any{"type": "string"},
				},
			},
			"replies": map[string]any{
				"type":        "array",
				"description": "Full reply bodies. Only populated with --with-replies.",
				"items": map[string]any{
					"type":                 "object",
					"additionalProperties": true,
					"properties": map[string]any{
						"id":     map[string]any{"type": "integer"},
						"author": map[string]any{"type": "string"},
						"date":   map[string]any{"type": "integer"},
						"text":   map[string]any{"type": "string"},
					},
				},
			},
			"url": map[string]any{"type": "string", "description": "Browser link to the thread. Omitted when no host is configured."},
		},
		"required": []any{"id", "kind", "resolved", "reply_count"},
	}
}

// threadSummarySchema describes the aggregate counts returned alongside the
// threads.  It always describes the pull request, never the active filter.
func threadSummarySchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": true,
		"description": "Counts across everything the source returned, before --state and --tasks-only are applied. " +
			"What that spans depends on `source`: 'activities' is the whole pull request, 'comments' is the " +
			"requested --path only, and 'blocker_comments' is tasks only. open_tasks is a subset of unresolved, " +
			"so the two must not be added together.",
		"properties": map[string]any{
			"total_threads":     map[string]any{"type": "integer", "minimum": 0},
			"unresolved":        map[string]any{"type": "integer", "minimum": 0},
			"resolved":          map[string]any{"type": "integer", "minimum": 0},
			"pending":           map[string]any{"type": "integer", "minimum": 0},
			"open_tasks":        map[string]any{"type": "integer", "minimum": 0},
			"resolved_tasks":    map[string]any{"type": "integer", "minimum": 0},
			"unresolved_inline": map[string]any{"type": "integer", "minimum": 0, "description": "Subset of unresolved anchored to a file."},
		},
		"required": []any{"total_threads", "unresolved", "resolved", "pending", "open_tasks", "resolved_tasks"},
	}
}

func pullRequestSchemas() map[string]map[string]any {
	return map[string]map[string]any{
		"output.pr.get.schema.json": jsonoutput.EnvelopeSchemaFor(
			"output.pr.get.schema.json",
			"bb pr get output",
			"JSON output schema for `bb pr get --json`. Data contains the repository, the pull request, and a "+
				"review summary describing outstanding review feedback.",
			map[string]any{
				"type":                 "object",
				"additionalProperties": true,
				"properties": map[string]any{
					"repository": pullRequestRepositoryRefSchema(),
					"pull_request": map[string]any{
						"type":                 "object",
						"additionalProperties": true,
						"properties": map[string]any{
							"id":            map[string]any{"type": "integer"},
							"title":         map[string]any{"type": "string"},
							"state":         map[string]any{"type": "string"},
							"open":          map[string]any{"type": "boolean"},
							"closed":        map[string]any{"type": "boolean"},
							"draft":         map[string]any{"type": "boolean"},
							"version":       map[string]any{"type": "integer"},
							"author":        map[string]any{"type": "string"},
							"source_branch": map[string]any{"type": "string"},
							"target_branch": map[string]any{"type": "string"},
							"source_commit": map[string]any{"type": "string"},
							"comment_count": nullableCount("Reported by Bitbucket on listings; absent on the single pull request endpoint in 10.x."),
							"open_task_count": nullableCount("Reported by Bitbucket on listings; absent on the single pull request endpoint in 10.x. " +
								"Absent means unknown, not zero."),
							"resolved_task_count": nullableCount("Reported by Bitbucket on listings; absent on the single pull request endpoint in 10.x."),
						},
						"required": []any{"id", "title", "state"},
					},
					"review_summary": reviewSummarySchema(),
				},
				"required": []any{"repository", "pull_request", "review_summary"},
			},
		),
		"output.pr.comment.list.schema.json": jsonoutput.EnvelopeSchemaFor(
			"output.pr.comment.list.schema.json",
			"bb pr comment list output",
			"JSON output schema for `bb pr comment list --json`. By default data carries the summarised thread view "+
				"(comments and tasks together, unresolved first). With --full it carries the raw Bitbucket comment "+
				"objects under `comments` instead.",
			map[string]any{
				"type":                 "object",
				"additionalProperties": true,
				"properties": map[string]any{
					"repository":      pullRequestRepositoryRefSchema(),
					"pull_request_id": map[string]any{"type": "string"},
					"source": map[string]any{
						"enum":        []any{"activities", "comments", "blocker_comments"},
						"description": "Which Bitbucket endpoint the comments came from.",
					},
					"path":    map[string]any{"type": "string", "description": "Path filter in effect, empty when unset."},
					"state":   map[string]any{"enum": []any{"all", "open", "resolved", "pending"}},
					"summary": threadSummarySchema(),
					"threads": map[string]any{
						"type":        "array",
						"items":       commentThreadSchema(),
						"description": "Comment threads, unresolved first. Absent when --full is used.",
					},
					"comments": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "object", "additionalProperties": true},
						"description": "Raw Bitbucket comment objects. Only present with --full.",
					},
				},
				"required": []any{"repository", "pull_request_id", "source", "path"},
				"oneOf": []any{
					map[string]any{
						"title":    "summarised thread view (default)",
						"required": []any{"state", "summary", "threads"},
					},
					map[string]any{
						"title":    "raw comment payload (--full)",
						"required": []any{"comments"},
					},
				},
			},
		),
	}
}
