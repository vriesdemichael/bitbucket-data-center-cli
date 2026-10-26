package config

// RegistryPolicyKey is the key under HKEY_LOCAL_MACHINE that Windows policy is
// read from.
const RegistryPolicyKey = `Software\Policies\bb`

// RegistryKind is how a policy value under RegistryPolicyKey is written.
type RegistryKind string

const (
	// RegistryBoolean is a REG_DWORD of 0 or 1, or a REG_SZ spelling of true
	// or false.
	RegistryBoolean RegistryKind = "boolean"
	// RegistryString is a REG_SZ.
	RegistryString RegistryKind = "string"
	// RegistryList is a REG_MULTI_SZ, or a comma-separated REG_SZ.
	RegistryList RegistryKind = "list"
)

// RegistryValue is one value bb reads under RegistryPolicyKey, and the policy
// key it sets. The Windows reader and the Group Policy template both come from
// RegistryValues, so neither can name a value the other does not.
type RegistryValue struct {
	Name string
	Key  string
	Kind RegistryKind
	// Title is what Group Policy calls the setting.
	Title string

	// restrictive is the side a boolean takes when its value is present but
	// cannot be read: the one that keeps a guarantee rather than lifting it,
	// so an unreadable value cannot open anything.
	restrictive bool
	boolean     func(*PolicyConfig) **bool
	text        func(*PolicyConfig) *string
	list        func(*PolicyConfig) *[]string
}

// RegistryValues are the values bb reads, in the order the reference page
// lists them. mcp_audit_file has none.
var RegistryValues = []RegistryValue{
	{
		Name: "RequireKeyring", Key: "require_keyring", Kind: RegistryBoolean,
		Title:       "Require the OS keyring for credentials",
		restrictive: true,
		boolean:     func(p *PolicyConfig) **bool { return &p.RequireKeyring },
	},
	{
		Name: "AllowedHosts", Key: "allowed_hosts", Kind: RegistryList,
		Title: "Allowed Bitbucket hosts",
		list:  func(p *PolicyConfig) *[]string { return &p.AllowedHosts },
	},
	{
		Name: "CAFile", Key: "ca_file", Kind: RegistryString,
		Title: "CA bundle bb must use",
		text:  func(p *PolicyConfig) *string { return &p.CAFile },
	},
	{
		Name: "AllowInsecureSkipVerify", Key: "allow_insecure_skip_verify", Kind: RegistryBoolean,
		Title:   "Allow skipping TLS verification",
		boolean: func(p *PolicyConfig) **bool { return &p.AllowInsecureSkipVerify },
	},
	{
		Name: "DisableUpdate", Key: "disable_update", Kind: RegistryBoolean,
		Title:       "Disable bb update",
		restrictive: true,
		boolean:     func(p *PolicyConfig) **bool { return &p.DisableUpdate },
	},
	{
		Name: "UpdateBaseURL", Key: "update_base_url", Kind: RegistryString,
		Title: "Release mirror for bb update",
		text:  func(p *PolicyConfig) *string { return &p.UpdateBaseURL },
	},
	{
		Name: "AllowHTTPUpdate", Key: "allow_http_update", Kind: RegistryBoolean,
		Title:   "Allow bb update over plain HTTP",
		boolean: func(p *PolicyConfig) **bool { return &p.AllowHTTPUpdate },
	},
	{
		Name: "UpdateTrustedRoot", Key: "update_trusted_root", Kind: RegistryString,
		Title: "Sigstore trusted root for bb update",
		text:  func(p *PolicyConfig) *string { return &p.UpdateTrustedRoot },
	},
	{
		Name: "UpdateTUFURL", Key: "update_tuf_url", Kind: RegistryString,
		Title: "Sigstore TUF mirror for bb update",
		text:  func(p *PolicyConfig) *string { return &p.UpdateTUFURL },
	},
	{
		Name: "UpdateSignatureIdentity", Key: "update_signature_identity", Kind: RegistryString,
		Title: "Expected identity of the release signer",
		text:  func(p *PolicyConfig) *string { return &p.UpdateSignatureIdentity },
	},
	{
		Name: "UpdateSignatureIssuer", Key: "update_signature_issuer", Kind: RegistryString,
		Title: "Expected issuer of the release signer",
		text:  func(p *PolicyConfig) *string { return &p.UpdateSignatureIssuer },
	},
	{
		Name: "AllowUnverifiedUpdate", Key: "allow_unverified_update", Kind: RegistryBoolean,
		Title:   "Allow bb update without signature verification",
		boolean: func(p *PolicyConfig) **bool { return &p.AllowUnverifiedUpdate },
	},
	{
		Name: "DisableBB", Key: "disable_bb", Kind: RegistryBoolean,
		Title:       "Switch bb off",
		restrictive: true,
		boolean:     func(p *PolicyConfig) **bool { return &p.DisableBB },
	},
	{
		Name: "DisableMCPServer", Key: "disable_mcp_server", Kind: RegistryBoolean,
		Title:       "Switch the MCP server off",
		restrictive: true,
		boolean:     func(p *PolicyConfig) **bool { return &p.DisableMCPServer },
	},
	{
		Name: "ReadOnly", Key: "read_only", Kind: RegistryBoolean,
		Title:       "Make bb read-only",
		restrictive: true,
		boolean:     func(p *PolicyConfig) **bool { return &p.ReadOnly },
	},
	{
		Name: "MCPElicitationOptIn", Key: "mcp_elicitation_opt_in", Kind: RegistryBoolean,
		Title:       "Ask before an MCP tool decides a merge",
		restrictive: true,
		boolean:     func(p *PolicyConfig) **bool { return &p.ElicitationOptIn },
	},
	{
		// A value bb cannot read keeps the line, the side that hides nothing.
		Name: "MCPAIDisclosure", Key: "mcp_ai_disclosure", Kind: RegistryBoolean,
		Title:       "Say that an AI agent wrote what the MCP server publishes",
		restrictive: true,
		boolean:     func(p *PolicyConfig) **bool { return &p.AIDisclosure },
	},
}
