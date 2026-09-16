package outcome_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// transientWithCause matches a failure wrapped as transient with a cause: the
// shape that overwrote whatever the transport had classified the cause as.
//
// Across lines as well as on one. Written to stop at a newline, it missed the
// gofmt-wrapped form every long call takes, which is where three of these
// survived the sweep that was supposed to remove them.
var transientWithCause = regexp.MustCompile(`(?s)apperrors\.New\(\s*apperrors\.KindTransient,.*?,\s*([A-Za-z_][A-Za-z0-9_.]*),?\s*\)`)

// wrappedThroughTransport counts the sites doing it the way that keeps the
// classification, for the sanity check below.
var wrappedThroughTransport = regexp.MustCompile(`apperrors\.Transport\(`)

// TestNoFailureIsWrappedAsTransient is #574's structure.
//
// The transport decides what a failed exchange means, and the kind a caller
// sees is the outermost one. 209 sites wrapped the transport's error with
// New(KindTransient, message, err), so a rejected certificate and a POST that
// may already have been applied both reached the caller as "retry later". They
// wrap with apperrors.Transport now, which keeps the kind; this keeps a new
// site from reintroducing the old shape.
//
// A transient error with no cause is the code deciding for itself, and stays
// allowed.
func TestNoFailureIsWrappedAsTransient(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..", "..")

	var offenders []string
	var transportSites int

	for _, tree := range []string{"cmd", "internal", "tools"} {
		err := filepath.WalkDir(filepath.Join(root, tree), func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				if entry.Name() == "generated" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}

			contents, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			source := string(contents)

			transportSites += len(wrappedThroughTransport.FindAllString(source, -1))
			for _, match := range transientWithCause.FindAllStringSubmatch(source, -1) {
				if match[1] == "nil" {
					continue
				}
				offenders = append(offenders, filepath.ToSlash(path)+": "+match[0])
			}

			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", tree, err)
		}
	}

	// A walk that matched nothing would report perfect compliance (ADR-067).
	if transportSites < 150 {
		t.Fatalf("found only %d apperrors.Transport sites, expected around two hundred.\n"+
			"The walk is probably broken, not the code.", transportSites)
	}

	if len(offenders) > 0 {
		sort.Strings(offenders)
		t.Fatalf("%d failure(s) wrapped as transient, which overwrites what the transport classified them as:\n  %s\n\n"+
			"Use apperrors.Transport(message, err). It keeps a rejected certificate permanent and a\n"+
			"mutation whose answer was lost unknown_outcome, and is transient when nothing classified the cause.",
			len(offenders), strings.Join(offenders, "\n  "))
	}
}

// TestTheTransientWrapScannerFindsAWrappedCall is the guard being seen to fail
// (ADR-067). Its first version matched one line only, so the shape gofmt gives
// every long call went unnoticed.
func TestTheTransientWrapScannerFindsAWrappedCall(t *testing.T) {
	t.Parallel()

	for name, source := range map[string]string{
		"on one line":                    `apperrors.New(apperrors.KindTransient, "one line", lookupErr)`,
		"across lines":                   "apperrors.New(\n\t\t\tapperrors.KindTransient,\n\t\t\tfmt.Sprintf(\"failed to read group %q\", group),\n\t\t\tlookupErr,\n\t\t)",
		"with a package-qualified cause": "apperrors.New(\n\tapperrors.KindTransient,\n\t\"message\",\n\tresult.err,\n)",
	} {
		match := transientWithCause.FindStringSubmatch(source)
		if match == nil {
			t.Errorf("%s: the scanner missed the wrap:\n%s", name, source)

			continue
		}
		if match[1] == "nil" {
			t.Errorf("%s: the cause was read as nil", name)
		}
	}

	// A transient error with no cause is the code deciding for itself, and the
	// walk skips it by reading the captured argument.
	decided := transientWithCause.FindStringSubmatch(`apperrors.New(apperrors.KindTransient, "nothing classified this", nil)`)
	if decided == nil || decided[1] != "nil" {
		t.Errorf("a transient error with no cause should be recognised as such, got %v", decided)
	}
}
