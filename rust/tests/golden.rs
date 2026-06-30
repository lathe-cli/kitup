use kitup::{
    classify_install_workflow_exit, compute_bundle_content_hash, detect_hosts, directory_bundle,
    files_bundle, install_bundled_skill, load_host_spec, parse_install_flags, plan_bundled_skill,
    resolve_hosts, resolve_install_selection, run_bundled_skill_install_with_io,
    uninstall_bundled_skill, update_bundled_skill, validate_skill_bundle, AgentSelector,
    BaseOptions, InstallFlagValues, InstallOptions, InstallSelectionOptions,
    InstallWorkflowOptions, ParsedInstallFlags, Scope, SkillBundle, SkillFile, UninstallOptions,
};
use serde::Deserialize;
use serde_json::{json, Map, Value};
use std::fs;
use std::io::Cursor;
use std::path::{Path, PathBuf};
use std::time::{SystemTime, UNIX_EPOCH};

#[derive(Deserialize)]
struct GoldenFile {
    cases: Vec<GoldenCase>,
}

#[derive(Clone, Deserialize)]
struct GoldenCase {
    id: String,
    operation: String,
    options: Map<String, Value>,
    given: Map<String, Value>,
    expected: Map<String, Value>,
}

#[test]
fn golden_cases() {
    let file: GoldenFile = read_json(&repo_path("testdata/cases/bundled-skill-install.json"));
    for case in file.cases {
        let root = temp_dir(&case.id);
        let home = root.join("home");
        let workspace = root.join("workspace");
        fs::create_dir_all(&home).unwrap();
        fs::create_dir_all(&workspace).unwrap();
        setup_given(&case, &home, &workspace);
        run_case(&case, &home, &workspace);
        let _ = fs::remove_dir_all(root);
    }
}

