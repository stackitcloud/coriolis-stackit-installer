# Architecture and technical workflow

[← Overview](../../README.md) | [Deutsch](../de/architecture.md)

## Detailed technical workflow

### 1. OVA inspection and pre-validation

The installer calculates SHA-256 over the complete OVA and reads OVF metadata. It
rejects undersized boot or scratch volumes, unknown availability zones, and machine
types with insufficient CPU or RAM before uploading the image. Without an explicit
availability zone, it prefers a zone ending in `-m`. Without a machine type, it
selects the smallest suitable type.

### 2. Image discovery and reuse

The OVA is identified with the `coriolis-sha256` label. Because of the STACKIT
label length limit, the first 32 hexadecimal characters are stored. A usable image
also carries `coriolis-normalized=agent-v2`.

The search order is:

1. an available local image in the target project;
2. a shared image visible to the target project;
3. an image import that is still running;
4. lookup or import in the configured image-owner project.

An explicit `image.id` bypasses automatic selection but must also be normalized as
`agent-v2` when the agent workflow is enabled. A `CREATING` import is monitored. An
owned import that has not changed for more than 30 minutes is considered stale,
removed, and recreated.

### 3. Normalization on the helper VM

Only when no suitable image exists, the installer creates:

- an Ubuntu helper VM with Server Agent;
- a source volume sized like the future boot disk;
- a scratch volume for the incoming VMDK and outgoing QCOW2;
- a temporary public IP, security group, and one-time Ed25519 key pair.

The helper VM SSH host key is first retrieved through the independent STACKIT
Server Agent. The subsequent Go SSH transfer accepts only that pinned host key. An
interrupted VMDK transfer resumes at the existing byte offset.

The helper VM then performs:

1. installation of `qemu-utils`;
2. VMDK to RAW conversion directly onto the high-performance source volume;
3. writable mounting of the appliance root partition;
4. offline installation and activation of the STACKIT Server Agent;
5. removal of machine-specific agent and cloud-init state only;
6. RAW to QCOW2 conversion on the scratch volume;
7. QCOW2 validation and upload through the STACKIT image upload URL;
8. waiting for image status `AVAILABLE`.

The source OVA remains unchanged. Coriolis configuration, support SSH, and
appliance application data are not modified during normalization.

After a successful import, the helper VM, both volumes, temporary public IP,
security group, and key pair are deleted. After an early failure, labelled volumes
may remain for diagnosis and recovery. A subsequent run discovers them and removes
stale helper access.

### 4. Server, network, and storage

Network and security group are reused by name unless a network ID is supplied. The
appliance VM is found by `server.id` or `server.name`. A new VM uses the normalized
image as its boot source, the configured performance class, and the Server Agent.

`storage_premium_perf12` is the recommended default for the appliance and
normalization volumes. Conversion, image upload, migrations, and backups create
sustained I/O load; `perf1` is often too slow for these paths. Only the small helper
OS disk uses `storage_premium_perf1`, because all payload I/O is placed on separate
perf12 volumes.

### 5. Appliance bootstrap

After boot, the installer waits for the STACKIT Server Agent and runs bootstrap in
the base operating system through Run Command. The Coriolis support SSH service,
its accounts, and its configuration are not changed.

Bootstrap:

- sets the hostname;
- waits for Keystone;
- sets the password of the Coriolis `admin` user in the `default` domain;
- persists the password in Kolla's `passwords.yml` source of truth and updates
  the local OpenRC file;
- invokes the vendor exposure logic;
- verifies the regenerated OpenRC, a real Keystone login, and the local web
  service after vendor reconfiguration;
- stores the password and idempotency marker below `/var/lib/coriolis-stackit`
  with root-only permissions.

If `bootstrap.admin_password` is empty, a random appliance-specific password is
generated once. Reruns return the same password.

### 6. Public IP and DNS

In direct mode, the installer uses this order:

1. a public IP already attached to the appliance NIC;
2. the free IP requested through `public_ip_id` or `public_ip_address`;
3. a free installer-managed public IP;
4. a newly reserved public IP.

A requested IP attached to another NIC causes a safe failure. The A record is
created or updated to the current address. A DNS zone can be selected by ID or
name and created automatically with `create_zone: true`.

### 7. Publicly trusted certificate

In recommended direct mode, the appliance generates its own RSA private key and a
CSR for the FQDN. The private key never leaves the VM. The Go program completes the
ACME DNS-01 challenge through STACKIT DNS and returns only leaf and issuer
certificates to the appliance.

Before activation, the installer verifies hostname, remaining lifetime, key pair,
and complete chain. It backs up active vendor files, installs the new chain, and
runs `expose_coriolis.py` with the private interface address. Only
`coriolis-web-proxy` and `coriolis-api` are then reloaded. The leaf fingerprints
actually served on ports 443 and 5000 must match the active file. On failure, the
installer restores the backup and previous hostname.

A valid certificate is reused. If the file is correct but a service has not yet
reloaded it, the existing key pair is reused without requesting another certificate.

### 8. Optional Application Load Balancer

With `exposure.mode: application_load_balancer`, a STACKIT ALB terminates HTTPS on
port 443. The certificate is issued through DNS-01 and stored in STACKIT Certificate
Service. By default, the ALB forwards unencrypted HTTP to port 80 on the private
appliance address.

The ALB is optional. Direct certificate installation keeps the default deployment
closer to the vendor product. The ALB covers only the configured Layer 7 web path;
migration and worker connections are not routed through it automatically.

