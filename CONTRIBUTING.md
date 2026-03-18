# Contributing to MXHook

Thanks for your interest in contributing to MXHook!

## Development Setup

1. **Prerequisites:** Go 1.26+, Docker, Docker Compose

2. **Clone and build:**
   ```bash
   git clone https://github.com/mxhook/mxhook.git
   cd mxhook
   make build
   ```

3. **Run locally:**
   ```bash
   make dev
   ```
   This starts the SMTP server on port 2525 and PostgreSQL on port 5432.

4. **Run tests:**
   ```bash
   make test
   ```

## Submitting Changes

1. Fork the repository
2. Create a feature branch (`git checkout -b my-feature`)
3. Make your changes
4. Run `make test` and `make lint`
5. Commit with a clear message
6. Open a pull request

## Code Style

- Follow standard Go conventions
- Run `gofumpt` before committing
- Keep changes focused — one feature or fix per PR

## Reporting Issues

Open an issue at https://github.com/mxhook/mxhook/issues with:
- What you expected
- What happened
- Steps to reproduce

## License

By contributing, you agree that your contributions will be licensed under the Apache 2.0 License.
