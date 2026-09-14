# Technische Voraussetzungen

[← Übersicht](../../README.de.md) | [English](../en/prerequisites.md)

### Bedienrechner

Für die Ausführung werden benötigt:

- das für das Betriebssystem und die CPU-Architektur gebaute Installer-Binary;
- lesender Zugriff auf das OVA;
- ausgehendes HTTPS zu den STACKIT APIs und – bei aktiviertem Zertifikat – zum
  ACME-Dienst;
- beim erstmaligen Normalisieren eines OVA ausgehendes TCP/22 zur temporären
  Public IP der Hilfs-VM;
- ausreichend freier lokaler Speicher zum Lesen des OVA. Die VMDK wird nicht lokal
  extrahiert und es wird lokal kein QCOW2 erzeugt.

Zum Bauen aus dem Quellcode wird Go 1.25 oder neuer benötigt. `qemu-img`, Terraform,
libguestfs, `virt-customize` und die STACKIT CLI sind auf dem Bedienrechner nicht
erforderlich.

### STACKIT-Projekt und Berechtigungen

Der Installer aktiviert den projektweiten STACKIT Run Command Service standardmäßig
selbst, bevor er weitere Cloud-Ressourcen anlegt. Dafür benötigt der Service Account
die Rolle **Project Editor**. Mit `agent.enable_service: false` kann die Aktivierung
unterdrückt werden; dann muss der Dienst bereits aktiviert sein.

Zusätzlich benötigt der Service Account Lese- und Änderungsrechte für die Ressourcen,
die der gewählte Ablauf nutzt:

- IaaS: Images, Server, Volumes, Netzwerke, NICs, Security Groups, Public IPs und
  temporäre Keypairs;
- Server Agent / Run Command;
- STACKIT DNS, wenn `dns.enabled: true` gesetzt ist;
- Application Load Balancer und Certificate Service nur im ALB-Modus.

Bei einem zentralen Image-Projekt werden die IaaS- und Image-Freigaberechte sowohl
im Image-Eigentümerprojekt als auch im Zielprojekt benötigt. Beide Projekte müssen
zur selben Organisation gehören und das Image in derselben Region verwenden.

Die Datei mit dem Service-Account-Key sollte nur für den aktuellen Benutzer lesbar
sein:

```bash
chmod 600 credentials.json
```

### Quotas und ausgehende Verbindungen

Beim ersten Import werden vorübergehend eine Hilfs-VM, zwei Datenvolumes, eine
Public IP, eine Security Group und ein Keypair benötigt. Zusätzlich muss Quota für
das normalisierte Image, die Appliance-VM und deren Boot-Volume vorhanden sein.

Die Hilfs-VM benötigt ausgehenden Zugriff auf Ubuntu-Paketquellen, den STACKIT-
Metadatendienst und die Image-Upload-URL. Das Zielnetz muss für den Server Agent und
die späteren Coriolis-Verbindungen ausgehenden Verkehr erlauben.

### OVA-Anforderungen

Das OVA muss ein TAR-basiertes OVA mit genau einem OVF und genau einer referenzierten
virtuellen Disk enthalten. Der Installer liest CPU, RAM, Diskgröße, Betriebssystem
und Firmware aus dem OVF. Aktuell wird genau eine Appliance-Disk unterstützt.
