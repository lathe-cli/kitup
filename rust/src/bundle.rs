use crate::github::resolve_github_bundle;
use crate::types::*;
use sha2::{Digest, Sha256};
use std::collections::{BTreeMap, HashMap};
use std::fs;
use std::io;
use std::path::{Path, PathBuf};

pub fn directory_bundle(path: impl Into<PathBuf>) -> SkillBundle {
    SkillBundle::Directory(path.into())
}

pub fn files_bundle(files: Vec<SkillFile>) -> SkillBundle {
    SkillBundle::Files(files)
}

#[cfg(feature = "include-dir")]
pub fn include_dir_bundle(dir: &include_dir::Dir<'_>) -> SkillBundle {
    fn collect(root: &Path, dir: &include_dir::Dir<'_>, files: &mut Vec<SkillFile>) {
        for file in dir.files() {
            let path = file.path();
            let path = if root.as_os_str().is_empty() {
                path
            } else {
                path.strip_prefix(root).unwrap_or(path)
            };
            files.push(SkillFile {
                path: path.to_string_lossy().replace('\\', "/"),
                contents: file.contents().to_vec(),
                mode: None,
            });
        }
        for dir in dir.dirs() {
            collect(root, dir, files);
        }
    }

    let mut files = Vec::new();
    collect(dir.path(), dir, &mut files);
    files_bundle(files)
}

pub fn github_bundle(options: GitHubBundleOptions) -> SkillBundle {
    SkillBundle::GitHub(options)
}

pub fn with_bundle_metadata(bundle: SkillBundle, metadata: BundledSkillMetadata) -> SkillBundle {
    SkillBundle::Metadata(Box::new(bundle), metadata)
}

#[derive(Clone, Debug)]
pub(crate) struct BundleFile {
    pub(crate) contents: Vec<u8>,
    pub(crate) mode: u32,
}

pub(crate) type NormalizedSkillBundle = BTreeMap<String, BundleFile>;

#[derive(Clone, Debug, Default)]
pub(crate) struct BundleMetadata {
    pub(crate) source: String,
    pub(crate) source_id: Option<String>,
    pub(crate) version: Option<String>,
    pub(crate) cli_version: Option<String>,
    pub(crate) cli_revision: Option<String>,
    pub(crate) provenance: BTreeMap<String, String>,
    pub(crate) explicit: bool,
}

pub fn validate_skill_bundle(bundle: &SkillBundle) -> SkillInfo {
    read_skill_bundle(bundle).map_or_else(
        |_| invalid_skill("invalid-skill-bundle"),
        |bundle| validate_normalized_skill(&bundle),
    )
}

pub(crate) fn validate_normalized_skill(bundle: &NormalizedSkillBundle) -> SkillInfo {
    let Some(file) = bundle.get("SKILL.md") else {
        return invalid_skill("missing-skill-md");
    };
    let content = String::from_utf8_lossy(&file.contents).replace("\r\n", "\n");
    let Some(rest) = content.strip_prefix("---\n") else {
        return invalid_skill("invalid-frontmatter");
    };
    let Some(end) = rest.find("\n---\n") else {
        return invalid_skill("invalid-frontmatter");
    };
    let fields = parse_frontmatter(&rest[..end]);
    let name = fields.get("name").cloned().unwrap_or_default();
    let description = fields.get("description").cloned().unwrap_or_default();
    if !valid_skill_name(&name) || description.is_empty() || description.len() > 1024 {
        return invalid_skill("invalid-frontmatter");
    }
    SkillInfo {
        valid: true,
        skill_name: Some(name),
        description: Some(description),
        error_code: None,
    }
}

pub fn compute_bundle_content_hash(bundle: &SkillBundle) -> io::Result<String> {
    Ok(content_hash(&read_skill_bundle(bundle)?))
}

pub(crate) fn content_hash(bundle: &NormalizedSkillBundle) -> String {
    let mut hash = Sha256::new();
    for (path, file) in bundle {
        hash.update(path.as_bytes());
        hash.update([0]);
        hash.update(&file.contents);
        hash.update([0]);
    }
    format!("sha256:{:x}", hash.finalize())
}

fn parse_frontmatter(content: &str) -> HashMap<String, String> {
    content
        .lines()
        .filter_map(|line| line.split_once(':'))
        .map(|(key, value)| (key.into(), value.trim().into()))
        .collect()
}

fn invalid_skill(reason: &str) -> SkillInfo {
    SkillInfo {
        error_code: Some(reason.into()),
        ..SkillInfo::default()
    }
}

pub(crate) fn valid_skill_name(name: &str) -> bool {
    name.split('-').all(|part| {
        !part.is_empty()
            && part
                .bytes()
                .all(|byte| byte.is_ascii_lowercase() || byte.is_ascii_digit())
    })
}

