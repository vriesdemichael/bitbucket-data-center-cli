package openapi

import (
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
)

// RepositoryPermission is a repository permission level as the Bitbucket API
// spells it.
//
// The generated name is an artefact of which operation oapi-codegen happened to
// derive the enum from, so it is aliased here and referred to by this name
// everywhere else.
type RepositoryPermission = openapigenerated.GetRepositories1ParamsPermission

// The generated constants these alias are not stable across spec versions.
// oapi-codegen emits a bare REPOWRITE when the value name is unique in the spec
// and a qualified GetRepositories1ParamsPermissionREPOWRITE once anything else
// collides with it, so the Bitbucket 10.2 to 10.4 bump renamed all three with no
// change in meaning. Depending on them directly put that churn in ninety call
// sites; depending on these puts it in three lines.
const (
	RepoRead  RepositoryPermission = openapigenerated.GetRepositories1ParamsPermissionREPOREAD
	RepoWrite RepositoryPermission = openapigenerated.GetRepositories1ParamsPermissionREPOWRITE
	RepoAdmin RepositoryPermission = openapigenerated.GetRepositories1ParamsPermissionREPOADMIN
)
