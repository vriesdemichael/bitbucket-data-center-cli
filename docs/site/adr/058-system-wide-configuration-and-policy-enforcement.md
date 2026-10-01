---
search:
  boost: 0.3
---

# ADR-058: System-wide configuration and administrative policy enforcement

bb reads configuration from a workspace, a user and a system file, on Linux, macOS and Windows, and an administrator sets policy in the system tier, out of a user's reach.

1. Precedence. A setting is taken from the first of these that sets it: a flag, the environment, the workspace file, the user's file, the system file, the built-in default. The workspace file is `.bb/config.yaml`, found by walking up from the working directory to the repository root (`.git` or `go.mod`), or the file `BB_WORKSPACE_CONFIG_PATH` names; it carries no policy. The user's file is `bb/config.yaml` in the OS user configuration directory, or the file `BB_CONFIG_PATH` names. The system file is `/etc/bb/config.yaml`, or `%ProgramData%\bb\config.yaml` on Windows. A host's profile, with its username, client certificate and stored credential, is the exception: it is looked up in the user's file first, then the workspace file, then the system file, and a workspace profile gives only its `url` and username (ADR-021). `BB_SYSTEM_CONFIG_PATH` names another system file under `go test` only, and on Windows the ProgramData directory is asked of the OS rather than read from `%ProgramData%`. The system file is the policy tier, and a released binary that let the environment relocate it would let anyone able to set a variable replace every policy with a file of their own. Automation that needs different policy writes the real path.

2. Policy. An administrator sets policy in the system file, at its top level or under `policies:` or `policy:`, and on Windows in the registry under `HKLM\Software\Policies\bb`, which is merged last. A policy key in a user's or a workspace file is ignored, and `bb doctor` names it, except `update_base_url`, which every file may set (ADR-059). Four keys are this record's, and each outranks flags, the environment and every other file. `require_keyring: true` mandates keyring-backed credential storage (ADR-047), and `BB_REQUIRE_KEYRING=0` does not lift it: bb warns on stderr and keeps enforcing. `ca_file` mandates a CA bundle: it is used when none is given, and a different one is refused. `allowed_hosts` lists the instances bb may reach, by URL or host name, and a command aimed at any other host, or a login for one, is refused. `allow_insecure_skip_verify: false` refuses `--insecure-skip-verify` and `BB_INSECURE_SKIP_VERIFY=true`. The update controls are ADR-059's, `mcp_audit_file` is ADR-062's, and `disable_bb`, `disable_mcp_server` and `read_only` are ADR-100's. A boolean registry value bb cannot read takes the restrictive side of its control, and `bb doctor` reports it.

3. Schema validation. Every configuration file is checked against the configuration schema compiled into bb and exported as `docs/reference/schemas/config.schema.json`. A file that does not match it is a file bb could not read (ADR-019).

4. Errors. A policy refusal is `KindAuthorization`, exit 3, and says administrative policy refused it. A refused plaintext fallback is `KindPermanent`, exit 1, and says how to supply the credential instead. A system file that exists and cannot be read fails closed: every command except the few ADR-100 leaves standing stops with `KindPermanent`, names the file, and tells the person to ask the administrator rather than remove it.

5. Deployment. An administrator creates the policy directory. bb reads the system file and never creates the directory holding it; the only configuration directory bb creates is the user's own. On Windows, `C:\ProgramData` lets any user add a subdirectory and hands its creator full control of it, so deployment creates `%ProgramData%\bb` from an elevated session and leaves unprivileged accounts read-only. On Linux and macOS `/etc` already requires root. `TestPolicyLoadingNeverCreatesTheSystemConfigDirectory` holds the bb half.

6. Registry parity. The registry carries every policy key except `mcp_audit_file`, which only the system file sets, so on Windows point 5 is the whole of what stands behind it.

Apply policy before any network or credential operation. Do not add code that creates the system configuration directory: a convenience `MkdirAll` on the way to reading it would create that tier as whichever account ran bb first. When documenting a policy setting as one a user cannot change, name the deployment step that makes it true, and check that the setting is readable from the channel you recommend.

An organisation that deploys bb through Ansible, Jamf, Intune or Group Policy needs controls a user cannot switch off: a corporate CA bundle, TLS verification, keyring storage, the hosts bb may reach. The environment and the user's own file belong to the user, and a workspace file arrives with a clone, so policy lives where only an administrator writes, and outranks all three. That only an administrator writes there is the deployment's to make true; bb cannot check it from inside.

## Not chosen

- **Only support environment variables for policy**: An unprivileged user can set or unset them in their own shell, defeating fleet-wide enforcement.
- **System files only, without the Windows registry**: Windows fleets are managed through Group Policy and Intune, which target `HKLM\Software\Policies`. Flat files alone would need custom scripting rather than standard GPO.
- **Check the owner and mode of the policy file before trusting it**: Polices an operating system administration problem from inside an application, and would have to decide what a correct owner is on Windows, where the answer is an ACL rather than a uid.
- **Have bb create the system configuration directory on first run**: On Windows it would then be created by the first unprivileged account to run bb, which would own the tier that outranks its own configuration.
