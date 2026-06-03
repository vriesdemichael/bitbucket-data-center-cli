#!/usr/bin/env python3

import argparse
from pathlib import Path


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Generate a Homebrew formula for the bb CLI from release checksums.")
    parser.add_argument("--version", required=True, help="Release version without leading v, for example 1.2.3")
    parser.add_argument("--repository", required=True, help="GitHub repository in owner/name form")
    parser.add_argument("--sha256sums", required=True, help="Path to the sha256sums.txt downloaded from the release")
    parser.add_argument("--output", required=True, help="Path to the formula file to write")
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


def render_formula(version: str, repository: str, hashes: dict[str, str]) -> str:
    base_url = f"https://github.com/{repository}/releases/download/v{version}"
    repo_url = f"https://github.com/{repository}"
    darwin_arm = hash_for(hashes, version, "darwin_arm64")
    darwin_amd = hash_for(hashes, version, "darwin_amd64")
    linux_arm = hash_for(hashes, version, "linux_arm64")
    linux_amd = hash_for(hashes, version, "linux_amd64")
    return f'''class Bb < Formula
  desc "A CLI for Bitbucket Server / Bitbucket Data Center"
  homepage "{repo_url}"
  version "{version}"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "{base_url}/bb_{version}_darwin_arm64.tar.gz"
      sha256 "{darwin_arm}"
    end
    on_intel do
      url "{base_url}/bb_{version}_darwin_amd64.tar.gz"
      sha256 "{darwin_amd}"
    end
  end

  on_linux do
    on_arm do
      url "{base_url}/bb_{version}_linux_arm64.tar.gz"
      sha256 "{linux_arm}"
    end
    on_intel do
      url "{base_url}/bb_{version}_linux_amd64.tar.gz"
      sha256 "{linux_amd}"
    end
  end

  def install
    bin.install "bb"
  end

  test do
    system "#{{bin}}/bb", "--version"
  end
end
'''


def main() -> None:
    args = parse_args()
    hashes = load_hashes(Path(args.sha256sums))
    formula = render_formula(args.version, args.repository, hashes)
    output = Path(args.output)
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(formula, encoding="utf-8")


if __name__ == "__main__":
    main()
