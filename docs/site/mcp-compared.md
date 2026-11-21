# bb's MCP Server and Atlassian's

Atlassian ships an MCP server for Bitbucket Data Center too: MCP for Bitbucket
DC, bundled with Bitbucket from 10.5 and installed from the Atlassian
Marketplace on earlier releases. The two do much of the same work and are built
differently. Atlassian's runs inside Bitbucket, and an administrator limits it
once for everyone. bb's runs on your own machine, and you set it up and limit it
yourself.

They do not exclude each other: one client can be configured with both.

This comparison was made on 2026-11-21 against the server bundled with Bitbucket
10.5.2, which names itself Atlassian DC MCP Server 2.0.5. It rests on
[Atlassian's documentation](https://confluence.atlassian.com/bitbucketserver/mcp-for-bitbucket-dc-1822723926.html),
on the list of tools that server gave a client, and on one comment written
through it. Atlassian's server will have changed since, so read its
documentation for what it does now.

## Which to use

Atlassian's fits when:

- an administrator wants one server for the whole instance, limited in one place
  that nobody can configure around;
- nothing may be installed on the machines people work on;
- the agent should do what bb's server leaves out: create repositories and
  branches, commit a change to a file, approve or decline a pull request.

bb's fits when:

- the Bitbucket instance cannot or should not be changed: nothing is installed
  on it, and bb works with 9.2 and later;
- an agent should only read, or reach only one project or repository;
- a review should stay yours: what the agent comments waits for you to publish,
  and it cannot approve;
- the agent works with builds: build statuses, required builds and auto-merge;
- you want the pull request itself in the chat, in a client that draws MCP Apps
  views.

## How they are built

| | bb | Atlassian's |
|---|---|---|
| Runs | On your machine, started by the MCP client, over stdio | Inside Bitbucket, as a plugin, over HTTP at `/rest/mcp/1.0/` |
| Set up by | You. Nothing changes on the Bitbucket instance | Nobody from 10.5, where it is bundled. An administrator on earlier releases |
| Bitbucket releases | 9.2 and later | 9.x and 10.x |
| The client needs | To start a local program | To reach Bitbucket over HTTP. A client that only starts local servers goes through the `mcp-remote` bridge |
| Credential | The token `bb auth login` stored in the OS keyring, or one of the agent's own | An HTTP access token, sent as a bearer header the client's configuration holds or names |
| Limited by | You, for each server, with flags. An administrator, for each machine, with system policy | An administrator, for the instance, with a file listing the REST endpoints to offer. A token is offered only the tools its permissions allow |
| Offers | Tools, resources, prompts and views | Tools |
| A tool is | Written for an agent: `get_pull_request` answers with what blocks a merge and what waits on the author | One Bitbucket REST endpoint, taking its parameters and answering with its JSON |

## What an agent is offered

The right-hand column is Atlassian's server as shipped. An administrator can
offer any other Bitbucket REST endpoint as a tool, and take any away.

| An agent can | bb | Atlassian's |
|---|---|---|
| Read pull requests, comments, commits, branches, tags and files | Yes | Yes |
| Read a pull request's diff | Yes | The files it changes, not the diff |
| Read build statuses and the builds a merge requires | Yes | No |
| Create and update a pull request | With `--allow-writes` | Yes |
| Comment on a pull request | With `--allow-writes`. The comment waits in your review until you publish it | Yes. The comment is published at once |
| Approve a pull request | No | Yes |
| Merge a pull request | With `--allow-writes` | Yes |
| Turn auto-merge on or off, report a build status | With `--allow-writes` | No |
| Decline or reopen a pull request | No | Yes |
| Create a tag | With `--allow-writes` | Yes |
| Create or delete a branch, delete a tag | No | Yes |
| Commit a change to a file | No | Yes |
| Create, fork or change a repository | No | Yes |
| Show a pull request, a list of them or a diff as a view in the chat | Yes | No |
| Attach a pull request, a diff or a file as a resource, and start from a prompt | Yes | No |

[MCP Reference](reference/mcp-tools.md) lists every tool, resource and prompt of
bb's server. What its server leaves out, the `bb` command line does, for an
agent that can run commands.

## Limiting an agent

| To | bb | Atlassian's |
|---|---|---|
| Keep an agent from changing anything | Nothing to do: the server offers only the tools that read unless you start it with `--allow-writes` | Take the endpoints that write out of the whitelist, for everyone. A token that could only read was still offered the tools that create, comment on, approve and decline a pull request |
| Confine it to one project or repository | `--project` or `--repo`, whatever the token reaches | A token that reaches no further |
| Choose which tools it has | `--tools` and `--exclude`, for each server | The whitelist, for the instance |
| Keep approving to people | Nothing to do: no tool approves | Take `approve_pull_request` out of the whitelist |
| Mark what an agent wrote | bb ends it with a line saying an AI agent wrote it | The text is published as the agent sent it, under your name |
| Record what the agent did | `--audit-file` records every call, the refused ones too | Nothing documented for the server itself |
| Decide all this for everyone | System policy, on each machine | The whitelist, once, in Bitbucket |

Atlassian's limits are enforced inside Bitbucket, so nobody can configure
around them. bb's are set on the machine it runs on; against someone who edits
their own configuration, what holds is the token the server runs under.
[MCP Server Governance](advanced/mcp-governance.md) says what each of bb's
limits is worth.
