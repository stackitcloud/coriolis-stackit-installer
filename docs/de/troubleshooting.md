# Fehleranalyse

[← Übersicht](../../README.de.md) | [English](../en/troubleshooting.md)

Empfohlene Reihenfolge:

1. `--dry-run` ausführen und OVA-/Größenfehler beheben.
2. `--check-cloud` für Berechtigungen, Region, Zone und Machine Type ausführen.
3. Quota für Server, Images, Volumes und Public IPs prüfen.
4. Bei `Service not enabled` sicherstellen, dass `agent.enable_service: true` gesetzt
   ist und der Service Account die Rolle `Project Editor` besitzt.
5. Bei einem Transferfehler denselben Installerbefehl erneut ausführen.
6. Bei DNS-/ACME-Fehlern Zone, Recordname und Service-Account-Rechte prüfen.
7. Bei einem übernommenen Server prüfen, ob der STACKIT Server Agent aktiv ist.
8. Keine VM oder Normalisierungsvolumes blind löschen: gelabelte Ressourcen können
   einen fortsetzbaren Zwischenstand enthalten.

Der Installer gibt Fehler mit der betroffenen Phase zurück. Beim Zertifikatseinbau
werden nur bereinigte Fehlerkategorien ausgegeben; interne Hersteller-Secrets werden
nicht in das Konsolenlog übernommen.
