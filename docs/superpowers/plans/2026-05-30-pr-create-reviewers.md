# `--reviewers` Flag for `bb pr create` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an optional `--reviewers` flag to `bb pr create` that embeds reviewers directly in the PR creation API payload.

**Architecture:** Extend the `CreateInput` struct with a `Reviewers` field and include it in the JSON payload built by `buildCreatePayload`. The CLI flag uses `string` (comma-separated). The MCP tool gets a comma-separated `reviewers` parameter.

**Tech Stack:** Go, Cobra CLI, httptest for unit tests

---

### Task 1: Add `Reviewers` field to `CreateInput` and update `buildCreatePayload`

**Files:**
- Modify: `internal/services/pullrequest/service.go:67-72` (CreateInput struct)
- Modify: `internal/services/pullrequest/service.go:860-886` (buildCreatePayload function)
- Test: `internal/services/pullrequest/service_test.go`

- [ ] **Step 1: Write the failing test for `buildCreatePayload` with reviewers**

Add to `internal/services/pullrequest/service_test.go`:

```go
func TestBuildCreatePayloadWithReviewers(t *testing.T) {
	payload, err := buildCreatePayload(CreateInput{
		FromRef:   "feature/my-work",
		ToRef:     "main",
		Title:     "My PR",
		Reviewers: []string{"alice", "bob"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if payload["title"] != "My PR" {
		t.Fatalf("expected title 'My PR', got %v", payload["title"])
	}

	reviewers, ok := payload["reviewers"].([]map[string]any)
	if !ok {
		t.Fatal("expected reviewers to be []map[string]any")
	}
	if len(reviewers) != 2 {
		t.Fatalf("expected 2 reviewers, got %d", len(reviewers))
	}

	firstUser := reviewers[0]["user"].(map[string]any)
	if firstUser["name"] != "alice" {
		t.Fatalf("expected first reviewer 'alice', got %v", firstUser["name"])
	}
	if reviewers[0]["role"] != "REVIEWER" {
		t.Fatalf("expected role 'REVIEWER', got %v", reviewers[0]["role"])
	}

	secondUser := reviewers[1]["user"].(map[string]any)
	if secondUser["name"] != "bob" {
		t.Fatalf("expected second reviewer 'bob', got %v", secondUser["name"])
	}
}

func TestBuildCreatePayloadWithoutReviewers(t *testing.T) {
	payload, err := buildCreatePayload(CreateInput{
		FromRef: "feature/my-work",
		ToRef:   "main",
		Title:   "My PR",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, exists := payload["reviewers"]; exists {
		t.Fatal("expected no reviewers key when none provided")
	}
}

func TestBuildCreatePayloadWithBlankReviewers(t *testing.T) {
	payload, err := buildCreatePayload(CreateInput{
		FromRef:   "feature/my-work",
		ToRef:     "main",
		Title:     "My PR",
		Reviewers: []string{"alice", "", "  ", "bob"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	reviewers := payload["reviewers"].([]map[string]any)
	if len(reviewers) != 2 {
		t.Fatalf("expected 2 reviewers (blank entries skipped), got %d", len(reviewers))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/services/pullrequest/ -run "TestBuildCreatePayloadWith" -v`
Expected: FAIL — `CreateInput` has no `Reviewers` field, and `buildCreatePayload` doesn't produce a `reviewers` key.

- [ ] **Step 3: Add `Reviewers` field to `CreateInput` struct**

In `internal/services/pullrequest/service.go`, change the `CreateInput` struct (line 67):

```go
type CreateInput struct {
	FromRef     string   `json:"from_ref"`
	ToRef       string   `json:"to_ref"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Reviewers   []string `json:"reviewers,omitempty"`
}
```

- [ ] **Step 4: Update `buildCreatePayload` to include reviewers**

In `internal/services/pullrequest/service.go`, add after the description block in `buildCreatePayload` (after line 883):

```go
	if len(input.Reviewers) > 0 {
		reviewers := make([]map[string]any, 0, len(input.Reviewers))
		for _, name := range input.Reviewers {
			if n := strings.TrimSpace(name); n != "" {
				reviewers = append(reviewers, map[string]any{
					"user": map[string]any{"name": n},
					"role": "REVIEWER",
				})
			}
		}
		if len(reviewers) > 0 {
			payload["reviewers"] = reviewers
		}
	}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/services/pullrequest/ -run "TestBuildCreatePayloadWith" -v`
Expected: PASS

- [ ] **Step 6: Run the full service test suite**

Run: `go test ./internal/services/pullrequest/ -v`
Expected: All tests PASS

- [ ] **Step 7: Commit**

```bash
git add internal/services/pullrequest/service.go internal/services/pullrequest/service_test.go
git commit -m "feat(pr): add Reviewers field to CreateInput and include in create payload"
```

---

### Task 2: Add `--reviewers` flag to the `pr create` CLI command

**Files:**
- Modify: `internal/cli/insights_pr_admin_commands.go:528-630` (create command definition and handler)

- [ ] **Step 1: Add the `--reviewers` flag variable and registration**

In `internal/cli/insights_pr_admin_commands.go`, add a `var createReviewers string` declaration alongside the other create variables (after line 531):

```go
	var createReviewers string
```

Then after the existing flag registrations (after line 626), add:

```go
	createCmd.Flags().StringVar(&createReviewers, "reviewers", "", "Reviewer usernames to add (comma-separated, e.g. --reviewers alice,bob)")
