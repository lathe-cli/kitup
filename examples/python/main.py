from dataclasses import asdict
import json

from kitup import BaseOptions, InstallOptions, directory_bundle, install_bundled_skill


report = install_bundled_skill(
    InstallOptions(
        base=BaseOptions(),
        app_id="kitup-example-python",
        skill_bundle=directory_bundle("../../skills/kitup"),
        scope="user",
    )
)
print(json.dumps(asdict(report)))
if report.errors or report.conflicts:
    raise SystemExit(1)
