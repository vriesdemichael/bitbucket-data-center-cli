package repocmd

// TestRepoCommandsHonourRefusedPermission is live now, in
// TestLiveDryRunPrechecksRefuseBeforePlanning.
//
// It read the permission enum each command passed to an injected checker, and
// needed newPermissiveServer -- a handwritten Bitbucket whose payloads existed
// only so a command could load enough state to reach the check.
//
// What that enum decides is where the line falls for a caller, and the line is
// drawn against a real account now. A user holding REPO_READ and nothing more
// is refused every write and admin command, with exit 3 and no preview
// written; and is given a preview for the ones read is enough for -- comment,
// fork, watch, unwatch. Both halves matter: refusing something Bitbucket
// allows closes a door that is open, and allowing something it refuses spends
// a request to be told no less clearly.
//
// The rows this file carried that the live table lacked -- labels, default
// tasks, fork sync, watch -- were added there rather than dropped.
