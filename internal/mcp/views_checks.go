package mcp

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/safederef"
	branchservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/branch"
	pullrequestservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequest"
	qualityservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/quality"
)

// The builds a pull request must pass before it merges: a card's checks board.
//
// They are Bitbucket's required-builds merge check, which vetoes a merge until
// every build its conditions require has passed. What the check does, as
// probed against its own veto on 10.4.3 and pinned by
// TestLiveRequiredChecksAgreeWithTheMergeVeto:
//
// A condition of the target repository applies to a pull request when it is
// required for pull requests, its refMatcher matches the target branch, and its
// exemptRefMatcher, if it has one, does not match the source branch. The keys
// of every condition that applies are required together, in one veto.
//
// A key is satisfied by a build of the pull request's latest source commit
// whose parent is the key, exactly, in state SUCCESSFUL. One such build is
// enough, whatever its siblings under the same parent did, and no other state
// counts. The build's own key plays no part: a build posted without a parent
// satisfies nothing, whatever its key, and neither does any build posted
// through the deprecated /rest/build-status endpoint, which drops the parent.
// A build posted for the source branch counts, and so does one posted for no
// branch; one posted for the target branch, or any other, does not.

// viewRequiredCheck is a build the pull request's target branch requires
// before it merges, and where it stands on the latest source commit.
type viewRequiredCheck struct {
	// Key is the required key, which a build names as its parent.
	Key string `json:"key"`
	// Name, State and URL are those of the build that speaks for the key: one
	// that passed, or else one still running, or else the latest of the rest.
	// State is empty when no build has reported under the key: it is missing.
	Name  string `json:"name,omitempty"`
	State string `json:"state,omitempty"`
	URL   string `json:"url,omitempty"`
	// Unparented says a missing key is the key of a build of the commit that
	// does not name it as its parent: one posted without a parent, or through
	// the deprecated endpoint, which drops it. The check does not count that
	// build, whatever its state, so a card that lists it passing beside the
	// missing key can say why.
	Unparented bool `json:"unparented,omitempty"`
}

const (
	// maxRequiredConditions is the most conditions a card reads for one
	// repository. One with more is a repository the card says nothing about.
	maxRequiredConditions = 500
	// maxRequiredBuilds is the most builds of one commit a card reads to decide
	// its requirements, requiredBuildsPage at a time: the most Bitbucket
	// answers with.
	maxRequiredBuilds  = 2000
	requiredBuildsPage = 500
)

// requiredChecksForView reads the required-build conditions of the target
// repository, keeps those that apply to the pull request, and says for each
// required key where the builds of the latest source commit stand, the ones
// that need attention first. Nothing required is known and empty. known is
// false when Bitbucket cannot answer, or bb cannot tell as Bitbucket would: a
// condition it cannot decide, a release without the listing it reads, a pull
// request that moved on while it read. The card then says nothing about
// requirements rather than something wrong.
func requiredChecksForView(ctx context.Context, c Clients, project, repo string, pr pullrequestservice.PullRequest) ([]viewRequiredCheck, bool) {
	if pr.SourceCommit == "" {
		return nil, false
	}
	listed, err := qualityservice.NewService(c.OpenAPI).ListRequiredBuildChecks(ctx,
		qualityservice.RepositoryRef{ProjectKey: project, Slug: repo}, maxRequiredConditions+1)
	if err != nil || len(listed) > maxRequiredConditions {
		return nil, false
	}
	conditions := requiredConditionsOf(listed)
	if len(conditions) == 0 {
		return nil, true
	}

	target, source, builds, err := requiredBuildsOf(ctx, c, project, repo, pr)
	if err != nil {
		return nil, false
	}
	keys, known := requiredKeys(conditions, target, source, readMatchFacts(ctx, c, project, repo, conditions))
	if !known {
		return nil, false
	}
	return requiredChecks(keys, builds), true
}

// requiredCondition is a required-build condition as a card matches it.
type requiredCondition struct {
	keys   []string
	target refMatcher
	exempt *refMatcher
}

// refMatcher is a condition's matcher: its type and the id it matches by.
type refMatcher struct {
	kind string
	id   string
}

