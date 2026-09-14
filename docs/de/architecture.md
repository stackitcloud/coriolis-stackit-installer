# Architektur und technischer Ablauf

[← Übersicht](../../README.de.md) | [English](../en/architecture.md)

## Detaillierter technischer Ablauf

### 1. OVA-Analyse und Vorabvalidierung

Der Installer berechnet den SHA-256 über das vollständige OVA und liest die OVF-
Metadaten. Er lehnt unter anderem zu kleine Boot- oder Scratch-Volumes, unbekannte
Availability Zones und Machine Types mit zu wenig CPU oder RAM vor dem Image-Upload
ab. Ohne explizite Availability Zone wird bevorzugt eine Zone mit dem Suffix `-m`
ausgewählt; ohne Machine Type wird der kleinste passende Typ gewählt.

### 2. Image-Suche und Wiederverwendung

Das OVA wird über das Label `coriolis-sha256` identifiziert. Wegen des STACKIT-
Label-Limits werden die ersten 32 Hex-Zeichen gespeichert. Ein verwendbares Image
trägt zusätzlich `coriolis-normalized=agent-v2`.

Die Suchreihenfolge ist:

1. lokales, verfügbares Image im Zielprojekt;
2. für das Zielprojekt sichtbares, geteiltes Image;
3. ein noch laufender eigener Image-Import;
4. Suche beziehungsweise Import im konfigurierten Image-Eigentümerprojekt.

Ein explizites `image.id` umgeht die automatische Auswahl, muss bei aktiviertem
Agent-Ablauf aber ebenfalls als `agent-v2` normalisiert sein. Ein `CREATING`-Import
wird beobachtet; ein eigener, seit mehr als 30 Minuten unveränderter Import wird als
stale entfernt und neu erzeugt.

### 3. Normalisierung auf der Hilfs-VM

Nur wenn kein geeignetes Image existiert, legt der Installer temporäre Ressourcen an:

- eine Ubuntu-Hilfs-VM mit Server Agent;
- ein Quellvolume in Größe der späteren Boot-Disk;
- ein Scratchvolume für eingehende VMDK und ausgehende QCOW2-Datei;
- eine temporäre Public IP, Security Group und ein einmaliges Ed25519-Keypair.

Der SSH-Hostkey der Hilfs-VM wird zuerst über den unabhängigen STACKIT Server Agent
ausgelesen. Der anschließende Go-SSH-Transfer akzeptiert ausschließlich diesen
gepinnten Hostkey. Ein abgebrochener VMDK-Transfer wird anhand der bereits übertragenen
Bytezahl fortgesetzt.

Auf der Hilfs-VM geschieht anschließend:

1. Installation von `qemu-utils`;
2. VMDK → RAW direkt auf das performante Quellvolume;
3. schreibbares Einhängen der Appliance-Rootpartition;
4. Offline-Installation und Aktivierung des STACKIT Server Agent;
5. Entfernen ausschließlich maschinenspezifischer Agent- und Cloud-init-Zustände;
6. RAW → QCOW2 auf das Scratchvolume;
7. Prüfung des QCOW2 und Upload über die STACKIT-Image-Upload-URL;
8. Warten auf den Image-Status `AVAILABLE`.

Das Original-OVA bleibt unverändert. Coriolis-Konfiguration, Support-SSH und
Appliance-Anwendungsdaten werden bei der Normalisierung nicht verändert.

Nach erfolgreichem Import werden die Hilfs-VM, die beiden Volumes, die temporäre
Public IP, Security Group und das Keypair entfernt. Bei einem frühen Fehler bleiben
gelabelte Volumes gegebenenfalls für Diagnose und Wiederaufnahme erhalten. Ein
Folgelauf erkennt sie und bereinigt veraltete Hilfszugänge.

### 4. Server, Netzwerk und Storage

Netzwerk und Security Group werden anhand ihrer Namen wiederverwendet, sofern keine
Netzwerk-ID vorgegeben wurde. Die Appliance-VM wird anhand von `server.id` oder
`server.name` gefunden. Eine neue VM erhält das normalisierte Image als Bootquelle,
die konfigurierte Performanceklasse und den Server Agent.

