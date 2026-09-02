package sshkeycmd

import (
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/safederef"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

// Key is one SSH key on the authenticated user's account.
//
// Text is the public key itself. It is a public key, so publishing it is not a
// disclosure -- but it is long, and it is the field a caller is least likely
// to want, so it comes last rather than beside the label.
type Key struct {
	ID                int32  `json:"id,omitempty" jsonschema:"Identifier to pass to bb auth ssh-key remove."`
	Label             string `json:"label,omitempty" jsonschema:"Label the key was added under."`
	Fingerprint       string `json:"fingerprint,omitempty" jsonschema:"Fingerprint Bitbucket computed for the key."`
	AlgorithmType     string `json:"algorithmType,omitempty" jsonschema:"Key algorithm, for example RSA or ED25519."`
	BitLength         int32  `json:"bitLength,omitempty" jsonschema:"Key length in bits."`
	CreatedDate       int64  `json:"createdDate,omitempty" jsonschema:"When the key was added, in milliseconds since the epoch."`
	ExpiryDays        int32  `json:"expiryDays,omitempty" jsonschema:"Days until the key expires, when the instance enforces expiry."`
	LastAuthenticated string `json:"lastAuthenticated,omitempty" jsonschema:"When the key was last used to authenticate, when the instance records it."`
	Warning           string `json:"warning,omitempty" jsonschema:"A warning from Bitbucket about the key, for example that its algorithm is deprecated."`
	Text              string `json:"text,omitempty" jsonschema:"The public key itself."`
}

// Removal is what `bb ssh-key remove` reports.
type Removal struct {
	result.Status
	Key string `json:"key" jsonschema:"Identifier of the key that was removed."`
}

func init() {
	result.Declare("ssh-key list", result.List[Key](nil))
	result.Declare("ssh-key add", result.For[Key](nil))
	result.Declare("ssh-key remove", result.For[Removal](nil))
}

// keyFrom converts one upstream key.
func keyFrom(upstream openapigenerated.RestSshKey) Key {
	converted := Key{
		Label:             safederef.String(upstream.Label),
		Fingerprint:       safederef.String(upstream.Fingerprint),
		AlgorithmType:     safederef.String(upstream.AlgorithmType),
		LastAuthenticated: safederef.String(upstream.LastAuthenticated),
		Warning:           safederef.String(upstream.Warning),
		Text:              safederef.String(upstream.Text),
	}
	if upstream.Id != nil {
		converted.ID = *upstream.Id
	}
	if upstream.BitLength != nil {
		converted.BitLength = *upstream.BitLength
	}
	if upstream.CreatedDate != nil {
		converted.CreatedDate = *upstream.CreatedDate
	}
	if upstream.ExpiryDays != nil {
		converted.ExpiryDays = *upstream.ExpiryDays
	}

	return converted
}

// keysFrom converts a list, preserving order and never returning nil.
func keysFrom(upstream []openapigenerated.RestSshKey) []Key {
	converted := make([]Key, 0, len(upstream))
	for _, one := range upstream {
		converted = append(converted, keyFrom(one))
	}

	return converted
}
