#!/usr/bin/env bash
# Provision the devcontainer: install mise, then let it install the pinned
# toolchain from mise.toml so local versions match CI exactly.
set -euo pipefail

if ! command -v mise >/dev/null 2>&1; then
  curl -fsSL https://mise.run | sh
  export PATH="$HOME/.local/bin:$PATH"
fi

for rc in "$HOME/.bashrc" "$HOME/.zshrc"; do
  [ -f "$rc" ] || continue
  grep -q 'mise activate' "$rc" && continue
  shell=$(basename "$rc" | sed 's/^\.//;s/rc$//')
  printf '\neval "$(mise activate %s)"\n' "$shell" >>"$rc"
done

# mise refuses to run untrusted config files.
mise trust --yes /workspace/mise.toml
mise install

mise exec -- make worker-deps

cat <<'EOF'

mxhook devcontainer ready.

  make help          list every target
  make check         gofmt, vet, tests, race
  make check-all     the above plus worker typecheck/lint and schema apply
  make test-postgres integration tests against the postgres service

TEST_DATABASE_URL is preset, so postgres-gated tests run instead of skipping.
EOF
