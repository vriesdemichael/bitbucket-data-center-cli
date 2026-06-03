# Scripts

Planned scripts:
- `seed_bitbucket.py`: create project/users/repos for deterministic live tests
- `reset_local_stack.ps1`: clear and recreate local state
- `wait_for_bitbucket.py`: poll readiness endpoint before live test execution

Not implemented in minimal scaffold.

Release packaging (used by `.github/workflows/release.yml`):
- `nfpm.yaml`: nFPM template for building the Linux `.deb`/`.rpm` packages; the workflow exports `PKG_ARCH`, `PKG_VERSION`, and `PKG_BINARY` per architecture
- `gen_homebrew_formula.py`: render the Homebrew formula pushed to `vriesdemichael/homebrew-tap` from `sha256sums.txt`
- `gen_pkgbuild.py`: render the AUR `bb-bin` PKGBUILD from `sha256sums.txt`

Coverage reporting workflow:
- `task quality:coverage:report:update` refreshes combined unit + live `docs/quality/coverage-report.json`
- `task quality:coverage:report:verify` recomputes and checks the committed report is current
- `task quality:coverage:report:verify:committed` validates the committed report in CI
- `lefthook.yml` wires this verification into `pre-push`
