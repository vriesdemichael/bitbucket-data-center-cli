package cli

import "testing"

// TestChangesThisMachineFollowsTheList: the commands that write to this machine
// are the ones clientLocalMutatingCommands names, and no others.
func TestChangesThisMachineFollowsTheList(t *testing.T) {
	t.Parallel()

	for path := range clientLocalMutatingCommands {
		if !ChangesThisMachine(path) {
			t.Errorf("%s writes to this machine, but ChangesThisMachine says it does not", path)
		}
	}
	if !ChangesThisMachine(" ai skill install ") {
		t.Error("a path with surrounding spaces is not recognised")
	}
	for _, path := range []string{"pr list", "doctor", "ai skill show", ""} {
		if ChangesThisMachine(path) {
			t.Errorf("%q only reads, but ChangesThisMachine says it writes to this machine", path)
		}
	}
}
