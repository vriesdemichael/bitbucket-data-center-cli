package diff

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

type OutputKind string

const (
	OutputKindRaw      OutputKind = "raw"
	OutputKindPatch    OutputKind = "patch"
	OutputKindStat     OutputKind = "stat"
	OutputKindNameOnly OutputKind = "name_only"
)

// AllResults asks CompareChanges for every change rather than a page of them.
//
// `bb repo compare` has no --limit: it passed 1000 as the page size and read to
// the last page, so the answer was always everything. The number was a page
// size then and is a cap now, and the cap has to stay out of the way of the
// comparison it is describing.
const AllResults = 1_000_000

type RepositoryRef struct {
	ProjectKey string
	Slug       string
}

type DiffRefsInput struct {
	Repository RepositoryRef
	From       string
	To         string
	Path       string
	Output     OutputKind
}

type DiffPRInput struct {
	Repository    RepositoryRef
	PullRequestID string
	Output        OutputKind
}

type DiffCommitInput struct {
	Repository RepositoryRef
	CommitID   string
	Path       string
}

type Result struct {
	Patch string       `json:"patch,omitempty"`
	Stats StatsSummary `json:"stats,omitempty"`
	Names []string     `json:"names,omitempty"`
}

// StatsSummary is the summary object a diff-stats-summary endpoint returns.
//
// A map rather than a struct, deliberately. Of the three operations the spec
// types one as RestDiff -- which shares no field with what Bitbucket sends, so
// the generated wrapper produced an empty struct, the payload marshalled to {},
// and omitempty removed the key (#526) -- and types the other two as a bare
// interface{}, which passed the whole object through untyped. Decoding into a
// fixed struct would have fixed the first and narrowed the other two to
// whichever fields were named here.
//
// So: publish what the server sent, and refuse to call an unreadable body an
// empty one.
type StatsSummary = map[string]any

// statsSummaryCounts are the fields observed on a running Bitbucket. At least
// one has to be present for a body to count as a summary rather than as some
// other shape that happened to parse.
var statsSummaryCounts = []string{"filesChanged", "totalInsertions", "totalDeletions"}

// decodeStatsSummary reads the body Bitbucket sent, not the one the spec
// promised.
//
// An undecodable body is an error rather than an empty summary -- the rule
// ADR-077 records after the same shape of defect hid in the comment endpoints.
func decodeStatsSummary(body []byte) (StatsSummary, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil, apperrors.New(apperrors.KindInternal, "bitbucket returned an empty diff stats summary", nil)
	}

	var summary StatsSummary
	if err := json.Unmarshal(trimmed, &summary); err != nil {
		return nil, apperrors.New(apperrors.KindInternal, "could not read the diff stats summary bitbucket returned", err)
	}
	if summary == nil {
		return nil, apperrors.New(apperrors.KindInternal, "bitbucket returned a null diff stats summary", nil)
	}

	// A body that decoded but holds none of the counts is a shape change, not a
	// diff with nothing in it. A real empty diff reports zeros.
	for _, field := range statsSummaryCounts {
		if _, ok := summary[field]; ok {
			return summary, nil
		}
	}

	return nil, apperrors.New(apperrors.KindInternal, "bitbucket returned a diff stats summary with no counts", nil)
}

type Service struct {
	client *openapigenerated.ClientWithResponses
}

func NewService(client *openapigenerated.ClientWithResponses) *Service {
	return &Service{client: client}
}

