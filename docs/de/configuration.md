# Konfigurationsreferenz

[← Übersicht](../../README.de.md) | [English](../en/configuration.md)

## Vollständige YAML-Referenz

### Allgemein

| Schlüssel | Bedeutung | Default |
|---|---|---|
| `project_id` | STACKIT-Zielprojekt | erforderlich |
| `credentials` | Pfad zum Service-Account-Key | erforderlich |
| `ova` | Pfad zur Coriolis-OVA | erforderlich |
| `region` | STACKIT-Region | `eu01` |
| `timeout` | Maximale Laufzeit jeder einzelnen Hauptphase | `90m` |
| `poll_interval` | Pollingintervall für asynchrone Ressourcen | `15s` |
| `upload_attempts` | Transferwiederholungen | `3` |

Jede Hauptphase erhält ein frisches Timeout-Budget. Der Wert sollte erhöht werden,
wenn eine einzelne Phase bei sehr großen Images oder langsamer Anbindung länger
dauern kann. Eine Timeout-Fehlermeldung nennt die Phase, deren Budget ausgeschöpft
wurde.

### `agent` und `bootstrap`

| Schlüssel | Bedeutung | Default |
|---|---|---|
| `agent.enabled` | Server-Agent-Management aktivieren | `true` |
| `agent.enable_service` | Run Command Service im Projekt bei Bedarf automatisch aktivieren | `true` |
| `bootstrap.enabled` | Hostname und Admin-Kennwort konfigurieren | `true` |
| `bootstrap.admin_password` | Festes Admin-Kennwort; leer erzeugt ein zufälliges | leer |
| `bootstrap.print_generated_password` | Generiertes Kennwort im Ergebnis ausgeben | `true` |

Für die vollständige Automatisierung müssen Agent und Bootstrap aktiviert bleiben.
Zur automatischen Einrichtung neuer Projekte muss außerdem `agent.enable_service`
aktiviert bleiben und der Service Account die Rolle `Project Editor` besitzen.
Ein festes Kennwort in YAML liegt dort im Klartext; die Datei muss entsprechend
geschützt werden. Für Kommandozeilenwerte gilt zusätzlich das Risiko der Shell-History.

### `normalization`

| Schlüssel | Bedeutung | Default |
|---|---|---|
| `performance_class` | Performanceklasse für Quell- und Scratchvolume | `storage_premium_perf12` |
| `scratch_size_gib` | Größe des Scratchvolumes | `64` |
| `helper_image_id` | Öffentliches Ubuntu-Image der Hilfs-VM | regionsabhängige ID im Beispiel |
| `helper_machine_type` | Machine Type der Hilfs-VM | `g1a.1d` |
| `helper_boot_size_gib` | Boot-Disk der Hilfs-VM | `16` |

Das Scratchvolume muss mindestens die virtuelle Diskgröße plus die komprimierte
VMDK-Größe und 2 GiB Reserve aufnehmen. Der Installer prüft dies vor Cloud-Änderungen.

### `image`

| Schlüssel | Bedeutung | Default |
|---|---|---|
| `id` | Explizite vorhandene Image-ID | leer |
| `owner_project_id` | Zentrales Image-Eigentümerprojekt | Zielprojekt |
| `name_prefix` | Präfix neuer Images | `coriolis-appliance` |
| `disk_bus` | Virtueller Disk-Bus | `virtio` |
| `nic_model` | Virtuelles NIC-Modell | `virtio` |
| `uefi` | UEFI aktivieren | `false` |
| `secure_boot` | Secure Boot aktivieren | `false` |
| `share.parent_organization` | Mit gesamter Parent Organization teilen | `false` |
| `share.project_ids` | Liste expliziter Consumer-Projekte | `[]` |

`parent_organization` und `project_ids` schließen sich gegenseitig aus.

### `server`

| Schlüssel | Bedeutung | Default |
|---|---|---|
| `id` | Vorhandenen Server ausdrücklich übernehmen | leer |
| `name` | Name der Appliance und Suchschlüssel | `coriolis-appliance` |
| `machine_type` | Gewünschter Machine Type | `c1a.4d` |
| `availability_zone` | Gewünschte Availability Zone | automatisch, im Beispiel `eu01-m` |
| `boot_volume_size_gib` | Größe des Boot-Volumes | `48` |
| `performance_class` | Performanceklasse des Boot-Volumes | `storage_premium_perf12` |
| `keypair_name` | Optionales vorhandenes Keypair | leer |
| `delete_boot_volume_on_termination` | Boot-Volume beim Löschen der VM löschen | `false` |

Die Boot-Disk darf nicht kleiner als die im OVF deklarierte Disk sein. Ein Keypair
aktiviert nicht automatisch den Coriolis-Support-SSH-Dienst.

### `network` und `security_group`

| Schlüssel | Bedeutung | Default |
|---|---|---|
| `network.id` | Vorhandenes Netzwerk verbindlich verwenden | leer |
| `network.name` | Netzwerk suchen oder neu anlegen | `coriolis-network` |
| `network.ipv4_prefix` | Prefix eines neu angelegten Netzwerks | `10.1.100.0/24` |
| `network.routed` | Geroutetes Netzwerk anlegen | `true` |
| `security_group.name` | Security-Group-Name | `coriolis-security` |
| `security_group.ingress[]` | Gewünschte Ingress-Regeln | TCP/443 aus dem Beispiel-CIDR |