Für die Appliance sowie die Normalisierungsvolumes ist
`storage_premium_perf12` der empfohlene Default. Konvertierung, Image-Upload,
Migrationen und Backups erzeugen anhaltende I/O-Last; `perf1` ist für diese Datenpfade
häufig zu langsam. Nur die kleine Betriebssystemdisk der Hilfs-VM verwendet fest
`storage_premium_perf1`, weil die Nutzdaten auf den separaten perf12-Volumes liegen.

### 5. Appliance-Bootstrap

Nach dem Boot wartet der Installer auf den STACKIT Server Agent und führt den
Bootstrap über Run Command im Basisbetriebssystem aus. Der Coriolis-Support-SSH-
Dienst, dessen Accounts und dessen Konfiguration werden nicht verändert.

Der Bootstrap:

- setzt den Hostnamen;
- wartet auf Keystone;
- setzt das Kennwort des Coriolis-Administrators `admin` in der Domain `default`;
- persistiert das Kennwort in Kollas maßgeblicher `passwords.yml` und
  aktualisiert die lokale OpenRC-Datei;
- führt die herstellereigene Exposure-Logik aus;
- prüft danach die regenerierte OpenRC-Datei, eine echte Keystone-Anmeldung und
  den lokalen Webdienst;
- speichert Kennwort und Idempotenzmarker unter `/var/lib/coriolis-stackit` mit
  Root-only-Berechtigungen.

Ist `bootstrap.admin_password` leer, wird einmalig ein zufälliges, Appliance-
spezifisches Kennwort erzeugt. Wiederholungen liefern dasselbe Kennwort zurück.

### 6. Public IP und DNS

Im Direct-Modus verwendet der Installer in dieser Reihenfolge:

1. eine bereits an der Appliance-NIC vorhandene Public IP;
2. die über `public_ip_id` oder `public_ip_address` verlangte freie IP;
3. eine freie, vom Installer verwaltete Public IP;
4. eine neu reservierte Public IP.

Eine verlangte IP, die an eine andere NIC gebunden ist, führt zum sicheren Abbruch.
Der A-Record wird angelegt oder auf die aktuelle Adresse aktualisiert. Eine DNS-Zone
kann über ID oder Namen ausgewählt und bei `create_zone: true` automatisch erzeugt
werden.

### 7. Öffentlich vertrauenswürdiges Zertifikat

Im empfohlenen Direct-Modus erzeugt die Appliance selbst einen RSA-Schlüssel und
einen CSR für den FQDN. Der private Schlüssel verlässt die VM nie. Das Go-Programm
führt die ACME-DNS-01-Challenge über STACKIT DNS aus und überträgt nur Leaf- und
Issuer-Zertifikate zurück auf die Appliance.

Vor der Aktivierung werden Hostname, Restlaufzeit, Key-Paar und vollständige Chain
geprüft. Der Installer sichert die aktiven Herstellerdateien, installiert die neue
Kette und ruft `expose_coriolis.py` mit der privaten Interface-IP auf. Anschließend
werden ausschließlich `coriolis-web-proxy` und `coriolis-api` neu geladen. Die
tatsächlich auf 443 und 5000 ausgelieferten Leaf-Fingerprints müssen mit der aktiven
Datei übereinstimmen. Bei einem Fehler wird auf die gesicherten Dateien und den
vorherigen Hostnamen zurückgerollt.

Ein gültiges Zertifikat wird wiederverwendet. Auch wenn die Datei bereits korrekt,
der Dienst aber noch nicht neu geladen ist, wird das vorhandene Key-Paar genutzt;
es wird dafür kein unnötiges neues Zertifikat angefordert.

### 8. Optionaler Application Load Balancer

Mit `exposure.mode: application_load_balancer` terminiert ein STACKIT ALB HTTPS auf
Port 443. Das Zertifikat wird per DNS-01 ausgestellt und im STACKIT Certificate
Service gespeichert. Der ALB leitet standardmäßig unverschlüsselt auf Port 80 der
privaten Appliance-IP weiter.

Der ALB ist optional. Für den Standardfall wird das Zertifikat direkt in der
Appliance installiert, damit Weboberfläche und Coriolis-Dienste möglichst nah am
Herstellerprodukt bleiben. Der ALB betrifft nur den konfigurierten Layer-7-Webpfad;
Migrations- und Worker-Verbindungen werden nicht automatisch durch ihn geführt.

