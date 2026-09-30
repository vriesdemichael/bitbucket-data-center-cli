# Fleet Deployment

Recipes for deploying `bb` and its policy to macOS, Linux and Windows workstations and to CI, how to verify a machine afterwards, and the work that follows: rotating tokens, upgrading, and removing `bb`. [Fleet Controls](fleet-controls.md) explains the settings the recipes deploy.

## macOS (Jamf Pro / Kandji / Intune)

macOS developer workstations authenticate through the **Apple Keychain** (Security framework). Deploy system configuration to `/etc/bb/config.yaml` so that all terminal sessions and GUI applications (like VS Code or Cursor invoking MCP servers) automatically inherit policy without relying on shell environment inheritance:

```bash
# 1. Distribute via Homebrew or universal binary
brew install vriesdemichael/tap/bb

# 2. Deploy Enterprise Root CA to System Keychain
sudo security add-trusted-cert -d -r trustRoot \
  -k /Library/Keychains/System.keychain \
  /Library/Application\ Support/Corporate/Certs/corp-root-ca.pem

# 3. Deploy Immutable System Configuration (/etc/bb/config.yaml)
# Note: Unlike /etc/zshenv, /etc/bb/config.yaml is read directly by bb in GUI IDEs as well.
sudo mkdir -p /etc/bb
sudo tee /etc/bb/config.yaml >/dev/null <<'EOF'
require_keyring: true
ca_file: /Library/Application Support/Corporate/Certs/corp-root-ca.pem
allowed_hosts:
  - https://bitbucket.example.com
allow_insecure_skip_verify: false
allow_http_update: false
disable_update: true
EOF
sudo chmod 644 /etc/bb/config.yaml
```


## Linux Workstations (Ansible)

Linux workstations authenticate through the **Secret Service API over D-Bus** (GNOME Keyring / KWallet). Deploy `/etc/bb/config.yaml` to enforce security postures across all local users:

```yaml
- name: Deploy and harden bb across Linux workstations
  hosts: workstations
  become: true
  vars:
    bb_version: "[[ bb_version ]]"
  tasks:
    - name: Deploy Corporate Root CA bundle
      copy:
        src: files/corp-root-ca.pem
        dest: /etc/ssl/certs/corp-root-ca.pem
        owner: root
        group: root
        mode: '0644'

    - name: Download verified bb Debian package
      get_url:
        url: "https://artifactory.example.com/binaries/bb_{{ bb_version }}_linux_amd64.deb"
        dest: "/tmp/bb_{{ bb_version }}_linux_amd64.deb"
        mode: '0644'
      when: ansible_os_family == "Debian"

    - name: Install bb package (Debian/Ubuntu)
      apt:
        deb: "/tmp/bb_{{ bb_version }}_linux_amd64.deb"
      when: ansible_os_family == "Debian"

    - name: Deploy system-wide policy configuration
      copy:
        dest: /etc/bb/config.yaml
        owner: root
        group: root
        mode: '0644'
        content: |
          require_keyring: true
          ca_file: /etc/ssl/certs/corp-root-ca.pem
          allowed_hosts:
            - https://bitbucket.example.com
          allow_insecure_skip_verify: false
          allow_http_update: false
          disable_update: true
          update_base_url: https://artifactory.example.com/artifactory/bb-releases
```


## Windows Workstations (Microsoft Intune / PowerShell / GPO)

Windows workstations authenticate through **Windows Credential Manager** (DPAPI). Administrators can deploy system configuration via `%ProgramData%\bb\config.yaml` or through native Windows Group Policy / Intune CSP targeting the Windows Registry (`HKLM\Software\Policies\bb`):

