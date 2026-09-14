# Entwicklung

[← Übersicht](../../README.de.md) | [English](../en/development.md)

### Projektstruktur

```text
.
├── cmd/
│   └── coriolis-stackit/
│       └── main.go            # schlanker Programmeinstieg
├── internal/
│   └── installer/             # Deploymentlogik und Unit-Tests
├── examples/
│   └── config.yaml            # vollständige, secret-freie Beispielkonfiguration
├── docs/
│   ├── en/                    # ausführliche englische Dokumentation
│   └── de/                    # ausführliche deutsche Dokumentation
├── Makefile
├── README.md
├── README.de.md
├── go.mod
└── go.sum
```

`internal/installer` ist absichtlich ein gemeinsames internes Paket: Die Phasen
teilen Konfiguration, Cloud-Client und Zustandsmodell und bilden keine öffentliche
Go-Bibliothek. Projektspezifische YAML-Dateien, Credentials, OVAs, Wartungsdaten und
gebaute Binaries bleiben durch `.gitignore` ausschließlich lokal.

### Bauen und testen

```bash
go test ./...
go vet ./...
go build -trimpath -o bin/coriolis-stackit ./cmd/coriolis-stackit
```

Oder über das Makefile:

```bash
make test
make build
make check
```

Die Cloud-Operationen verwenden die offiziellen STACKIT Go SDKs für IaaS, Run
Command, DNS, ALB und Certificates. Projektgebundene Diagnose- und
Integrationstests gehören nicht zum veröffentlichten Quellbestand; sie dürfen nur
lokal und gegen disposable Testsysteme ausgeführt werden.
