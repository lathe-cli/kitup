# Release

`kitup` publishes one version across five package surfaces:

- npm: `@kitup/sdk`
- PyPI: `kitup-sdk`
- crates.io: `kitup`
- Go module: `github.com/lathe-cli/kitup/go`
- Go Cobra adapter: `github.com/lathe-cli/kitup/go-cobra`

## Normal Release

Start from an up-to-date `main` branch:

```bash
git checkout main
git pull --ff-only
```

Prepare the release branch and version commit:

```bash
make release-patch
# or
make release-minor
# or
make release-major
```

The release target creates `release/vX.Y.Z`, updates:

- `ts/package.json`
- `python/pyproject.toml`
- `python/uv.lock`
- `examples/python/uv.lock`
- `rust/Cargo.toml`
- `rust/Cargo.lock`
- `examples/rust/Cargo.lock`
- `go-cobra/go.mod`

It then runs `make check` and commits:

```bash
chore: prepare vX.Y.Z release
```

Open the release PR manually. After it is merged, tag the merge commit on `main` manually:

```bash
git checkout main
git pull --ff-only
git tag vX.Y.Z
git push origin vX.Y.Z
```

Do not tag the release branch. Do not publish packages by hand during the normal flow.

The release workflow creates `go/vX.Y.Z`, verifies the Cobra adapter against that published core version, creates `go-cobra/vX.Y.Z`, publishes npm, PyPI, and crates.io packages, creates the GitHub Release, and runs the public install smoke check. Both Go modules keep the same release version; only their publication steps are ordered by dependency.

## First npm Release

npm trusted publishing is configured in the npm package settings. For the first package version, the package settings may not exist yet.

If the workflow cannot create the first npm package, publish the npm package once from a local authenticated npm session:

```bash
cd ts
npm publish --access public
```

Then rerun the failed release workflow. The workflow detects already-published npm, PyPI, and crate versions and skips them.

## Recovery

The release workflow is resumable:

- If npm already has the version, npm publish is skipped.
- If PyPI already has the version, Python build and publish are skipped.
- If crates.io already has the version, crate publish is skipped.
- If `go/vX.Y.Z` or `go-cobra/vX.Y.Z` already exists, the workflow verifies that it points at the release commit.
- If Cobra verification fails after the core tag is published, rerunning the workflow reuses the verified core tag and does not create the Cobra tag until its declared core dependency passes.

Do not delete and recreate a release tag after any registry has accepted the version unless the tag points at the wrong commit and the recovery plan is explicit.

## Smoke Check

Run the public install smoke check manually with:

```bash
scripts/smoke-release.sh X.Y.Z
```

The smoke check installs from npm, PyPI (`kitup-sdk`), crates.io, the public Go module, and the public Go Cobra adapter, then verifies that each SDK can load the default host spec or instantiate its adapter.
