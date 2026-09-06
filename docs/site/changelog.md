# Changelog

This page is generated at release time from the published GitHub releases, and
the copy in the repository is a placeholder.

It used to be a committed snapshot, which went stale the moment a release
happened and stayed that way: the version in the repository stopped at v1.11.8
while releases carried on without it. Nothing noticed, because every consumer
overwrites this file before using it — CI renders it before validating the site,
and the release job renders it before publishing — so the committed bytes were
read by nobody except a person browsing the repository, who got a changelog two
majors out of date.

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
