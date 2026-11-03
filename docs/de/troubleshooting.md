# Fehlerbehebung

[← Übersicht](../../README.de.md) | [English](../en/troubleshooting.md)

## Deployment-Probleme

Zuerst die lokale Konfiguration und anschließend den Cloud-Zugriff prüfen:

```bash
bin/coriolis-stackit --config config.local.yaml --dry-run
bin/coriolis-stackit --config config.local.yaml --check-cloud
```

| Problem | Prüfen oder unternehmen |
|---|---|
| OVA oder Größenangaben werden abgewiesen | OVA-Pfad, unterstütztes Format, Boot-Volume-Größe und Machine Type prüfen |
| Cloud-Zugriff oder Platzierung scheitert | Zugangsdaten, Projekt, Region, Availability Zone und Berechtigungen prüfen |
| Run Command nicht verfügbar | Für die automatische Deployment-Aktivierung `agent.enable_service: true` und `Project Editor` sicherstellen |
| Ressource kann nicht angelegt werden | Quota für Server, Images, Volumes und Public IPs prüfen |
| Image-Übertragung wurde unterbrochen | Denselben Deployment-Befehl mit unveränderter Konfiguration erneut ausführen |
| DNS oder Zertifikatseinrichtung scheitert | Zone, Recordname, DNS-Berechtigungen und ACME-Kontaktadresse prüfen |
| Übernommener Server antwortet nicht | Netzwerkzuordnung und funktionsfähigen STACKIT Server Agent prüfen |

Vorhandene VMs und temporäre Import-Ressourcen nicht ungeprüft löschen. Sie können
für die Wiederaufnahme des Deployments benötigt werden. Eine einzelne fertige
Phase bedeutet noch nicht, dass der gesamte Vorgang abgeschlossen ist.

## Wartungs- und Update-Probleme

Den aktuellen Betriebszustand mit `status` prüfen. Für einen bereits gestarteten
Update-Versuch dieselbe Statusdatei weiterverwenden:

```bash
SERVER_ID="<server-id>"
STATE_FILE="<statusdatei-des-update-versuchs>"

bin/coriolis-stackit status --config config.local.yaml --server-id "$SERVER_ID"
bin/coriolis-stackit update-status --config config.local.yaml \
  --server-id "$SERVER_ID" --state-file "$STATE_FILE"
```

| Problem | Prüfen oder unternehmen |
|---|---|
| `status` meldet fehlgeschlagene Prüfungen | Die unter `errors` aufgeführten Ursachen vor einem neuen Update beheben |
| Paketmanager nicht bereit | Eine laufende Paketverwaltung abwarten oder den Zustand administrativ klären lassen |
| Ausführungen oder Zeitpläne verhindern die Planung | Arbeiten aller Mandanten abschließen lassen und sämtliche Transfer-Zeitpläne deaktivieren |
| Speicherbelegung verhindert die Planung | Genügend Speicher freigeben; die geprüften Dateisysteme müssen unter 80 % belegt sein |
| Versionssuche scheitert | Erreichbarkeit der Release Notes und benötigten Images sowie Zugriff der Appliance prüfen; nicht als „aktuell“ werten |
| Snapshot-Erstellung scheitert | Snapshot-Quota, Berechtigungen und tatsächlichen VM-/Snapshot-Zustand prüfen |
| Freigabe passt nicht mehr | Einen neuen Plan erstellen und prüfen, sofern noch kein Update gestartet wurde |
| Verbindung oder API vorübergehend nicht verfügbar | Lesende Statusabfrage später erneut versuchen; einen gestarteten Update-Aufruf nicht erneut absenden |
| Wartungssperre oder unklarer Update-Status | Vorhandenen Versuch, Statusdatei und Cloud-Ressourcen prüfen; keine Sperre ungeprüft entfernen |
| Verifikation schlägt fehl | Wartungsfenster beibehalten, Daten und Konfiguration prüfen lassen und nicht allein aufgrund der angezeigten Version freigeben |

**`update --apply` nach einem Timeout oder Fehler niemals blind wiederholen.**
Es gibt keinen automatischen Rollback. Die vollständige Vorgehensweise steht in
der [Wartungsanleitung](operations.md#appliance-wartung).

## Hilfe anfordern

Version des Installers, betroffene Funktion, Zeitpunkt, Fehlerkategorie und
nachvollziehbare Schritte angeben. Konfigurationen, Ergebnisdateien und Logs vor
der Weitergabe von Zugangsdaten, Kennwörtern und vertraulichen Projektinformationen
bereinigen. Keine OVA-Dateien oder privaten Schlüssel anhängen.