fn run_case(case: &GoldenCase, home: &Path, workspace: &Path) {
    let options = expand_value(&Value::Object(case.options.clone()), home, workspace);
    let options = options.as_object().unwrap();
    match case.operation.as_str() {
        "resolve-hosts" => {
            let hosts_file = repo_path(case.given["hostsFile"].as_str().unwrap());
            let hosts = load_host_spec(Some(&hosts_file)).unwrap();
            let (hosts, errors) = resolve_hosts(&agent_selector(&options["agents"]), &hosts);
            if let Some(count) = case.expected.get("count") {
                assert_eq!(hosts.len(), count.as_u64().unwrap() as usize);
            }
            if let Some(expected) = case.expected.get("hostIds") {
                assert_json_eq(&json!(host_ids(&hosts)), expected.clone());
            }
            if let Some(expected) = case.expected.get("resolvedHostIds") {
                assert_json_eq(&json!(host_ids(&hosts)), expected.clone());
            }
            if let Some(expected) = case.expected.get("errors") {
                assert_json_eq(&json!(errors), expected.clone());
            }
        }
        "validate" => {
            let result = validate_skill_bundle(&skill_bundle_from_options(options));
            assert_eq!(result.valid, case.expected["valid"].as_bool().unwrap());
            assert_eq!(
                result.error_code.as_deref(),
                case.expected.get("errorCode").and_then(Value::as_str)
            );
        }
        "parse-install-flags" => {
            let parsed = parse_install_flags(InstallFlagValues {
                scope: options
                    .get("scope")
                    .and_then(Value::as_str)
                    .map(String::from),
                scope_set: options
                    .get("scopeSet")
                    .and_then(Value::as_bool)
                    .unwrap_or_else(|| options.get("scope").is_some()),
                agents: options
                    .get("agents")
                    .and_then(Value::as_array)
                    .map(|items| {
                        items
                            .iter()
                            .map(|item| item.as_str().unwrap().to_string())
                            .collect()
                    })
                    .unwrap_or_default(),
                yes: options.get("yes").and_then(Value::as_bool).unwrap_or(false),
                dry_run: options
                    .get("dryRun")
                    .and_then(Value::as_bool)
                    .unwrap_or(false),
            });
            assert_json_eq(
                &normalized_parsed_flags(&parsed),
                case.expected["parsed"].clone(),
            );
        }
        "resolve-install-selection" => {
            let selection = resolve_install_selection(&InstallSelectionOptions {
                base: BaseOptions {
                    home: Some(home.to_path_buf()),
                    cwd: Some(workspace.to_path_buf()),
                    hosts_file: Some(repo_path("spec/hosts.json")),
                },
                scope: Some(scope(options["scope"].as_str().unwrap())),
                agents: options.get("agents").map(agent_selector),
                yes: options.get("yes").and_then(Value::as_bool).unwrap_or(false),
                stdin_tty: options
                    .get("stdinTTY")
                    .and_then(Value::as_bool)
                    .unwrap_or(false),
                current_agent: options
                    .get("currentAgent")
                    .and_then(Value::as_str)
                    .map(String::from),
            })
            .unwrap();
            assert_selection(
                serde_json::to_value(selection).unwrap(),
                case.expected["selection"].clone(),
            );
        }
        "run-install-workflow" => {
            let mut input = Cursor::new(
                options
                    .get("input")
                    .and_then(Value::as_str)
                    .unwrap_or("")
                    .as_bytes()
                    .to_vec(),
            );
            let mut output = Vec::new();
            let workflow = run_bundled_skill_install_with_io(
                &InstallWorkflowOptions {
                    install: InstallOptions {
                        base: BaseOptions {
                            home: Some(home.to_path_buf()),
                            cwd: Some(workspace.to_path_buf()),
                            hosts_file: Some(repo_path("spec/hosts.json")),
                        },
                        app_id: options["appId"].as_str().unwrap().to_string(),
                        skill_bundle: skill_bundle_from_options(options),
                        scope: scope(
                            options
                                .get("scope")
                                .and_then(Value::as_str)
                                .unwrap_or("user"),
                        ),
                        agents: options
                            .get("agents")
                            .map(agent_selector)
                            .unwrap_or(AgentSelector::Auto),
                    },
                    yes: options.get("yes").and_then(Value::as_bool).unwrap_or(false),
                    dry_run: options
                        .get("dryRun")
                        .and_then(Value::as_bool)
                        .unwrap_or(false),
                    stdin_tty: options
                        .get("stdinTTY")
                        .and_then(Value::as_bool)
                        .unwrap_or(false),
                    current_agent: options
                        .get("currentAgent")
                        .and_then(Value::as_str)
                        .map(String::from),
                    default_scope: options
                        .get("defaultScope")
                        .and_then(Value::as_str)
                        .map(scope),
                    scope_set: options
                        .get("scopeSet")
                        .and_then(Value::as_bool)
                        .unwrap_or_else(|| options.get("scope").is_some()),
                    prompt_scope: options
                        .get("promptScope")
                        .and_then(Value::as_bool)
                        .unwrap_or(false),
                },
                &mut input,
                &mut output,
            )
            .unwrap();
            let workflow_value = serde_json::to_value(&workflow).unwrap();
            assert_workflow(&workflow_value, case.expected.get("workflow"));
            if let Some(expected) = case.expected.get("exit") {
                assert_json_eq(
                    &serde_json::to_value(classify_install_workflow_exit(&workflow)).unwrap(),
                    expected.clone(),
                );
            }
            assert_output(
                &String::from_utf8(output.clone()).unwrap(),
                case.expected.get("output"),
            );
            assert_output_contains(
                &String::from_utf8(output).unwrap(),
                case.expected.get("outputContains"),
            );
            if let Some(expected) = case.expected.get("report") {
                assert_json_eq(
                    &serde_json::to_value(&workflow.report).unwrap(),
                    expand_value(expected, home, workspace),
                );
            }
            assert_expected_files(case, home, workspace);
            assert_expected_metadata(case, home, workspace);
        }
        _ => {
            if let Some(expected) = case.expected.get("detectedHosts") {
                let hosts = detect_hosts(
                    &BaseOptions {
                        home: Some(home.to_path_buf()),
                        cwd: Some(workspace.to_path_buf()),
                        hosts_file: Some(repo_path("spec/hosts.json")),
                    },
                    Some(scope(options["scope"].as_str().unwrap())),
                )
                .unwrap();
                assert_json_eq(&json!(host_ids(&hosts)), expected.clone());
            }
            let report = run_report_case(case, options, home, workspace);
            if let Some(expected) = case.expected.get("report") {
                assert_json_eq(&report, expand_value(expected, home, workspace));
            }
            assert_expected_write_counts(case, &report, home, workspace);
            assert_expected_files(case, home, workspace);
            assert_expected_metadata(case, home, workspace);
        }
    }
}

