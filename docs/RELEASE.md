# Release

`kitup` publishes one version across three SDKs:

- npm: `@kitup/sdk`
- crates.io: `kitup`
- Go module: `github.com/samzong/kitup/go`

## Normal Release

1. Update package versions.
2. Run:

```bash
make check
```

3. Merge to `main`.
4. Push a release tag:

```bash
git tag vX.Y.Z
git push origin vX.Y.Z
```

The release workflow publishes npm and crates.io packages, creates the `go/vX.Y.Z` tag, creates the GitHub Release, and runs the public install smoke check.

## First npm Release

npm trusted publishing is configured in the npm package settings. For the first package version, the package settings may not exist yet.

If the workflow cannot create the first npm package, publish the npm package once from a local authenticated npm session:

```bash
cd ts
npm publish --access public
```

Then rerun the failed release workflow. The workflow detects already-published npm and crate versions and skips them.

## Recovery

The release workflow is resumable:

- If npm already has the version, npm publish is skipped.
- If crates.io already has the version, crate publish is skipped.
- If `go/vX.Y.Z` already exists, the workflow verifies that it points at the release commit.

Do not delete and recreate a release tag after any registry has accepted the version unless the tag points at the wrong commit and the recovery plan is explicit.

## Smoke Check

Run the public install smoke check manually with:

```bash
scripts/smoke-release.sh X.Y.Z
```

The smoke check installs from npm, crates.io, and the public Go module, then verifies that each SDK can load the default host spec.
