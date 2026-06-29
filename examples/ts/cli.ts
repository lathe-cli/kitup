import { directoryBundle, installBundledSkill } from "@kitup/sdk";

const report = await installBundledSkill({
  appId: "kitup-example-ts",
  skillBundle: directoryBundle("../../skills/kitup"),
  scope: "user",
});

console.log(JSON.stringify(report));
if (report.errors.length + report.conflicts.length > 0) process.exit(1);