```powershell
# Run as Administrator via Intune or administrative PowerShell
$Version = "[[ bb_version ]]"

# 1. Install via WinGet
winget install --id vriesdemichael.bb --exact --version $Version --accept-source-agreements --accept-package-agreements

# 2. Deploy Corporate Root CA
$CertDir = "C:\ProgramData\Corporate\Certs"
New-Item -ItemType Directory -Force -Path $CertDir | Out-Null
Copy-Item ".\corp-root-ca.pem" -Destination "$CertDir\corp-root-ca.pem"

# 3. Option A: Deploy System Configuration File (%ProgramData%\bb\config.yaml)
#    Create this directory as an administrator before any developer runs bb. C:\ProgramData
#    lets an unprivileged account create a subdirectory and become its owner, and bb never
#    creates the directory itself (ADR-058, point 5). The ACL below removes the inherited
#    Users write entry so the policy file cannot be replaced by the accounts it governs.
$ConfigDir = "C:\ProgramData\bb"
New-Item -ItemType Directory -Force -Path $ConfigDir | Out-Null
$Acl = Get-Acl $ConfigDir
$Acl.SetAccessRuleProtection($true, $false)
foreach ($Identity in "SYSTEM", "Administrators") {
    $Acl.AddAccessRule((New-Object Security.AccessControl.FileSystemAccessRule(
        $Identity, "FullControl", "ContainerInherit,ObjectInherit", "None", "Allow")))
}
$Acl.AddAccessRule((New-Object Security.AccessControl.FileSystemAccessRule(
    "Users", "ReadAndExecute", "ContainerInherit,ObjectInherit", "None", "Allow")))
Set-Acl -Path $ConfigDir -AclObject $Acl
@"
require_keyring: true
ca_file: $CertDir\corp-root-ca.pem
allowed_hosts:
  - https://bitbucket.example.com
allow_insecure_skip_verify: false
allow_http_update: false
disable_update: true
update_base_url: https://artifactory.example.com/artifactory/bb-releases
"@ | Set-Content -Path "$ConfigDir\config.yaml" -Encoding UTF8

# 4. Option B: Native Windows Registry GPO Policies (HKLM\Software\Policies\bb)
$RegPath = "HKLM:\Software\Policies\bb"
if (!(Test-Path $RegPath)) { New-Item -Path $RegPath -Force | Out-Null }
Set-ItemProperty -Path $RegPath -Name "RequireKeyring" -Value 1 -Type DWord
Set-ItemProperty -Path $RegPath -Name "CAFile" -Value "$CertDir\corp-root-ca.pem" -Type String
Set-ItemProperty -Path $RegPath -Name "AllowedHosts" -Value "https://bitbucket.example.com" -Type String
Set-ItemProperty -Path $RegPath -Name "AllowInsecureSkipVerify" -Value 0 -Type DWord
Set-ItemProperty -Path $RegPath -Name "AllowHTTPUpdate" -Value 0 -Type DWord
Set-ItemProperty -Path $RegPath -Name "DisableUpdate" -Value 1 -Type DWord
# Optional: no MCP server, or a read-only bb (ADR-100)
# Set-ItemProperty -Path $RegPath -Name "DisableMCPServer" -Value 1 -Type DWord
# Set-ItemProperty -Path $RegPath -Name "ReadOnly" -Value 1 -Type DWord
```


## CI/CD & Headless Containers (Docker / Kubernetes)

Headless runners do not have interactive desktop sessions or D-Bus daemons. Configure runners to read secrets entirely from process memory and bypass disk storage.

```dockerfile
# Hardened CI Container Pattern
FROM alpine:3.21

ARG BB_VERSION=[[ bb_version ]]

# Install runtime dependencies (ca-certificates and git)
RUN apk add --no-cache ca-certificates git curl

# Download and install official release binary
RUN curl -fsSL "https://github.com/vriesdemichael/bitbucket-data-center-cli/releases/download/v${BB_VERSION}/bb_${BB_VERSION}_linux_amd64.tar.gz" \
    | tar -xz -C /usr/local/bin bb \
    && chmod +x /usr/local/bin/bb

# Install corporate CA
COPY corp-root-ca.pem /etc/ssl/certs/corp-root-ca.pem

# Environment flags for headless isolation
ENV BB_CA_FILE=/etc/ssl/certs/corp-root-ca.pem
ENV BB_DISABLE_STORED_CONFIG=1

# Execution in CI: pass the token through the environment, so nothing is written to disk
# docker run --rm -e BITBUCKET_TOKEN=$SECRET -e BITBUCKET_URL=https://bitbucket.example.com my-image bb repo list
```



## Verification Audit
Run the machine-readable auth status check to verify configuration:

```bash
bb auth status --json
```

Confirm:
- `.data.credentialStorage`: Must report `keyring` (on workstations) or `environment` (in CI). If it reports `config-file-plaintext`, `BB_REQUIRE_KEYRING=1` is missing.
- Check active git helper for your Bitbucket host:
  ```bash
  git config --global --get "credential.https://bitbucket.example.com.helper"
  ```

Check the deployed configuration itself. `bb doctor` needs no host and no login, so it runs as soon as the file is in place:

```bash
bb doctor --json
```

Confirm:
- It exits `0`. Any issue exits `1`, and the output is then the failure envelope rather than the report, with each issue in `.error.details` under its own key: `violation/system/policies/require_keyrng` for a key the schema rejects, `ignored/stored/require_keyring` for a policy key in a user's own file, which mandates nothing.
- Each policy setting you deployed has a `source.kind` of `system` or `registry` in `.data.settings`, not `default`.

## Helpdesk Troubleshooting Guide

<!-- docs-lint: message-of bb crypto/x509 -->

