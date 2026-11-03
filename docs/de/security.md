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
Group für TCP/22 zur Hilfs-VM. Dieser Port ist vorübergehend aus `0.0.0.0/0`
erreichbar; die Anmeldung erfordert einen einmaligen Schlüssel. Die eigene
Sicherheitsrichtlinie muss diesen vorübergehenden Zugriff erlauben.
Nach erfolgreichem Import werden
Public IP, Keypair und Security Group entfernt. Nach einem Fehler sollte der
Folgelauf zeitnah gestartet oder die gelabelte Hilfsinfrastruktur kontrolliert
bereinigt werden.

Dieser temporäre SSH-Kanal gehört nur zur Image-Erstellung. Er verändert und nutzt
nicht den Support-SSH-Dienst der Coriolis-Appliance.

### Zugangsdaten und Ergebnisse schützen

- Service-Account-Keys und Konfigurationen mit Kennwörtern nur für berechtigte
  Benutzer lesbar speichern und nicht einchecken.
- Die Deployment-Ausgabe kann das generierte Admin-Kennwort enthalten. Sie gehört
  in einen geschützten Secret Store, nicht in öffentliche Build-Logs.
- Ein per `--admin-password` übergebenes Kennwort kann in Shell-History und
  Prozessargumenten sichtbar sein. Diesen Weg in gemeinsam genutzten Umgebungen
  vermeiden.
- Beim direkt installierten Zertifikat bleibt der private Schlüssel auf der
  Appliance.
- Wartungsdateien, Backups und Diagnosedaten geschützt aufbewahren. Vor einer
  Weitergabe an den Support vertrauliche Inhalte entfernen.

### Sicherungen und Wartung

Service-Updates erfordern kalte Snapshots aller Appliance-Volumes und ein exklusives
Wartungsfenster. Sie ersetzen keine unabhängige Sicherung. Externe Datenbanken,
Worker sowie entfernte Quell- und Zielsysteme sind nicht mitgesichert.

Die Statusdatei bis zum Abschluss aufbewahren und die Appliance erst nach
erfolgreicher Verifikation und Funktionstest wieder freigeben. Es gibt keinen
automatischen Rollback. Siehe [Wartungsanleitung](operations.md#appliance-wartung).

## Grenzen und bewusste Schutzmechanismen

- Genau eine Disk pro OVA wird unterstützt.
- Eine neue OVA führt zu einem neuen Image, aber niemals zum automatischen Austausch
  einer bestehenden VM.
- Service-Updates erfolgen nur über den ausdrücklich freigegebenen
  Wartungsablauf. Release-Stream-Wechsel und Betriebssystem-Upgrades sind nicht
  Teil dieses Ablaufs.
- Daten und Lizenzen werden nicht automatisch auf eine andere Appliance übertragen.
- Ein vorhandener Server mit abweichendem Netzwerk wird nicht automatisch umgehängt.
- Eine fehlerhafte VM wird nicht automatisch gelöscht.
- Das Tool entfernt keine bestehenden Security-Group-Regeln.
- Der optionale ALB deckt nur seinen HTTPS-Webpfad ab, nicht sämtliche Coriolis-
  Migrationsverbindungen.
- Nach Änderungen am ALB Erreichbarkeit, Backend-Verbindungen und Zertifikat
  separat prüfen.

Diese Grenzen verhindern, dass ein Wiederholungslauf unbeabsichtigt eine lizenzierte
oder bereits konfigurierte Appliance ersetzt.
