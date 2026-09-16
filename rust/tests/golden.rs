use kitup::*;
use serde_json::{json, Value};
use std::fs;
use std::io::{self, Cursor};
use std::path::PathBuf;
use std::process::Command;

#[test]
fn golden_cases() {
    let Ok(input) = std::env::var("KITUP_GOLDEN_INPUT") else {
        let root = std::env::var_os("KITUP_TEST_REPO_ROOT")
            .map(PathBuf::from)
            .unwrap_or_else(|| PathBuf::from(env!("CARGO_MANIFEST_DIR")).join(".."));
        assert!(Command::new("node")
            .arg(root.join("scripts/golden.mjs"))
            .arg("--")
            .arg(std::env::current_exe().unwrap())
            .args(["--exact", "golden_cases", "--nocapture"])
            .status()
            .unwrap()
            .success());
        return;
    };
    let requests: Vec<Value> = serde_json::from_slice(&fs::read(input).unwrap()).unwrap();
    let results: Vec<_> = requests
        .iter()
        .map(|request| {
            for name in ["KITUP_GITHUB_API_BASE_URL", "KITUP_GITHUB_RAW_BASE_URL"] {
                if let Some(value) = request["env"][name].as_str() {
                    std::env::set_var(name, value);
                } else {
                    std::env::remove_var(name);
                }
            }
            let mut result = dispatch(request)
                .unwrap_or_else(|error| json!({"threw": true, "error": error.to_string()}));
            result["id"] = request["id"].clone();
            result
        })
        .collect();
    fs::write(
        std::env::var("KITUP_GOLDEN_OUTPUT").unwrap(),
        serde_json::to_vec(&results).unwrap(),
    )
    .unwrap();
}

fn dispatch(request: &Value) -> io::Result<Value> {
    let options = &request["options"];
    let base = BaseOptions {
        home: optional_text(options, "home").map(PathBuf::from),
        cwd: optional_text(options, "cwd").map(PathBuf::from),
        hosts_file: optional_text(options, "hostsFile").map(PathBuf::from),
    };
    let operation = request["operation"].as_str().unwrap();
    let scope = if operation == "parse-install-flags" {
        Scope::User
    } else {
        scope(options["scope"].as_str().unwrap_or("user"))
    };
    let agents = options
        .get("agents")
        .map(agent_selector)
        .unwrap_or(AgentSelector::Auto);
    let detected = if flag(request, "detect") {
        Some(json!(host_ids(&detect_hosts(&base, Some(scope))?)))
    } else {
        None
    };
    let mut result = match operation {
        "resolve-hosts" => {
            let hosts_file = PathBuf::from(request["given"]["hostsFile"].as_str().unwrap());
            let (hosts, errors) = resolve_hosts(&agents, &load_host_spec(Some(&hosts_file))?);
            json!({
                "count": hosts.len(), "hostIds": host_ids(&hosts),
                "resolvedHostIds": host_ids(&hosts), "errors": errors,
            })
        }
        "validate" => {
            let skill = validate_skill_bundle(&skill_bundle(options));
            json!({"valid": skill.valid, "errorCode": skill.error_code})
        }
        "parse-install-flags" => {
            let parsed = parse_install_flags(InstallFlagValues {
                scope: optional_text(options, "scope"),
                scope_set: scope_set(options),
                agents: options["agents"]
                    .as_array()
                    .into_iter()
                    .flatten()
                    .map(|value| value.as_str().unwrap().to_string())
                    .collect(),
                yes: flag(options, "yes"),
                dry_run: flag(options, "dryRun"),
                force: flag(options, "force"),
            });
            let (kind, ids) = match parsed.agents {
                AgentSelector::Auto => ("auto", vec![]),
                AgentSelector::All => ("*", vec![]),
                AgentSelector::Explicit(ids) => ("explicit", ids),
            };
            json!({"parsed": {
                "scope": if parsed.scope == Scope::User { "user" } else { "project" },
                "scopeSet": parsed.scope_set, "agentKind": kind, "agentIds": ids,
                "yes": parsed.yes, "dryRun": parsed.dry_run,
                "force": parsed.force, "errors": parsed.errors,
            }})
        }
        "resolve-install-selection" => {
            let selection = resolve_install_selection(&InstallSelectionOptions {
                base: base.clone(),
                scope: Some(scope),
                agents: options.get("agents").map(agent_selector),
                yes: flag(options, "yes"),
                stdin_tty: flag(options, "stdinTTY"),
                current_agent: optional_text(options, "currentAgent"),
            })?;
            json!({"selection": selection})
        }
        "resolve-install-targets" => {
            let (targets, _, _) = resolve_install_targets(
                &base,
                &agents,
                scope,
                options["skillName"].as_str().unwrap(),
            )?;
            let targets: Vec<_> = targets
                .into_iter()
                .map(|target| {
                    json!({
                        "hostIds": target.host_ids,
                        "skillName": target.skill_name, "targetDir": target.target_dir,
                    })
                })
                .collect();
            json!({"targets": targets})
        }
        "status" => json!({"report": status_bundled_skill(&StatusOptions {
            base: base.clone(), app_id: text(options, "appId"),
            skill_name: text(options, "skillName"), scope, agents,
        })?}),
        "uninstall" => json!({"report": uninstall_bundled_skill(&UninstallOptions {
            base: base.clone(), app_id: text(options, "appId"),
            skill_name: text(options, "skillName"), scope, agents,
        })?}),
        operation => {
            let install = InstallOptions {
                base: base.clone(),
                app_id: text(options, "appId"),
                skill_bundle: skill_bundle(options),
                scope,
                agents,
                force: flag(options, "force"),
            };
            match operation {
                "run-install-workflow" => {
                    let mut input = Cursor::new(options["input"].as_str().unwrap_or("").as_bytes());
                    let mut output = Vec::new();
                    let workflow = run_bundled_skill_install_with_io(
                        &InstallWorkflowOptions {
                            install,
                            yes: flag(options, "yes"),
                            dry_run: flag(options, "dryRun"),
                            stdin_tty: flag(options, "stdinTTY"),
                            current_agent: optional_text(options, "currentAgent"),
                            default_scope: options["defaultScope"].as_str().map(crate::scope),
                            scope_set: scope_set(options),
                            prompt_scope: flag(options, "promptScope"),
                        },
                        &mut input,
                        &mut output,
                    )?;
                    json!({
                        "exit": classify_install_workflow_exit(&workflow),
                        "report": workflow.report, "workflow": workflow,
                        "output": String::from_utf8(output).unwrap(),
                    })
                }
                "install" => json!({"report": install_bundled_skill(&install)?}),
                "update" => json!({"report": update_bundled_skill(&install)?}),
                "plan" => json!({"report": plan_bundled_skill(&install)?}),
                other => panic!("unsupported operation: {other}"),
            }
        }
    };
    if let Some(detected) = detected {
        result["detectedHosts"] = detected;
    }
    Ok(result)
}

