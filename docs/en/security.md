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
for TCP/22 to the helper VM. The current implementation temporarily permits
`0.0.0.0/0`. Access requires a random one-time key, and the host key is pinned
through Server Agent. Public IP, key pair, and security group are removed after a
successful import. After an error, rerun promptly or inspect and clean up the
labelled helper infrastructure.

This temporary SSH channel is only used for image creation. It neither modifies
nor uses the Coriolis appliance support SSH service.

### Secret handling

- The service account key is read only for SDK authentication.
- Credentials are never included in the result structure.
- The generated admin password is stored root-only on the appliance.
- Run Command output containing the admin password is not streamed live.
- With direct certificates, the TLS private key remains on the appliance.
- Vendor reconfiguration logs are not streamed and are removed after completion
  or rollback.
- A password supplied through `--admin-password` may remain in shell history.

## Limitations and safeguards

- Exactly one disk per OVA is supported.
- A new OVA creates a new image but never replaces an existing VM automatically.
- There is no automated data, license, or Coriolis application upgrade workflow.
- An existing server is not moved to another network automatically.
- A failed VM is not deleted automatically.
- Existing security-group rules are never removed.
- The optional ALB covers only its HTTPS web path, not every Coriolis migration
  connection.
- Changes to an existing ALB should be verified separately; reuse is primarily
  based on name and certificate.

These safeguards prevent a rerun from unintentionally replacing a licensed or
already configured appliance.

