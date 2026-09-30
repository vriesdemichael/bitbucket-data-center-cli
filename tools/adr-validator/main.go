// Command adr-validator checks the decision records in docs/site/adr.
//
// A record is NNN-slug.md and opens with # ADR-NNN: Title, the number in its
// name and its heading agree, and no two records share a number. Every
// ADR-NNN mentioned anywhere in the repository naming a record that exists is
// checked by TestEveryADRMentionHasARecord in tools/gateparity, which walks the
// whole tree.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/adr"
)

func main() {
	directory := flag.String("dir", adr.Directory, "Directory holding the decision records")
	// Accepted so the task that has always run it keeps working; validating is
	// all this command does.
	flag.Bool("validate", true, "Validate the decision records")
	flag.Parse()

	records, err := adr.Load(*directory)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERROR:", err)
		os.Exit(1)
	}

	fmt.Printf("%d decision records are well formed\n", len(records))
}