Eine Ingress-Regel enthält `protocol`, `port`, `cidr` und eine eindeutige
`description`. Fehlende Regeln werden ergänzt; vorhandene Regeln werden nicht
automatisch entfernt. Für produktive Installationen sollte TCP/443 auf bekannte
Administrator-, Proxy- oder VPN-Netze begrenzt werden, sofern kein öffentlicher
Zugriff benötigt wird.

### Public IP und DNS

| Schlüssel | Bedeutung | Default |
|---|---|---|
| `public_ip` | Public IP im Direct-Modus sicherstellen | `true` |
| `public_ip_id` | Bestimmte reservierte Public IP per ID verwenden | leer |
| `public_ip_address` | Bestimmte reservierte Public IP per Adresse verwenden | leer |
| `dns.enabled` | DNS verwalten | `false` im Code, im Beispiel aktiviert |
| `dns.create_zone` | Fehlende Zone anlegen | `false` |
| `dns.zone_id` | Vorhandene Zone per ID | leer |
| `dns.zone_name` | Zonenname oder DNS-Name | erforderlich, wenn keine ID gesetzt ist |
| `dns.record_name` | Hostname oder vollständiger FQDN | erforderlich bei aktiviertem DNS |
| `dns.ttl` | TTL des A-Records | `300` |

Ein direkt installiertes Zertifikat benötigt DNS und eine Public IP. Im ALB-Modus
zeigt der A-Record auf die externe Adresse des Load Balancers.

### `exposure`

| Schlüssel | Bedeutung | Default |
|---|---|---|
| `mode` | `direct` oder `application_load_balancer` | `direct` |
| `certificate.enabled` | Zertifikat direkt in der Appliance verwalten | `false` |
| `certificate.email` | ACME-Kontaktadresse | erforderlich bei Zertifikatsnutzung |
| `certificate.staging` | Let's-Encrypt-Staging verwenden | `false` |
| `certificate.renew_before_days` | Erneuerungsfenster | `30` |
| `certificate.name_prefix` | Präfix im Certificate Service, nur ALB | `coriolis-tls` |
| `load_balancer.name` | Name des ALB | `coriolis-alb` |
| `load_balancer.plan_id` | STACKIT-ALB-Plan | `p10` |
| `load_balancer.backend_port` | Port der Appliance | `80` |
| `load_balancer.health_check_path` | HTTP-Health-Check-Pfad | `/` |

Für erste ACME-Tests sollte `certificate.staging: true` verwendet werden. Erst nach
einem erfolgreichen Ablauf sollte auf die produktive CA umgestellt werden, um
Rate-Limits zu vermeiden.

## CLI-Überschreibungen

CLI-Parameter überschreiben die entsprechenden YAML-Werte:

| Parameter | YAML-Ziel |
|---|---|
| `--project-id` | `project_id` |
| `--credentials` | `credentials` |
| `--ova` | `ova` |
| `--region` | `region` |
| `--image-id` | `image.id` |
| `--image-owner-project-id` | `image.owner_project_id` |
| `--share-image-with-organization` | `image.share.parent_organization: true` |
| `--share-image-with-projects` | `image.share.project_ids`, kommasepariert |
| `--server-id` | `server.id` |
| `--availability-zone` | `server.availability_zone` |
| `--machine-type` | `server.machine_type` |
| `--performance-class` | `server.performance_class` |
| `--boot-volume-size` | `server.boot_volume_size_gib` |
| `--normalization-performance-class` | `normalization.performance_class` |
| `--normalization-scratch-size` | `normalization.scratch_size_gib` |
| `--normalization-helper-image-id` | `normalization.helper_image_id` |
| `--normalization-helper-machine-type` | `normalization.helper_machine_type` |
| `--normalization-helper-boot-size` | `normalization.helper_boot_size_gib` |
| `--network-id` | `network.id` |
| `--public-ip-id` | `public_ip_id`, aktiviert zugleich `public_ip` |
| `--public-ip-address` | `public_ip_address`, aktiviert zugleich `public_ip` |
| `--dns-zone-id` | `dns.zone_id` |
| `--dns-zone-name` | `dns.zone_name` |
| `--dns-name` | `dns.record_name`, aktiviert zugleich DNS |
| `--certificate-email` | `exposure.certificate.email`, aktiviert im Direct-Modus das Zertifikat |
| `--admin-password` | `bootstrap.admin_password`, aktiviert zugleich Bootstrap |
| `--enable-run-command-service` | `agent.enable_service=true` |
| `--disable-run-command-service-activation` | `agent.enable_service=false` |
| `--dry-run` | Nur lokale Validierung und Plan-Ausgabe |
| `--check-cloud` | Read-only-Cloud-Prüfung |
| `--version` | Build-Version ausgeben |

Beispiel ohne YAML für die drei Pflichtwerte:

```bash
./bin/coriolis-stackit \
  --credentials credentials.json \
  --project-id 00000000-0000-0000-0000-000000000000 \
  --ova coriolis-appliance-stackit-0.ova
```

Für alle weiteren Werte gelten die Defaults. Für reproduzierbare Installationen wird
eine versionierte YAML-Datei ohne Secrets empfohlen.

