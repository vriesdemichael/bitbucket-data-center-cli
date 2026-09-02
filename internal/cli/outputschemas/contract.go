package outputschemas

// CommandsWithoutDataContract names the commands whose stdout under --json is
// deliberately not a data payload, each with the reason.
//
// This list is the rule, made checkable, and it is deliberately the only copy.
// It is consulted in three places -- the walk that holds every command to the
// envelope contract, the exemption test beside it, and the coverage report that
// counts published schemas -- and a second copy would let a command be exempt in
// one and counted in another.
//
// Adding an entry is a decision, not a formality. The question to answer is
// whether the command returns *data* or produces a *document or stream*. Data
// owes the caller an envelope (ADR-014) and a published schema. A document does
// not, because wrapping markdown or a diff in a JSON string helps nobody.
//
// help and completion are not listed: Cobra injects them at execute time, so
// they are not in the command tree and are not part of the surface this project
// documents or ships schemas for.
var CommandsWithoutDataContract = map[string]string{
	"ai skill show":       "prints a SKILL.md document; wrapping markdown in a data string helps nobody",
	"api":                 "streams the upstream response body verbatim, which is the point of the escape hatch",
	"ai mcp serve":        "runs an MCP server on stdio until it is stopped; the protocol is the output",
	"auth git-credential": "speaks git's credential helper protocol on stdout, which git parses and nothing else reads",
}

// CommandsWithoutDeclarableShape names the commands that do return a payload but
// have no shape bb can promise, each with the reason.
//
// A separate list from CommandsWithoutDataContract, because it answers a
// different question. Those commands produce no data payload at all and are
// exempt from the envelope contract. These produce one and keep the envelope --
// they just cannot say what is inside it, because the service hands back
// whatever Bitbucket sent as an untyped value and the command prints it without
// reading a field. Declaring a bare object for them would claim a contract that
// says nothing, which is worse than --describe answering honestly.
//
// Typing one of these means typing the service response first. Until someone
// does, the entry is the honest answer rather than a gap.
var CommandsWithoutDeclarableShape = map[string]string{
	"webhook test":          "returns whatever the webhook endpoint answered, untyped, and prints it without reading a field",
	"webhook stats":         "returns whatever the statistics endpoint answered, untyped, and prints it without reading a field",
	"project webhook test":  "returns whatever the webhook endpoint answered, untyped, and prints it without reading a field",
	"project webhook stats": "returns whatever the statistics endpoint answered, untyped, and prints it without reading a field",
}
