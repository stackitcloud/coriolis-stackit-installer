# Development

[← Overview](../../README.md) | [Deutsch](../de/development.md)

### Project layout

```text
.
├── cmd/
│   └── coriolis-stackit/
│       └── main.go            # small executable entry point
├── internal/
│   └── installer/             # deployment logic and unit tests
├── examples/
│   └── config.yaml            # complete, secret-free example
├── docs/
│   ├── en/                    # detailed English documentation
│   └── de/                    # detailed German documentation
├── Makefile
├── README.md
├── README.de.md
├── go.mod
└── go.sum
```

`internal/installer` intentionally remains one internal package. Its phases share
configuration, cloud clients, and a state model and do not form a public Go
library. Project-specific YAML, credentials, OVAs, maintenance artifacts, and built
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
