use crate::bundle::{normalize_skill_files, BundleMetadata, NormalizedSkillBundle};
use crate::types::{GitHubBundleOptions, SkillFile};
use serde_json::Value;
use std::io;
use std::time::Duration;

pub(crate) fn resolve_github_bundle(
    options: &GitHubBundleOptions,
) -> io::Result<(NormalizedSkillBundle, BundleMetadata)> {
    let root = options.path.trim_matches('/').to_string();
    if options.owner.is_empty()
        || options.repo.is_empty()
        || root.is_empty()
        || options.ref_name.is_empty()
    {
        return Err(io::Error::new(
            io::ErrorKind::InvalidInput,
            "invalid github bundle",
        ));
    }
    let api_base = env_base_url("KITUP_GITHUB_API_BASE_URL", "https://api.github.com");
    let raw_base = env_base_url(
        "KITUP_GITHUB_RAW_BASE_URL",
        "https://raw.githubusercontent.com",
    );
    let commit: Value = get_json(&format!(
        "{}/repos/{}/{}/commits/{}",
        api_base,
        escape_path_part(&options.owner),
        escape_path_part(&options.repo),
        escape_path_part(&options.ref_name)
    ))?;
    let resolved_commit = commit["sha"].as_str().unwrap_or("").to_string();
    let tree_sha = commit["commit"]["tree"]["sha"]
        .as_str()
        .unwrap_or("")
        .to_string();
    if resolved_commit.is_empty() || tree_sha.is_empty() {
        return Err(io::Error::new(
            io::ErrorKind::InvalidData,
            "invalid github commit",
        ));
    }
    let tree: Value = get_json(&format!(
        "{}/repos/{}/{}/git/trees/{}?recursive=1",
        api_base,
        escape_path_part(&options.owner),
        escape_path_part(&options.repo),
        escape_path_part(&tree_sha)
    ))?;
    let prefix = format!("{root}/");
    let mut files = Vec::new();
    for item in tree["tree"].as_array().into_iter().flatten() {
        let path = item["path"].as_str().unwrap_or("");
        if item["type"].as_str() != Some("blob") || !path.starts_with(&prefix) {
            continue;
        }
        let contents = get_bytes(&format!(
            "{}/{}/{}/{}/{}",
            raw_base,
            escape_path_part(&options.owner),
            escape_path_part(&options.repo),
            escape_path_part(&resolved_commit),
            escape_path(path)
        ))?;
        let mode = if item["mode"].as_str() == Some("100755") {
            Some(0o755)
        } else {
            Some(0o644)
        };
        files.push(SkillFile {
            path: path.strip_prefix(&prefix).unwrap().to_string(),
            contents,
            mode,
        });
    }
    if files.is_empty() {
        return Err(io::Error::new(
            io::ErrorKind::NotFound,
            "github bundle path not found",
        ));
    }
    let provenance = [
        ("owner", options.owner.clone()),
        ("repo", options.repo.clone()),
        ("path", root.clone()),
        ("ref", options.ref_name.clone()),
        ("resolvedCommit", resolved_commit),
    ]
    .into_iter()
    .map(|(key, value)| (key.into(), value))
    .collect();
    Ok((
        normalize_skill_files(files)?,
        BundleMetadata {
            source: "github".to_string(),
            source_id: Some(format!(
                "github:{}/{}/{}",
                options.owner, options.repo, root
            )),
            version: Some(options.ref_name.clone()),
            provenance,
            ..BundleMetadata::default()
        },
    ))
}

fn env_base_url(name: &str, fallback: &str) -> String {
    std::env::var(name)
        .unwrap_or_else(|_| fallback.to_string())
        .trim_end_matches('/')
        .to_string()
}

fn get_json(url: &str) -> io::Result<Value> {
    serde_json::from_slice(&get_bytes(url)?).map_err(io::Error::other)
}

fn get_bytes(url: &str) -> io::Result<Vec<u8>> {
    let agent = ureq::AgentBuilder::new()
        .timeout(Duration::from_secs(30))
        .build();
    let response = agent
        .get(url)
        .set("User-Agent", "kitup")
        .call()
        .map_err(io::Error::other)?;
    if response.status() < 200 || response.status() > 299 {
        return Err(io::Error::other(format!("github request failed: {url}")));
    }
    let mut reader = response.into_reader();
    let mut data = Vec::new();
    std::io::Read::read_to_end(&mut reader, &mut data)?;
    Ok(data)
}

fn escape_path(value: &str) -> String {
    value
        .split('/')
        .map(escape_path_part)
        .collect::<Vec<_>>()
        .join("/")
}

fn escape_path_part(value: &str) -> String {
    let mut out = String::new();
    for byte in value.bytes() {
        if byte.is_ascii_alphanumeric() || matches!(byte, b'-' | b'_' | b'.' | b'~') {
            out.push(byte as char);
        } else {
            out.push_str(&format!("%{byte:02X}"));
        }
    }
    out
}
