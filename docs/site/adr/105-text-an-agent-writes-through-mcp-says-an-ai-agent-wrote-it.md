---
search:
  boost: 0.3
---

# ADR-105: Text an agent writes through MCP says an AI agent wrote it

bb ends every comment, pull request description and tag message written through MCP with a line saying an AI agent wrote it for the person, through bb: `_Written by an AI agent for @alice, via bb._`. This is how prose the model composed is marked as the agent's (ADR-104). bb appends it in code, after the model's text, so the model cannot leave it out, and once: text that already carries it, as a description read back and sent again does, keeps the one it has. The person is named by their login name as a Bitbucket mention, quoted unless it is plain, and the line names nobody when Bitbucket does not say who they are. A comment the person writes in a view carries it too, since bb cannot tell their call from the model's; it waits in their review, where they remove the line if they adopt the text. The CLI adds no line, and the skill tells an agent at the shell to add it.

The line is on unless something turns it off: `--ai-disclosure=false`, `mcp_ai_disclosure: false` in the person's own configuration file, or the `mcp_ai_disclosure` policy key, which decides it either way for every server on the machine, for an organisation that cannot allow the line or one that requires it. Off, prose cannot be marked, so it is left to the person (ADR-104): comments still wait in their review, a tag is created without a message, and the tools that publish a pull request's title and description at once, `create_pull_request` and `update_pull_request`, are not offered.

Through MCP bb writes with the person's token, under their name, and Bitbucket has no mark for text an AI wrote. An MCP server acts as the person, so a bot account is not an option for it; an agent that should speak as itself is a pipeline with an account of its own.

Pass prose that others read and that a new MCP tool writes through disclosed, and withhold a tool that publishes it at once on a server without the line. Take the name from X-AUSERNAME, decoded, and never from the slug, which differs from it for a name with an @ or a + in it.

## Not chosen

- **Leave the line to the model, as the skill does at the shell**: A model drops an instruction, and through MCP bb can add the line itself.
- **Mark the text with a comment property**: Bitbucket shows properties nowhere, in its interface or its notifications.
- **Publish unmarked prose when the line is off**: It would pass for the person's own words, with nobody's okay but the client's.
