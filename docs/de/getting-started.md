# Schnellstart

[← Übersicht](../../README.de.md) | [English](../en/getting-started.md)

## Schnellstart

### 1. Binary bauen

Dieser Schritt entfällt, wenn bereits ein passendes Binary vorliegt.

```bash
make check
```

Das Binary wird als `bin/coriolis-stackit` erzeugt. `make check` führt vorher die
Go-Tests aus.

### 2. Konfiguration anlegen

[`examples/config.yaml`](../../examples/config.yaml) enthält alle Einstellungen. Für ein
neues Projekt empfiehlt sich eine eigene Datei:

```bash
cp examples/config.yaml config.yaml
```

Ein minimales, für den direkten HTTPS-Zugriff geeignetes Beispiel:

```yaml
project_id: 00000000-0000-0000-0000-000000000000
credentials: credentials.json
ova: coriolis-appliance-stackit-0.ova
region: eu01

server:
  name: coriolis-appliance
  machine_type: c1a.4d
  availability_zone: eu01-m
  boot_volume_size_gib: 48
  performance_class: storage_premium_perf12

network:
  name: coriolis-network
  ipv4_prefix: 10.1.100.0/24
  routed: true

public_ip: true

dns:
  enabled: true
  create_zone: true
  zone_name: my-coriolis.runs.onstackit.cloud
  record_name: appliance
  ttl: 300

exposure:
  mode: direct
  certificate:
    enabled: true
    email: admin@example.com
    staging: false
    renew_before_days: 30

bootstrap:
  enabled: true
  admin_password: ""
  print_generated_password: true
```

`project_id` bestimmt nicht automatisch die Region. `region` sollte deshalb immer
bewusst gesetzt werden. Die IDs öffentlicher Helper-Images sind regionsabhängig.

### 3. Lokal prüfen

```bash
./bin/coriolis-stackit --config config.yaml --dry-run
```

`--dry-run` liest und validiert die Konfiguration, analysiert das OVA und gibt den
aufgelösten Plan als JSON aus. Es werden keine Cloud-Ressourcen verändert.

### 4. Cloud-Zugriff und Platzierung prüfen

```bash
./bin/coriolis-stackit --config config.yaml --check-cloud
```

Dieser Check authentifiziert sich, prüft Region, Availability Zone, Machine Type
und – soweit aktiviert – den DNS- beziehungsweise ALB-Zugriff. Er reserviert keine
Ressourcen und ersetzt keine vollständige Quota-Prüfung.

### 5. Deployment starten

```bash
./bin/coriolis-stackit --config config.yaml
```

Der erste Lauf kann wegen Upload, zwei Konvertierungen und Image-Import deutlich
länger dauern. Für den VMDK-Transfer sowie den Image-Upload wird regelmäßig ein
prozentualer Fortschritt mit Datenmenge und Übertragungsrate ausgegeben.

### 6. Ergebnis sicher speichern

Bei Erfolg schreibt das Programm ein JSON-Objekt nach stdout, beispielsweise:

```json
{
  "ProjectID": "00000000-0000-0000-0000-000000000000",
  "Region": "eu01",
  "ImageID": "...",
  "NetworkID": "...",
  "ServerID": "...",
  "SecurityGroupID": "...",
  "PublicIP": "192.0.2.10",
  "LoginURL": "https://appliance.my-coriolis.runs.onstackit.cloud",
  "LoginUser": "admin",
  "generated_password": "..."
}
```

Wenn `bootstrap.print_generated_password: true` gesetzt ist, enthält die Ausgabe
das generierte Kennwort. Die Ausgabe sollte dann direkt in einen geschützten Secret
Store übernommen und nicht in Build-Logs archiviert werden. Bei `false` wird das
Kennwort nicht ausgegeben.

### 7. Wiederholung testen

Der gleiche Befehl darf erneut ausgeführt werden:

```bash
./bin/coriolis-stackit --config config.yaml
```

Ein erfolgreicher Wiederholungslauf verwendet Image und Infrastruktur erneut. Bei
einem aktuellen direkten Zertifikat erscheint `appliance certificate is current`;
es findet dann weder eine neue ACME-Anforderung noch ein Container-Reconfigure statt.
