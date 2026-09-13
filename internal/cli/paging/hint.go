package paging

import (
	"fmt"
	"io"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/style"
)

// Hint tells a person reading text output that a list stopped at --limit.
//
// meta.limitReached answers this under --json. Without the text equivalent, a
// person reading 25 rows is where a pipeline was before #573: unable to tell a
// full page from all there is. It says what to do about it, and it goes to
// stderr so that `bb repo list | wc -l` still counts rows.
//
// A command that lists several capped sets passes the largest count, since any
// one of them reaching the cap leaves the output incomplete.
func Hint(out io.Writer, options Options, count int) {
	if !LimitReached(options, count) {
		return
	}

	fmt.Fprintln(out, style.Empty.Render(fmt.Sprintf(
		"Stopped at the limit of %d. There may be more: raise --limit or pass --all.",
		options.effectiveLimit(),
	)))
}
