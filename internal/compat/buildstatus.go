package compat

// BuildStatusRepository is Bitbucket naming the repository on a build status
// read through a repository.
//
// From 9.4 a status read through a repository's builds endpoint carries that
// repository's projectKey and repositorySlug -- a commit-level status too, which
// that endpoint also returns. An earlier release carries neither (observed on
// 9.2.1 and 9.3.2), so bb names the repository the status was read through,
// which is what the newest release answers with.
//
// The commit-level listing is not this difference: it names no repository on
// any release, 10.4.3 included, and bb leaves it out there as it always has.
var BuildStatusRepository = Difference{
	What:  "a build status naming its repository",
	Since: Release{Major: 9, Minor: 4},
}
