package auth

import (
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/result"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
)

// Check is one thing `bb auth status` verified rather than merely reported.
//
// This is statusCheck's published form. The internal type keeps its own tags
// because it is also what the human renderer walks; the two are kept in step by
// checksFrom, which is the only thing that builds this one.
type Check struct {
	Name     string `json:"name" jsonschema:"What was checked."`
	OK       bool   `json:"ok" jsonschema:"Whether the check passed."`
	Advisory bool   `json:"advisory" jsonschema:"Whether failing this check means the setup is broken. An advisory failure does not, and never changes the exit status."`
	Detail   string `json:"detail,omitempty" jsonschema:"The finding: who you are, or what went wrong."`
	Remedy   string `json:"remedy,omitempty" jsonschema:"What to do about it. Only set when ok is false."`
}

// Status is what `bb auth status` returns.
//
// ok is the verdict. Under --json the exit status is always zero -- machine
// output is a single document, and a failing exit would replace the findings
// with an error envelope -- so this field is how a caller learns the answer.
type Status struct {
	OK                     bool    `json:"ok" jsonschema:"Whether every non-advisory check passed."`
	BitbucketURL           string  `json:"bitbucketUrl" jsonschema:"The configured Bitbucket base URL."`
	BitbucketVersionTarget string  `json:"bitbucketVersionTarget" jsonschema:"Version the operator pinned, empty when none was."`
	AuthMode               string  `json:"authMode" jsonschema:"How bb authenticates: token, basic, or none."`
	AuthSource             string  `json:"authSource" jsonschema:"Where that credential came from: env, keyring, or config."`
	CredentialStorage      string  `json:"credentialStorage" jsonschema:"How the credential is held. Reported here as well as at login so an operator auditing a machine does not have to read the config file."`
	Checks                 []Check `json:"checks" jsonschema:"What was verified, in the order it was checked."`
}

// Login is what `bb auth login` reports.
type Login struct {
	Host                string   `json:"host" jsonschema:"Host the credentials were stored for."`
	Aliases             []string `json:"aliases" jsonschema:"Aliases stored for this server context. Empty rather than absent when there are none."`
	AuthMode            string   `json:"authMode" jsonschema:"Stored authentication mode: token or basic."`
	UsedInsecureStorage bool     `json:"usedInsecureStorage" jsonschema:"True when the system keyring was unavailable and the credential went to the config file instead."`
}

// Identity is what `bb auth identity` returns.
type Identity struct {
	BitbucketURL string      `json:"bitbucketUrl" jsonschema:"Base URL the identity lookup went to."`
	User         result.User `json:"user"`
}

// TokenURL is what `bb auth token-url` returns.
type TokenURL struct {
	BitbucketURL string `json:"bitbucketUrl" jsonschema:"Base URL the token page belongs to."`
	TokenURL     string `json:"tokenUrl" jsonschema:"Page to create a personal access token on. Per-user when bb could resolve who you are, generic when it could not."`
}

// ServerContext is one stored server context.
type ServerContext struct {
	Host      string   `json:"host" jsonschema:"Bitbucket base URL."`
	Aliases   []string `json:"aliases" jsonschema:"Alternate hostnames that resolve to this context, for example an SSH clone host."`
	AuthMode  string   `json:"authMode" jsonschema:"How bb authenticates to this host: token, basic, or none."`
	Username  string   `json:"username,omitempty" jsonschema:"Stored username, present for basic auth."`
	IsDefault bool     `json:"isDefault" jsonschema:"Whether this is the context bb uses when no host is named."`
}

// ServerContexts is what `bb auth server list` returns.
type ServerContexts struct {
	Servers []ServerContext `json:"servers" jsonschema:"Stored server contexts. Empty rather than absent when none are stored."`
}

// DefaultServer is what `bb auth server use` reports.
type DefaultServer struct {
	result.Status
	DefaultHost string `json:"defaultHost" jsonschema:"Host bb will now use when no other is named."`
}

// Aliases is what `bb auth alias list`, `add` and `remove` return.
//
// host is reported by all three. Before this only list carried it, so a caller
// adding an alias got back a list with nothing saying which context it belonged
// to -- which matters exactly because --host decides that and defaults are in
// play.
type Aliases struct {
	Host    string   `json:"host" jsonschema:"Server context the aliases belong to."`
	Aliases []string `json:"aliases" jsonschema:"Aliases now stored for that context. Empty rather than absent when there are none."`
}

// DiscoveredAliases is what `bb auth alias discover` returns.
type DiscoveredAliases struct {
	Host       string   `json:"host" jsonschema:"Server context that was probed."`
	Aliases    []string `json:"aliases" jsonschema:"Aliases now stored, which is the discovered set merged with what was already there unless --replace was passed."`
	Discovered []string `json:"discovered" jsonschema:"What this run found. Empty rather than absent when nothing was."`
	Removed    []string `json:"removed" jsonschema:"Aliases that were stored before and are not any more, which only --replace can produce. Named rather than dropped silently: they are configuration someone put there by hand."`
}

