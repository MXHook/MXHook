# Contributing to MXHook

This guide covers how changes are proposed, reviewed, and merged. For the
toolchain, local setup, and the commands CI runs, see
[DEVELOPING.md](DEVELOPING.md). For deployment and usage, start with the
[README](README.md).

## Use this guide when

- Submitting a bug fix or feature
- Reviewing a PR

## Find something to work on

Check the [issue tracker](https://github.com/MXHook/MXHook/issues) for labels like `good first issue` or `help wanted`.

## Submit a change

1. **Branch:** Branch from `next`, the default branch, named
   `<type>/<kebab-topic>` with a Conventional Commit type (`feat/`, `fix/`,
   `docs/`, `chore/`, `ci/`, `build/`, `refactor/`, `perf/`, `test/`,
   `style/`), e.g. `feat/maildir-quota`.
2. **Atomic Changes:** Keep PRs focused. One logical change per PR.
3. **Tests:** All changes must include corresponding tests. Aim to match or improve existing coverage.
4. **Documentation:**
    - If you change user-facing behavior, update `README.md` and the relevant
      file under `docs/`.
    - If you change the internal structure, update `docs/architecture.md`.
5. **Checks:** Run `make check` and `make lint` before pushing (see
   [DEVELOPING.md](DEVELOPING.md) for the worker and schema checks).
6. **Commit:** [Conventional Commits](https://www.conventionalcommits.org/):
   `<type>(<scope>): <subject>`, lowercase imperative subject. `feat`, `fix`
   and `perf` appear in the changelog; `docs`, `test`, `ci`, `build`,
   `refactor`, `chore` and `style` do not. CI changes use `ci:`. Breaking
   changes take `!` or a `BREAKING CHANGE:` footer. No sign-off footer.
7. **PR:** The title is a Conventional Commit (no `(#N)` suffix). Describe the
   root cause or gap and how you verified the change. One maintainer approval
   merges it; PRs are squash-merged.

## Architecture & Design

MXHook follows a pipeline-based architecture. Before proposing a major change, please review:
- [Architecture Overview](docs/architecture.md)

## Code of Conduct

This project follows the [Contributor Covenant 2.1](CODE_OF_CONDUCT.md).
Report unacceptable behavior to conduct@mxhook.dev.

## Security

Do not open public issues for vulnerabilities. Email security@mxhook.dev; see the
[security policy](https://github.com/MXHook/.github/blob/main/SECURITY.md).

## Where to ask questions

- Technical questions: Open a [Discussion](https://github.com/MXHook/MXHook/discussions) on GitHub.
- Bugs: Open an [Issue](https://github.com/MXHook/MXHook/issues).

## License

By contributing, you agree that your contributions are licensed under the
GNU Affero General Public License v3.0 only (`AGPL-3.0-only`), the license of
this repository. Contributions to `ee/` are licensed under the Business Source
License 1.1 (see `ee/LICENSE`).
