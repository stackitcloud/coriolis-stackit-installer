# Technical prerequisites

[← Overview](../../README.md) | [Deutsch](../de/prerequisites.md)

### Operator workstation

The operator workstation needs:

- an installer binary built for its operating system and CPU architecture;
- read access to the OVA;
- outbound HTTPS to STACKIT APIs and, when certificates are enabled, the ACME CA;
- outbound TCP/22 to the temporary helper VM's public IP while normalizing a new
  OVA;
- enough local storage to read the OVA. The VMDK is not extracted and no QCOW2 is
  created locally.

Building from source requires Go 1.25 or newer. `qemu-img`, Terraform, libguestfs,
`virt-customize`, and the STACKIT CLI are not required on the operator workstation.

### STACKIT project and permissions

By default, the installer enables the project-wide STACKIT Run Command service
before creating other cloud resources. The service account requires the
**Project Editor** role for this operation. Set `agent.enable_service: false` to
disable automatic activation; the service must then already be enabled.

The service account also needs read and write permissions for the resources used
by the selected workflow:

- IaaS images, servers, volumes, networks, NICs, security groups, public IPs, and
  temporary key pairs;
- Server Agent and Run Command;
- STACKIT DNS when `dns.enabled: true`;
- Application Load Balancer and Certificate Service only in ALB mode.

With a central image project, IaaS and image-sharing permissions are required in
both the image-owner project and the target project. Both projects must belong to
the same organization and use the same region.

Restrict the service account key to the current user:

```bash
chmod 600 credentials.json
```

### Quotas and outbound connectivity

The first import temporarily needs one helper VM, two data volumes, one public IP,
one security group, and one key pair. Additional quota is required for the
normalized image, appliance VM, and its boot volume.

The helper VM needs outbound access to Ubuntu package repositories, the STACKIT
metadata service, and the image upload URL. The target network must allow outbound
traffic for the Server Agent and later Coriolis connections.

### OVA requirements

The OVA must be TAR-based and contain exactly one OVF plus exactly one referenced
virtual disk. The installer reads CPU, RAM, disk size, operating system, and
firmware from the OVF. Exactly one appliance disk is currently supported.