pub(crate) fn resolve_skill_bundle(
    bundle: &SkillBundle,
) -> io::Result<(NormalizedSkillBundle, BundleMetadata)> {
    let files = match bundle {
        SkillBundle::GitHub(options) => return resolve_github_bundle(options),
        SkillBundle::Metadata(bundle, supplied) => {
            let (bundle, mut metadata) = resolve_skill_bundle(bundle)?;
            if let Some(source_id) = supplied
                .source_id
                .as_ref()
                .filter(|value| !value.is_empty())
            {
                metadata.source_id = Some(source_id.clone());
            }
            for (target, value) in [
                (&mut metadata.cli_version, &supplied.cli_version),
                (&mut metadata.cli_revision, &supplied.cli_revision),
            ] {
                *target = value.as_ref().filter(|value| !value.is_empty()).cloned();
            }
            metadata.provenance.extend(supplied.provenance.clone());
            metadata.explicit = true;
            return Ok((bundle, metadata));
        }
        SkillBundle::Directory(root) => read_directory_bundle_files(root)?,
        SkillBundle::Files(files) => files.clone(),
    };
    Ok((
        normalize_skill_files(files)?,
        BundleMetadata {
            source: "bundled".into(),
            ..BundleMetadata::default()
        },
    ))
}

fn read_skill_bundle(bundle: &SkillBundle) -> io::Result<NormalizedSkillBundle> {
    resolve_skill_bundle(bundle).map(|(bundle, _)| bundle)
}

pub(crate) fn is_github_bundle(bundle: &SkillBundle) -> bool {
    match bundle {
        SkillBundle::GitHub(_) => true,
        SkillBundle::Metadata(bundle, _) => is_github_bundle(bundle),
        _ => false,
    }
}

fn read_directory_bundle_files(root: &Path) -> io::Result<Vec<SkillFile>> {
    let mut files = Vec::new();
    collect_directory_bundle_files(root, root, &mut files)?;
    Ok(files)
}

fn collect_directory_bundle_files(
    root: &Path,
    dir: &Path,
    files: &mut Vec<SkillFile>,
) -> io::Result<()> {
    for entry in fs::read_dir(dir)? {
        let entry = entry?;
        let name = entry.file_name();
        let name = name.to_string_lossy();
        if skip_name(&name) {
            continue;
        }
        let path = entry.path();
        let metadata = entry.metadata()?;
        if metadata.is_dir() {
            collect_directory_bundle_files(root, &path, files)?;
        } else if metadata.is_file() {
            let rel = path.strip_prefix(root).unwrap();
            files.push(SkillFile {
                path: rel.to_string_lossy().replace('\\', "/"),
                contents: fs::read(&path)?,
                mode: mode_bits(&metadata),
            });
        }
    }
    Ok(())
}

pub(crate) fn normalize_skill_files(files: Vec<SkillFile>) -> io::Result<NormalizedSkillBundle> {
    let mut by_path = BTreeMap::new();
    for file in files {
        let Some(path) = normalize_bundle_path(&file.path)? else {
            continue;
        };
        if by_path.contains_key(&path) {
            return Err(io::Error::new(
                io::ErrorKind::InvalidInput,
                format!("duplicate skill file: {path}"),
            ));
        }
        let mode = file.mode.unwrap_or_else(|| default_bundle_file_mode(&path));
        by_path.insert(
            path,
            BundleFile {
                contents: file.contents,
                mode,
            },
        );
    }
    Ok(by_path)
}

fn default_bundle_file_mode(path: &str) -> u32 {
    if path.starts_with("scripts/") {
        0o755
    } else {
        0o644
    }
}

fn normalize_bundle_path(value: &str) -> io::Result<Option<String>> {
    if value.is_empty()
        || value.contains('\\')
        || value.starts_with('/')
        || value.as_bytes().get(1) == Some(&b':')
    {
        return Err(io::Error::new(
            io::ErrorKind::InvalidInput,
            format!("invalid skill file path: {value}"),
        ));
    }
    for part in value.split('/') {
        if part.is_empty() || part == "." || part == ".." {
            return Err(io::Error::new(
                io::ErrorKind::InvalidInput,
                format!("invalid skill file path: {value}"),
            ));
        }
        if skip_name(part) {
            return Ok(None);
        }
    }
    Ok(Some(value.to_string()))
}

#[cfg(unix)]
pub(crate) fn mode_bits(metadata: &fs::Metadata) -> Option<u32> {
    use std::os::unix::fs::PermissionsExt;
    Some(metadata.permissions().mode() & 0o777)
}

#[cfg(not(unix))]
pub(crate) fn mode_bits(_metadata: &fs::Metadata) -> Option<u32> {
    None
}

#[cfg(unix)]
pub(crate) fn set_mode(path: &Path, mode: u32) -> io::Result<()> {
    use std::os::unix::fs::PermissionsExt;
    fs::set_permissions(path, fs::Permissions::from_mode(mode))
}

#[cfg(not(unix))]
pub(crate) fn set_mode(_path: &Path, _mode: u32) -> io::Result<()> {
    Ok(())
}

fn skip_name(name: &str) -> bool {
    name == ".git"
        || name == ".kitup.json"
        || name == ".DS_Store"
        || name.ends_with(".swp")
        || name.ends_with('~')
}
