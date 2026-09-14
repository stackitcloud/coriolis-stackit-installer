# Getting started

[← Overview](../../README.md) | [Deutsch](../de/getting-started.md)

## Quick start

### 1. Build the binary

Skip this step when a suitable binary already exists.

```bash
make check
```

The binary is created as `bin/coriolis-stackit`. `make check` runs the Go tests
before building.

### 2. Create a configuration

[`examples/config.yaml`](../../examples/config.yaml) contains every setting. Copy it for
a new project:

```bash
cp examples/config.yaml config.yaml
```

A minimal example suitable for direct HTTPS access is:

```yaml
project_id: 00000000-0000-0000-0000-000000000000
credentials: credentials.json
ova: coriolis-appliance-stackit-0.ova
region: eu01

server:
  name: coriolis-appliance
  machine_type: c1a.4d
  availability_zone: eu01-m
  boot_volume_size_gib: 48
  performance_class: storage_premium_perf12

network:
  name: coriolis-network
  ipv4_prefix: 10.1.100.0/24
  routed: true

public_ip: true

dns:
  enabled: true
  create_zone: true
  zone_name: my-coriolis.runs.onstackit.cloud
  record_name: appliance
  ttl: 300

exposure:
  mode: direct
  certificate:
    enabled: true
    email: admin@example.com
    staging: false
    renew_before_days: 30

bootstrap:
  enabled: true
  admin_password: ""
  print_generated_password: true
```

`project_id` does not automatically determine the region. Always set `region`
deliberately. Public helper image IDs are region-specific.

### 3. Validate locally

```bash
./bin/coriolis-stackit --config config.yaml --dry-run
```

`--dry-run` reads and validates the configuration, inspects the OVA, and prints
the resolved plan as JSON. It does not modify cloud resources.

### 4. Validate cloud access and placement

```bash
./bin/coriolis-stackit --config config.yaml --check-cloud
```

This check authenticates and validates the region, availability zone, machine
type, and—when enabled—DNS or ALB access. It does not reserve resources and does
not replace a complete quota check.

### 5. Start the deployment

```bash
./bin/coriolis-stackit --config config.yaml
```

The first run can take considerably longer because it includes transfer, two
conversions, and image import. The VMDK transfer and image upload regularly print
percentage, transferred bytes, and throughput.

### 6. Store the result securely

On success, the program writes a JSON object to stdout, for example:

```json
{
  "ProjectID": "00000000-0000-0000-0000-000000000000",
  "Region": "eu01",
  "ImageID": "...",
  "NetworkID": "...",
  "ServerID": "...",
  "SecurityGroupID": "...",
  "PublicIP": "192.0.2.10",
  "LoginURL": "https://appliance.my-coriolis.runs.onstackit.cloud",
  "LoginUser": "admin",
  "generated_password": "..."
}
```

When `bootstrap.print_generated_password: true`, the result includes the generated
password. Send this output directly to a protected secret store; do not archive it
in build logs. With `false`, the password is not printed.

### 7. Test a repeated run

Run the same command again:

```bash
./bin/coriolis-stackit --config config.yaml
```

A successful rerun reuses the image and infrastructure. When a current direct
certificate is installed, the output contains `appliance certificate is current`;
no new ACME order or container reconfiguration is performed.
