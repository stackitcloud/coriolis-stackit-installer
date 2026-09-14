# Operations and common scenarios

[← Overview](../../README.md) | [Deutsch](../de/operations.md)

## Common scenarios

### Use an existing public IP

By ID:

```yaml
public_ip: true
public_ip_id: 00000000-0000-0000-0000-000000000000
```

Or by address:

```yaml
public_ip: true
public_ip_address: 192.0.2.10
```

### Central image project

```yaml
image:
  owner_project_id: 11111111-1111-1111-1111-111111111111
  share:
    project_ids:
      - 22222222-2222-2222-2222-222222222222
```

If the image is missing, the installer normalizes the OVA in the owner project.
The current target project is added as a consumer unless the image is shared with
the entire parent organization.

Organization-wide sharing:

```yaml
image:
  owner_project_id: 11111111-1111-1111-1111-111111111111
  share:
    parent_organization: true
```

### Explicitly use an existing normalized image

```yaml
image:
  id: 33333333-3333-3333-3333-333333333333
```

The OVA is still required because its hash and hardware requirements are used for
validation and resource matching.

### Adopt an existing server

```yaml
server:
  id: 44444444-4444-4444-4444-444444444444
  name: coriolis-appliance
```

The installer does not replace the boot volume and does not change appliance ID,
Coriolis data, or license. It requires the server to be attached to the configured
network and adds the security group when necessary.

Important: enabled bootstrap or certificate management subsequently changes the
hostname, admin password, or TLS configuration of the adopted server. Create a
volume backup before adopting a licensed production appliance for the first time.
Agent-controlled operations also require a working STACKIT Server Agent on that
server.

### Optional ALB

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

## Idempotency and failure behavior

The installer has no local state file. It discovers resources through IDs, names,
relationships, and labels such as `managed-by`, `coriolis-sha256`, and
`coriolis-normalized`.

| Situation | Behavior |
|---|---|
| Image is already available | Reuse it |
| Owned image is still `CREATING` | Monitor until terminal state |
| Image import is recognizably stale | Remove and restart it |
| VMDK transfer was interrupted | Resume at existing byte offset |
| Normalization volumes exist | Reuse them for recovery |
| Server name exists with another image label | Fail; never replace implicitly |
| Server is `ERROR` or `DELETED` | Fail without automatic deletion |
| Public IP is attached to the correct NIC | Reuse it |
| Requested public IP belongs to another NIC | Fail safely |
| DNS A record exists | Update it to the requested IP |
| Admin password was generated previously | Reuse the same appliance password |
| Certificate and running service are current | No ACME or reconfigure action |
| Certificate installation fails | Restore vendor files and hostname |
| Run Command returns a transient API error | Retry polling or bootstrap within limits |

Existing networks are reused by name; their prefix and routing are not changed.
Security-group rules are added but never removed. Prepare such changes explicitly
or use new resource names.

