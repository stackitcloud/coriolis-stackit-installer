# Network and security

[← Overview](../../README.md) | [Deutsch](../de/security.md)

## Network and security model

### Permanent ingress rules

By default, the appliance security group opens only TCP/443. SSH is deliberately
not opened or reconfigured. Coriolis support can continue to enable and use its
vendor-provided SSH service as intended.

The STACKIT Server Agent needs no ingress rule; it uses a separate outbound
management channel.

For migrations, the appliance primarily establishes outbound connections to source
and target APIs and temporary workers. Depending on the provider, TCP/22, 4433,
5566, and 5986 may be relevant. Remote access to Coriolis APIs or external workers
can require additional narrowly scoped ingress rules. Refer to the
[Coriolis port matrix](https://cloudbase.it/coriolis-network-ports-requirements/).

### Temporary helper SSH access

Only while importing a new OVA, the installer creates a separate security group
for TCP/22 to the helper VM. This port is temporarily reachable from `0.0.0.0/0`;
login requires a one-time key. Your security policy must permit this temporary
access. Public IP, key pair, and security group are removed after a
successful import. After an error, rerun promptly or inspect and clean up the
labelled helper infrastructure.

This temporary SSH channel is only used for image creation. It neither modifies
nor uses the Coriolis appliance support SSH service.

### Protect credentials and results

- Restrict service account keys and configurations containing passwords to
  authorized users; never commit them.
- Deployment output can include the generated admin password. Store it in a
  protected secret store, not public build logs.
- A password supplied through `--admin-password` may be visible in shell history
  and process arguments. Avoid this method in shared environments.
- With a directly installed certificate, the private key stays on the appliance.
- Protect maintenance files, backups, and diagnostics. Remove confidential content
  before sharing data with support.

### Backups and maintenance

Service updates require cold snapshots of all appliance volumes and exclusive
maintenance access. They do not replace an independent backup. External databases,
workers, and remote source and target systems are not covered.

Retain the status file until completion and resume use only after successful
verification and a functional test. There is no automatic rollback.
See the [maintenance guide](operations.md#appliance-maintenance).

## Limitations and safeguards

- Exactly one disk per OVA is supported.
- A new OVA creates a new image but never replaces an existing VM automatically.
- Service updates require the explicitly approved maintenance workflow.
  Cross-stream and operating-system upgrades are outside its scope.
- Data and licenses are not automatically transferred to another appliance.
- An existing server is not moved to another network automatically.
- A failed VM is not deleted automatically.
- Existing security-group rules are never removed.
- The optional ALB covers only its HTTPS web path, not every Coriolis migration
  connection.
- After changing an ALB, verify reachability, backend connections, and certificates
  separately.

These safeguards prevent a rerun from unintentionally replacing a licensed or
already configured appliance.
