package cli

// Two auto-merge tests went live.
//
// The dry-run permission precheck answered an empty repository listing so the
// check would conclude the caller lacks REPO_WRITE. That says the CLI believes
// an empty listing means no permission -- which is our own inference, not
// Bitbucket's answer. The two commands are rows in
// TestLiveDryRunPrechecksRefuseBeforePlanning now, run as an account that
// really does hold read and nothing more.
//
// The immediate merge is the outcome that is easy to misreport: a pull request
// whose checks already pass merges the moment auto-merge is armed, and saying
// "enabled auto-merge" there describes a pending state that will never fire.
// Whether Bitbucket merges rather than queues is entirely the server's
// decision, and the mock decided it. TestLivePullRequestAutoMergeMergesImmediately
// arms a real one and reads back both the payload and the human line.

// Six suites are live now.
//
// The draft flag on create and on update, and the update previews that go with
// them, are TestLivePullRequestDraftState: created as a draft, taken out of
// draft, and then asked what it would predict for each direction. The three
// auto-merge commands are TestLivePullRequestAutoMergeEnable, which arms one
// against a real merge check and reads the server's own view back rather than
// bb's echo of its own request.
//
// TestPRAutoMergeErrorPaths went with them, and it is the one worth saying
// something about. It read as local validation and is not: bb takes a
// selector that is not a number as a branch name and asks the server which
// pull request is open on it, so a refusal is Bitbucket answering that none
// is. The mock answered that question itself. It is three rows in the live
// auto-merge test now, and the previews it also covered are rows in
// TestLiveDryRunPrechecksRefuseBeforePlanning.