// requiredConditionsOf keeps the conditions that apply to pull requests at all.
// One that states no scope applies to them: qualityservice reports the scope
// of a release that has none, which applies every condition to pull requests.
func requiredConditionsOf(listed []openapigenerated.RestRequiredBuildCondition) []requiredCondition {
	conditions := make([]requiredCondition, 0, len(listed))
	for _, condition := range listed {
		if condition.RequiredForPullRequest != nil && !*condition.RequiredForPullRequest {
			continue
		}
		var required requiredCondition
		if condition.BuildParentKeys != nil {
			required.keys = *condition.BuildParentKeys
		}
		// A matcher without a type is one bb cannot decide, and says so when it
		// matters (see matches).
		if matcher := condition.RefMatcher; matcher != nil {
			required.target.id = safederef.String(matcher.Id)
			if matcher.Type != nil {
				required.target.kind = string(matcher.Type.Id)
			}
		}
		if matcher := condition.ExemptRefMatcher; matcher != nil {
			exempt := refMatcher{id: safederef.String(matcher.Id)}
			if matcher.Type != nil {
				exempt.kind = string(matcher.Type.Id)
			}
			required.exempt = &exempt
		}
		conditions = append(conditions, required)
	}
	return conditions
}

// requiredRef is a ref as the matchers read it: its full id, such as
// refs/heads/release/1.0, and its display id, release/1.0.
type requiredRef struct {
	ID        string `json:"id"`
	DisplayID string `json:"displayId"`
}

// requiredBuild is a build as the required-builds check reads it.
type requiredBuild struct {
	Key         string `json:"key"`
	Parent      string `json:"parent"`
	Name        string `json:"name"`
	State       string `json:"state"`
	URL         string `json:"url"`
	UpdatedDate int64  `json:"updatedDate"`
}

// pullRequestBuildsPage is a page of the listing requiredBuildsOf reads, with
// the pull request it lists the builds of.
type pullRequestBuildsPage struct {
	Page struct {
		Values        []requiredBuild `json:"values"`
		IsLastPage    bool            `json:"isLastPage"`
		NextPageStart *int            `json:"nextPageStart"`
	} `json:"page"`
	PullRequest struct {
		FromRef struct {
			requiredRef
			LatestCommit string `json:"latestCommit"`
		} `json:"fromRef"`
		ToRef requiredRef `json:"toRef"`
	} `json:"pullRequest"`
}

// requiredBuildsOf reads what the required-builds check reads: the builds of
// the pull request's latest source commit that count for it, each with its
// parent, and the refs its conditions are matched against, the target's and
// then the source's.
//
// It is the listing Bitbucket's own pull request page reads, under /rest/ui as
// the code owners are (ADR-080): the repository's builds endpoint answers one
// key at a time, and the commit listing buildsForView reads drops the parent
// and lists the builds of other branches too. The listing answers for the
// commit it is asked about, so a pull request that has moved on since it was
// read fails the read: its builds would describe a commit the card does not
// show.
func requiredBuildsOf(ctx context.Context, c Clients, project, repo string, pr pullrequestservice.PullRequest) (requiredRef, requiredRef, []requiredBuild, error) {
	path := fmt.Sprintf("/rest/ui/latest/projects/%s/repos/%s/pull-requests/%d/builds",
		url.PathEscape(project), url.PathEscape(repo), pr.ID)
	var builds []requiredBuild
	for start := 0; ; {
		var page pullRequestBuildsPage
		query := map[string]string{
			"commitId": pr.SourceCommit,
			"start":    strconv.Itoa(start),
			"limit":    strconv.Itoa(requiredBuildsPage),
		}
		if err := c.HTTP.GetJSON(ctx, path, query, &page); err != nil {
			return requiredRef{}, requiredRef{}, nil, err
		}
		from, to := page.PullRequest.FromRef, page.PullRequest.ToRef
		if from.LatestCommit != pr.SourceCommit || from.ID == "" || to.ID == "" {
			return requiredRef{}, requiredRef{}, nil, errors.New("the listing is not of the pull request's latest commit")
		}
		builds = append(builds, page.Page.Values...)
		if page.Page.IsLastPage || page.Page.NextPageStart == nil {
			return to, from.requiredRef, builds, nil
		}
		if len(builds) >= maxRequiredBuilds || *page.Page.NextPageStart <= start {
			return requiredRef{}, requiredRef{}, nil, errors.New("the commit has more builds than a card reads")
		}
		start = *page.Page.NextPageStart
	}
}

// matchFacts are what the branching model and default branch matchers compare
// a ref with. Each is read only when a condition has such a matcher, and is nil
// when it could not be read: a matcher that needs it cannot say.
type matchFacts struct {
	model         *branchModel
	defaultBranch *string
}

// branchModel is the part of a repository's branching model its matchers read,
// as the completion of matcher ids reads it. A category the repository has
// switched off is not in it.
type branchModel struct {
	Development *requiredRef    `json:"development"`
	Production  *requiredRef    `json:"production"`
	Types       []modelCategory `json:"types"`
}

type modelCategory struct {
	ID     string `json:"id"`
	Prefix string `json:"prefix"`
}