func (service *Service) DiffRefs(ctx context.Context, input DiffRefsInput) (Result, error) {
	if err := validateRepoRef(input.Repository); err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(input.From) == "" || strings.TrimSpace(input.To) == "" {
		return Result{}, apperrors.New(apperrors.KindValidation, "from and to refs are required", nil)
	}
	if input.Output == "" {
		input.Output = OutputKindRaw
	}

	from := strings.TrimSpace(input.From)
	to := strings.TrimSpace(input.To)

	switch input.Output {
	case OutputKindPatch:
		if strings.TrimSpace(input.Path) != "" {
			return Result{}, apperrors.New(apperrors.KindValidation, "--path is not supported with patch output for ref diffs", nil)
		}
		response, err := service.client.StreamPatchWithResponse(ctx, input.Repository.ProjectKey, input.Repository.Slug, &openapigenerated.StreamPatchParams{
			Since: &from,
			Until: &to,
		})
		if err != nil {
			return Result{}, apperrors.New(apperrors.KindTransient, "failed to stream patch", err)
		}
		if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
			return Result{}, err
		}
		return Result{Patch: string(response.Body)}, nil
	case OutputKindStat:
		response, err := service.client.GetDiffStatsSummary1WithResponse(
			ctx,
			input.Repository.ProjectKey,
			input.Repository.Slug,
			pathOrDot(input.Path),
			&openapigenerated.GetDiffStatsSummary1Params{From: &from, To: &to},
		)
		if err != nil {
			return Result{}, apperrors.New(apperrors.KindTransient, "failed to get diff stats", err)
		}
		if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
			return Result{}, err
		}
		summary, err := decodeStatsSummary(response.Body)
		if err != nil {
			return Result{}, err
		}

		return Result{Stats: summary}, nil
	case OutputKindRaw, OutputKindNameOnly:
		body, err := service.streamRefRawDiff(ctx, input.Repository, input.Path, from, to)
		if err != nil {
			return Result{}, err
		}
		if input.Output == OutputKindNameOnly {
			return Result{Names: extractNamesFromUnifiedDiff(body)}, nil
		}
		return Result{Patch: body}, nil
	default:
		return Result{}, apperrors.New(apperrors.KindValidation, "unsupported diff output mode", nil)
	}
}

func (service *Service) DiffPR(ctx context.Context, input DiffPRInput) (Result, error) {
	if err := validateRepoRef(input.Repository); err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(input.PullRequestID) == "" {
		return Result{}, apperrors.New(apperrors.KindValidation, "pull request id is required", nil)
	}
	if input.Output == "" {
		input.Output = OutputKindRaw
	}

	prID := strings.TrimSpace(input.PullRequestID)

	switch input.Output {
	case OutputKindPatch:
		response, err := service.client.StreamPatch1WithResponse(ctx, input.Repository.ProjectKey, input.Repository.Slug, prID)
		if err != nil {
			return Result{}, apperrors.New(apperrors.KindTransient, "failed to stream pull request patch", err)
		}
		if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
			return Result{}, err
		}
		return Result{Patch: string(response.Body)}, nil
	case OutputKindStat:
		response, err := service.client.GetDiffStatsSummary2WithResponse(ctx, input.Repository.ProjectKey, input.Repository.Slug, prID, ".", nil)
		if err != nil {
			return Result{}, apperrors.New(apperrors.KindTransient, "failed to get pull request diff stats", err)
		}
		if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
			return Result{}, err
		}
		summary, err := decodeStatsSummary(response.Body)
		if err != nil {
			return Result{}, err
		}

		return Result{Stats: summary}, nil
	case OutputKindRaw, OutputKindNameOnly:
		response, err := service.client.StreamRawDiff2WithResponse(ctx, input.Repository.ProjectKey, input.Repository.Slug, prID, nil)
		if err != nil {
			return Result{}, apperrors.New(apperrors.KindTransient, "failed to stream pull request diff", err)
		}
		if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
			return Result{}, err
		}
		diffText := string(response.Body)
		if input.Output == OutputKindNameOnly {
			return Result{Names: extractNamesFromUnifiedDiff(diffText)}, nil
		}
		return Result{Patch: diffText}, nil
	default:
		return Result{}, apperrors.New(apperrors.KindValidation, "unsupported diff output mode", nil)
	}
}