fn run_report_case(
    case: &GoldenCase,
    options: &Map<String, Value>,
    home: &Path,
    workspace: &Path,
) -> Value {
    let base = BaseOptions {
        home: Some(home.to_path_buf()),
        cwd: Some(workspace.to_path_buf()),
        hosts_file: Some(repo_path("spec/hosts.json")),
    };
    match case.operation.as_str() {
        "uninstall" => serde_json::to_value(
            uninstall_bundled_skill(&UninstallOptions {
                base,
                app_id: options["appId"].as_str().unwrap().to_string(),
                skill_name: options["skillName"].as_str().unwrap().to_string(),
                scope: scope(options["scope"].as_str().unwrap()),
                agents: agent_selector(&options["agents"]),
            })
            .unwrap(),
        )
        .unwrap(),
        "install" | "update" | "plan" => {
            let options = InstallOptions {
                base,
                app_id: options["appId"].as_str().unwrap().to_string(),
                skill_bundle: skill_bundle_from_options(options),
                scope: scope(options["scope"].as_str().unwrap()),
                agents: agent_selector(&options["agents"]),
            };
            match case.operation.as_str() {
                "update" => serde_json::to_value(update_bundled_skill(&options).unwrap()).unwrap(),
                "plan" => serde_json::to_value(plan_bundled_skill(&options).unwrap()).unwrap(),
                _ => serde_json::to_value(install_bundled_skill(&options).unwrap()).unwrap(),
            }
        }
        other => panic!("unsupported operation: {other}"),
    }
}

fn setup_given(case: &GoldenCase, home: &Path, workspace: &Path) {
    for dir in case
        .given
        .get("dirs")
        .and_then(Value::as_array)
        .into_iter()
        .flatten()
    {
        fs::create_dir_all(expand_string(dir.as_str().unwrap(), home, workspace)).unwrap();
    }
    if let Some(files) = case.given.get("files").and_then(Value::as_object) {
        for (path, value) in files {
            write_fixture_file(&expand_string(path, home, workspace), value);
        }
    }
    if let Some(target) = case.given.get("copySkillBundleTo").and_then(Value::as_str) {
        let target = expand_string(target, home, workspace);
        let _ = fs::remove_dir_all(&target);
        copy_dir(&case_skill_bundle_dir(case), &target);
    }
    if let Some(metadata) = case.given.get("metadata").and_then(Value::as_object) {
        write_metadata_fixture(case, home, workspace, metadata);
    }
}

fn assert_expected_files(case: &GoldenCase, home: &Path, workspace: &Path) {
    for path in case
        .expected
        .get("filesPresent")
        .and_then(Value::as_array)
        .into_iter()
        .flatten()
    {
        let path = expand_string(path.as_str().unwrap(), home, workspace);
        assert!(path.exists(), "expected file to exist: {}", path.display());
    }
    for path in case
        .expected
        .get("filesAbsent")
        .and_then(Value::as_array)
        .into_iter()
        .flatten()
    {
        let path = expand_string(path.as_str().unwrap(), home, workspace);
        assert!(
            !path.exists(),
            "expected file to be absent: {}",
            path.display()
        );
    }
}

fn assert_expected_metadata(case: &GoldenCase, home: &Path, workspace: &Path) {
    let Some(metadata) = case.expected.get("metadata").and_then(Value::as_object) else {
        return;
    };
    let path = expand_string(metadata["path"].as_str().unwrap(), home, workspace);
    let actual: Value = read_json(&path);
    for (key, value) in metadata["fields"].as_object().unwrap() {
        assert_json_eq(&actual[key], value.clone());
    }
    let mut hash = metadata["hash"].as_str().unwrap().to_string();
    if hash == "from-skill-bundle-dir" {
        hash = compute_bundle_content_hash(&directory_bundle(repo_path(
            case.options["skillBundleDir"].as_str().unwrap(),
        )))
        .unwrap();
    } else if hash == "from-skill-files" {
        hash = compute_bundle_content_hash(&files_bundle(skill_files(
            case.options["skillFiles"].as_array().unwrap(),
        )))
        .unwrap();
    }
    assert_eq!(actual["hash"], hash);
}