// readMatchFacts reads what the conditions' branching model and default branch
// matchers compare with, and nothing a condition does not need.
func readMatchFacts(ctx context.Context, c Clients, project, repo string, conditions []requiredCondition) matchFacts {
	var facts matchFacts
	if usesMatcher(conditions, "MODEL_BRANCH", "MODEL_CATEGORY") {
		var model branchModel
		path := fmt.Sprintf("/rest/branch-utils/latest/projects/%s/repos/%s/branchmodel", url.PathEscape(project), url.PathEscape(repo))
		if err := c.HTTP.GetJSON(ctx, path, nil, &model); err == nil {
			facts.model = &model
		}
	}
	if usesMatcher(conditions, "DEFAULT_BRANCH") {
		ref, err := branchservice.NewService(c.OpenAPI).GetDefault(ctx, branchservice.RepositoryRef{ProjectKey: project, Slug: repo})
		if err == nil {
			id := safederef.String(ref.Id)
			facts.defaultBranch = &id
		}
	}
	return facts
}

// usesMatcher reports whether any condition matches by one of the kinds.
func usesMatcher(conditions []requiredCondition, kinds ...string) bool {
	for _, condition := range conditions {
		for _, kind := range kinds {
			if condition.target.kind == kind || (condition.exempt != nil && condition.exempt.kind == kind) {
				return true
			}
		}
	}
	return false
}

// requiredKeys are the keys the conditions that apply to a pull request from
// source into target require, each once. known is false when a condition
// cannot be decided.
func requiredKeys(conditions []requiredCondition, target, source requiredRef, facts matchFacts) ([]string, bool) {
	var keys []string
	seen := map[string]bool{}
	for _, condition := range conditions {
		applies, certain := condition.appliesTo(target, source, facts)
		if !certain {
			return nil, false
		}
		if !applies {
			continue
		}
		for _, key := range condition.keys {
			if !seen[key] {
				seen[key] = true
				keys = append(keys, key)
			}
		}
	}
	return keys, true
}

// appliesTo reports whether the condition applies to a pull request from
// source into target, and whether bb can be certain of it. The exemption
// matters only when the matcher matches, so a condition for another branch is
// decided whatever its exemption is.
func (condition requiredCondition) appliesTo(target, source requiredRef, facts matchFacts) (applies, certain bool) {
	matches, certain := condition.target.matches(target, facts)
	if !certain || !matches {
		return false, certain
	}
	if condition.exempt == nil {
		return true, true
	}
	exempted, certain := condition.exempt.matches(source, facts)
	return certain && !exempted, certain
}

// matches reports whether the matcher matches a ref, as Bitbucket's matcher of
// its kind does, and whether bb can be certain of it:
//
//   - ANY_REF matches every ref.
//   - BRANCH names a branch: its id is the ref's full id, as given or qualified
//     with refs/heads/, so master, heads/master and refs/heads/master all name
//     refs/heads/master.
//   - PATTERN is a pattern for the ref's full id (see patternMatches).
//   - MODEL_BRANCH names the branching model's development or production
//     branch, and compares that branch's full id with the ref's, case and all.
//     An unconfigured branch, or any other name, matches nothing.
//   - MODEL_CATEGORY names a category of the branching model, and matches a
//     branch whose display id starts with the category's prefix, case and all.
//     A category the model has switched off matches nothing.
//   - DEFAULT_BRANCH matches the repository's default branch. Bitbucket
//     accepts it though its REST documentation lists only the other five.
//
// BRANCH and PATTERN ignore case, as Bitbucket does unless an administrator
// sets plugin.bitbucket-ref-restriction.case.insensitive=false, which bb cannot
// see: an answer that case decides is not certain. Nor is one from a kind bb
// does not know, or one that needs a fact it could not read.
func (matcher refMatcher) matches(ref requiredRef, facts matchFacts) (matches, certain bool) {
	switch matcher.kind {
	case "ANY_REF":
		return true, true
	case "BRANCH":
		folded := branchMatches(matcher.id, ref.ID, true)
		return folded, folded == branchMatches(matcher.id, ref.ID, false)
	case "PATTERN":
		// Braces are Spring's template variables, which may carry a regular
		// expression of Java's; bb matches no pattern that has them.
		if strings.ContainsAny(matcher.id, "{}") {
			return false, false
		}
		folded := patternMatches(strings.ToLower(matcher.id), strings.ToLower(ref.ID))
		return folded, folded == patternMatches(matcher.id, ref.ID)
	case "MODEL_BRANCH":
		if facts.model == nil {
			return false, false
		}
		var branch *requiredRef
		switch matcher.id {
		case "development":
			branch = facts.model.Development
		case "production":
			branch = facts.model.Production
		}
		return branch != nil && branch.ID == ref.ID, true
	case "MODEL_CATEGORY":
		if facts.model == nil {
			return false, false
		}
		for _, category := range facts.model.Types {
			if category.ID == matcher.id {
				return strings.HasPrefix(ref.DisplayID, category.Prefix), true
			}
		}
		return false, true
	case "DEFAULT_BRANCH":
		if facts.defaultBranch == nil {
			return false, false
		}
		return *facts.defaultBranch == ref.ID, true
	}
	return false, false
}