func (service *Service) DiffCommit(ctx context.Context, input DiffCommitInput) (Result, error) {
	if err := validateRepoRef(input.Repository); err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(input.CommitID) == "" {
		return Result{}, apperrors.New(apperrors.KindValidation, "commit id is required", nil)
	}

	commitID := strings.TrimSpace(input.CommitID)
	body, err := service.streamRefRawDiff(ctx, input.Repository, input.Path, "", commitID)
	if err != nil {
		return Result{}, err
	}

	return Result{Patch: body}, nil
}

func (service *Service) CompareChanges(ctx context.Context, repo RepositoryRef, from, to string, maxResults int) ([]openapigenerated.RestChange, error) {
	if err := validateRepoRef(repo); err != nil {
		return nil, err
	}

	if maxResults <= 0 {
		maxResults = 25
	}

	var fromParam *string
	if from != "" {
		f := from
		fromParam = &f
	}
	var toParam *string
	if to != "" {
		t := to
		toParam = &t
	}

	return openapi.PageThrough(ctx, 0, maxResults,
		func(ctx context.Context, start, limit int) (openapi.Page[openapigenerated.RestChange], error) {
			startValue, limitValue := float32(start), float32(limit)
			params := &openapigenerated.StreamChangesParams{
				Start: &startValue,
				Limit: &limitValue,
				From:  fromParam,
				To:    toParam,
			}

			response, err := service.client.StreamChangesWithResponse(ctx, repo.ProjectKey, repo.Slug, params)
			if err != nil {
				return openapi.Page[openapigenerated.RestChange]{}, apperrors.New(apperrors.KindTransient, "failed to stream compare changes", err)
			}
			if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
				return openapi.Page[openapigenerated.RestChange]{}, err
			}

			page := response.ApplicationjsonCharsetUTF8200
			if page == nil || page.Values == nil {
				return openapi.Page[openapigenerated.RestChange]{}, nil
			}

			return openapi.Page[openapigenerated.RestChange]{
				Values:        *page.Values,
				IsLastPage:    page.IsLastPage,
				NextPageStart: page.NextPageStart,
			}, nil
		})
}

func (service *Service) CompareDiff(ctx context.Context, repo RepositoryRef, from, to string) (*openapigenerated.RestDiff, error) {
	if err := validateRepoRef(repo); err != nil {
		return nil, err
	}

	var fromParam *string
	if from != "" {
		f := from
		fromParam = &f
	}
	var toParam *string
	if to != "" {
		t := to
		toParam = &t
	}

	params := &openapigenerated.StreamDiff1Params{
		From: fromParam,
		To:   toParam,
	}

	resp, err := service.client.StreamDiff1WithResponse(ctx, repo.ProjectKey, repo.Slug, "", params)
	if err != nil {
		return nil, apperrors.New(apperrors.KindTransient, "failed to stream compare diff", err)
	}

	if err := openapi.MapStatusError(resp.StatusCode(), resp.Body); err != nil {
		return nil, err
	}

	if resp.ApplicationjsonCharsetUTF8200 == nil {
		return nil, apperrors.New(apperrors.KindPermanent, "empty diff response from server", nil)
	}

	return resp.ApplicationjsonCharsetUTF8200, nil
}