fn assert_selection(mut actual: Value, mut expected: Value) {
    if let Some(selected_count) = expected.get("selectedCount").and_then(Value::as_u64) {
        assert_eq!(
            actual["selectedHostIds"].as_array().unwrap().len(),
            selected_count as usize
        );
        actual.as_object_mut().unwrap().remove("selectedHostIds");
        expected.as_object_mut().unwrap().remove("selectedCount");
    }
    if let Some(candidate_count) = expected.get("candidateCount").and_then(Value::as_u64) {
        assert_eq!(
            actual["candidateHostIds"].as_array().unwrap().len(),
            candidate_count as usize
        );
        actual.as_object_mut().unwrap().remove("candidateHostIds");
        expected.as_object_mut().unwrap().remove("candidateCount");
    }
    assert_json_eq(&actual, expected);
}

fn normalized_parsed_flags(parsed: &ParsedInstallFlags) -> Value {
    let (agent_kind, agent_ids) = match &parsed.agents {
        AgentSelector::Auto => ("auto", vec![]),
        AgentSelector::All => ("*", vec![]),
        AgentSelector::Explicit(ids) => ("explicit", ids.clone()),
    };
    json!({
        "scope": match parsed.scope {
            Scope::User => "user",
            Scope::Project => "project",
        },
        "scopeSet": parsed.scope_set,
        "agentKind": agent_kind,
        "agentIds": agent_ids,
        "yes": parsed.yes,
        "dryRun": parsed.dry_run,
        "errors": parsed.errors
    })
}

fn assert_workflow(actual: &Value, expected: Option<&Value>) {
    let Some(expected) = expected.and_then(Value::as_object) else {
        return;
    };
    for (key, value) in expected {
        assert_json_eq(&actual[key], value.clone());
    }
}

fn assert_output_contains(actual: &str, expected: Option<&Value>) {
    let Some(expected) = expected.and_then(Value::as_array) else {
        return;
    };
    for value in expected {
        let text = value.as_str().unwrap();
        assert!(
            actual.contains(text),
            "expected output to contain {text}, got:\n{actual}"
        );
    }
}

fn assert_output(actual: &str, expected: Option<&Value>) {
    let Some(expected) = expected.and_then(Value::as_str) else {
        return;
    };
    assert_eq!(actual, expected);
}

fn assert_expected_write_counts(case: &GoldenCase, report: &Value, home: &Path, workspace: &Path) {
    let Some(expected) = case.expected.get("writeCountByTargetDir") else {
        return;
    };
    let mut actual = Map::new();
    for key in ["installed", "updated"] {
        for item in report[key].as_array().unwrap() {
            let target_dir = item["targetDir"].as_str().unwrap();
            let count = actual.get(target_dir).and_then(Value::as_u64).unwrap_or(0) + 1;
            actual.insert(target_dir.to_string(), json!(count));
        }
    }
    assert_json_eq(
        &Value::Object(actual),
        expand_value(expected, home, workspace),
    );
}

fn write_metadata_fixture(
    case: &GoldenCase,
    home: &Path,
    workspace: &Path,
    metadata: &Map<String, Value>,
) {
    let mut fields = metadata["fields"].as_object().unwrap().clone();
    let mut hash = metadata["hash"].as_str().unwrap().to_string();
    if hash == "from-skill-bundle-dir" {
        hash = compute_bundle_content_hash(&directory_bundle(case_skill_bundle_dir(case))).unwrap();
    }
    fields.insert("hash".to_string(), json!(hash));
    write_fixture_file(
        &expand_string(metadata["path"].as_str().unwrap(), home, workspace),
        &Value::Object(fields),
    );
}

fn write_fixture_file(path: &Path, value: &Value) {
    fs::create_dir_all(path.parent().unwrap()).unwrap();
    if let Some(text) = value.as_str() {
        fs::write(path, text).unwrap();
    } else {
        fs::write(
            path,
            format!("{}\n", serde_json::to_string_pretty(value).unwrap()),
        )
        .unwrap();
    }
}

