# Netzwerk und Sicherheit

[← Übersicht](../../README.de.md) | [English](../en/security.md)

## Netzwerk- und Sicherheitsmodell

### Dauerhafte Ingress-Regeln

Standardmäßig öffnet die Appliance-Security-Group nur TCP/443. SSH wird bewusst
nicht geöffnet oder umkonfiguriert. Der Coriolis-Support kann seinen vorgesehenen
SSH-Dienst weiterhin wie vom Hersteller vorgesehen aktivieren und verwenden.

Der STACKIT Server Agent benötigt keine Ingress-Regel; er nutzt einen separaten,
ausgehenden Managementkanal.

Für Migrationen baut die Appliance die Verbindungen zu Quell- und Ziel-APIs sowie
temporären Workern überwiegend ausgehend auf. Abhängig vom Provider können unter
anderem TCP/22, 4433, 5566 und 5986 relevant sein. Remote-Zugriffe auf Coriolis APIs
oder externe Worker können zusätzliche, eng begrenzte Ingress-Regeln erfordern. Die
maßgebliche Referenz ist die
[Coriolis Port Matrix](https://cloudbase.it/coriolis-network-ports-requirements/).

### Temporärer SSH-Zugang der Hilfs-VM

Nur während eines neuen OVA-Imports erzeugt der Installer eine separate Security
Group für TCP/22 zur Hilfs-VM. Die aktuelle Implementierung erlaubt dort temporär
`0.0.0.0/0`. Der Zugriff ist ausschließlich mit einem zufälligen Einmal-Key möglich,
und der Hostkey wird über den Server Agent gepinnt. Nach erfolgreichem Import werden
Public IP, Keypair und Security Group entfernt. Nach einem Fehler sollte der
Folgelauf zeitnah gestartet oder die gelabelte Hilfsinfrastruktur kontrolliert
bereinigt werden.

Dieser temporäre SSH-Kanal gehört nur zur Image-Erstellung. Er verändert und nutzt
nicht den Support-SSH-Dienst der Coriolis-Appliance.

### Umgang mit Secrets

- Der Service-Account-Key wird nur zur SDK-Authentifizierung gelesen.
- Credentials werden nicht in die Ergebnisstruktur aufgenommen.
- Das generierte Admin-Kennwort liegt in der Appliance root-only.
- Run-Command-Ausgaben mit dem Admin-Kennwort werden nicht live gestreamt.
- Beim direkten Zertifikat bleibt der TLS-Private-Key auf der Appliance.
- Hersteller-Reconfigure-Logs werden nicht nach außen gestreamt und nach Abschluss
  beziehungsweise Rollback entfernt.
- Ein per `--admin-password` übergebenes Kennwort kann in der Shell-History landen.

## Grenzen und bewusste Schutzmechanismen

- Genau eine Disk pro OVA wird unterstützt.
- Eine neue OVA führt zu einem neuen Image, aber niemals zum automatischen Austausch
  einer bestehenden VM.
- Es gibt keinen automatischen Daten-, Lizenz- oder Coriolis-Upgrade-Workflow.
- Ein vorhandener Server mit abweichendem Netzwerk wird nicht automatisch umgehängt.
- Eine fehlerhafte VM wird nicht automatisch gelöscht.
- Das Tool entfernt keine bestehenden Security-Group-Regeln.
- Der optionale ALB deckt nur seinen HTTPS-Webpfad ab, nicht sämtliche Coriolis-
  Migrationsverbindungen.
- Änderungen an einem vorhandenen ALB sollten nach dem Deployment separat geprüft
  werden; die Wiederverwendung orientiert sich primär an Name und Zertifikat.

Diese Grenzen verhindern, dass ein Wiederholungslauf unbeabsichtigt eine lizenzierte
oder bereits konfigurierte Appliance ersetzt.