// branchMatches is Bitbucket's BRANCH matcher: the id names the ref as given,
// or qualified with refs/heads/ as Bitbucket qualifies a branch name.
func branchMatches(id, ref string, ignoreCase bool) bool {
	same := func(a, b string) bool {
		if ignoreCase {
			return strings.EqualFold(a, b)
		}
		return a == b
	}
	return same(id, ref) || same(qualifiedBranch(id), ref)
}

// qualifiedBranch is a branch name as a full ref id. A name that already
// starts with refs/heads/, or with heads/, is completed rather than prefixed.
func qualifiedBranch(name string) string {
	if strings.IndexByte(name, '/') > 0 {
		switch {
		case strings.HasPrefix(name, "refs/heads/"):
			return name
		case strings.HasPrefix(name, "heads/"):
			return "refs/" + name
		}
	}
	return "refs/heads/" + name
}

// patternMatches is Bitbucket's PATTERN matcher, minding case. A pattern that
// starts with neither ** nor refs/ matches at any depth (**/ is put in front of
// it), and one that ends in a separator matches everything below it (** is put
// after it); Spring's AntPathMatcher then matches it against the ref's full id.
// So release/* matches refs/heads/release/1.0 and not refs/heads/release/1/2,
// release/ and release/** match both, and heads/master matches
// refs/heads/master.
func patternMatches(pattern, ref string) bool {
	if !strings.HasPrefix(pattern, "**") && !strings.HasPrefix(pattern, "refs/") {
		pattern = "**/" + pattern
	}
	if strings.HasSuffix(pattern, "/") || strings.HasSuffix(pattern, `\`) {
		pattern += "**"
	}
	return antMatch(pattern, ref)
}

// antMatch matches a path against a pattern as Spring's AntPathMatcher does
// with its defaults, which is what Bitbucket's PATTERN matcher calls: the
// segments are what lies between separators, empty ones dropped; a segment **
// stands for any number of segments; within one, * is any run of characters
// and ? exactly one. It follows Spring's doMatch step for step, so that an edge
// of Spring's is an edge here too.
func antMatch(pattern, path string) bool {
	if strings.HasPrefix(path, "/") != strings.HasPrefix(pattern, "/") {
		return false
	}
	patternSegments, pathSegments := antSegments(pattern), antSegments(path)
	patternStart, patternEnd := 0, len(patternSegments)-1
	pathStart, pathEnd := 0, len(pathSegments)-1

	// The segments before the first **.
	for patternStart <= patternEnd && pathStart <= pathEnd {
		if patternSegments[patternStart] == "**" {
			break
		}
		if !segmentMatches(patternSegments[patternStart], pathSegments[pathStart]) {
			return false
		}
		patternStart++
		pathStart++
	}
	if pathStart > pathEnd {
		// The path is used up, so what is left of the pattern must match
		// nothing.
		if patternStart > patternEnd {
			return strings.HasSuffix(pattern, "/") == strings.HasSuffix(path, "/")
		}
		if patternStart == patternEnd && patternSegments[patternStart] == "*" && strings.HasSuffix(path, "/") {
			return true
		}
		return onlyAnyDepth(patternSegments[patternStart : patternEnd+1])
	}
	if patternStart > patternEnd {
		return false
	}

	// The segments after the last **.
	for patternStart <= patternEnd && pathStart <= pathEnd {
		if patternSegments[patternEnd] == "**" {
			break
		}
		if !segmentMatches(patternSegments[patternEnd], pathSegments[pathEnd]) {
			return false
		}
		if patternEnd == len(patternSegments)-1 && strings.HasSuffix(pattern, "/") != strings.HasSuffix(path, "/") {
			return false
		}
		patternEnd--
		pathEnd--
	}
	if pathStart > pathEnd {
		return onlyAnyDepth(patternSegments[patternStart : patternEnd+1])
	}

	// Between two **, each run of segments where it first fits.
	for patternStart != patternEnd && pathStart <= pathEnd {
		next := patternStart + 1
		for patternSegments[next] != "**" {
			next++
		}
		if next == patternStart+1 {
			patternStart++
			continue
		}
		run := patternSegments[patternStart+1 : next]
		found := -1
	search:
		for at := pathStart; at+len(run)-1 <= pathEnd; at++ {
			for offset, segment := range run {
				if !segmentMatches(segment, pathSegments[at+offset]) {
					continue search
				}
			}
			found = at
			break
		}
		if found < 0 {
			return false
		}
		patternStart = next
		pathStart = found + len(run)
	}
	return onlyAnyDepth(patternSegments[patternStart : patternEnd+1])
}

// antSegments splits a path at its separators, dropping empty segments.
func antSegments(path string) []string {
	var segments []string
	for _, segment := range strings.Split(path, "/") {
		if segment != "" {
			segments = append(segments, segment)
		}
	}
	return segments
}

// onlyAnyDepth reports whether every segment is **, which matches no segment
// as well as many.
func onlyAnyDepth(segments []string) bool {
	for _, segment := range segments {
		if segment != "**" {
			return false
		}
	}
	return true
}

// segmentMatches matches one segment against one segment of a pattern: * is
// any run of characters, ? exactly one, and anything else itself.
func segmentMatches(pattern, segment string) bool {
	wanted, got := []rune(pattern), []rune(segment)
	at, from := 0, 0
	star, starFrom := -1, 0
	for from < len(got) {
		switch {
		case at < len(wanted) && wanted[at] == '*':
			star, starFrom = at, from
			at++
		case at < len(wanted) && (wanted[at] == '?' || wanted[at] == got[from]):
			at++
			from++
		case star >= 0:
			// Let the last * take one more character, and try again from
			// there.
			starFrom++
			at, from = star+1, starFrom
		default:
			return false
		}
	}
	for at < len(wanted) && wanted[at] == '*' {
		at++
	}
	return at == len(wanted)
}

// requiredChecks says for each required key where the builds under it stand,
// the ones that need attention first: missing, failed, running, canceled,
// unknown, then passed. Within each, the keys go as Bitbucket's veto names
// them, by key regardless of case.
func requiredChecks(keys []string, builds []requiredBuild) []viewRequiredCheck {
	if len(keys) == 0 {
		return nil
	}
	byParent := map[string][]requiredBuild{}
	ownKeys := map[string]bool{}
	for _, build := range builds {
		if build.Parent != "" {
			byParent[build.Parent] = append(byParent[build.Parent], build)
		}
		if build.Parent != build.Key {
			ownKeys[build.Key] = true
		}
	}

	checks := make([]viewRequiredCheck, 0, len(keys))
	for _, key := range keys {
		check := viewRequiredCheck{Key: key}
		if build, ok := speakingBuild(byParent[key]); ok {
			check.Name, check.State, check.URL = build.Name, build.State, build.URL
		} else {
			check.Unparented = ownKeys[key]
		}
		checks = append(checks, check)
	}
	sort.SliceStable(checks, func(i, j int) bool {
		if a, b := attentionRank(checks[i].State), attentionRank(checks[j].State); a != b {
			return a < b
		}
		if a, b := strings.ToLower(checks[i].Key), strings.ToLower(checks[j].Key); a != b {
			return a < b
		}
		return checks[i].Key < checks[j].Key
	})
	return checks
}

// speakingBuild is the build that says where a required key stands: one that
// passed, which is all the check asks for, or else one still running, which
// may yet pass, or else the latest of the rest.
func speakingBuild(builds []requiredBuild) (requiredBuild, bool) {
	var best requiredBuild
	found := false
	for _, build := range builds {
		rank, bestRank := standingRank(build.State), standingRank(best.State)
		if !found || rank < bestRank || (rank == bestRank && build.UpdatedDate > best.UpdatedDate) {
			best, found = build, true
		}
	}
	return best, found
}

// standingRank orders the states of the builds under one key by how near they
// bring the key to satisfied.
func standingRank(state string) int {
	switch strings.ToUpper(state) {
	case "SUCCESSFUL":
		return 0
	case "INPROGRESS":
		return 1
	case "FAILED":
		return 2
	case "CANCELLED":
		return 3
	default:
		return 4
	}
}

// attentionRank orders required checks as the card lists them, what needs the
// person first, as buildCountsText orders the build counts.
func attentionRank(state string) int {
	switch strings.ToUpper(state) {
	case "":
		return 0
	case "FAILED":
		return 1
	case "INPROGRESS":
		return 2
	case "CANCELLED":
		return 3
	case "SUCCESSFUL":
		return 5
	default:
		return 4
	}
}
