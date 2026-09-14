# Configuration reference

[← Overview](../../README.md) | [Deutsch](../de/configuration.md)

## Complete YAML reference

### General

| Key | Meaning | Default |
|---|---|---|
| `project_id` | STACKIT target project | required |
| `credentials` | Service account key path | required |
| `ova` | Coriolis OVA path | required |
| `region` | STACKIT region | `eu01` |
| `timeout` | Maximum runtime of each major deployment phase | `90m` |
| `poll_interval` | Polling interval for asynchronous resources | `15s` |
| `upload_attempts` | Transfer attempts | `3` |

Each major phase receives a fresh timeout budget. Increase `timeout` when a single
phase can take longer, for example when normalizing a very large image over a slow
connection. A timeout error names the phase that exhausted its budget.

### `agent` and `bootstrap`

| Key | Meaning | Default |
|---|---|---|
| `agent.enabled` | Enable Server Agent management | `true` |
| `agent.enable_service` | Automatically enable Run Command in the project | `true` |
| `bootstrap.enabled` | Configure hostname and admin password | `true` |
| `bootstrap.admin_password` | Fixed password; empty generates a random one | empty |
| `bootstrap.print_generated_password` | Include generated password in result | `true` |

Keep agent and bootstrap enabled for complete automation. Automatic setup of new
projects also requires `agent.enable_service` and the `Project Editor` role. A
fixed password in YAML is plain text; protect the file accordingly. CLI values may
also be retained in shell history.

### `normalization`

| Key | Meaning | Default |
|---|---|---|
| `performance_class` | Source and scratch volume performance | `storage_premium_perf12` |
| `scratch_size_gib` | Scratch volume size | `64` |
| `helper_image_id` | Public Ubuntu helper image | region-specific ID in example |
| `helper_machine_type` | Helper VM machine type | `g1a.1d` |
| `helper_boot_size_gib` | Helper OS disk size | `16` |

The scratch volume must hold the virtual disk size, compressed VMDK size, and a
2 GiB reserve. The installer validates this before cloud changes.

### `image`

| Key | Meaning | Default |
|---|---|---|
| `id` | Explicit existing image ID | empty |
| `owner_project_id` | Central image-owner project | target project |
| `name_prefix` | Prefix for new images | `coriolis-appliance` |
| `disk_bus` | Virtual disk bus | `virtio` |
| `nic_model` | Virtual NIC model | `virtio` |
| `uefi` | Enable UEFI | `false` |
| `secure_boot` | Enable Secure Boot | `false` |
| `share.parent_organization` | Share with entire parent organization | `false` |
| `share.project_ids` | Explicit consumer project IDs | `[]` |

`parent_organization` and `project_ids` are mutually exclusive.

### `server`

| Key | Meaning | Default |
|---|---|---|
| `id` | Explicitly adopt an existing server | empty |
| `name` | Appliance name and discovery key | `coriolis-appliance` |
| `machine_type` | Requested machine type | `c1a.4d` |
| `availability_zone` | Requested availability zone | automatic; example uses `eu01-m` |
| `boot_volume_size_gib` | Boot volume size | `48` |
| `performance_class` | Boot volume performance | `storage_premium_perf12` |
| `keypair_name` | Optional existing key pair | empty |
| `delete_boot_volume_on_termination` | Delete boot volume with VM | `false` |

The boot disk must not be smaller than the disk declared in the OVF. A key pair
does not automatically enable the Coriolis support SSH service.

### `network` and `security_group`

| Key | Meaning | Default |
|---|---|---|
| `network.id` | Require a specific existing network | empty |
| `network.name` | Find or create a network by name | `coriolis-network` |
| `network.ipv4_prefix` | Prefix for a new network | `10.1.100.0/24` |
| `network.routed` | Create a routed network | `true` |
| `security_group.name` | Security group name | `coriolis-security` |
| `security_group.ingress[]` | Desired ingress rules | TCP/443 from example CIDR |

An ingress rule contains `protocol`, `port`, `cidr`, and a unique `description`.
Missing rules are added; existing rules are not removed. For production, restrict
TCP/443 to known administrator, proxy, or VPN ranges unless public access is needed.