fn copy_dir(src: &Path, dest: &Path) {
    fs::create_dir_all(dest).unwrap();
    for entry in fs::read_dir(src).unwrap() {
        let entry = entry.unwrap();
        let to = dest.join(entry.file_name());
        if entry.file_type().unwrap().is_dir() {
            copy_dir(&entry.path(), &to);
        } else {
            fs::copy(entry.path(), to).unwrap();
        }
    }
}

fn agent_selector(value: &Value) -> AgentSelector {
    if let Some(text) = value.as_str() {
        if text == "*" {
            AgentSelector::All
        } else {
            AgentSelector::Auto
        }
    } else {
        AgentSelector::Explicit(
            value
                .as_array()
                .unwrap()
                .iter()
                .map(|item| item.as_str().unwrap().to_string())
                .collect(),
        )
    }
}

fn skill_bundle_from_options(options: &Map<String, Value>) -> SkillBundle {
    if let Some(files) = options.get("skillFiles").and_then(Value::as_array) {
        return files_bundle(skill_files(files));
    }
    if let Some(dir) = options.get("skillBundleDir").and_then(Value::as_str) {
        return directory_bundle(repo_path(dir));
    }
    files_bundle(Vec::new())
}

fn skill_files(values: &[Value]) -> Vec<SkillFile> {
    values
        .iter()
        .map(|value| {
            let value = value.as_object().unwrap();
            SkillFile {
                path: value["path"].as_str().unwrap().to_string(),
                contents: value["contents"].as_str().unwrap().as_bytes().to_vec(),
                mode: None,
            }
        })
        .collect()
}

fn scope(value: &str) -> Scope {
    match value {
        "user" => Scope::User,
        "project" => Scope::Project,
        other => panic!("bad scope: {other}"),
    }
}

fn case_skill_bundle_dir(case: &GoldenCase) -> PathBuf {
    if let Some(skill_bundle_dir) = case.options.get("skillBundleDir").and_then(Value::as_str) {
        repo_path(skill_bundle_dir)
    } else {
        repo_path(&format!(
            "testdata/skills/{}",
            case.options["skillName"].as_str().unwrap()
        ))
    }
}

fn repo_path(path: &str) -> PathBuf {
    let path = PathBuf::from(path);
    if path.is_absolute() {
        path
    } else {
        PathBuf::from("..").join(path)
    }
}

fn host_ids(hosts: &[kitup::Host]) -> Vec<String> {
    hosts.iter().map(|host| host.id.clone()).collect()
}

fn expand_value(value: &Value, home: &Path, workspace: &Path) -> Value {
    match value {
        Value::String(text) => json!(expand_string(text, home, workspace)),
        Value::Array(items) => Value::Array(
            items
                .iter()
                .map(|item| expand_value(item, home, workspace))
                .collect(),
        ),
        Value::Object(object) => Value::Object(
            object
                .iter()
                .map(|(key, value)| {
                    (
                        expand_string(key, home, workspace)
                            .to_string_lossy()
                            .to_string(),
                        expand_value(value, home, workspace),
                    )
                })
                .collect(),
        ),
        _ => value.clone(),
    }
}

fn expand_string(value: &str, home: &Path, workspace: &Path) -> PathBuf {
    PathBuf::from(
        value
            .replace("$HOME", &home.to_string_lossy())
            .replace("$WORKSPACE", &workspace.to_string_lossy()),
    )
}

fn assert_json_eq(left: &Value, right: Value) {
    assert_eq!(
        left,
        &right,
        "got:\n{}\nwant:\n{}",
        serde_json::to_string_pretty(left).unwrap(),
        serde_json::to_string_pretty(&right).unwrap()
    );
}

fn read_json<T: for<'de> Deserialize<'de>>(path: &Path) -> T {
    serde_json::from_slice(&fs::read(path).unwrap()).unwrap()
}

fn temp_dir(name: &str) -> PathBuf {
    let suffix = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap()
        .as_nanos();
    let path = std::env::temp_dir().join(format!("kitup-{name}-{suffix}"));
    fs::create_dir_all(&path).unwrap();
    path
}
