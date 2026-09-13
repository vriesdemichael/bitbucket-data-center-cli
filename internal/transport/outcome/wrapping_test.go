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
var transientWithCause = regexp.MustCompile(`apperrors\.New\(apperrors\.KindTransient,[^\n]*,\s*([A-Za-z_][A-Za-z0-9_.]*)\)`)

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
