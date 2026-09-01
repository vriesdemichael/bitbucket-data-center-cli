package insightscmd

import (
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/result"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
)

// ReportDatum is one labelled figure on a code insight report.
//
// Value stays open: Bitbucket lets a reporter attach a number, a percentage, a
// duration or a link, and type says which. Constraining it would describe the
// reports this project happened to see rather than the ones a reporter may
// send.
type ReportDatum struct {
	Title string         `json:"title,omitempty" jsonschema:"Label for the figure."`
	Type  string         `json:"type,omitempty" jsonschema:"How to read value: NUMBER, PERCENTAGE, DURATION, TEXT or LINK."`
	Value map[string]any `json:"value,omitempty" jsonschema:"The figure itself. Its shape depends on type, which is why it is left open."`
}

// Report is one code insight report attached to a commit.
type Report struct {
	Key         string        `json:"key,omitempty" jsonschema:"Report key, unique per commit."`
	Title       string        `json:"title,omitempty" jsonschema:"Report title."`
	Details     string        `json:"details,omitempty" jsonschema:"Longer description."`
	Result      string        `json:"result,omitempty" jsonschema:"PASS or FAIL. Absent when the reporter did not state one."`
	Reporter    string        `json:"reporter,omitempty" jsonschema:"Which system produced the report."`
	Link        string        `json:"link,omitempty" jsonschema:"Link back to the reporting system."`
	LogoURL     string        `json:"logoUrl,omitempty" jsonschema:"Logo shown beside the report in the Bitbucket UI."`
	CreatedDate float64       `json:"createdDate,omitempty" jsonschema:"When the report was created, in milliseconds since the epoch."`
	Data        []ReportDatum `json:"data,omitempty" jsonschema:"Labelled figures attached to the report."`
}

// Annotation is one line-level finding within a report.
type Annotation struct {
	ExternalID string `json:"externalId,omitempty" jsonschema:"Reporter's own identifier, used to update or delete a single annotation."`
	ReportKey  string `json:"reportKey,omitempty" jsonschema:"Report the annotation belongs to."`
	Path       string `json:"path,omitempty" jsonschema:"File the finding is on."`
	Line       int32  `json:"line,omitempty" jsonschema:"Line within that file. Zero means the annotation is on the file rather than a line."`
	Message    string `json:"message,omitempty" jsonschema:"What the reporter found."`
	Severity   string `json:"severity,omitempty" jsonschema:"LOW, MEDIUM or HIGH."`
	Type       string `json:"type,omitempty" jsonschema:"VULNERABILITY, CODE_SMELL or BUG."`
	Link       string `json:"link,omitempty" jsonschema:"Link to the finding in the reporting system."`
}

// ReportChange is what `bb insights report delete` reports.
type ReportChange struct {
	result.Status
	Commit string `json:"commit" jsonschema:"Commit the report was attached to."`
	Key    string `json:"key" jsonschema:"Report key."`
}

// AnnotationsAdded is what `bb insights annotation add` reports.
type AnnotationsAdded struct {
	result.Status
	Count int `json:"count" jsonschema:"How many annotations were added."`
}

// AnnotationChange is what `bb insights annotation delete` reports.
type AnnotationChange struct {
	result.Status
	ExternalID string `json:"externalId,omitempty" jsonschema:"Annotation that was deleted, when a single one was named. Absent when every annotation on the report was deleted."`
}

var (
	reportResults      = []string{"PASS", "FAIL"}
	annotationSeverity = []string{"LOW", "MEDIUM", "HIGH"}
	annotationTypes    = []string{"VULNERABILITY", "CODE_SMELL", "BUG"}
)

func init() {
	reportEnums := map[string][]string{"result": reportResults}
	annotationEnums := map[string][]string{
		"severity": annotationSeverity,
		"type":     annotationTypes,
	}

	result.Declare("insights report set", result.For[Report](reportEnums))
	result.Declare("insights report get", result.For[Report](reportEnums))
	result.Declare("insights report list", result.List[Report](reportEnums))
	result.Declare("insights report delete", result.For[ReportChange](nil))

	result.Declare("insights annotation add", result.For[AnnotationsAdded](nil))
	result.Declare("insights annotation list", result.List[Annotation](annotationEnums))
	result.Declare("insights annotation set", result.For[Annotation](annotationEnums))
	result.Declare("insights annotation delete", result.For[AnnotationChange](nil))
}

// reportFrom converts one upstream report.
func reportFrom(upstream openapigenerated.RestInsightReport) Report {
	converted := Report{
		Key:      safeString(upstream.Key),
		Title:    safeString(upstream.Title),
		Details:  safeString(upstream.Details),
		Reporter: safeString(upstream.Reporter),
		Link:     safeString(upstream.Link),
		LogoURL:  safeString(upstream.LogoUrl),
	}
	if upstream.Result != nil {
		converted.Result = string(*upstream.Result)
	}
	if upstream.CreatedDate != nil {
		converted.CreatedDate = float64(*upstream.CreatedDate)
	}
	if upstream.Data != nil {
		converted.Data = make([]ReportDatum, 0, len(*upstream.Data))
		for _, datum := range *upstream.Data {
			entry := ReportDatum{
				Title: safeString(datum.Title),
				Type:  safeString(datum.Type),
			}
			if datum.Value != nil {
				entry.Value = *datum.Value
			}
			converted.Data = append(converted.Data, entry)
		}
	}

	return converted
}

// reportsFrom converts a list, preserving order and never returning nil.
func reportsFrom(upstream []openapigenerated.RestInsightReport) []Report {
	converted := make([]Report, 0, len(upstream))
	for _, one := range upstream {
		converted = append(converted, reportFrom(one))
	}

	return converted
}

// annotationFrom converts one upstream annotation.
func annotationFrom(upstream openapigenerated.RestInsightAnnotation) Annotation {
	converted := Annotation{
		ExternalID: safeString(upstream.ExternalId),
		ReportKey:  safeString(upstream.ReportKey),
		Path:       safeString(upstream.Path),
		Message:    safeString(upstream.Message),
		Severity:   safeString(upstream.Severity),
		Type:       safeString(upstream.Type),
		Link:       safeString(upstream.Link),
	}
	if upstream.Line != nil {
		converted.Line = *upstream.Line
	}

	return converted
}

// annotationsFrom converts a list, preserving order and never returning nil.
func annotationsFrom(upstream []openapigenerated.RestInsightAnnotation) []Annotation {
	converted := make([]Annotation, 0, len(upstream))
	for _, one := range upstream {
		converted = append(converted, annotationFrom(one))
	}

	return converted
}
