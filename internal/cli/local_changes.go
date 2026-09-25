package cli

import "strings"

// ChangesThisMachine reports whether the command at path writes to the machine
// it runs on -- a stored credential, a skill file, a working copy -- rather
// than only reading it or reaching Bitbucket, going by
// clientLocalMutatingCommands.
//
// A test that runs commands side by side in one environment needs to know:
// what these write is what the others read, so they cannot share it with them.
func ChangesThisMachine(path string) bool {
	_, changes := clientLocalMutatingCommands[strings.TrimSpace(path)]

	return changes
}
