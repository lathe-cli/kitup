# Examples

Tiny consumer CLIs that bundle `skills/kitup` and install it through the local SDK.

The examples show the smallest installer primitive integration: pass `appId`, `skillBundle`, and `scope`, then print the structured report.

The temporary `HOME` wrapper keeps the demos from writing to your real user skill directory and provides a Codex detection path for the SDK's default auto agent selection.

```bash
tmp="$(mktemp -d)" && mkdir -p "$tmp/.codex" && HOME="$tmp" pnpm --dir examples/ts install-skill

cd examples/go
tmp="$(mktemp -d)" && mkdir -p "$tmp/.codex" && HOME="$tmp" go run .

cd examples/rust
tmp="$(mktemp -d)" && mkdir -p "$tmp/.codex" && CARGO_HOME="${CARGO_HOME:-$HOME/.cargo}" RUSTUP_HOME="${RUSTUP_HOME:-$HOME/.rustup}" HOME="$tmp" cargo run --quiet

cd examples/python
tmp="$(mktemp -d)" && mkdir -p "$tmp/.codex" && HOME="$tmp" uv run python main.py
```

Production CLIs should call the workflow API before installing so explicit agents, `*`, `--yes`, TTY, non-TTY, summary confirmation, scope prompting, and cancellation behavior stay aligned across languages.
