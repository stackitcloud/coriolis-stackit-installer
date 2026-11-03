# Coriolis STACKIT Installer

[English](README.md) | **Deutsch**

Eine Cloudbase Coriolis Appliance mit einem Binary und einer YAML-Konfiguration
auf STACKIT bereitstellen und warten. Terraform, die STACKIT CLI und eine manuelle
Einrichtung über die VM-Konsole sind nicht erforderlich.

Für ein Deployment werden ein STACKIT Service-Account-Key, Projekt-ID, Region und
eine Coriolis-OVA benötigt. Die Wartung einer vorhandenen Appliance benötigt keine
OVA.

## Funktionsumfang

- Eine neue Appliance bereitstellen oder einen bestehenden Server ausdrücklich
  übernehmen.
- Vorhandene Netzwerke, Images und Public IPs verwenden oder benötigte Ressourcen
  anlegen.
- Images zwischen Projekten teilen oder in einem zentralen Image-Projekt verwalten.
- Hostname und Admin-Kennwort der Appliance konfigurieren.
- HTTPS mit direkt installiertem Zertifikat oder optionalem STACKIT Application
  Load Balancer bereitstellen.
- Betriebszustand prüfen und geeignete Update-Ziele ermitteln.
- Ausdrücklich freigegebene Service-Updates im selben Release-Stream mit
  verpflichtenden kalten Snapshots aller Appliance-Volumes ausführen.
- Bei wiederholten Deployment-Aufrufen passende Ressourcen weiterverwenden, statt
  die Appliance auszutauschen.

Das Deployment aktualisiert eine vorhandene Appliance niemals durch Austausch
ihrer VM oder des Boot-Volumes. Für Service-Updates dienen die separaten
Wartungsbefehle.

## Einstieg

Mit den [Voraussetzungen](docs/de/prerequisites.md) beginnen und anschließend den
[Schnellstart](docs/de/getting-started.md) durchgehen.

Mit einem passenden Binary und einer eigenen Konfigurationsdatei:

```bash
# Konfiguration und OVA prüfen, ohne Cloud-Ressourcen zu verändern.
bin/coriolis-stackit --config config.local.yaml --dry-run

# Cloud-Zugriff und Platzierung prüfen.
bin/coriolis-stackit --config config.local.yaml --check-cloud

# Deployment ausführen.
bin/coriolis-stackit --config config.local.yaml
```

[examples/config.yaml](examples/config.yaml) als Ausgangspunkt verwenden und die
Beispielwerte ersetzen. Ausgewählte Kommandozeilenparameter überschreiben
YAML-Einstellungen. Alle Optionen stehen in der
[Konfigurationsreferenz](docs/de/configuration.md).

**Zugangsdaten und Deployment-Ausgabe schützen.** Das Ergebnis kann das generierte
Admin-Kennwort enthalten. Konfigurationen mit Secrets, Service-Account-Keys, OVAs
und Ergebnisdateien weder einchecken noch in öffentlichen Build-Logs speichern.

## Bestehende Appliance warten

```bash
SERVER_ID="<server-id>"

bin/coriolis-stackit status --config config.local.yaml --server-id "$SERVER_ID"
bin/coriolis-stackit versions --config config.local.yaml --server-id "$SERVER_ID"

# Nur planen; führt kein Update aus.
bin/coriolis-stackit update --config config.local.yaml \
  --server-id "$SERVER_ID" --latest-compatible
```

Ein Update erfordert einen geprüften Plan, eine ausdrückliche Freigabe und ein
Wartungsfenster. Das CLI stoppt Dienste und VM, erstellt neue native
STACKIT-Snapshots, startet dieselbe Appliance wieder und beginnt das Service-Update.
Erst die erfolgreiche Verifikation mit `update-status` bestätigt den Abschluss.

Die vollständigen Schritte für Freigabe, Sicherung, Ausführung und Prüfung stehen
in der [Wartungsanleitung](docs/de/operations.md#appliance-wartung).

## Ablauf und Dauer eines Deployments

Das erste Deployment importiert die OVA und benötigt vorübergehend zusätzliche
Cloud-Ressourcen. Weitere Deployments können das vorbereitete Image verwenden.
Die Dauer hängt von Image-Größe, Uploadgeschwindigkeit, Storage-Performance und
STACKIT-Verfügbarkeit ab:

| Szenario | Unverbindlicher Richtwert |
|---|---|
| Erstes Deployment einschließlich Image-Import | 40–100 Minuten |
| Deployment mit vorhandenem vorbereitetem Image | 8–25 Minuten |
| Wiederholung ohne wesentliche Änderungen | 2–10 Minuten |
| Optionaler Load Balancer | Meist zusätzlich 5–15 Minuten |

Dies sind Schätzungen, keine Fristen. `timeout` sollte für die längste einzelne
Deployment-Phase ausreichen. Als Storage-Performanceklasse wird
`storage_premium_perf12` empfohlen.

Fortschrittsmeldungen erscheinen auf stderr, das abschließende JSON-Ergebnis auf
stdout. Ein `[DONE]` für eine einzelne Phase bedeutet nicht, dass der gesamte
Ablauf abgeschlossen ist. Die Rückkehr des Befehls abwarten und die Prüfungen der
jeweiligen Anleitung durchführen.

## Dokumentation

### Anwenderanleitungen

| Anleitung | Inhalt |
|---|---|
| [Voraussetzungen](docs/de/prerequisites.md) | Arbeitsplatz, Berechtigungen, Quotas und Verbindungen |
| [Schnellstart](docs/de/getting-started.md) | Vorbereiten, validieren und bereitstellen |
| [Konfiguration](docs/de/configuration.md) | YAML-Einstellungen und Kommandozeilenparameter |
| [Betrieb](docs/de/operations.md) | Vorhandene Ressourcen, Appliance-Status und sichere Updates |
| [Netzwerk und Sicherheit](docs/de/security.md) | Zugriffsregeln, Zugangsdaten, Sicherungen und Grenzen |
| [Fehlerbehebung](docs/de/troubleshooting.md) | Probleme bei Deployment und Wartung klären |

### Für Mitwirkende und Maintainer

Architektur, Implementierungsdetails und Entwicklungsanleitungen stehen getrennt
in der [Entwicklerdokumentation](docs/developer/README.md).

## Mitwirken

Beiträge sind willkommen. Die [Beitragsrichtlinien](CONTRIBUTING.md) und den
[Verhaltenskodex](CODE_OF_CONDUCT.md) beachten. Bei sichtbaren Verhaltensänderungen
beide Sprachfassungen der Anwenderanleitungen aktualisieren.

## Lizenz

Installer, Quellcode, Dokumentation und Beispiele stehen unter der
[Apache License, Version 2.0](LICENSE) (`Apache-2.0`).

Für die Cloudbase-Coriolis-Appliance und Drittkomponenten gelten weiterhin deren
eigene Lizenzen und Bedingungen.
