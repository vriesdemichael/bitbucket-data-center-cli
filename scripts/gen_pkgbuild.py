#!/usr/bin/env python3

import argparse
from pathlib import Path


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Generate an AUR PKGBUILD for the bb-bin package from release checksums.")
    parser.add_argument("--version", required=True, help="Release version without leading v, for example 1.2.3")
    parser.add_argument("--repository", required=True, help="GitHub repository in owner/name form")
    parser.add_argument("--sha256sums", required=True, help="Path to the sha256sums.txt downloaded from the release")
    parser.add_argument("--output", required=True, help="Path to the PKGBUILD file to write")
    return parser.parse_args()


def load_hashes(sums_path: Path) -> dict[str, str]:
    hashes: dict[str, str] = {}
    for raw_line in sums_path.read_text(encoding="utf-8").splitlines():
        line = raw_line.strip()
        if not line:
            continue
        digest, _, name = line.partition(" ")
        name = name.strip().lstrip("*")
        if name.startswith("./"):
            name = name[2:]
        if name:
            hashes[name] = digest
    return hashes


def hash_for(hashes: dict[str, str], version: str, target: str) -> str:
    filename = f"bb_{version}_{target}.tar.gz"
    try:
        return hashes[filename]
    except KeyError:
        raise SystemExit(f"Missing checksum for {filename} in the provided sha256sums file.")


def render_pkgbuild(version: str, repository: str, hashes: dict[str, str]) -> str:
    base_url = f"https://github.com/{repository}/releases/download/v{version}"
    repo_url = f"https://github.com/{repository}"
    sha_x86 = hash_for(hashes, version, "linux_amd64")
    sha_arm = hash_for(hashes, version, "linux_arm64")
    return f'''# Maintainer: Michael de Vries <vriesdemichael@users.noreply.github.com>
pkgname=bb-bin
pkgver={version}
pkgrel=1
pkgdesc="A CLI for Bitbucket Server / Bitbucket Data Center"
arch=('x86_64' 'aarch64')
url="{repo_url}"
license=('Apache-2.0')
provides=('bb')
conflicts=('bb')
source_x86_64=("bb-$pkgver-x86_64.tar.gz::{base_url}/bb_{version}_linux_amd64.tar.gz")
source_aarch64=("bb-$pkgver-aarch64.tar.gz::{base_url}/bb_{version}_linux_arm64.tar.gz")
sha256sums_x86_64=('{sha_x86}')
sha256sums_aarch64=('{sha_arm}')

package() {{
  install -Dm755 "$srcdir/bb" "$pkgdir/usr/bin/bb"
}}
'''


def main() -> None:
    args = parse_args()
    hashes = load_hashes(Path(args.sha256sums))
    pkgbuild = render_pkgbuild(args.version, args.repository, hashes)
    output = Path(args.output)
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(pkgbuild, encoding="utf-8")


if __name__ == "__main__":
    main()