// GitCredentialSetup is what `bb auth setup-git` reports.
type GitCredentialSetup struct {
	Host   string `json:"host" jsonschema:"Scheme and host the helper was configured for."`
	Key    string `json:"key" jsonschema:"Git config key that was written."`
	Helper string `json:"helper" jsonschema:"Value written to that key. An absolute path, because git resolves the helper through a shell whose PATH may not match yours."`
	Scope  string `json:"scope" jsonschema:"Which git config the entry went into: global or local."`
}

// GpgSubKey is a subkey of a GPG key.
//
// expiryDate is milliseconds since the epoch, the same as the parent key's.
// Upstream declares the parent's as an integer and the subkey's as an RFC 3339
// string, so the generated client hands back a time.Time here and a number
// there -- one concept, two encodings, in the same object. bb publishes one.
type GpgSubKey struct {
	ExpiryDate  int64  `json:"expiryDate,omitempty" jsonschema:"When the subkey expires, in milliseconds since the epoch. Absent when it does not."`
	Fingerprint string `json:"fingerprint,omitempty" jsonschema:"Fingerprint of the subkey."`
}

// GpgKey is one personal GPG key.
type GpgKey struct {
	ID           string      `json:"id,omitempty" jsonschema:"Key identifier, which bb auth gpg-key remove also accepts."`
	EmailAddress string      `json:"emailAddress,omitempty" jsonschema:"Email address the key is bound to."`
	Fingerprint  string      `json:"fingerprint,omitempty" jsonschema:"Fingerprint, which bb auth gpg-key remove also accepts."`
	ExpiryDate   int64       `json:"expiryDate,omitempty" jsonschema:"When the key expires, in milliseconds since the epoch. Absent when it does not."`
	Text         string      `json:"text,omitempty" jsonschema:"The public key itself."`
	SubKeys      []GpgSubKey `json:"subKeys,omitempty" jsonschema:"Subkeys, when the key has any."`
}

// GpgKeyRemoval is what `bb auth gpg-key remove` reports.
type GpgKeyRemoval struct {
	result.Status
	Removed string `json:"removed" jsonschema:"The id or fingerprint that was removed, as it was given on the command line."`
}

// AccessToken is one HTTP access token.
//
// Bitbucket does not report a token's permissions or expiry on read, only on
// creation, so neither is published: a field that is always absent describes
// nothing.
type AccessToken struct {
	ID          string `json:"id,omitempty" jsonschema:"Token identifier, for bb auth token get, update and revoke."`
	Name        string `json:"name,omitempty" jsonschema:"Token name."`
	CreatedDate int64  `json:"createdDate,omitempty" jsonschema:"When the token was created, in milliseconds since the epoch."`
}

// CreatedAccessToken is what `bb auth token create` returns.
//
// token carries the secret. It is the only time Bitbucket ever returns it, so a
// caller that does not capture it here has to revoke the token and make another
// one.
type CreatedAccessToken struct {
	AccessToken
	Token string `json:"token" jsonschema:"The token secret. Returned once, at creation, and never again."`
}

func init() {
	result.Declare("auth status", result.For[Status](map[string][]string{
		"authMode": {"token", "basic", "none"},
	}))
	result.Declare("auth login", result.For[Login](map[string][]string{
		"authMode": {"token", "basic"},
	}))
	result.Declare("auth identity", result.For[Identity](nil))
	result.Declare("auth token-url", result.For[TokenURL](nil))
	result.Declare("auth logout", result.For[result.Status](nil))

	result.Declare("auth server list", result.For[ServerContexts](map[string][]string{
		"servers.authMode": {"token", "basic", "none"},
	}))
	result.Declare("auth server use", result.For[DefaultServer](nil))

	result.Declare("auth alias list", result.For[Aliases](nil))
	result.Declare("auth alias add", result.For[Aliases](nil))
	result.Declare("auth alias remove", result.For[Aliases](nil))
	result.Declare("auth alias discover", result.For[DiscoveredAliases](nil))

	result.Declare("auth setup-git", result.For[GitCredentialSetup](map[string][]string{
		"scope": {"global", "local"},
	}))

	result.Declare("auth gpg-key list", result.List[GpgKey](nil))
	result.Declare("auth gpg-key add", result.List[GpgKey](nil))
	result.Declare("auth gpg-key remove", result.For[GpgKeyRemoval](nil))
	result.Declare("auth gpg-key clear", result.For[result.Status](nil))

	result.Declare("auth token list", result.List[AccessToken](nil))
	result.Declare("auth token get", result.For[AccessToken](nil))
	result.Declare("auth token create", result.For[CreatedAccessToken](nil))
	result.Declare("auth token update", result.For[AccessToken](nil))
	result.Declare("auth token revoke", result.For[result.Status](nil))

	// auth git-credential is deliberately not declared. It speaks git's
	// credential helper protocol on stdout -- key=value lines git parses -- and
	// has no JSON mode at all, so there is no payload to describe.
}

