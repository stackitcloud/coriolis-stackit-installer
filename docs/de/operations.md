# Betrieb und typische Szenarien

[← Übersicht](../../README.de.md) | [English](../en/operations.md)

Diese Anleitung beschreibt die Nutzung bestehender Ressourcen und die Wartung
einer laufenden Coriolis-Appliance. Für die Erstinstallation siehe
[Schnellstart](getting-started.md); alle Einstellungen stehen in der
[Konfigurationsreferenz](configuration.md).

## Typische Nutzungsszenarien

### Vorhandene Public IP verwenden

Eine vorhandene Public IP kann über ihre ID angegeben werden:

```yaml
public_ip: true
public_ip_id: 00000000-0000-0000-0000-000000000000
```

Alternativ über ihre Adresse:

```yaml
public_ip: true
public_ip_address: 192.0.2.10
```

Die IP darf nicht bereits einer anderen Netzwerkschnittstelle zugewiesen sein.

### Ein Image in mehreren Projekten nutzen

Das Image kann in einem zentralen Projekt verwaltet und für weitere Projekte
freigegeben werden:

```yaml
image:
  owner_project_id: 11111111-1111-1111-1111-111111111111
  share:
    project_ids:
      - 22222222-2222-2222-2222-222222222222
```

Fehlt das Image, erstellt der Installer es im Eigentümerprojekt. Das aktuelle
Zielprojekt wird ebenfalls für die Nutzung freigeschaltet.

Für eine organisationsweite Freigabe:

```yaml
image:
  owner_project_id: 11111111-1111-1111-1111-111111111111
  share:
    parent_organization: true
```

### Vorhandenes normalisiertes Image verwenden

```yaml
image:
  id: 33333333-3333-3333-3333-333333333333
```

Für ein Deployment bleibt die OVA zur Prüfung des Images und der
Hardwareanforderungen erforderlich. Die unten beschriebenen Wartungsbefehle
benötigen dagegen keine OVA.

### Bestehenden Server übernehmen

```yaml
server:
  id: 44444444-4444-4444-4444-444444444444
  name: coriolis-appliance
```

Der Server muss am konfigurierten Netzwerk angeschlossen sein. Der Installer
verwendet ihn weiter, ohne das Boot-Volume auszutauschen oder Appliance-ID,
Coriolis-Daten und Lizenz zu ersetzen.

**Vor der erstmaligen Übernahme eine Sicherung erstellen.** Aktivierter
Bootstrap kann Hostname und Admin-Kennwort ändern; aktivierte Zertifikatsverwaltung
kann die TLS-Konfiguration ändern. Für Agent-gesteuerte Funktionen muss der
STACKIT Server Agent bereits funktionieren.

Wenn nur Zustand oder Updates einer vorhandenen Appliance geprüft werden sollen,
die Wartungsbefehle statt des Deployment-Aufrufs verwenden.

### Application Load Balancer verwenden

```yaml
dns:
  enabled: true
  create_zone: true
  zone_name: my-coriolis.runs.onstackit.cloud
  record_name: appliance

exposure:
  mode: application_load_balancer
  certificate:
    email: admin@example.com
    staging: false
    renew_before_days: 30
    name_prefix: coriolis-tls
  load_balancer:
    name: coriolis-alb
    plan_id: p10
    backend_port: 80
    health_check_path: /
```

Domain, Kontaktadresse und Load-Balancer-Plan an die eigene Umgebung anpassen.

## Appliance-Wartung

| Befehl | Zweck |
|---|---|
| `status` | Installierte Version und Betriebszustand prüfen |
| `versions` | Veröffentlichte Releases und geeignete Update-Ziele ermitteln |
| `update` | Ein Update planen oder nach ausdrücklicher Freigabe ausführen |
| `update-status` | Ein gestartetes Update verfolgen und das Ergebnis verifizieren |

