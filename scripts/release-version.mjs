import { readFileSync } from "node:fs";

export function releaseVersion(version) {
  const match =
    /^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-(alpha|beta|rc)\.([1-9][0-9]*))?$/.exec(
      version,
    );
  if (!match || match[0] !== version)
    throw new Error(`Invalid release version: ${version}`);
  const [, major, minor, patch, stage, number] = match;
  const core = `${major}.${minor}.${patch}`;
  const pythonVersion = stage
    ? `${core}${{ alpha: "a", beta: "b", rc: "rc" }[stage]}${number}`
    : core;
  return {
    version,
    core,
    stage: stage ?? "",
    number: number ?? "0",
    pythonVersion,
  };
}

export function verifyReleaseVersions(root, version) {
  const metadata = releaseVersion(version);
  const read = (path) => readFileSync(new URL(path, root), "utf8");
  const expected = new Map([
    ["ts/package.json", [JSON.parse(read("ts/package.json")).version, version]],
    [
      "rust/Cargo.toml",
      [read("rust/Cargo.toml").match(/^version = "([^"]+)"$/m)?.[1], version],
    ],
    [
      "python/pyproject.toml",
      [
        read("python/pyproject.toml").match(/^version = "([^"]+)"$/m)?.[1],
        metadata.pythonVersion,
      ],
    ],
    [
      "go-cobra/go.mod",
      [
        read("go-cobra/go.mod").match(
          /^\s*github\.com\/lathe-cli\/kitup\/go v([^\s]+)$/m,
        )?.[1],
        version,
      ],
    ],
  ]);
  for (const [path, [actual, wanted]] of expected) {
    if (actual !== wanted)
      throw new Error(`${path} version ${actual} != ${wanted}`);
  }
  return metadata;
}
