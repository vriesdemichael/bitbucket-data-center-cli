package updatecmd

import (
	"fmt"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
	updateworkflow "github.com/vriesdemichael/bitbucket-data-center-cli/internal/workflows/update"
)

// Update is what `bb update` reports.
//
// Grouped rather than flat, because the flat version had 28 sibling fields and
// a reader had to know which of signatureVerified, signatureSkipped,
// checksumVerified and transparencyLogVerified answered "is this release
// trustworthy". They are one object now, and the question has one place to
// look.
//
// The workflow's own Result type stays flat and unchanged: it is internal, and
// the shape a caller reads is not obliged to be the shape the code carries.
//
// Scheduled, Staged, Paths.Staged and Paths.SwapResult are deprecated and never
// set. They are registered in internal/deprecation, which decides the major
// they are removed in (ADR-084).
type Update struct {
	CurrentVersion  string `json:"currentVersion" jsonschema:"Version currently installed."`
	LatestVersion   string `json:"latestVersion" jsonschema:"Newest published release."`
	UpdateAvailable bool   `json:"updateAvailable" jsonschema:"Whether the latest release is newer than the installed one."`
	UpToDate        bool   `json:"upToDate" jsonschema:"Whether the installed version is already the latest."`
	Comparison      string `json:"comparison,omitempty" jsonschema:"How the two versions compare, when both could be parsed as semver."`

	// Comparability is reported rather than assumed: a locally built binary
	// stamps a version that is not semver, and saying so is the difference
	// between "no update needed" and "could not tell".
	CurrentVersionComparable bool `json:"currentVersionComparable" jsonschema:"Whether the installed version parsed as semver. False for a locally built binary, where updateAvailable is a guess rather than a comparison."`
	LatestVersionComparable  bool `json:"latestVersionComparable" jsonschema:"Whether the published version parsed as semver."`

	DryRun        bool   `json:"dryRun" jsonschema:"Whether this run verified the latest release and installed nothing. A dry run verifies that release even when it is the installed version."`
	Applied       bool   `json:"applied" jsonschema:"Whether the new binary is now in place."`
	Scheduled     bool   `json:"scheduled" jsonschema:"Deprecated, and always false: bb update replaces the binary itself, before it exits, on every operating system. Read applied instead."`
	Staged        bool   `json:"staged" jsonschema:"Deprecated, and always false: the run that downloads a verified binary puts it in place. Read applied instead."`
	PlannedAction string `json:"plannedAction,omitempty" jsonschema:"What the run would do, or did."`

	Release  ReleaseSource `json:"release,omitzero" jsonschema:"Where the release was fetched from."`
	Trust    Trust         `json:"trust,omitzero" jsonschema:"What was verified about the release before it was trusted."`
	Paths    Paths         `json:"paths,omitzero" jsonschema:"Filesystem locations involved in the swap."`
	Platform string        `json:"platform,omitempty" jsonschema:"Operating system and architecture the asset targets."`
}

// ReleaseSource names the artifacts a run selected.
type ReleaseSource struct {
	URL                      string `json:"url,omitempty" jsonschema:"Release page for the selected version."`
	AssetName                string `json:"assetName,omitempty" jsonschema:"Archive chosen for this platform."`
	AssetURL                 string `json:"assetUrl,omitempty" jsonschema:"Download URL for that archive."`
	ChecksumAssetName        string `json:"checksumAssetName,omitempty" jsonschema:"Checksum file published alongside it."`
	SignatureBundleAssetName string `json:"signatureBundleAssetName,omitempty" jsonschema:"Sigstore bundle published alongside it."`
}

