---
search:
  boost: 0.3
---

# ADR-061: The MCP surface runs on the official SDK and every tool names its payload

The MCP server is built on github.com/modelcontextprotocol/go-sdk. Every tool is registered through `mcp.AddTool[In, Out]`, by way of `toolSpec` or `askingSpec` in internal/mcp/server.go. The input schema is derived from In and the output schema from Out, so a handler cannot answer in a shape its schema does not describe. The SDK validates the arguments before the handler runs and the result before it leaves. Out is a struct that names its payload. A collection is named for what it holds (`branches`, `commits`), and a single object is named too (`commit`, `tag`). There is never a bare array and never a generic `items`. A failure is a Go error, which the SDK returns as a result with isError set. There is no versioned envelope.

Define an input struct and an output struct, and never write a schema by hand. A field without omitempty is required and one with it is optional, and the jsonschema tag carries the description. Put no enum on an input the service layer normalises, such as a role it accepts in any letter case; list the values in the description instead. Enums are for vocabularies the service does not widen. Give a new tool its arguments in the live conformance sweep (`seedMCPToolArguments`) in the same change. `TestLiveMCPEveryToolReturnsAClientCompatibleResult` fails for a tool without them. It checks what the SDK does not: that structuredContent is a JSON object, and that a text fallback exists.

Deriving both schemas from the handler's types means they cannot drift apart, which a schema kept beside the handler always could. A model reads each result fresh, with no parser holding a field path, so the name is what tells it what it is looking at, and one naming rule replaces several conventions. An object at the top also serves clients on revisions before SEP-2106, which reject an array outright. Since SEP-2106 an array is legal, so naming is a choice made for the model, not a workaround to remove, and it leaves room beside a collection for a field saying whether the list stopped at its limit (ADR-074). The CLI's envelope (ADR-095) is for scripts that pin field paths. A model gains nothing from one, so a shape change here is cheap.

## Not chosen

- **Add output schemas to the mark3labs/mcp-go server**: Leaves each schema written by hand beside its handler, the drift this repository has repeatedly paid for.
- **Keep the `items` wrapper and only add schemas**: The key tells a model nothing about what it holds.
- **The CLI's machine envelope**: Buys forward-compatible parsing for a consumer that does no parsing, at the cost of a wrapper the model reads past on every call.
- **Narrow view types instead of the generated OpenAPI types**: The generated types the tools return are shallow and publish cleanly. A second definition would be one more thing to keep in step.