fn skill_bundle(options: &Value) -> SkillBundle {
    let bundle = if let Some(files) = options["skillFiles"].as_array() {
        files_bundle(
            files
                .iter()
                .map(|file| SkillFile {
                    path: text(file, "path"),
                    contents: text(file, "contents").into_bytes(),
                    mode: file["mode"].as_u64().map(|mode| mode as u32),
                })
                .collect(),
        )
    } else if let Some(path) = options["skillBundleDir"].as_str() {
        directory_bundle(path)
    } else if let Some(github) = options.get("githubBundle") {
        github_bundle(GitHubBundleOptions {
            owner: text(github, "owner"),
            repo: text(github, "repo"),
            path: text(github, "path"),
            ref_name: text(github, "ref"),
        })
    } else {
        files_bundle(vec![])
    };
    let Some(metadata) = options.get("bundleMetadata") else {
        return bundle;
    };
    with_bundle_metadata(
        bundle,
        BundledSkillMetadata {
            source_id: optional_text(metadata, "sourceId"),
            cli_version: optional_text(metadata, "cliVersion"),
            cli_revision: optional_text(metadata, "cliRevision"),
            provenance: metadata["provenance"]
                .as_object()
                .into_iter()
                .flatten()
                .map(|(key, value)| (key.clone(), value.as_str().unwrap().to_string()))
                .collect(),
        },
    )
}

fn agent_selector(value: &Value) -> AgentSelector {
    match value.as_str() {
        Some("*") => AgentSelector::All,
        Some(_) => AgentSelector::Auto,
        None => AgentSelector::Explicit(
            value
                .as_array()
                .unwrap()
                .iter()
                .map(|value| value.as_str().unwrap().to_string())
                .collect(),
        ),
    }
}

fn host_ids(hosts: &[Host]) -> Vec<&str> {
    hosts.iter().map(|host| host.id.as_str()).collect()
}

fn scope(value: &str) -> Scope {
    match value {
        "user" => Scope::User,
        "project" => Scope::Project,
        other => panic!("bad scope: {other}"),
    }
}

fn scope_set(options: &Value) -> bool {
    options["scopeSet"]
        .as_bool()
        .unwrap_or_else(|| options.get("scope").is_some())
}

fn flag(options: &Value, key: &str) -> bool {
    options[key].as_bool().unwrap_or(false)
}

fn optional_text(options: &Value, key: &str) -> Option<String> {
    options[key].as_str().map(String::from)
}

fn text(options: &Value, key: &str) -> String {
    options[key].as_str().unwrap().to_string()
}

#[cfg(feature = "include-dir")]
#[test]
fn include_dir_bundle_installs_embedded_tree() {
    static SKILLS: include_dir::Dir<'_> =
        include_dir::include_dir!("$CARGO_MANIFEST_DIR/../testdata/skills");
    let root = std::env::temp_dir().join(format!(
        "kitup-include-dir-{}-{}",
        std::process::id(),
        std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap()
            .as_nanos()
    ));
    let home = root.join("home");
    let report = install_bundled_skill(&InstallOptions {
        base: BaseOptions {
            home: Some(home.clone()),
            ..BaseOptions::default()
        },
        app_id: "example-cli".into(),
        skill_bundle: include_dir_bundle(SKILLS.get_dir("basic").unwrap()),
        scope: Scope::User,
        agents: AgentSelector::Explicit(vec!["codex".into()]),
        force: false,
    })
    .unwrap();
    let target = home.join(".agents/skills/basic");
    assert_eq!(report.installed.len(), 1);
    for file in ["SKILL.md", "references/guide.md", "assets/template.json"] {
        assert!(target.join(file).is_file());
    }
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        assert_eq!(
            fs::metadata(target.join("scripts/helper.sh"))
                .unwrap()
                .permissions()
                .mode()
                & 0o777,
            0o755
        );
    }
    let _ = fs::remove_dir_all(root);
}
