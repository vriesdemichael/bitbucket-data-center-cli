#!/usr/bin/env python3

import argparse
import json
import re
from pathlib import Path
from typing import cast


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Render the docs changelog page from GitHub releases JSON.")
    parser.add_argument("--releases-json", required=True, help="Path to a JSON array returned by the GitHub releases API")
    parser.add_argument("--releases-page-url", required=True, help="URL of the repository releases page")
    parser.add_argument("--output", required=True, help="Path to the markdown file to write")
    return parser.parse_args()


def load_releases(releases_path: Path) -> list[dict[str, object]]:
    try:
        payload = json.loads(releases_path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as exc:
        raise SystemExit(f"Failed to decode releases JSON from {releases_path}: {exc}") from exc

    if not isinstance(payload, list):
        raise SystemExit(
            f"Expected {releases_path} to contain a JSON array of releases, got {type(payload).__name__}."
        )

    releases: list[dict[str, object]] = []
    for index, release in enumerate(payload):
        if not isinstance(release, dict):
            raise SystemExit(
                f"Expected release entry {index} in {releases_path} to be an object, got {type(release).__name__}."
            )
        releases.append(cast(dict[str, object], release))

    return releases


def strip_duplicate_heading(tag: str, body: str) -> str:
    text = (body or "").strip()
    if not text:
        return ""

    lines = text.splitlines()
    first_non_empty = next((index for index, line in enumerate(lines) if line.strip()), None)
    if first_non_empty is None:
        return ""

    heading_pattern = re.compile(rf"^#{{1,6}}\s+{re.escape(tag)}\s*$")
    if heading_pattern.match(lines[first_non_empty].strip()):
        lines = lines[first_non_empty + 1 :]
        while lines and not lines[0].strip():
            lines.pop(0)

    return "\n".join(lines).strip()


def flatten_body_headings(body: str) -> str:
    text = body.strip()
    if not text:
        return ""

    flattened: list[str] = []
    heading_pattern = re.compile(r"^(#{1,6})\s+(.+?)\s*$")

    for line in text.splitlines():
        match = heading_pattern.match(line.strip())
        if not match:
            flattened.append(line)
            continue

        label = match.group(2).strip()
        if not label:
            continue

        if flattened and flattened[-1].strip():
            flattened.append("")
        flattened.append(f"**{label}**")

    return "\n".join(flattened).strip()


BARE_SHA = re.compile(r"\(([0-9a-f]{7,40})\)")


def link_bare_shas(body: str, repository_url: str) -> str:
    # tools/release-notes names each commit by its bare short SHA, which GitHub
    # links in a release body and mkdocs does not. Older bodies carry a written
    # link, "([abc1234](...))", which this leaves alone: a "[" follows the "(".
    return BARE_SHA.sub(lambda match: f"([{match.group(1)}]({repository_url}/commit/{match.group(1)}))", body)


def render_release(release: dict[str, object], repository_url: str) -> list[str]:
    tag = str(release.get("tag_name") or release.get("name") or "Unversioned release")
    url = str(release.get("html_url") or "").strip()
    published_at = str(release.get("published_at") or "").strip()
    body = flatten_body_headings(strip_duplicate_heading(tag, str(release.get("body") or "")))
    body = link_bare_shas(body, repository_url)

    heading = f"## [{tag}]({url})" if url else f"## {tag}"
    lines = [heading, ""]

    if published_at:
        lines.append(f"Published: {published_at[:10]}")
        lines.append("")

    if body:
        lines.append(body)
    else:
        lines.append("Release notes are available on GitHub.")

    lines.append("")
    return lines


def main() -> None:
    args = parse_args()
    releases_path = Path(args.releases_json)
    output_path = Path(args.output)

    releases = load_releases(releases_path)
    published_releases = [release for release in releases if not release.get("draft")]

    lines = ["# Changelog", ""]
    if not published_releases:
        lines.append("Release notes are published on the GitHub Releases page as versions become available.")
        lines.append("")
        lines.append(f"- GitHub Releases: {args.releases_page_url}")
    else:
        repository_url = args.releases_page_url.rstrip("/").removesuffix("/releases")
        for release in published_releases:
            lines.extend(render_release(release, repository_url))

    output_path.parent.mkdir(parents=True, exist_ok=True)
    output_path.write_text("\n".join(lines).rstrip() + "\n", encoding="utf-8")


if __name__ == "__main__":
    main()