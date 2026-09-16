use crate::bundle::{valid_skill_name, BundleMetadata};
use crate::types::InstalledMetadata;
use std::fs;
use std::io;
use std::path::Path;

pub fn read_installed_metadata(target_dir: &Path) -> io::Result<Option<InstalledMetadata>> {
    match read_metadata(target_dir) {
        MetadataState::Missing => Ok(None),
        MetadataState::Conflict(_) => Err(io::Error::new(
            io::ErrorKind::InvalidData,
            "unmanaged install metadata",
        )),
        MetadataState::Managed(metadata) => Ok(Some(*metadata)),
    }
}

pub(crate) enum MetadataState {
    Missing,
    Conflict(&'static str),
    Managed(Box<InstalledMetadata>),
}

impl MetadataState {
    pub(crate) fn for_owner(self, app_id: &str, skill_name: &str) -> Self {
        match self {
            Self::Managed(metadata) if metadata.skill_name != skill_name => {
                Self::Conflict("unmanaged")
            }
            Self::Managed(metadata) if metadata.app_id != app_id => {
                Self::Conflict("owner-mismatch")
            }
            state => state,
        }
    }
}

pub(crate) fn write_metadata(target_dir: &Path, metadata: &InstalledMetadata) -> io::Result<()> {
    let mut data = serde_json::to_vec_pretty(metadata)?;
    data.push(b'\n');
    fs::write(target_dir.join(".kitup.json"), data)
}

pub(crate) fn installed_metadata(
    app_id: &str,
    skill_name: &str,
    hash: &str,
    bundle_metadata: &BundleMetadata,
) -> InstalledMetadata {
    InstalledMetadata {
        schema_version: 1,
        app_id: app_id.to_string(),
        skill_name: skill_name.to_string(),
        source: bundle_metadata.source.clone(),
        hash: hash.to_string(),
        source_id: bundle_metadata.source_id.clone(),
        version: bundle_metadata.version.clone(),
        cli_version: bundle_metadata.cli_version.clone(),
        cli_revision: bundle_metadata.cli_revision.clone(),
        provenance: bundle_metadata.provenance.clone(),
    }
}

pub(crate) fn read_metadata(target_dir: &Path) -> MetadataState {
    if !target_dir.exists() {
        return MetadataState::Missing;
    }
    let Ok(data) = fs::read(target_dir.join(".kitup.json")) else {
        return MetadataState::Conflict("unmanaged");
    };
    match serde_json::from_slice::<InstalledMetadata>(&data) {
        Ok(mut meta) if is_owned_metadata(&meta) => {
            for field in [
                &mut meta.source_id,
                &mut meta.version,
                &mut meta.cli_version,
                &mut meta.cli_revision,
            ] {
                if field.as_deref() == Some("") {
                    *field = None;
                }
            }
            MetadataState::Managed(Box::new(meta))
        }
        _ => MetadataState::Conflict("unmanaged"),
    }
}

fn is_owned_metadata(meta: &InstalledMetadata) -> bool {
    meta.schema_version == 1
        && !meta.app_id.is_empty()
        && valid_skill_name(&meta.skill_name)
        && (meta.source == "bundled" || meta.source == "github")
        && !meta.hash.is_empty()
}
