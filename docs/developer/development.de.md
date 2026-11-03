# Entwicklung

[← Entwicklerdokumentation](README.md) | [English](development.md)

### Projektstruktur

```text
.
├── cmd/
│   └── coriolis-stackit/
│       └── main.go            # schlanker Programmeinstieg
├── installer/                 # Deployment, Wartung und Tests
├── examples/
│   └── config.yaml            # vollständige, secret-freie Beispielkonfiguration
├── docs/
│   ├── en/                    # englische Anwenderdokumentation
│   ├── de/                    # deutsche Anwenderdokumentation
│   └── developer/             # Architektur und Entwicklung
├── Makefile
├── README.md
├── README.de.md
├── go.mod
└── go.sum
```

Das Paket `installer` bündelt die Implementierung: Die Phasen teilen Konfiguration,
Cloud-Client und Zustandsmodell. Seine Go-Schnittstellen sind keine zugesicherte
Bibliotheks-API. Projektspezifische YAML-Dateien, Credentials, OVAs, Wartungsdaten und
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

### Dokumentation pflegen

`docs/de/` und `docs/en/` enthalten ausschließlich Anwenderdokumentation:
Voraussetzungen, Konfiguration, Bedienung und Fehlerbehebung. Änderungen an
Funktionen und CLI-Parametern in beiden Sprachen nachführen.

Implementierungsentscheidungen, technische Abläufe und Entwicklungsregeln gehören
nach `docs/developer/`. Der [Architekturleitfaden](architecture.de.md) beschreibt
die gemeinsamen Sicherheitsgrenzen von Deployment und Wartung.

Instanzbezogene Untersuchungen, private Logs, Zugangsdaten und Fehler-Screenshots
gehören nicht in die versionierte Dokumentation. Allgemein erforderliches Wissen
ohne Projekt- oder Kundendaten in den Entwicklerleitfaden übernehmen.
