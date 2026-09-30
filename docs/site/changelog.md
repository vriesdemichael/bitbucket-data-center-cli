# Changelog

This page is generated at release time from the published GitHub releases, and
the copy in the repository is a placeholder. CI renders it before it validates
the site, and the release job before it publishes.

The current changelog is on the
[GitHub Releases page](https://github.com/vriesdemichael/bitbucket-data-center-cli/releases),
and the published version of this page carries the same content.

To render it locally:

```bash
gh api "repos/vriesdemichael/bitbucket-data-center-cli/releases?per_page=100" --paginate > .tmp/releases.json
python scripts/render_docs_changelog.py \
  --releases-json .tmp/releases.json \
  --releases-page-url "https://github.com/vriesdemichael/bitbucket-data-center-cli/releases" \
  --output docs/site/changelog.md
```
