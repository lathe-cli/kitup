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

## Prereleases

Use an explicit SemVer version to prepare a prerelease:

```bash
node scripts/prepare-release.mjs 0.1.5-alpha.1 --dry-run
node scripts/prepare-release.mjs 0.1.5-alpha.1
```

The same command accepts `-beta.N`, `-rc.N`, and stable versions. Each number starts at 1 and has no leading zeros.
The requested version must exceed the current version. From a prerelease, `patch` promotes its core version to stable.
The script keeps SemVer in TypeScript, Rust, and Go dependencies. It maps Python versions to `aN`, `bN`, and `rcN`.
For example, `0.1.5-rc.1` maps to `0.1.5rc1` in Python manifests and lockfiles.

After checkout and Node setup, the workflow validates the tag and manifests before `make check` or publication.
The workflow also requires the checked-out commit to match the root tag.
Prereleases use the matching npm channel, such as `rc`. GitHub prereleases use `--prerelease --latest=false`.
Release notes start from the nearest ancestor root tag that matches `^v[0-9]`. Go module tags do not select the release range.
Published tags remain immutable. This process does not rename or replace earlier tags.

Run the release checks without publishing packages:

```bash
node --test scripts/release.test.mjs
node scripts/check.mjs source
```

The checks create local fixtures and intercept publication commands. They do not publish packages or GitHub Releases.

## First npm Release

npm trusted publishing is configured in the npm package settings. For the first package version, the package settings may not exist yet.

If the workflow cannot create the first npm package, publish the npm package once from a local authenticated npm session:

```bash
cd ts
npm publish --access public
```

Then rerun the failed release workflow. The workflow detects already-published npm, PyPI, and crate versions and skips them.

## Recovery

crates.io uses Trusted Publishing. Configure a GitHub publisher for `lathe-cli/kitup` and `release.yml` in the crate settings. Keep the requirement for Trusted Publishing enabled. The workflow exchanges its GitHub OIDC identity for a temporary crates.io token. It does not use a repository token secret.

The release workflow is resumable:

- If npm already has the version, npm publish is skipped.
- If PyPI already has the version, Python build and publish are skipped.
- If crates.io already has the version, crate publish is skipped.
- If `go/vX.Y.Z` or `go-cobra/vX.Y.Z` already exists, the workflow verifies that it points at the release commit.
- If Cobra verification fails after the core tag is published, rerunning the workflow reuses the verified core tag and does not create the Cobra tag until its declared core dependency passes.

If recovery needs a workflow fix, merge the fix into `main`, then run the updated workflow against the existing root tag:

```bash
gh workflow run release.yml --repo lathe-cli/kitup --ref main -f tag=v0.1.5
```

Replace `v0.1.5` with the release tag to recover. GitHub loads the workflow from `main`. Checkout, version checks, Go module tags, package publication, release notes, and smoke checks use the tagged source. Registry provenance from recovery runs identifies the `main` workflow commit. The version gate separately binds the package source to the release tag. Rerunning the original failed run uses its original workflow definition.

Do not delete, recreate, or move an existing release tag. If authentication fails, stop and fix the registry's Trusted Publishing configuration before retrying.

## Smoke Check

Run the public install smoke check manually with:

```bash
scripts/smoke-release.sh X.Y.Z
```

The smoke check installs from npm, PyPI (`kitup-sdk`), crates.io, the public Go module, and the public Go Cobra adapter, then verifies that each SDK can load the default host spec or instantiate its adapter.