Die Wartung erfolgt über den STACKIT Server Agent und Run Command. Eine manuelle
SSH- oder Konsolenanmeldung ist nicht erforderlich. Die Befehle verwenden die
bestehende Appliance; sie ersetzen weder VM noch Boot-Volume. Sie führen keinen
Deployment-Bootstrap aus, ändern kein Admin-Kennwort und erneuern keine Zertifikate.

### Voraussetzungen

- Eine Konfigurationsdatei mit Projekt, Region und passenden STACKIT-Zugangsdaten.
  Eine vorhandene Deployment-Konfiguration kann weiterverwendet werden.
- Ein funktionierender Server Agent und bereits aktivierter Run Command Service.
- Berechtigungen für Server- und Volume-Abfragen sowie Run Command; für Updates
  zusätzlich Server-Stopp/-Start und Volume-Snapshots.
- Für die Versionssuche Zugriff auf die offiziellen Release Notes und die
  benötigten Container-Images über die Appliance.
- Ausreichende Snapshot-Quota und Speicherplatz.

Die Beispiele werden aus dem Projektverzeichnis ausgeführt. Relative Pfade in der
Konfiguration werden vom aktuellen Arbeitsverzeichnis aus aufgelöst.
Zunächst die eigene Konfigurationsdatei und Server-ID einsetzen:

```bash
CONFIG="config.local.yaml"
SERVER_ID="<server-id>"
```

Für Wartungsarbeiten die Server-ID ausdrücklich angeben, um die richtige
Appliance auszuwählen. Ohne `--server-id` muss der konfigurierte Servername
eindeutig sein.

### 1. Zustand prüfen

```bash
bin/coriolis-stackit status --config "$CONFIG" --server-id "$SERVER_ID"
```

Der JSON-Bericht zeigt unter anderem die installierte Version, den Zustand der
Container, Speicherbelegung, aktive Ausführungen und aktivierte Zeitpläne.
`healthy: true` bedeutet, dass die Betriebsprüfungen erfolgreich waren.
`package_manager_ready: true` bestätigt, dass der Paketmanager für die Wartung
bereit ist.

Fehlgeschlagene Prüfungen stehen unter `errors`; der Befehl endet dann mit einem
Fehlercode. Diese Ursachen vor einer Update-Planung beheben. `status` verändert
keine Appliance-Konfiguration.

### 2. Zielversion auswählen

```bash
bin/coriolis-stackit versions --config "$CONFIG" --server-id "$SERVER_ID"
```

Der Befehl listet veröffentlichte Releases und bewertet die möglichen Ziele:

- `eligible: true`: neueres Ziel im unterstützten Release-Stream, dessen
  benötigte Komponenten-Images verfügbar sind.
- `latest_compatible`: neuestes solches Ziel.
- `up_to_date_within_stream: true`: kein neueres vollständiges Ziel im geprüften
  Stream gefunden.

Ein Fehler bei der Abfrage bedeutet nicht, dass die Appliance aktuell ist.
Die Versionssuche führt kein Update aus und lädt keine Container-Layer herunter.