// Trust is what was checked before the release was accepted.
//
// Together these answer one question -- may this binary be trusted -- which was
// previously spread across four sibling booleans with nothing saying they were
// related.
type Trust struct {
	ChecksumAvailable       bool   `json:"checksumAvailable" jsonschema:"Whether the checksum file has an entry for this platform's archive."`
	ChecksumVerified        bool   `json:"checksumVerified" jsonschema:"Whether the download matched its checksum. Detects corruption, not tampering."`
	SignatureVerified       bool   `json:"signatureVerified" jsonschema:"Whether the Sigstore signature verified. This is the check that detects tampering."`
	SignatureSkipped        bool   `json:"signatureSkipped" jsonschema:"Whether signature verification was deliberately not performed, because administrative policy set allow_unverified_update. True here with signatureVerified false is a policy decision rather than a failure."`
	TransparencyLogVerified bool   `json:"transparencyLogVerified" jsonschema:"Whether the signature was found in the Sigstore transparency log."`
	Source                  string `json:"source,omitempty" jsonschema:"Where the Sigstore trust material came from, so an operator can confirm an offline trust root is actually in use."`
	Identity                string `json:"identity,omitempty" jsonschema:"Signing identity the signature was issued to."`
	Issuer                  string `json:"issuer,omitempty" jsonschema:"OIDC issuer that vouched for that identity."`
}

// Paths are the filesystem locations a swap touched.
type Paths struct {
	Install    string `json:"install,omitempty" jsonschema:"Where the running binary lives, and where a new one is written."`
	Staged     string `json:"staged,omitempty" jsonschema:"Deprecated, and always empty: no verified binary waits to be swapped in after bb update exits. Read applied instead."`
	SwapResult string `json:"swapResult,omitempty" jsonschema:"Deprecated, and always empty: bb update reports the outcome of the swap itself, in this result or as an error. Read applied instead."`
}

func init() {
	result.Declare("update", result.For[Update](nil))
}

// updateFrom regroups the workflow's flat result into the reported shape.
func updateFrom(workflow updateworkflow.Result) Update {
	return Update{
		CurrentVersion:           workflow.CurrentVersion,
		LatestVersion:            workflow.LatestVersion,
		UpdateAvailable:          workflow.UpdateAvailable,
		UpToDate:                 workflow.UpToDate,
		Comparison:               workflow.Comparison,
		CurrentVersionComparable: workflow.CurrentVersionComparable,
		LatestVersionComparable:  workflow.LatestVersionComparable,
		DryRun:                   workflow.DryRun,
		Applied:                  workflow.Applied,
		PlannedAction:            workflow.PlannedAction,
		Platform:                 workflow.TargetPlatform,
		Release: ReleaseSource{
			URL:                      workflow.ReleaseURL,
			AssetName:                workflow.AssetName,
			AssetURL:                 workflow.AssetURL,
			ChecksumAssetName:        workflow.ChecksumAssetName,
			SignatureBundleAssetName: workflow.SignatureBundleAssetName,
		},
		Trust: Trust{
			ChecksumAvailable:       workflow.ChecksumAvailable,
			ChecksumVerified:        workflow.ChecksumVerified,
			SignatureVerified:       workflow.SignatureVerified,
			SignatureSkipped:        workflow.SignatureSkipped,
			TransparencyLogVerified: workflow.TransparencyLogVerified,
			Source:                  workflow.TrustSource,
			Identity:                workflow.SignatureIdentity,
			Issuer:                  workflow.SignatureIssuer,
		},
		Paths: Paths{
			Install: workflow.InstallPath,
		},
	}
}

// updatePreview is the verdict of a dry run (ADR-096): the one change bb update
// makes, replacing the binary, and the report of what it checked as data. The
// release, its signature and its checksum are checked against what the release
// source serves, so the verdict is server-validated; a check that fails is the
// error the dry run answers with instead.
func updatePreview(workflow updateworkflow.Result) jsonoutput.Preview {
	effect := jsonoutput.Effect{
		Action:  "update",
		Target:  map[string]any{"binary": workflow.InstallPath, "version": workflow.LatestVersion},
		Outcome: jsonoutput.OutcomeNoOp,
		Reasons: []string{fmt.Sprintf("%s is installed, and %s is the latest release", workflow.CurrentVersion, workflow.LatestVersion)},
	}
	if workflow.UpdateAvailable {
		effect.Outcome = jsonoutput.OutcomeWouldApply
		effect.Reasons = []string{fmt.Sprintf("%s would replace %s", workflow.LatestVersion, workflow.CurrentVersion)}
	}

	return jsonoutput.Preview{
		Tier:    jsonoutput.TierServerValidated,
		Effects: []jsonoutput.Effect{effect},
		Data:    updateFrom(workflow),
	}
}
