---
search:
  boost: 0.3
---

# ADR-019: Configuration and secret handling policy

Configuration is typed and validated when it loads, and a value that does not validate stops the command with a message that names the setting and carries no secret. A secret bb holds is never logged, printed, put in a JSON document or left in panic output, and diagnostics redact it. The one secret bb prints is a token `bb auth token create` has just made, because Bitbucket shows it once and the person has to keep it. A credential comes from the environment, which a `.env` file may fill, or from a host profile in the user's or the system file (ADR-021, ADR-047), never from the workspace file, which arrives with a clone.

A configuration file bb found and could not read is an error that names the file, never an empty configuration, and bb does not rewrite a file it could not read: no command does, and no flag makes it. `BB_DISABLE_STORED_CONFIG=1` means a command loads its configuration without the user's own file and without any stored credential.

A failure at load time is cheaper to trace than one halfway through a command. A damaged file read as an empty one tells the person they are not logged in, and the login that message suggests writes the file back with every other host gone.

## Not chosen

- **Best-effort validation and permissive startup**: Delays failures and makes them harder to trace.
- **Print secrets for troubleshooting**: A secret printed once is in a scrollback, a CI log or a support ticket from then on.