### Public IP and DNS

| Key | Meaning | Default |
|---|---|---|
| `public_ip` | Ensure a public IP in direct mode | `true` |
| `public_ip_id` | Use a specific reserved public IP by ID | empty |
| `public_ip_address` | Use a specific reserved public IP by address | empty |
| `dns.enabled` | Manage DNS | `false` in code; enabled in example |
| `dns.create_zone` | Create a missing zone | `false` |
| `dns.zone_id` | Existing zone ID | empty |
| `dns.zone_name` | Zone name or DNS name | required without an ID |
| `dns.record_name` | Hostname or full FQDN | required when DNS is enabled |
| `dns.ttl` | A-record TTL | `300` |

A directly installed certificate requires DNS and a public IP. In ALB mode, the A
record points to the load balancer's external address.

### `exposure`

| Key | Meaning | Default |
|---|---|---|
| `mode` | `direct` or `application_load_balancer` | `direct` |
| `certificate.enabled` | Manage certificate directly on appliance | `false` |
| `certificate.email` | ACME contact address | required when using certificates |
| `certificate.staging` | Use Let's Encrypt staging | `false` |
| `certificate.renew_before_days` | Renewal window | `30` |
| `certificate.name_prefix` | Certificate Service prefix; ALB only | `coriolis-tls` |
| `load_balancer.name` | ALB name | `coriolis-alb` |
| `load_balancer.plan_id` | STACKIT ALB plan | `p10` |
| `load_balancer.backend_port` | Appliance backend port | `80` |
| `load_balancer.health_check_path` | HTTP health-check path | `/` |

Use `certificate.staging: true` for initial ACME tests. Switch to the production CA
only after a successful test to avoid rate limits.

## CLI overrides

CLI flags override the corresponding YAML values:

| Flag | YAML target |
|---|---|
| `--project-id` | `project_id` |
| `--credentials` | `credentials` |
| `--ova` | `ova` |
| `--region` | `region` |
| `--image-id` | `image.id` |
| `--image-owner-project-id` | `image.owner_project_id` |
| `--share-image-with-organization` | `image.share.parent_organization: true` |
| `--share-image-with-projects` | comma-separated `image.share.project_ids` |
| `--server-id` | `server.id` |
| `--availability-zone` | `server.availability_zone` |
| `--machine-type` | `server.machine_type` |
| `--performance-class` | `server.performance_class` |
| `--boot-volume-size` | `server.boot_volume_size_gib` |
| `--normalization-performance-class` | `normalization.performance_class` |
| `--normalization-scratch-size` | `normalization.scratch_size_gib` |
| `--normalization-helper-image-id` | `normalization.helper_image_id` |
| `--normalization-helper-machine-type` | `normalization.helper_machine_type` |
| `--normalization-helper-boot-size` | `normalization.helper_boot_size_gib` |
| `--network-id` | `network.id` |
| `--public-ip-id` | `public_ip_id`; also enables `public_ip` |
| `--public-ip-address` | `public_ip_address`; also enables `public_ip` |
| `--dns-zone-id` | `dns.zone_id` |
| `--dns-zone-name` | `dns.zone_name` |
| `--dns-name` | `dns.record_name`; also enables DNS |
| `--certificate-email` | `exposure.certificate.email`; enables direct certificate |
| `--admin-password` | `bootstrap.admin_password`; also enables bootstrap |
| `--enable-run-command-service` | `agent.enable_service=true` |
| `--disable-run-command-service-activation` | `agent.enable_service=false` |
| `--dry-run` | Local validation and plan only |
| `--check-cloud` | Read-only cloud validation |
| `--version` | Print build version |

Example without YAML for the three required values:

```bash
./bin/coriolis-stackit \
  --credentials credentials.json \
  --project-id 00000000-0000-0000-0000-000000000000 \
  --ova coriolis-appliance-stackit-0.ova
```

All other values use defaults. For reproducible deployments, prefer a versioned
YAML file that does not contain secrets.

