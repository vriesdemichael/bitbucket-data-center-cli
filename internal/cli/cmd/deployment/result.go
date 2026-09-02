package deploymentcmd

import (
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/safederef"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

// Environment is where a deployment went.
type Environment struct {
	Key         string `json:"key,omitempty" jsonschema:"Environment identifier, unique within the repository."`
	DisplayName string `json:"displayName,omitempty" jsonschema:"Human-readable environment name."`
	Type        string `json:"type,omitempty" jsonschema:"Environment classification, for example PRODUCTION or STAGING."`
	URL         string `json:"url,omitempty" jsonschema:"Link to the environment, when one is configured."`
}

// Deployment is one deployment recorded against a commit.
//
// The upstream object nests an entire repository -- links, hierarchy id, fork
// origin, the lot -- inside every deployment. That is the repository the caller
// already named to ask the question, so it is reduced to the reference. Nothing
// rendered the rest, and publishing it would make a deployment payload larger
// than the deployment.
type Deployment struct {
	Key                      string            `json:"key,omitempty" jsonschema:"Deployment key, unique per environment and commit."`
	DisplayName              string            `json:"displayName,omitempty" jsonschema:"Human-readable deployment name."`
	Description              string            `json:"description,omitempty" jsonschema:"Longer description, when one was given."`
	State                    string            `json:"state,omitempty" jsonschema:"Where the deployment got to."`
	URL                      string            `json:"url,omitempty" jsonschema:"Link to the deployment in the deploying system."`
	Environment              Environment       `json:"environment,omitzero" jsonschema:"Environment deployed to."`
	Repository               result.Repository `json:"repository,omitzero" jsonschema:"Repository the deployed commit belongs to."`
	FromCommit               string            `json:"fromCommit,omitempty" jsonschema:"Commit that was deployed."`
	DeploymentSequenceNumber int64             `json:"deploymentSequenceNumber,omitempty" jsonschema:"Ordinal of this deployment within its environment, which is how two deployments of the same key are told apart."`
	LastUpdated              int64             `json:"lastUpdated,omitempty" jsonschema:"When the record last changed, in milliseconds since the epoch."`
}

// Deletion is what `bb deployment delete` reports.
//
// repository is the reference object, not the "PROJECT/slug" string the command
// used to emit. That string was a third spelling of the same field: elsewhere
// it was an object, and elsewhere again an untagged Go struct publishing
// PascalCase.
type Deletion struct {
	result.Status
	Repository result.Repository `json:"repository"`
	Commit     string            `json:"commit" jsonschema:"Commit the deployment was recorded against."`
	Key        string            `json:"key,omitempty" jsonschema:"Deployment key that was deleted, when one was given."`
}

// deploymentStates is the closed set Bitbucket uses, serving both the enum on
// the published output schema and the values --state will accept.
var deploymentStates = []string{"PENDING", "IN_PROGRESS", "SUCCESSFUL", "FAILED", "CANCELLED", "ROLLED_BACK", "UNKNOWN"}

func init() {
	states := map[string][]string{"state": deploymentStates}

	result.Declare("deployment create", result.For[Deployment](states))
	result.Declare("deployment get", result.For[Deployment](states))
	result.Declare("deployment delete", result.For[Deletion](nil))
}

// deploymentFrom converts one upstream deployment.
func deploymentFrom(upstream openapigenerated.RestDeployment) Deployment {
	converted := Deployment{
		Key:         safederef.String(upstream.Key),
		DisplayName: safederef.String(upstream.DisplayName),
		Description: safederef.String(upstream.Description),
		URL:         safederef.String(upstream.Url),
	}

	if upstream.State != nil {
		converted.State = string(*upstream.State)
	}
	if upstream.DeploymentSequenceNumber != nil {
		converted.DeploymentSequenceNumber = *upstream.DeploymentSequenceNumber
	}
	if upstream.LastUpdated != nil {
		converted.LastUpdated = *upstream.LastUpdated
	}
	if upstream.Environment != nil {
		converted.Environment = Environment{
			Key:         upstream.Environment.Key,
			DisplayName: upstream.Environment.DisplayName,
			Type:        safederef.String(upstream.Environment.Type),
			URL:         safederef.String(upstream.Environment.Url),
		}
	}
	if upstream.FromCommit != nil {
		converted.FromCommit = safederef.String(upstream.FromCommit.Id)
	}
	if upstream.Repository != nil {
		converted.Repository = result.Repository{
			Slug: safederef.String(upstream.Repository.Slug),
		}
		if upstream.Repository.Project != nil {
			converted.Repository.ProjectKey = upstream.Repository.Project.Key
		}
	}

	return converted
}

// environmentTypes are the deployment environments Bitbucket recognises.
// UNKNOWN is in the set although the help text omitted it: the server's own
// enum has it, and leaving it out would reject a value the server accepts --
// the same oversight the build --state help had with CANCELLED.
var environmentTypes = []string{"PRODUCTION", "STAGING", "TESTING", "DEVELOPMENT", "UNKNOWN"}