```

- [ ] **Step 2: Pass `createReviewers` into `CreateInput.Reviewers`**

In the non-dry-run `service.Create` call (line 605-610), add the `Reviewers` field, parsing the comma-separated string via a helper:

```go
				created, err := service.Create(cmd.Context(), repo, pullrequestservice.CreateInput{
					FromRef:     createFromRef,
					ToRef:       createToRef,
					Title:       createTitle,
					Description: createDescription,
					Reviewers:   parseCLICommaList(createReviewers),
				})
```

Where `parseCLICommaList` splits a comma-separated string into `[]string`, trimming whitespace and skipping blanks.

- [ ] **Step 3: Include reviewers in the dry-run preview target**

In the dry-run preview `Target` map (line 580), add the reviewers key:

```go
							Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "from_ref": createFromRef, "to_ref": createToRef, "title": createTitle, "reviewers": parseCLICommaList(createReviewers)},
```

- [ ] **Step 4: Add the `parseCLICommaList` helper function**

Add at the end of `internal/cli/insights_pr_admin_commands.go`:

```go
func parseCLICommaList(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}
```

- [ ] **Step 5: Build and verify the CLI compiles**

Run: `go build ./cmd/bb/`
Expected: Build succeeds with no errors

- [ ] **Step 6: Commit**

```bash
git add internal/cli/insights_pr_admin_commands.go
git commit -m "feat(pr): add --reviewers flag to pr create command"
```

---

### Task 3: Add `reviewers` parameter to the MCP `create_pull_request` tool

**Files:**
- Modify: `internal/mcp/tools_pr.go:67-99` (specCreatePullRequest function)

- [ ] **Step 1: Add the `reviewers` parameter to the tool spec**

In `internal/mcp/tools_pr.go`, add a `reviewers` parameter to the tool definition (after the `description` parameter on line 75):

```go
			mcpgo.WithString("reviewers", mcpgo.Description("Comma-separated reviewer usernames to add (e.g. alice,bob)")),
```

- [ ] **Step 2: Parse and pass `reviewers` into `CreateInput.Reviewers`**

In the handler function (around line 84-91), parse the comma-separated reviewers and pass them in:

```go
					pr, err := svc.Create(ctx,
						pullrequestservice.RepositoryRef{ProjectKey: project, Slug: repo},
						pullrequestservice.CreateInput{
							FromRef:     fromRef,
							ToRef:       req.GetString("to_ref", ""),
							Title:       title,
							Description: req.GetString("description", ""),
							Reviewers:   parseCommaList(req.GetString("reviewers", "")),
						},
					)
```

- [ ] **Step 3: Add the `parseCommaList` helper function**

Add a helper function in `internal/mcp/tools_pr.go` (or in a shared helpers file if one exists). Check first if a similar helper already exists in the `internal/mcp/` package. If not, add at the end of `tools_pr.go`:

```go
func parseCommaList(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}
```

Ensure `strings` is in the import list for `tools_pr.go`.

- [ ] **Step 4: Build and verify the MCP package compiles**

Run: `go build ./internal/mcp/`
Expected: Build succeeds with no errors

- [ ] **Step 5: Commit**

```bash
git add internal/mcp/tools_pr.go
git commit -m "feat(mcp): add reviewers parameter to create_pull_request tool"
```

---

### Task 4: Add integration test for PR create with reviewers

**Files:**
- Modify: `internal/services/pullrequest/service_test.go`

- [ ] **Step 1: Write a test that creates a PR with reviewers via the service**

Add to `internal/services/pullrequest/service_test.go`:

```go
func TestCreatePullRequestWithReviewers(t *testing.T) {
	var receivedBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests" {
			receivedBody = readBody(t, r)
			_, _ = fmt.Fprint(w, `{"id":42,"title":"Feature","state":"OPEN","open":true,"closed":false,"fromRef":{"displayId":"feature/a"},"toRef":{"displayId":"main"},"reviewers":[{"role":"REVIEWER","status":"UNAPPROVED","approved":false,"user":{"name":"alice"}},{"role":"REVIEWER","status":"UNAPPROVED","approved":false,"user":{"name":"bob"}}]}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")

	cfg, err := config.LoadFromEnv()
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	service := NewService(httpclient.NewFromConfig(cfg))
	created, err := service.Create(context.Background(), RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, CreateInput{
		FromRef:   "feature/a",
		ToRef:     "main",
		Title:     "Feature",
		Reviewers: []string{"alice", "bob"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if created.ID != 42 {
		t.Fatalf("expected PR ID 42, got %d", created.ID)
	}

	// Verify the request body included reviewers
	if !strings.Contains(receivedBody, `"reviewers"`) {
		t.Fatal("expected request body to contain 'reviewers'")
	}
	if !strings.Contains(receivedBody, `"alice"`) || !strings.Contains(receivedBody, `"bob"`) {
		t.Fatal("expected request body to contain reviewer names")
	}
}
```

- [ ] **Step 2: Run the test**

Run: `go test ./internal/services/pullrequest/ -run TestCreatePullRequestWithReviewers -v`
Expected: PASS

- [ ] **Step 3: Run the full test suite**

Run: `go test ./internal/services/pullrequest/ -v`
Expected: All tests PASS

- [ ] **Step 4: Commit**

```bash
git add internal/services/pullrequest/service_test.go
git commit -m "test(pr): add integration test for create with reviewers"
```

---

### Task 5: Final verification

- [ ] **Step 1: Run the full unit test suite**

Run: `go test ./... -count=1`
Expected: All tests PASS

- [ ] **Step 2: Build the binary**

Run: `go build -o bb.exe ./cmd/bb/`
Expected: Build succeeds

- [ ] **Step 3: Verify the flag appears in help**

Run: `./bb.exe pr create --help`
Expected: Output includes `--reviewers` flag with description

- [ ] **Step 4: Final commit (if any fixups needed)**

If any adjustments were made, commit them. Otherwise skip.
