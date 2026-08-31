# ADR 058: System-wide configuration and administrative policy enforcement

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `058`
- Title: `System-wide configuration and administrative policy enforcement`
- Category: `architecture`
- Status: `accepted`
- Provenance: `guided-ai`
- Source: `docs/decisions/058-system-wide-configuration-and-policy-enforcement.yaml`

## Decision

Establish a multi-tiered configuration hierarchy and machine-level administrative policy enforcement for enterprise fleet management across Linux, macOS, and Windows.
1. Multi-Tiered Precedence: CLI Flags > Environment Variables > Workspace Configuration
   (`.bb/config.yaml`) > User Configuration (`~/.config/bb/config.yaml` or `%APPDATA%\bb\config.yaml`) >
   System Configuration (`/etc/bb/config.yaml` or `%ProgramData%\bb\config.yaml`) > Built-in Defaults.
   Workspace configuration is located by traversing up from the working directory towards the repository
   root (`.git` or `go.mod`). `BB_WORKSPACE_CONFIG_PATH` overrides the workspace path, which carries no
   policy. `BB_SYSTEM_CONFIG_PATH` overrides the system path under `go test` only: the system file is the
   policy tier, and a released binary that relocated it on request would let anyone able to set an
   environment variable replace every policy below with a file of their own. On Windows the ProgramData
   directory holding that file is likewise resolved through the OS rather than read from `%ProgramData%`,
   for the same reason. Automation that needs different policy writes the real path.

2. Administrative Policy Invariants: System administrators can mandate security policies either via
   system configuration YAML (`/etc/bb/config.yaml` or `%ProgramData%\bb\config.yaml`) or native Windows
   Registry policy keys (`HKLM\Software\Policies\bb`). These policies take immutable precedence over
   user and workspace configurations:
   - `require_keyring: true`: Mandates OS keyring-backed credential storage machine-wide. Refuses fallback
      to plaintext config storage. Unsetting user environment variables cannot bypass this policy. If
      `BB_REQUIRE_KEYRING=0` is passed in user environment, a warning is printed to stderr and policy
      remains enforced.
   - `ca_file: <path>`: Mandates a corporate Root CA bundle. Defaulting to this CA when unspecified, and
      rejecting user attempts to supply a conflicting CA file.
   - `allowed_hosts: [...]`: Whitelists permitted Bitbucket Server / Data Center instances by URL or
      hostname. Connection attempts and login storage for unlisted hosts are rejected.
   - `allow_insecure_skip_verify: false`: Prohibits disabling TLS verification. Any attempt to pass
      `--insecure-skip-verify` or `BB_INSECURE_SKIP_VERIFY=true` is rejected.

3. Schema Validation: All configuration files (`/etc/bb/config.yaml`, `%ProgramData%\bb\config.yaml`,
   `.bb/config.yaml`, and user config) are validated against a versioned JSON Schema (`config.schema.json`)
   exported to `docs/reference/schemas/` to ensure syntax, types, and supported properties are strictly checked.

4. Error Handling Contract: All administrative policy violations are classified as `KindAuthorization`
   (exit code 3) or `KindPermanent` (exit code 1 for storage policy) with actionable guidance indicating
   that settings are governed by administrative policy.

5. Deployment: the policy directory is created by an administrator, not by `bb`. `bb` reads the system
   configuration file and never creates the directory holding it; the only directory it creates is the
   per-user one. That matters on Windows, where `C:\ProgramData` lets any user add a subdirectory and
   hands its creator full control of it -- so deployment creates `%ProgramData%\bb` from an elevated
   session and leaves unprivileged accounts read-only. On Linux and macOS `/etc` already requires root.
   TestPolicyLoadingNeverCreatesTheSystemConfigDirectory pins the `bb` half.

6. Registry policy carries every setting except `mcp_audit_file`. `HKLM\Software\Policies\bb` needs
   administrator rights and is merged last, so it is the stronger channel on Windows -- but
   parseRegistryPolicy has no branch for `mcp_audit_file`, which is therefore file-only, with point 5
   as the whole of what stands behind it.

## Agent Instructions

When evaluating configuration and options, always adhere to the 6-tier hierarchy (Flags > Env > Workspace > User > System > Defaults). Enforce administrative policies unconditionally before executing network or credential operations. Policy refusal errors must return KindAuthorization or KindPermanent with descriptive, actionable explanations. Do not add code that creates the system configuration directory. A convenience MkdirAll on the way to reading it would create that tier as whichever account ran bb first. When documenting a policy setting as one a user cannot change, name the deployment step that makes it true, and check the setting is readable from the channel you are recommending.

## Rationale

In enterprise deployments, IT security teams require authoritative control over CLI behavior across workstations and CI/CD agents. Previously, configuration was loaded strictly from the environment or the user's home directory (`~/.config/bb/config.yaml`), allowing operators to bypass corporate CA bundles, disable TLS verification via `BB_INSECURE_SKIP_VERIFY=true`, or store credentials insecurely when the OS keyring failed.
By supporting system-wide configuration (`/etc/bb/config.yaml`, `%ProgramData%\bb\config.yaml`) and Windows Group Policy (`HKLM\Software\Policies\bb`), organizations deploying via Ansible, Jamf, Intune, or GPO can enforce non-negotiable security postures without interfering with team-level workspace settings or user convenience profiles. Points 5 and 6 came from a report that bb trusts the policy file without checking its owner. The remedy was wrong -- an application does not audit who may write a machine-wide path, and none of /etc's other consumers do -- but it established that the directory does not exist until somebody creates it, and that on Windows that somebody need not be an administrator. The answer is a deployment step, not a check inside bb. The registry advice that came with it was wrong too, which is why point 6 states the parity rather than assuming it.

## Rejected Alternatives

- `Only support environment variables for policy overrides`: Environment variables can be easily overwritten or unset by unprivileged users in user-space shells, defeating fleet-wide security enforcement.
- `Rely exclusively on system-level configuration files without Windows Registry support`: Windows enterprise fleet management relies heavily on Group Policy Objects (GPO) and Intune CSPs targeting HKLM\Software\Policies. Restricting policy to flat files would require custom scripting rather than standard GPO.
- `Check the owner and mode of the policy file before trusting it`: Polices an OS administration problem from inside an application, and would have to decide what a correct owner is on Windows, where the answer is an ACL rather than a uid.
- `Have bb create the system configuration directory on first run`: On Windows it would then be created by the first unprivileged account to run bb, which would own the tier that outranks its own configuration.
