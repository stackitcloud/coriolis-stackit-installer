# Contributing

Contributions to the Coriolis STACKIT Installer are welcome: bug reports,
documentation improvements, tests, and code. Please follow our
[Code of Conduct](CODE_OF_CONDUCT.md).

## Discussing changes and reporting bugs

Search existing issues and pull requests before opening a new one. Small fixes
can go directly into a pull request. For larger features, new dependencies, or
changes to the deployment workflow, open an issue first to discuss the approach.

For a bug report, include:

- The installer version or commit and your operating system and architecture.
- Steps to reproduce the problem.
- Expected and actual behavior.
- Relevant, sanitized logs and a minimal configuration, where useful.

Remove credentials, tokens, private keys, passwords, and sensitive customer or
project information before sharing files or logs. Do not attach appliance OVAs.
This repository covers the installer; appliance licensing and vendor support
remain outside its scope.

## Security vulnerabilities

Do not disclose vulnerability details in a public issue or pull request. Check
the repository's [Security tab](https://github.com/stackitcloud/coriolis-stackit-installer/security)
for a **Report a vulnerability** option and use it if available. If private
vulnerability reporting is unavailable, open an issue asking maintainers to
arrange a private reporting channel, without including vulnerability details.
Do not use security advisories for Code of Conduct reports.

## Development workflow

1. Fork the repository and create a branch from `main`.
2. Use the Go version required by [go.mod](go.mod), or a compatible newer version.
3. Keep changes focused and add or update tests for changed behavior.
4. Format changed Go files with `gofmt -w path/to/changed.go`.
5. Run the checks below from the repository root.

```sh
go test ./...
go vet ./...
go build -trimpath -o bin/coriolis-stackit ./cmd/coriolis-stackit
```

`make check` runs tests and the build; run `go vet ./...` separately.
See the [development guide](docs/en/development.md) for more background.

Preserve safe repeated deployments and interrupted-run recovery. Changes must
respect existing resources and keep credentials out of logs and error messages.
Add regression tests for bug fixes where practical. Run cloud integration checks
only against disposable systems you are authorized to use; these operations can
create billable resources. Ordinary contributions do not require cloud access.

Update the English and German documentation and configuration examples when
user-visible behavior or settings change. Keep local credentials, configurations,
appliance images, and build artifacts out of commits.

## Opening a pull request

Describe the problem, the resulting behavior, and how you verified the change.
Link related issues and mention compatibility changes or checks you could not
run. Maintainers review contributions and may request changes before merging.
English is preferred for shared issues and pull requests.

## License

By submitting a contribution for inclusion in this project, you agree that it is
provided under the project's [Apache License, Version 2.0](LICENSE). Only submit
work you have the right to contribute. Preserve applicable third-party license
and attribution notices, and identify any new dependencies and their licenses.