| Symptom / Error Message | Root Cause | Remediation |
|---|---|---|
| `read CA bundle: open ...: no such file or directory` | Imaging race condition: `BB_CA_FILE` was set before the CA certificate was written to disk. | Ensure the provisioning script copies the `.pem` file before setting the environment variable. |
| `OS keyring is unavailable and keyring-backed storage is required` | A headless Linux host or SSH session: no D-Bus session bus, or no Secret Service provider on it. | Start both, as [Troubleshooting](../troubleshooting.md#os-keyring-is-unavailable-and-keyring-backed-storage-is-required) shows, or supply credentials via `BITBUCKET_TOKEN`. |
| Git prompts for password on `git push`/`git pull` | Git credential helper is not scoped to the exact URL or scheme used by the remote. | Run `git remote -v` and configure: `bb auth setup-git --host <remote-url>`. |
| `certificate signed by unknown authority` | `BB_CA_FILE` is not set, or a GUI IDE failed to inherit shell environment variables. | Set `BB_CA_FILE` in the IDE's `"env"` block or export it in `/etc/zshenv` / `/etc/profile.d/bb.sh`. |
| `host "..." is not permitted by administrative policy` | Target Bitbucket instance is not listed in `allowed_hosts` in system configuration or registry policy. | Connect only to approved corporate hosts, or request security to add the instance to `allowed_hosts`. |
| `insecure TLS verification is disabled by administrative policy` | Attempted `--insecure-skip-verify` when prohibited by `allow_insecure_skip_verify: false` in system policy. | Configure the corporate CA certificate rather than disabling TLS verification. |
| `overriding CA bundle is disabled by administrative policy` | Attempted to override mandated corporate CA bundle with a conflicting custom certificate. | Remove user-level `BB_CA_FILE` override and use the mandated corporate CA. |
| `self-update is disabled by administrative policy` | Self-update is disabled machine-wide (`disable_update: true` or `BB_DISABLE_UPDATE=1`). | Update `bb` through your IT system package manager (`apt`, `dnf`, `brew`, `winget`). |
| `bb is disabled on this machine by administrative policy` | Policy sets `disable_bb`. | Ask the administrator named by the message's file or registry key. `bb doctor` still runs and shows the setting. |
| `bb's MCP server is disabled on this machine by administrative policy` | Policy sets `disable_mcp_server`. | Use `bb` in the terminal; no MCP client may start it on this machine. |
| `changes Bitbucket, and bb is read-only on this machine by administrative policy` | Policy sets `read_only`, and the command would change something in Bitbucket. | Reads and `--dry-run` still work. Make the change in Bitbucket itself, or ask the administrator. |
| `could not load the Sigstore trust material needed to verify the release manifest` | The host cannot reach `https://tuf-repo-cdn.sigstore.dev`, and no offline trust root is configured. The release itself is not implicated. | Deploy a `trusted_root.json` and set `update_trusted_root` in system configuration (or `update_tuf_url` for a mirrored TUF repository). |
| `update_trusted_root is invalid` | The configured trusted root path does not exist on this host — typically an imaging race, the same one that bites `ca_file`. | Ensure the provisioning script writes `trusted_root.json` before the configuration file that references it. |
| `update_trusted_root and update_tuf_url are mutually exclusive` | Both Sigstore trust sources are configured. | Keep the trusted root file for air-gapped hosts, or the TUF mirror URL — not both. |
| `the system configuration at ... could not be read` | The system configuration file is malformed, typically from a provisioning template or a partial write. bb fails closed rather than run without the policy. | Run `bb doctor` on the host: it lists every problem in the file with its line (see [Checking the configuration](../troubleshooting.md#checking-the-configuration)). Redeploy the corrected file. Users cannot work around it, by design. |
| A policy setting is not enforced | The key is in a user or workspace configuration file, which bb reads no policy from. | Run `bb doctor`: it lists the key as `ignored` in that file, and names the source each policy setting comes from. Move the key to the system configuration. |
| `update_tuf_url must be an absolute https URL` | The configured mirror is a bare hostname, a relative path, or plain `http`. | Give the full origin, for example `https://artifactory.example.com/tuf`. |
| `uses plain HTTP; pass --allow-http or set BB_ALLOW_HTTP_UPDATE=1 to permit it` | The update base URL, an asset URL in the mirror's manifest, or a redirect uses `http://`. | Serve the mirror over `https`, or opt in explicitly for a mirror that has no TLS. |
| `plain-HTTP update URLs are disabled by administrative policy` | `--allow-http` or `BB_ALLOW_HTTP_UPDATE` was set on a host whose policy sets `allow_http_update: false`. | Serve the mirror over `https`; the policy exists so the fleet cannot fall back to plain HTTP. |
| `uses plain HTTP, which administrative policy forbids` | An update URL uses `http://` and policy sets `allow_http_update: false`. | Serve the mirror over `https`. |



## PAT Expiration & Rotation
Personal Access Tokens expire based on enterprise TTL policies (e.g. 90 days). When rotating a token:
1. Generate a replacement token in Bitbucket (`bb auth token create "Dev Token" --expiry-days 90` or via web UI).
2. Update the stored credential in the OS keyring without downtime:
   ```bash
   printf "%s" "$NEW_TOKEN" | bb auth login https://bitbucket.example.com --token-stdin
   ```
3. Because git queries `bb` dynamically, all local repositories immediately begin using the new token without needing `.git/config` updates.

## Fleet Upgrades & Rollback
- **Upgrades**: Deploy new packages via system package managers (`apt`, `dnf`, `brew`, `winget`). Stored credentials and git helpers persist across version upgrades.
- **De-provisioning & Rollback**:
  ```bash
  # 1. Log out and remove secrets from the OS Keyring
  bb auth logout --host https://bitbucket.example.com

  # 2. Remove git credential helper configuration
  git config --global --unset-all "credential.https://bitbucket.example.com.helper"

  # 3. Remove package
  apt remove bb   # or: brew uninstall bb / winget uninstall vriesdemichael.bb
  ```


