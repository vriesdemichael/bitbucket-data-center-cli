# Set Up the MCP Server in Your Client

Every client starts the server the same way: the command `bb`, with
`ai mcp serve` and its flags. Confine it to the project the agent works in with
`--project`, and add `--allow-writes` when the agent should change Bitbucket as
well as read it. [The MCP server](../ai-and-llms.md#the-mcp-server) covers the
flags. What differs between clients is where the entry goes, and how it hands
the server a token.

## A token for the agent

Neither way below puts a secret in the client's file.

- **Your own login.** An entry with no credential uses the one `bb auth login`
  stored for the host, so the agent acts with your token's rights.
- **A token of its own.** Log the agent's token in to a configuration file of
  its own, and name that file in the entry with `BB_CONFIG_PATH`. A read-only
  token keeps an agent that should only read from changing anything, whatever
  the server offers.

=== "Bash"

    ```bash
    export BB_CONFIG_PATH="$HOME/.config/bb/agent.yaml"
    printf '%s' "$AGENT_TOKEN" | bb auth login https://bitbucket.example.com --token-stdin
    ```

=== "PowerShell"

    ```powershell
    $env:BB_CONFIG_PATH = "$env:APPDATA\bb\agent.yaml"
    $env:AGENT_TOKEN | bb auth login https://bitbucket.example.com --token-stdin
    ```

The token goes to the keyring, and the file holds the host. The entry then
carries only the path, written in full, since a client expands no `~`:

```json
"env": { "BB_CONFIG_PATH": "/home/alice/.config/bb/agent.yaml" }
```

Some clients can also pass `BITBUCKET_TOKEN` from your environment or a prompt
of their own; the sections below say which.

## Claude Code

```bash
claude mcp add bb -e BB_CONFIG_PATH=/home/alice/.config/bb/agent.yaml -- bb ai mcp serve --project PAY
```

Everything after `--` is the server's command line. `--scope project` writes the
entry to `.mcp.json` in the repository, for everyone who works in it. Claude
Code expands `${VAR}` in that file, so a shared entry can take each person's
token from their environment:

```json
{
  "mcpServers": {
    "bb": {
      "command": "bb",
      "args": ["ai", "mcp", "serve", "--project", "PAY"],
      "env": { "BITBUCKET_TOKEN": "${BB_MCP_TOKEN}" }
    }
  }
}
```

A variable that is not set where Claude Code starts is passed as written, and
`claude mcp list` warns about it.

## Claude Desktop

Settings, then Developer, then **Edit Config** opens
`claude_desktop_config.json`: in `~/Library/Application Support/Claude/` on
macOS and in `%APPDATA%\Claude\` on Windows.

```json
{
  "mcpServers": {
    "bb": {
      "command": "bb",
      "args": ["ai", "mcp", "serve", "--project", "PAY"],
      "env": { "BB_CONFIG_PATH": "/Users/alice/.config/bb/agent.yaml" }
    }
  }
}
```

Use your own login or `BB_CONFIG_PATH` here, not a variable: the app is not
started from your shell, so it does not see what you exported there. For the
same reason it may not find `bb` on your shell's `PATH`; name it in full, as
`command -v bb` or `(Get-Command bb).Source` prints it. Quit the app before
editing the file, which it can write back while it runs, and start it again to
load the server.

## Cursor

`.cursor/mcp.json` in a project, or `~/.cursor/mcp.json` for every project.
Cursor fills in `${env:NAME}` from its environment:

```json
{
  "mcpServers": {
    "bb": {
      "command": "bb",
      "args": ["ai", "mcp", "serve", "--project", "PAY"],
      "env": { "BITBUCKET_TOKEN": "${env:BB_MCP_TOKEN}" }
    }
  }
}
```

## VS Code

`.vscode/mcp.json` in a workspace. VS Code asks for a value an `inputs` entry
names the first time the server starts, hides it as it is typed when
`password` is true, and keeps it:

```json
{
  "inputs": [
    { "type": "promptString", "id": "bb-token", "description": "Bitbucket token for the agent", "password": true }
  ],
  "servers": {
    "bb": {
      "type": "stdio",
      "command": "bb",
      "args": ["ai", "mcp", "serve", "--project", "PAY"],
      "env": { "BITBUCKET_TOKEN": "${input:bb-token}" }
    }
  }
}
```

## Any other client

A client that starts stdio servers needs the command `bb`, the arguments
`ai mcp serve` and the flags, and, for a token of the agent's own,
`BB_CONFIG_PATH` in the server's environment. The server is not an HTTP server,
and there is no port to connect to.
