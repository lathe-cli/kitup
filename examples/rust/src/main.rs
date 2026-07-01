use kitup::{
    directory_bundle, install_bundled_skill, AgentSelector, BaseOptions, InstallOptions, Scope,
};

fn main() -> Result<(), Box<dyn std::error::Error>> {
    let report = install_bundled_skill(&InstallOptions {
        base: BaseOptions::default(),
        app_id: "kitup-example-rust".to_string(),
        skill_bundle: directory_bundle("../../skills/kitup"),
        scope: Scope::User,
        agents: AgentSelector::Auto,
        force: false,
    })?;

    println!("{}", serde_json::to_string(&report)?);
    if !report.errors.is_empty() || !report.conflicts.is_empty() {
        std::process::exit(1);
    }
    Ok(())
}
