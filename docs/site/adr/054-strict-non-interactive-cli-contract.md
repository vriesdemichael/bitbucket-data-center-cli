# ADR 054: Strict non-interactive CLI contract and fail-fast validation invariant

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `054`
- Title: `Strict non-interactive CLI contract and fail-fast validation invariant`
- Category: `architecture`
- Status: `accepted`
- Provenance: `guided-ai`
- Source: `docs/decisions/054-strict-non-interactive-cli-contract.yaml`

## Decision

The `bb` CLI operates under a strict non-interactive contract across all commands and subcommands. Commands must never block on standard input (`os.Stdin` / `cmd.InOrStdin()`) to conduct interactive terminal surveys, question wizards, or confirmation prompts (such as `[y/N]` prompts).
1. Fail-Fast Validation: Any missing, incomplete, or ambiguous input (e.g. required positional arguments,
   mutually exclusive options, or absent mandatory flags) must immediately terminate execution with a
   non-zero exit code and an actionable error message detailing the missing flag, allowed values, and remediation.

2. Deterministic Confirmations: Destructive or mutating operations (e.g. `bb repo delete`, `bb branch delete`,
   `bb pr decline`) must never prompt for interactive confirmation. Safety against accidental execution
   is provided via explicit CLI flags (e.g. `--yes`, `--force`), explicit target arguments, or `--dry-run`
   pre-flight checks.

3. First-Class Agent & Automation Execution: Because `bb` treats AI agents and CI/CD pipelines as first-class
   operators (ADR-003), stdin blocking is treated as a severe bug (an indefinite hang). Commands must
   always be completely drivable via CLI arguments, flags, standard pipes, or environment variables.

## Agent Instructions

Never add interactive prompts, `bufio.Reader` scans, or prompt packages (like `survey`, `promptui`, or `huh`) to CLI command handlers. If a parameter is required, declare it as a required Cobra flag/argument or validate it explicitly and return a descriptive error. When writing tests or scripts, rely on explicit flags rather than expecting interactive input.

## Rationale

CLI tools designed for dual human and machine consumption frequently suffer when interactive prompts are interspersed in standard command paths. In automated scripts, headless CI/CD environments, and agentic workflows, a prompt on stdin causes the runner or agent to hang indefinitely until a timeout occurs.
While tools like GitHub CLI (`gh`) provide interactive surveys when arguments are omitted in an interactive TTY, such dual-mode behaviour adds significant state complexity and creates subtle failure modes across different terminal environments, subprocess wrappers, and IDE integrations. Enforcing a uniform fail-fast invariant guarantees predictability, robust scripting, deterministic testing, and zero-hang agentic execution.

## Rejected Alternatives

- `Introduce TTY-detected interactive surveys when arguments are omitted (gh parity)`: Bitbucket DC management workflows are heavily automated in CI/CD pipelines, cron jobs, and agent tools. Dual-mode TTY detection is notoriously fragile across Windows shells, SSH sessions, and multiplexers, often leading to silent hangs or unexpected exit codes. A strict fail-fast policy provides total determinism.
- `Interactive `[y/N]` confirmation prompts for destructive commands`: Prompts break automation unless an escape flag (`--yes` or `-y`) is supplied. Mandating explicit flags or `--dry-run` inspection upfront avoids hangs entirely without sacrificing safety.
