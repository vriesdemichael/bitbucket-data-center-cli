# Scripts

The local Bitbucket instance:
- `stack.sh`: the instance the live suite runs against, one per checkout; every `task stack:*` target calls it
- `bootstrap-bitbucket.sh`: takes a fresh instance through its setup wizard and licence and enables basic auth; `stack.sh up` runs it
- `purge-live-fixtures.sh`: removes the projects earlier live runs left on an instance; `task stack:purge-fixtures`

Release packaging:
- `nfpm.yaml`: nFPM template for building the Linux `.deb`/`.rpm` packages in `.github/workflows/release-artifacts.yml`; the workflow exports `PKG_ARCH`, `PKG_VERSION`, `PKG_BINARY`, and `PKG_COMPLETIONS` per architecture
- `nfpm-postinstall.sh`: the packages' post-install script, which names what each user sets up for themselves
- `gen_homebrew_formula.py`: render the Homebrew formula pushed to `vriesdemichael/homebrew-tap` from `sha256sums.txt`, in `.github/workflows/release.yml`
- `render_docs_changelog.py`: renders the published GitHub releases into `docs/site/changelog.md`, in the release workflow and before CI builds the site

Tests:
- `completion-shells/`: drives bb's completion in real bash, zsh, fish and PowerShell in a container, set up by hand and by the Linux packages; `task completion:shells`
