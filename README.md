# Bitbucket Server CLI (`bb`)

[![codecov](https://codecov.io/gh/vriesdemichael/bitbucket-server-cli/branch/main/graph/badge.svg?flag=combined_scoped)](https://codecov.io/gh/vriesdemichael/bitbucket-server-cli?flag=combined_scoped)

`bb` is a production-focused CLI for automating Bitbucket Server/Data Center workflows.
It combines scriptable machine output, safe dry-run planning, and high-coverage live-behavior
validation against real Bitbucket APIs.

It is designed as the `gh`-style CLI experience for Bitbucket Server/Data Center, including
repository cloning and browser navigation ergonomics tailored to Bitbucket-hosted projects.

## Why teams adopt `bb`

- **Operationally safe by default**: dry-run planning for server mutations and explicit bulk plan/apply workflows.
- **Automation friendly**: stable JSON envelope contract (`bb.machine`, `v2`) for CI/CD and internal tooling.
- **Spec-driven API interactions**: client/server interactions are derived from Bitbucket Server's official OpenAPI spec.
- **Git-native ergonomics**: repository discovery from matching remotes to reduce repetitive `--repo` usage.
- **Enterprise-ready auth model**: token/basic auth with persisted server contexts and secure credential handling.
- **Live-tested command behavior**: command workflows are validated against a real Bitbucket Data Center server, not mocks alone.

## What you can do with it

- Manage repositories, permissions, hooks, branches, tags, commits, and refs.
- Work with pull requests, comments, build statuses, and merge checks.
- Run project/admin operations and cross-repository search.
- Apply policy-driven multi-repository changes via bulk plan/review/apply workflows.
- Clone repositories and open repository pages quickly (`bb repo clone`, `bb browse`).

## Quick start

Install on Windows via WinGet:

```powershell
winget install vriesdemichael.bb
```

Install on Windows via Scoop:

```powershell
scoop bucket add vriesdemichael https://github.com/vriesdemichael/scoop
scoop install vriesdemichael/bb
```

Install on macOS or Linux via Homebrew:

```bash
brew install vriesdemichael/tap/bb
```

Install on Arch Linux from the AUR:

```bash
yay -S bb-bin
```

Install on Debian/Ubuntu or RHEL/Fedora from the release `.deb`/`.rpm`:

```bash
VERSION=v1.0.0
# Debian/Ubuntu
curl -LO "https://github.com/vriesdemichael/bitbucket-server-cli/releases/download/${VERSION}/bb_${VERSION#v}_linux_amd64.deb"
sudo dpkg -i "bb_${VERSION#v}_linux_amd64.deb"
# RHEL/Fedora
curl -LO "https://github.com/vriesdemichael/bitbucket-server-cli/releases/download/${VERSION}/bb_${VERSION#v}_linux_amd64.rpm"
sudo rpm -i "bb_${VERSION#v}_linux_amd64.rpm"
```

Install from Releases (Linux amd64 example):

```bash
VERSION=v1.0.0
curl -LO "https://github.com/vriesdemichael/bitbucket-server-cli/releases/download/${VERSION}/bb_${VERSION#v}_linux_amd64.tar.gz"
curl -LO "https://github.com/vriesdemichael/bitbucket-server-cli/releases/download/${VERSION}/sha256sums.txt"
sha256sum -c sha256sums.txt --ignore-missing
tar -xzf "bb_${VERSION#v}_linux_amd64.tar.gz"
install -m 0755 bb /usr/local/bin/bb
```

**Authenticate** — store a token for your Bitbucket instance:

```bash
bb auth login --host https://bitbucket.acme.corp --token "$BB_TOKEN"
bb auth status
```

```text
Target Bitbucket: https://bitbucket.acme.corp (expected version 9.4.16, auth=token, source=stored/default)
```

**Clone and browse** — no need to look up URLs:

```bash
bb repo clone PLATFORM/api
bb browse --repo PLATFORM/api
```

```text
Cloning into 'api'...
Cloned PLATFORM/api into api
# browse opens https://bitbucket.acme.corp/projects/PLATFORM/repos/api in your browser
```

**Search** — find repositories across all projects:

```bash
bb search repos --limit 20
```

```text
PLATFORM/api      API Service
PLATFORM/backend  Backend Service
PLATFORM/web      Frontend Web App
```

**Machine mode** — stable JSON envelope for scripting and CI/CD:

```bash
bb --json auth status
```

```json
{
  "version": "v2",
  "data": {
    "bitbucket_url": "https://bitbucket.acme.corp",
    "bitbucket_version_target": "9.4.16",
    "auth_mode": "token",
    "auth_source": "stored/default"
  },
  "meta": {
    "contract": "bb.machine"
  }
}
```

## Docs

- Full docs site: <https://vriesdemichael.github.io/bitbucket-server-cli/latest/>
- LLM guide (`llms.txt`): <https://vriesdemichael.github.io/bitbucket-server-cli/latest/llms.txt>
- AI and llms.txt overview: [docs/site/ai-and-llms.md](docs/site/ai-and-llms.md)
- Installation and Quickstart: [docs/site/installation-and-quickstart.md](docs/site/installation-and-quickstart.md)
- Basic Usage: [docs/site/basic-usage.md](docs/site/basic-usage.md)
- Advanced Topics: [docs/site/advanced/index.md](docs/site/advanced/index.md)
- Command Reference (generated): [docs/site/reference/commands/index.md](docs/site/reference/commands/index.md)
- ADR Index: [docs/site/adr/index.md](docs/site/adr/index.md)

## Compatibility and contracts

- Primary target: Atlassian Bitbucket Data Center `9.4.x`
- API contract source: Atlassian Bitbucket `9.4` OpenAPI (`docs/reference/atlassian/bitbucket-9.4-openapi.json`)
- CLI identity and machine contract: `bb` / `bb.machine` `v2`
- JSON schemas for bulk policy/plan/status published in docs and versioned with releases

## For contributors

This README is an adopter-focused landing page.

- Development workflows and project tasks: `Taskfile.yml`
- Decision records: `docs/decisions/`
- Generated docs and docs tooling: `docs/site/`, `tools/cli-docs-export/`, `tools/adr-markdown-export/`

## License and platform note

Atlassian Bitbucket Server/Data Center is proprietary software.
Use of local Docker images and server instances must comply with Atlassian licensing terms.
