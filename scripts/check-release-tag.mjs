import { appendFileSync } from "node:fs";
import { execFileSync } from "node:child_process";
import { verifyReleaseVersions } from "./release-version.mjs";

const tag = process.env.RELEASE_TAG ?? process.env.GITHUB_REF_NAME;
if (!tag?.startsWith("v")) throw new Error(`Invalid release tag: ${tag}`);
const { version, stage, pythonVersion } = verifyReleaseVersions(
  new URL("../", import.meta.url),
  tag.slice(1),
);
const git = (...args) => execFileSync("git", args, { encoding: "utf8" }).trim();
if (
  git("rev-parse", "HEAD") !== git("rev-parse", `refs/tags/${tag}^{commit}`)
) {
  throw new Error(`HEAD does not match release tag: ${tag}`);
}
if (process.env.GITHUB_OUTPUT) {
  appendFileSync(
    process.env.GITHUB_OUTPUT,
    `version=${version}\npython_version=${pythonVersion}\nstage=${stage}\nis_prerelease=${Boolean(stage)}\n`,
  );
}
console.log(`Release versions match: ${tag} (Python ${pythonVersion})`);
