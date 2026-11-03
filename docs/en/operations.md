# Operations and common scenarios

[← Overview](../../README.md) | [Deutsch](../de/operations.md)

This guide covers using existing resources and maintaining a running Coriolis
appliance. For the initial installation, see
[Getting started](getting-started.md); all settings are described in the
[configuration reference](configuration.md).

## Common scenarios

### Use an existing public IP

Specify an existing public IP by ID:

```yaml
public_ip: true
public_ip_id: 00000000-0000-0000-0000-000000000000
```

Or by address:

```yaml
public_ip: true
public_ip_address: 192.0.2.10
```

The IP must not already be assigned to another network interface.

### Use one image across multiple projects

Manage the image in a central project and share it with other projects:

```yaml
image:
  owner_project_id: 11111111-1111-1111-1111-111111111111
  share:
    project_ids:
      - 22222222-2222-2222-2222-222222222222
```

If the image is missing, the installer creates it in the owner project. The
current target project is also granted access.

For organization-wide sharing:

```yaml
image:
  owner_project_id: 11111111-1111-1111-1111-111111111111
  share:
    parent_organization: true
```

### Use an existing normalized image

```yaml
image:
  id: 33333333-3333-3333-3333-333333333333
```

Deployment still requires the OVA to validate the image and hardware requirements.
The maintenance commands below do not require an OVA.

### Adopt an existing server

```yaml
server:
  id: 44444444-4444-4444-4444-444444444444
  name: coriolis-appliance
```

The server must be attached to the configured network. The installer reuses it
without replacing the boot volume, appliance ID, Coriolis data, or license.

**Create a backup before first adopting a server.** Enabled bootstrap can change
the hostname and admin password; enabled certificate management can change the
TLS configuration. Agent-controlled functions require a working STACKIT Server
Agent.

To inspect or update an existing appliance without running deployment, use the
maintenance commands instead.

### Use an Application Load Balancer

```yaml
dns:
  enabled: true
  create_zone: true
  zone_name: my-coriolis.runs.onstackit.cloud
  record_name: appliance

exposure:
  mode: application_load_balancer
  certificate:
    email: admin@example.com
    staging: false
    renew_before_days: 30
    name_prefix: coriolis-tls
  load_balancer:
    name: coriolis-alb
    plan_id: p10
    backend_port: 80
    health_check_path: /
```

Adjust the domain, contact address, and load balancer plan to your environment.

## Appliance maintenance

| Command | Purpose |
|---|---|
| `status` | Inspect the installed version and appliance health |
| `versions` | Discover published releases and eligible update targets |
| `update` | Plan an update or execute it after explicit approval |
| `update-status` | Monitor a launched update and verify its result |

Maintenance uses STACKIT Server Agent and Run Command. No manual SSH or console
login is required. These commands use the existing appliance without replacing
its VM or boot volume. They do not run deployment bootstrap, change the admin
password, or renew certificates.

### Prerequisites

- A configuration file with the project, region, and suitable STACKIT credentials.
  An existing deployment configuration can be reused.
- A working Server Agent and an already enabled Run Command service.
- Permissions to query servers and volumes and use Run Command; updates also
  require server stop/start and volume snapshot permissions.
- For version discovery, access to the official release notes and the required
  container images through the appliance.
- Sufficient snapshot quota and free storage.

Run the examples from the project directory. Relative configuration paths are
resolved from the current working directory. First set your configuration file
and server ID:

```bash
CONFIG="config.local.yaml"
SERVER_ID="<server-id>"
```

Specify the server ID explicitly during maintenance to select the correct
appliance. Without `--server-id`, the configured server name must be unambiguous.

### 1. Check appliance health

```bash
bin/coriolis-stackit status --config "$CONFIG" --server-id "$SERVER_ID"
```

The JSON report includes the installed version, container health, storage usage,
active executions, and enabled schedules. `healthy: true` means the operational
checks passed. `package_manager_ready: true` confirms that the package manager
is ready for maintenance.

Failed checks appear in `errors` and cause a nonzero exit code. Resolve these
issues before planning an update. `status` does not change appliance configuration.

### 2. Select a target version

```bash
bin/coriolis-stackit versions --config "$CONFIG" --server-id "$SERVER_ID"
```

The command lists published releases and evaluates possible targets:

- `eligible: true`: a newer target in the supported release stream with all
  required component images available.
- `latest_compatible`: the newest such target.
- `up_to_date_within_stream: true`: no newer complete target was found in the
  inspected stream.

A failed query does not mean the appliance is up to date. Version discovery does
not perform an update or download container layers.

