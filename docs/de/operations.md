# Betrieb und typische Szenarien

[← Übersicht](../../README.de.md) | [English](../en/operations.md)

## Typische Nutzungsszenarien

### Vorhandene Public IP verwenden

Per ID:

```yaml
public_ip: true
public_ip_id: 00000000-0000-0000-0000-000000000000
```

Oder per Adresse:

```yaml
public_ip: true
public_ip_address: 192.0.2.10
```

### Zentrales Image-Projekt

```yaml
image:
  owner_project_id: 11111111-1111-1111-1111-111111111111
  share:
    project_ids:
      - 22222222-2222-2222-2222-222222222222
```

Fehlt das Image, normalisiert der Installer das OVA im Eigentümerprojekt. Das
aktuelle Zielprojekt wird automatisch als Consumer ergänzt, sofern nicht mit der
gesamten Parent Organization geteilt wird.

Organisationsweite Freigabe:

```yaml
image:
  owner_project_id: 11111111-1111-1111-1111-111111111111
  share:
    parent_organization: true
```

### Vorhandenes normalisiertes Image explizit verwenden

```yaml
image:
  id: 33333333-3333-3333-3333-333333333333
```

Die OVA bleibt trotzdem erforderlich, weil ihr Hash und ihre Hardwareanforderungen
für Validierung und Zuordnung verwendet werden.

### Bestehenden Server übernehmen

```yaml
server:
  id: 44444444-4444-4444-4444-444444444444
  name: coriolis-appliance
```

Der Installer ersetzt das Boot-Volume nicht und verändert weder Appliance-ID noch
Coriolis-Daten oder Lizenz. Er verlangt aber, dass der Server am konfigurierten
Netzwerk hängt, und ergänzt die Security Group bei Bedarf.

Wichtig: Aktivierter Bootstrap oder Zertifikatsbetrieb verändert anschließend
Hostname, Admin-Kennwort beziehungsweise TLS-Konfiguration des übernommenen Servers.
Vor der erstmaligen Übernahme einer produktiven, lizenzierten Appliance ist deshalb
ein Volume-Backup erforderlich. Für Agent-gesteuerte Schritte muss auf dem bestehenden
Server bereits ein funktionsfähiger STACKIT Server Agent vorhanden sein.

### Optionaler ALB

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

## Idempotenz und Verhalten bei Fehlern

Der Installer verwendet keine lokale State-Datei. Er erkennt Ressourcen über IDs,
Namen, Beziehungen und Labels wie `managed-by`, `coriolis-sha256` und
`coriolis-normalized`.

| Situation | Verhalten |
|---|---|
| Image ist bereits verfügbar | Wiederverwenden |
| Eigenes Image ist noch `CREATING` | Bis zum Endstatus beobachten |
| Image-Import ist erkennbar stale | Alten Import entfernen und neu starten |
| VMDK-Transfer wurde unterbrochen | Ab der vorhandenen Byteposition fortsetzen |
| Normalisierte Volumes sind vorhanden | Für die Fortsetzung wiederverwenden |
| Servername existiert mit anderem Image-Label | Abbruch, kein impliziter Ersatz |
| Server ist `ERROR` oder `DELETED` | Abbruch, keine automatische Löschung |
| Public IP ist bereits an der richtigen NIC | Wiederverwenden |
| Verlangte Public IP hängt an anderer NIC | Abbruch |
| DNS-A-Record existiert | Auf gewünschte IP aktualisieren |
| Admin-Kennwort wurde bereits generiert | Dasselbe Appliance-Kennwort wiederverwenden |
| Zertifikat und laufender Dienst sind aktuell | Keine ACME- oder Reconfigure-Aktion |
| Zertifikatseinbau scheitert | Herstellerdateien und Hostname zurückrollen |
| Run Command liefert vorübergehenden API-Fehler | Polling beziehungsweise Bootstrap begrenzt wiederholen |

Bestehende Netzwerke werden nach Namen wiederverwendet; Prefix und Routing eines
bereits vorhandenen Netzwerks werden nicht automatisch geändert. Security-Group-
Regeln werden ergänzt, aber nicht entfernt. Eine Änderung dieser Parameter sollte
daher bewusst mit neuen Ressourcennamen oder administrativ vorbereitet werden.

