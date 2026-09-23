package errors

// StateExit is a command reporting through its exit status the state it read,
// rather than a failure of bb's (ADR-091). bb pr checks exits 1 when a build
// failed and 8 while one has not finished, as gh pr checks does: a script
// gates on `bb pr checks 42 || exit 1` the way it does on gh.
//
// It travels as an error only because the exit status leaves through the one
// path every command's result takes. The command has done its work and
// written its output; Reason is the line a person reads on stderr beside it.
// It is not a kind, and it never reaches a --json document: a command reports
// state this way only in its text output, where the exit status is the only
// place left to put it.
type StateExit struct {
	Code   int
	Reason string
}

func (exit *StateExit) Error() string {
	return exit.Reason
}