func FormatRestDiff(diff *openapigenerated.RestDiff) string {
	if diff == nil {
		return ""
	}

	var sb strings.Builder

	srcPath := ""
	if diff.Source != nil && diff.Source.Components != nil {
		srcPath = strings.Join(*diff.Source.Components, "/")
	}
	dstPath := ""
	if diff.Destination != nil && diff.Destination.Components != nil {
		dstPath = strings.Join(*diff.Destination.Components, "/")
	}

	if srcPath != "" {
		sb.WriteString("--- a/" + srcPath + "\n")
	} else {
		sb.WriteString("--- /dev/null\n")
	}
	if dstPath != "" {
		sb.WriteString("+++ b/" + dstPath + "\n")
	} else {
		sb.WriteString("+++ /dev/null\n")
	}

	if diff.Binary != nil && *diff.Binary {
		sb.WriteString("Binary files differ\n")
		return sb.String()
	}

	if diff.Hunks == nil {
		return sb.String()
	}

	for _, hunk := range *diff.Hunks {
		srcLine := int32(0)
		if hunk.SourceLine != nil {
			srcLine = *hunk.SourceLine
		}
		srcSpan := int32(0)
		if hunk.SourceSpan != nil {
			srcSpan = *hunk.SourceSpan
		}
		dstLine := int32(0)
		if hunk.DestinationLine != nil {
			dstLine = *hunk.DestinationLine
		}
		dstSpan := int32(0)
		if hunk.DestinationSpan != nil {
			dstSpan = *hunk.DestinationSpan
		}

		ctxText := ""
		if hunk.Context != nil {
			ctxText = *hunk.Context
		}

		sb.WriteString(fmt.Sprintf("@@ -%d,%d +%d,%d @@ %s\n", srcLine, srcSpan, dstLine, dstSpan, ctxText))

		if hunk.Segments == nil {
			continue
		}

		for _, segment := range *hunk.Segments {
			prefix := " "
			if segment.Type != nil {
				switch *segment.Type {
				case openapigenerated.RestDiffSegmentTypeADDED:
					prefix = "+"
				case openapigenerated.RestDiffSegmentTypeREMOVED:
					prefix = "-"
				case openapigenerated.RestDiffSegmentTypeCONTEXT:
					prefix = " "
				}
			}

			if segment.Lines == nil {
				continue
			}

			for _, line := range *segment.Lines {
				lineText := ""
				if line.Line != nil {
					lineText = *line.Line
				}
				sb.WriteString(prefix + lineText + "\n")
			}
		}
	}

	return sb.String()
}

func (service *Service) streamRefRawDiff(ctx context.Context, repo RepositoryRef, path, from, to string) (string, error) {
	params := &openapigenerated.StreamRawDiffParams{Until: &to}
	if from != "" {
		params.Since = &from
	}

	if strings.TrimSpace(path) == "" {
		response, err := service.client.StreamPatchWithResponse(ctx, repo.ProjectKey, repo.Slug, &openapigenerated.StreamPatchParams{Since: params.Since, Until: params.Until})
		if err != nil {
			return "", apperrors.New(apperrors.KindTransient, "failed to stream raw diff", err)
		}
		if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
			return "", err
		}
		return string(response.Body), nil
	}

	response, err := service.client.StreamRawDiff1WithResponse(ctx, repo.ProjectKey, repo.Slug, strings.TrimSpace(path), &openapigenerated.StreamRawDiff1Params{Since: params.Since, Until: params.Until})
	if err != nil {
		return "", apperrors.New(apperrors.KindTransient, "failed to stream raw diff for file path", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return "", err
	}

	return string(response.Body), nil
}

func validateRepoRef(repo RepositoryRef) error {
	if strings.TrimSpace(repo.ProjectKey) == "" || strings.TrimSpace(repo.Slug) == "" {
		return apperrors.New(apperrors.KindValidation, "repository must be specified as project/repo", nil)
	}

	return nil
}

func pathOrDot(value string) string {
	if strings.TrimSpace(value) == "" {
		return "."
	}

	return strings.TrimSpace(value)
}

func extractNamesFromUnifiedDiff(diffText string) []string {
	lines := strings.Split(diffText, "\n")
	seen := map[string]struct{}{}
	names := make([]string, 0)

	for _, line := range lines {
		if !strings.HasPrefix(line, "diff --git ") {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) < 4 {
			continue
		}

		candidate := strings.TrimPrefix(parts[3], "b/")
		if candidate == "dev/null" || candidate == "/dev/null" || candidate == "" {
			candidate = strings.TrimPrefix(parts[2], "a/")
		}
		if candidate == "" || candidate == "dev/null" || candidate == "/dev/null" {
			continue
		}

		if _, exists := seen[candidate]; exists {
			continue
		}
		seen[candidate] = struct{}{}
		names = append(names, candidate)
	}

	return names
}
