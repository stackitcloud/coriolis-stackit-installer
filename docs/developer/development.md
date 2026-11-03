# Development

[← Developer documentation](README.md) | [Deutsch](development.de.md)

### Project layout

```text
.
├── cmd/
│   └── coriolis-stackit/
│       └── main.go            # small executable entry point
├── installer/                 # deployment, maintenance, and tests
├── examples/
│   └── config.yaml            # complete, secret-free example
├── docs/
│   ├── en/                    # English user documentation
│   ├── de/                    # German user documentation
│   └── developer/             # architecture and development
├── Makefile
├── README.md
├── README.de.md
├── go.mod
└── go.sum
```

The `installer` package contains the implementation. Its phases share configuration,
cloud clients, and a state model; its Go interfaces are not a guaranteed library
API. Project-specific YAML, credentials, OVAs, maintenance artifacts, and built
binaries remain local through `.gitignore`.

### Build and test

```bash
go test ./...
go vet ./...
go build -trimpath -o bin/coriolis-stackit ./cmd/coriolis-stackit
```

Or use the Makefile:

```bash
make test
make build
make check
```

Cloud operations use the official STACKIT Go SDKs for IaaS, Run Command, Service
Enablement, DNS, ALB, and Certificates. Project-bound diagnostics and integration
tests are not part of the published source; run them only locally and against
disposable test systems.

### Maintaining documentation

`docs/de/` and `docs/en/` contain user documentation only: prerequisites,
configuration, operation, and troubleshooting. Update both languages when
features or CLI options change.

Implementation decisions, technical workflows, and development rules belong in
`docs/developer/`. The [architecture guide](architecture.md) describes the shared
safety boundaries for deployment and maintenance.

Instance-specific investigations, private logs, credentials, and error screenshots
do not belong in versioned documentation. Preserve generally useful knowledge in
the developer guide without project or customer data.
