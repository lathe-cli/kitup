# @kitup/sdk

TypeScript SDK for `kitup`, a shared installer for bundled Agent Skills.

## Install

```bash
npm install @kitup/sdk
```

## Use

```ts
import { directoryBundle, runBundledSkillInstall } from "@kitup/sdk";

await runBundledSkillInstall({
  appId: "mycli",
  skillBundle: directoryBundle("./skills/mycli"),
  scope: "user",
});
```

Embed a skill directory shipped next to the module with:

```ts
import { moduleDirBundle } from "@kitup/sdk";

const bundle = await moduleDirBundle(import.meta.url, "./skills/mycli");
```

See the repository README for product scope and examples:
https://github.com/lathe-cli/kitup
