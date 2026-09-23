package completion

import (
	"context"
	"fmt"
	"time"

	bulkcmd "github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/cmd/bulk"
	bulkworkflow "github.com/vriesdemichael/bitbucket-data-center-cli/internal/workflows/bulk"
)

func init() {
	register(KindBulkOperation, bulkOperationSource)
}

// bulkOperationSource offers the bulk runs saved on this machine.
//
// An operation id is random -- op-3fa29c1e77b0d412 -- so it is the one value in
// this package nobody could type from memory, and the description is the only
// way to tell one run from another. The runs are this machine's own, written
// by bb bulk apply, so nothing here asks the server.
func bulkOperationSource(context.Context, *Environment, Request) (Result, error) {
	directory, err := bulkcmd.StatusStoreDir()
	if err != nil {
		return Result{}, err
	}

	saved, err := bulkworkflow.NewStatusStore(directory).Recent(maxCandidates)
	if err != nil {
		return Result{}, err
	}

	candidates := make([]Candidate, 0, len(saved))
	for _, run := range saved {
		candidates = append(candidates, Candidate{Value: run.OperationID, Description: describeRun(run, time.Now())})
	}

	// Newest first, and kept that way: the run you just applied is the one you
	// are about to ask about, and a shell sorting random hex would bury it.
	return Result{Candidates: candidates, KeepOrder: true}, nil
}

// describeRun says when a run finished and how it went, in the terms bb bulk
// status reports it.
func describeRun(run bulkworkflow.SavedStatus, now time.Time) string {
	when := run.SavedAt.Local().Format("2006-01-02 15:04")
	if run.SavedAt.Local().Format("2006-01-02") == now.Local().Format("2006-01-02") {
		when = "today " + run.SavedAt.Local().Format("15:04")
	}

	targets := run.Summary.TargetCount
	failed := run.Summary.FailedTargets

	switch {
	case failed == 0:
		return fmt.Sprintf("%s, %d %s succeeded", when, targets, plural(targets, "target", "targets"))
	case failed == targets:
		return fmt.Sprintf("%s, all %d %s failed", when, targets, plural(targets, "target", "targets"))
	default:
		return fmt.Sprintf("%s, %d of %d targets failed", when, failed, targets)
	}
}

func plural(count int, one, many string) string {
	if count == 1 {
		return one
	}

	return many
}
