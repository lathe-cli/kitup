---
name: kitup-release
description: Use when preparing or publishing a kitup release, including release branch preparation, version bumps, manual PR handoff, manual tags, GitHub Actions publishing, and public smoke checks.
---

# kitup Release

Use this only inside `/Users/x/git/samzong/kitup` or the upstream kitup repository.

## Release Contract

Do not publish from a pull request. Do not tag the release branch.

The maintainer-owned flow is:

1. Start from a clean, up-to-date `main`.
2. Run one release prep command:

```bash
make release-patch
make release-minor
make release-major
```

3. Let the command create `release/vX.Y.Z`, update versions, run `make check`, and commit `chore: prepare vX.Y.Z release`.
4. Ask the maintainer to open and merge the release PR manually.
5. After merge, the maintainer tags `main` manually:

```bash
git checkout main
git pull --ff-only
git tag vX.Y.Z
git push origin vX.Y.Z
```

## Version Surfaces

Release prep must keep these in sync:

- `ts/package.json`
- `rust/Cargo.toml`
- `rust/Cargo.lock`
- `examples/rust/Cargo.lock`
- `go-cobra/go.mod`

## Automation

The root `vX.Y.Z` tag triggers `.github/workflows/release.yml`. The workflow runs `make check`, verifies package versions, publishes npm and crates.io packages, creates `go/vX.Y.Z` and `go-cobra/vX.Y.Z`, creates GitHub Release notes, and runs `scripts/smoke-release.sh X.Y.Z`.

If a registry already accepted a version, do not delete and recreate tags without an explicit recovery plan.
