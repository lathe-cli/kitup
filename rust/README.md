# kitup

Rust SDK for `kitup`, a shared installer for bundled Agent Skills.

Embed a skill directory in a released Rust CLI with:

```rust
static SKILL: include_dir::Dir<'_> =
    include_dir::include_dir!("$CARGO_MANIFEST_DIR/skills/mycli");

let bundle = kitup::include_dir_bundle(&SKILL);
```

Enable the `kitup` crate feature `include-dir` and add a direct `include_dir` dependency for the macro.

See the repository README for product scope and examples:
https://github.com/lathe-cli/kitup
