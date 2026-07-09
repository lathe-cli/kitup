# kitup Python SDK

Shared installer SDK for bundled Agent Skills.

## Install

```bash
pip install kitup-sdk
```

## Use

Use the workflow API for user-facing install commands:

```python
from kitup import (
    BaseOptions,
    InstallOptions,
    InstallWorkflowOptions,
    directory_bundle,
    run_bundled_skill_install,
)

workflow = run_bundled_skill_install(
    InstallWorkflowOptions(
        install=InstallOptions(
            base=BaseOptions(),
            app_id="mycli",
            skill_bundle=directory_bundle("./skills/mycli"),
            scope="user",
        ),
        stdin_tty=True,
        prompt_scope=True,
    )
)
```

Embed a skill directory shipped as package data with:

```python
from importlib.resources import files
from kitup import resources_bundle

bundle = resources_bundle(files("mycli.skills") / "mycli")
```

Call `install_bundled_skill` when your CLI already knows the target scope and agents and does not need the interactive workflow surface.