Es werden nur neuere numerische Releases innerhalb desselben YYMM-Streams
unterstützt. Stream-Wechsel, Downgrades und Neuinstallationen sind ausgeschlossen.
Die technische Eignung ersetzt nicht die Prüfung der
[Release Notes](https://cloudbase.github.io/coriolis-docs/reference/coriolis-release-notes.html)
und gegebenenfalls die Abstimmung des Update-Pfads mit Cloudbase. Ein Service-Update
enthält nicht zwangsläufig alle Betriebssystemänderungen einer neueren OVA.

### 3. Wartung vorbereiten und Update planen

Vor der Ausführung:

1. Ein Wartungsfenster mit Ausfallzeit einplanen.
2. Laufende Ausführungen aller Mandanten abschließen lassen und sämtliche
   Transfer-Zeitpläne deaktivieren.
3. Benutzer, Automatisierungen und externe Worker daran hindern, während der
   Wartung neue Arbeiten einzureichen.
4. Sicherstellen, dass alle geprüften Dateisysteme unter 80 % belegt sind und die
   Appliance gesund ist. Ungeklärte Änderungen an der Herstellerinstallation
   oder eine bestehende Wartungssperre müssen vorab geklärt werden.
5. Für produktive Systeme eine unabhängige Sicherung und einen erprobten
   Wiederherstellungsplan vorhalten.

Ein ausdrücklich gewähltes Ziel planen:

```bash
TARGET_VERSION="<zielversion-aus-versions>"
bin/coriolis-stackit update --config "$CONFIG" \
  --server-id "$SERVER_ID" --target-version "$TARGET_VERSION"
```

Alternativ das neueste geeignete Ziel automatisch auswählen lassen:

```bash
bin/coriolis-stackit update --config "$CONFIG" \
  --server-id "$SERVER_ID" --latest-compatible
```

Ohne `--apply` wird nur ein Plan ausgegeben. Es werden keine Dienste gestoppt,
Snapshots erstellt oder Updates ausgeführt. Im Plan das konkrete `target_version`,
die Appliance und die unter `backup` aufgeführten Volumes prüfen. Den Wert
`approval` für die Freigabe übernehmen.

`--latest-compatible` und `--target-version` schließen sich aus; der wörtliche
Wert `latest` ist keine Zielversion. Nach automatischer Auswahl für die Ausführung
vorzugsweise die im Plan genannte numerische Version verwenden.

### 4. Update mit automatischer Sicherung ausführen

Für jeden Versuch eine neue Statusdatei verwenden und dauerhaft aufbewahren:

```bash
mkdir -p -m 700 .coriolis-stackit
STATE_FILE=".coriolis-stackit/update-$(date -u +%Y%m%dT%H%M%SZ).json"

bin/coriolis-stackit update --config "$CONFIG" \
  --server-id "$SERVER_ID" --target-version "$TARGET_VERSION" \
  --apply --approve-plan "<approval-aus-dem-geprueften-plan>" \
  --confirm-server-id "$SERVER_ID" \
  --state-file "$STATE_FILE"
```

Die Freigabe gilt nur für den geprüften Zustand und das gewählte Ziel. Verändert
sich die Appliance zwischen Planung und Ausführung, einen neuen Plan erstellen
und erneut prüfen.

Der Befehl übernimmt:

1. Vorbereitung und Sicherung der Appliance-Konfiguration.
2. Geordnetes Stoppen der Appliance-Dienste und anschließend der VM.
3. Neue native STACKIT-Snapshots aller angehängten Volumes einschließlich des
   Boot-Volumes. Die VM bleibt gestoppt, bis alle Snapshots verfügbar sind.
4. Start derselben VM und Prüfung ihres Betriebszustands.
5. Start des Coriolis-Service-Updates.

Die Snapshots sind verpflichtend und werden nicht automatisch gelöscht. Sie
decken die Appliance-Volumes ab, nicht jedoch externe Datenbanken, externe Worker
oder entfernte Quell- und Zielsysteme. Nicht unterstützte Speicheranbindungen
verhindern das Update. Snapshots ersetzen kein unabhängiges Backup.

**Das CLI bis zur Rückkehr des Befehls verbunden lassen.** Erst der gestartete
Hintergrund-Updater läuft unabhängig vom lokalen Terminal weiter. Die erfolgreiche
Rückkehr von `update --apply` bestätigt seinen Start, noch nicht den Update-Erfolg.

### 5. Ergebnis prüfen

Dieselbe Statusdatei wie bei der Ausführung verwenden:

```bash
bin/coriolis-stackit update-status --config "$CONFIG" \
  --server-id "$SERVER_ID" --state-file "$STATE_FILE"
```

Den Befehl bei noch laufendem Update erneut aufrufen:

| `stage` im Wartungsjournal | Bedeutung |
|---|---|
| `running` | Der Updater wurde gestartet; das Ergebnis steht noch aus |
| `verifying` | Die abschließenden Prüfungen sind noch nicht erfolgreich |
| `verified` | Zielversion und Betriebszustand sowie erhaltene Daten-, Konfigurations- und Zertifikatsprüfungen sind bestätigt |
| `failed` | Der Updater ist fehlgeschlagen; weitere Wartungsschritte sind erforderlich |

**Erst `verified` bestätigt den erfolgreichen Abschluss.** Danach einen
repräsentativen Funktionstest durchführen, bevor Benutzer, Automatisierungen,
Worker und Zeitpläne wieder freigegeben werden.

Für einen späteren aktuellen Gesundheitscheck erneut `status` verwenden.
`update-status` zeigt nach erfolgreichem Abschluss das gespeicherte Ergebnis
dieses Update-Versuchs.

### Bei Abbruch oder Fehlern

- `update --apply` nicht blind wiederholen. Ein Timeout oder Verbindungsabbruch
  bedeutet nicht, dass die entfernte Aktion beendet wurde.
- Statusdatei und Snapshot-IDs aufbewahren. Den vorhandenen Versuch mit
  `update-status` prüfen; bei unklarem Zustand auch den VM- und Snapshot-Zustand
  in STACKIT kontrollieren.
- Eine bestehende Wartungssperre nicht ohne Prüfung entfernen. Dienste oder VM
  können absichtlich gestoppt bleiben, bis der Fehler geklärt ist.
- Es gibt keinen automatischen Rollback. Eine erforderliche Wiederherstellung
  muss den vollständigen Snapshot-Satz berücksichtigen und bei gestoppter VM
  geplant werden. Änderungen seit dem Snapshot gehen dabei verloren.
- Bei ungeklärtem Zustand die zuständige Administration oder den Support
  einbeziehen. Diagnosedaten vor einer Weitergabe auf vertrauliche Inhalte prüfen.

Snapshots nach erfolgreichem Update entsprechend der eigenen Aufbewahrungsrichtlinie
verwalten. Sie bleiben bestehen und verursachen Speicherkosten.

Weitere Hilfe: [Fehlerbehebung](troubleshooting.md),
[Coriolis aktualisieren](https://cloudbase.github.io/coriolis-docs/guides/upgrading-coriolis.html)
und [Betriebssystem- gegenüber Container-Updates](https://cloudbase.github.io/coriolis-docs/guides/security-updates-and-hardening.html).

## Erneute Deployment-Aufrufe

Ein erneuter Deployment-Aufruf verwendet passende vorhandene Ressourcen weiter.
Er ist kein Ersatz für den oben beschriebenen Update-Ablauf.

| Situation | Verhalten |
|---|---|
| Passendes Image oder passender Server vorhanden | Ressource weiterverwenden |
| Name bereits für eine nicht passende Ressource vergeben | Abbruch statt automatischem Ersatz |
| Server in einem Fehlerzustand | Keine automatische Löschung |
| Gewünschte Public IP bereits korrekt zugewiesen | Zuweisung beibehalten |
| Gewünschte Public IP an anderer Schnittstelle | Abbruch |
| DNS-A-Record vorhanden | Auf die gewünschte IP aktualisieren |
| Generiertes Admin-Kennwort bereits vorhanden | Dasselbe Kennwort weiterverwenden |
| Zertifikat und laufender Dienst aktuell | Keine erneute Ausstellung oder Umkonfiguration |
| Zertifikatseinbau scheitert | Vorherige Zertifikatskonfiguration wiederherstellen |

Bestehende Netzwerke werden nach Namen wiederverwendet; ihr Prefix und Routing
werden nicht automatisch geändert. Security-Group-Regeln werden ergänzt, aber
nicht entfernt. Solche Änderungen administrativ vorbereiten oder neue
Ressourcennamen verwenden.
