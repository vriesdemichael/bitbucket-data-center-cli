---
hide:
  - toc
---

<section class="bb-hero">
	<p class="bb-eyebrow">Bitbucket Data Center</p>
	<h1>The operational CLI for Bitbucket Data Center.</h1>
	<p class="bb-lead">
		<code>bb</code> gives Bitbucket Data Center teams a safer command line:
		machine-readable output, dry-run planning, repository-aware ergonomics, and behavior validated against a real server.
	</p>
	<div class="bb-actions">
		<a class="md-button md-button--primary" href="installation-and-quickstart/">Get Started</a>
		<a class="md-button" href="reference/commands/">Browse Commands</a>
		<a class="md-button" href="advanced/">See Safety Model</a>
	</div>
</section>

<section class="bb-grid bb-grid-3">
	<article class="bb-card bb-card-accent">
		<p class="bb-card-kicker">Operational Safety</p>
		<h2>Plan server changes before you apply them</h2>
		<p>Dry-run planning and bulk review/apply workflows reduce the usual risk of shelling directly into enterprise Bitbucket instances.</p>
	</article>
	<article class="bb-card">
		<p class="bb-card-kicker">Automation Contract</p>
		<h2>Stable output for CI, tooling, and agents</h2>
		<p>The CLI exposes a versioned <code>bb.machine</code> envelope so scripts, pipelines, and LLM agents are not parsing human prose.</p>
	</article>
	<article class="bb-card">
		<p class="bb-card-kicker">Live-Tested</p>
		<h2>Validated against real Bitbucket behavior</h2>
		<p>Command workflows are validated against Bitbucket Data Center APIs, which keeps the docs and the binary grounded in real platform behavior.</p>
	</article>
</section>

## Start here

<section class="bb-grid bb-grid-2">
	<article class="bb-card bb-link-card">
		<p class="bb-card-kicker">Install</p>
		<h2><a href="installation-and-quickstart/">Installation and Quickstart</a></h2>
		<p>Download a release, verify checksums, install <code>bb</code>, authenticate with a PAT, and run the first useful commands.</p>
	</article>
	<article class="bb-card bb-link-card">
		<p class="bb-card-kicker">Operate</p>
		<h2><a href="basic-usage/">Basic Usage</a></h2>
		<p>Learn the command discovery pattern, repository inference rules, dry-run behavior, and the JSON machine contract.</p>
	</article>
	<article class="bb-card bb-link-card">
		<p class="bb-card-kicker">Reference</p>
		<h2><a href="reference/commands/">All Commands</a></h2>
		<p>Use the generated command tree as the exact public surface area for every command, argument, and flag.</p>
	</article>
	<article class="bb-card bb-link-card">
		<p class="bb-card-kicker">Automate</p>
		<h2><a href="advanced/">Advanced Topics</a></h2>
		<p>Dive into repository discovery, dry-run semantics, bulk operations, and diagnostics for higher-trust automation.</p>
	</article>
</section>

## What this docs site contains

<section class="bb-grid bb-grid-2">
	<article class="bb-panel">
		<h2>Operator-facing guidance</h2>
		<ul>
			<li><a href="installation-and-quickstart/">Installation and Quickstart</a></li>
			<li><a href="basic-usage/">Basic Usage</a></li>
			<li><a href="advanced/">Advanced Topics</a></li>
			<li><a href="reference/schemas/">JSON Schemas</a></li>
		</ul>
	</article>
	<article class="bb-panel">
		<h2>Source-of-truth generated reference</h2>
		<ul>
			<li><a href="reference/overview/">Command Reference Overview</a></li>
			<li><a href="reference/commands/">Complete CLI command reference</a></li>
			<li><a href="ai-and-llms/">AI and llms.txt guide</a></li>
			<li><a href="adr/">Architecture decision records</a></li>
			<li><a href="changelog/">Release changelog</a></li>
		</ul>
	</article>
</section>

## Documentation model

- Command and ADR pages are generated from source-of-truth code and decision records.
- Bulk policy, plan, and apply schemas are generated from validated workflow models.
- The hand-written docs focus on usage patterns, safety contracts, and operator workflows around the CLI.