Only newer numeric releases within the same YYMM stream are supported.
Cross-stream upgrades, downgrades, and reinstalls are excluded. Technical
eligibility does not replace reviewing the
[release notes](https://cloudbase.github.io/coriolis-docs/reference/coriolis-release-notes.html)
and, where necessary, confirming the upgrade path with Cloudbase. A service
update does not necessarily include every OS change in a newer OVA.

### 3. Prepare maintenance and plan the update

Before execution:

1. Schedule a maintenance window with downtime.
2. Let running executions finish across all tenants and disable every transfer
   schedule.
3. Prevent users, automations, and external workers from submitting new work
   during maintenance.
4. Ensure every inspected filesystem is below 80% usage and the appliance is
   healthy. Resolve unexplained changes to the vendor installation or an existing
   maintenance lock first.
5. For production systems, maintain an independent backup and a tested recovery
   plan.

Plan an explicitly selected target:

```bash
TARGET_VERSION="<target-version-from-versions>"
bin/coriolis-stackit update --config "$CONFIG" \
  --server-id "$SERVER_ID" --target-version "$TARGET_VERSION"
```

Alternatively, select the newest eligible target automatically:

```bash
bin/coriolis-stackit update --config "$CONFIG" \
  --server-id "$SERVER_ID" --latest-compatible
```

Without `--apply`, the command only prints a plan. It does not stop services,
create snapshots, or execute updates. Review the plan's explicit `target_version`,
the appliance, and the volumes listed under `backup`. Copy the `approval` value
for execution.

`--latest-compatible` and `--target-version` are mutually exclusive; the literal
value `latest` is not a target version. After automatic selection, preferably use
the plan's numeric version explicitly for execution.

### 4. Execute the update with automatic backup

Use a new status file for every attempt and retain it on persistent storage:

```bash
mkdir -p -m 700 .coriolis-stackit
STATE_FILE=".coriolis-stackit/update-$(date -u +%Y%m%dT%H%M%SZ).json"

bin/coriolis-stackit update --config "$CONFIG" \
  --server-id "$SERVER_ID" --target-version "$TARGET_VERSION" \
  --apply --approve-plan "<approval-from-reviewed-plan>" \
  --confirm-server-id "$SERVER_ID" \
  --state-file "$STATE_FILE"
```

Approval applies only to the reviewed state and selected target. If the appliance
changes between planning and execution, generate and review a new plan.

The command handles:

1. Preparation and backup of the appliance configuration.
2. Graceful shutdown of appliance services, followed by stopping the VM.
3. New native STACKIT snapshots of every attached volume, including the boot
   volume. The VM stays stopped until all snapshots are available.
4. Starting the same VM and checking its operational state.
5. Launching the Coriolis service update.

Snapshots are mandatory and are not deleted automatically. They cover appliance
volumes, not external databases, external workers, or remote source and target
systems. Unsupported storage configurations prevent the update. Snapshots do not
replace an independent backup.

**Keep the CLI connected until the command returns.** Only the launched background
updater continues independently of the local terminal. A successful return from
`update --apply` confirms launch, not completion of the update.

### 5. Verify the result

Use the same status file as for execution:

```bash
bin/coriolis-stackit update-status --config "$CONFIG" \
  --server-id "$SERVER_ID" --state-file "$STATE_FILE"
```

Repeat the command while the update is still running:

| `stage` in the maintenance journal | Meaning |
|---|---|
| `running` | The updater has been launched; its result is not yet known |
| `verifying` | The final checks have not passed yet |
| `verified` | Target version and health, plus preserved data, configuration, and certificate checks, are confirmed |
| `failed` | The updater failed; further maintenance is required |

**Only `verified` confirms successful completion.** Then perform a representative
functional test before allowing users, automations, workers, and schedules to
resume.

For a later current health check, run `status` again. Once verification succeeds,
`update-status` reports the saved result of that update attempt.

### Interruptions and failures

- Do not blindly repeat `update --apply`. A timeout or disconnected terminal does
  not mean the remote operation has stopped.
- Retain the status file and snapshot IDs. Inspect the existing attempt with
  `update-status`; if its state is unclear, also check the VM and snapshots in
  STACKIT.
- Do not remove an existing maintenance lock without investigation. Services or
  the VM may intentionally remain stopped until the issue is resolved.
- There is no automatic rollback. Any required restore must consider the complete
  snapshot set and be planned with the VM stopped. Changes made after the snapshot
  will be lost.
- Involve the responsible administrator or support team if the state remains
  unclear. Review diagnostic data for confidential content before sharing it.

Manage snapshots after a successful update according to your retention policy.
They remain present and incur storage costs.

Further help: [troubleshooting](troubleshooting.md),
[upgrading Coriolis](https://cloudbase.github.io/coriolis-docs/guides/upgrading-coriolis.html),
and [OS versus container updates](https://cloudbase.github.io/coriolis-docs/guides/security-updates-and-hardening.html).

## Repeating deployment

Repeating deployment reuses matching existing resources. It is not a substitute
for the update workflow above.

| Situation | Behavior |
|---|---|
| Matching image or server exists | Reuse the resource |
| A name is already assigned to an incompatible resource | Stop rather than replace it automatically |
| Server is in an error state | Do not delete it automatically |
| Requested public IP is already assigned correctly | Keep the assignment |
| Requested public IP belongs to another interface | Stop |
| DNS A record exists | Update it to the requested IP |
| A generated admin password already exists | Reuse that password |
| Certificate and running service are current | Do not reissue or reconfigure |
| Certificate installation fails | Restore the previous certificate configuration |

Existing networks are reused by name; their prefix and routing are not changed
automatically. Security-group rules are added but not removed. Prepare those
changes administratively or use new resource names.