// checksFrom converts what bb auth status verified.
//
// A conversion rather than a field-by-field copy, and deliberately so: Go only
// allows it while the two structs carry the same fields, so adding one to
// statusCheck stops this compiling until someone decides whether it belongs in
// the published payload. A copy would have let it slip in either direction
// unnoticed.
func checksFrom(checks []statusCheck) []Check {
	converted := make([]Check, 0, len(checks))
	for _, check := range checks {
		converted = append(converted, Check(check))
	}

	return converted
}

// serverContextsFrom converts the stored contexts.
//
// config.ServerContext carries no JSON tags, so publishing it directly said
// {"Host":...,"AuthMode":...} -- Go field names reaching a caller because the
// struct that happened to be in scope became the contract.
func serverContextsFrom(contexts []config.ServerContext) []ServerContext {
	converted := make([]ServerContext, 0, len(contexts))
	for _, context := range contexts {
		aliases := context.Aliases
		if aliases == nil {
			aliases = []string{}
		}
		converted = append(converted, ServerContext{
			Host:      context.Host,
			Aliases:   aliases,
			AuthMode:  context.AuthMode,
			Username:  context.Username,
			IsDefault: context.IsDefault,
		})
	}

	return converted
}

// gpgKeyFrom converts one upstream GPG key.
func gpgKeyFrom(upstream openapigenerated.RestGpgKey) GpgKey {
	converted := GpgKey{
		ID:           safeString(upstream.Id),
		EmailAddress: safeString(upstream.EmailAddress),
		Fingerprint:  safeString(upstream.Fingerprint),
		Text:         safeString(upstream.Text),
	}
	if upstream.ExpiryDate != nil {
		converted.ExpiryDate = *upstream.ExpiryDate
	}
	if upstream.SubKeys != nil {
		converted.SubKeys = make([]GpgSubKey, 0, len(*upstream.SubKeys))
		for _, sub := range *upstream.SubKeys {
			entry := GpgSubKey{Fingerprint: safeString(sub.Fingerprint)}
			if sub.ExpiryDate != nil {
				entry.ExpiryDate = sub.ExpiryDate.UnixMilli()
			}
			converted.SubKeys = append(converted.SubKeys, entry)
		}
	}

	return converted
}

// gpgKeysFrom converts a list, preserving order and never returning nil.
func gpgKeysFrom(upstream []openapigenerated.RestGpgKey) []GpgKey {
	converted := make([]GpgKey, 0, len(upstream))
	for _, one := range upstream {
		converted = append(converted, gpgKeyFrom(one))
	}

	return converted
}

// accessTokenFrom converts one upstream token.
func accessTokenFrom(upstream openapigenerated.RestAccessToken) AccessToken {
	converted := AccessToken{
		ID:   safeString(upstream.Id),
		Name: safeString(upstream.Name),
	}
	if upstream.CreatedDate != nil {
		converted.CreatedDate = *upstream.CreatedDate
	}

	return converted
}

// accessTokensFrom converts a list, preserving order and never returning nil.
func accessTokensFrom(upstream []openapigenerated.RestAccessToken) []AccessToken {
	converted := make([]AccessToken, 0, len(upstream))
	for _, one := range upstream {
		converted = append(converted, accessTokenFrom(one))
	}

	return converted
}

// createdAccessTokenFrom converts the one response that carries the secret.
func createdAccessTokenFrom(upstream openapigenerated.RestRawAccessToken) CreatedAccessToken {
	converted := CreatedAccessToken{
		AccessToken: AccessToken{
			ID:   safeString(upstream.Id),
			Name: safeString(upstream.Name),
		},
		Token: safeString(upstream.Token),
	}
	if upstream.CreatedDate != nil {
		converted.AccessToken.CreatedDate = *upstream.CreatedDate
	}

	return converted
}

// listOrEmpty makes sure a list field is published as a list.
//
// A nil slice marshals to null, and every list on these payloads is declared
// without omitempty -- so the schema promises an array while the document says
// null. bb auth alias discover hit this on an instance whose repositories have
// no clone links: nothing was discovered, and the field said null rather than
// saying so.
func listOrEmpty(values []string) []string {
	if values == nil {
		return []string{}
	}

	return values
}
