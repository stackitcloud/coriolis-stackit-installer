# Troubleshooting

[← Overview](../../README.md) | [Deutsch](../de/troubleshooting.md)

## Deployment problems

Check local configuration first, then cloud access:

```bash
bin/coriolis-stackit --config config.local.yaml --dry-run
bin/coriolis-stackit --config config.local.yaml --check-cloud
```

| Problem | Check or action |
|---|---|
| OVA or sizing is rejected | Check the OVA path, supported format, boot volume size, and machine type |
| Cloud access or placement fails | Check credentials, project, region, availability zone, and permissions |
| Run Command is unavailable | For automatic deployment activation, ensure `agent.enable_service: true` and `Project Editor` |
| A resource cannot be created | Check server, image, volume, and public IP quotas |
| Image transfer was interrupted | Repeat the same deployment command with unchanged configuration |
| DNS or certificate setup fails | Check zone, record name, DNS permissions, and ACME contact address |
| An adopted server does not respond | Check network attachment and a functional STACKIT Server Agent |

Do not delete existing VMs or temporary import resources without investigation.
They may be needed to resume deployment. A completed individual phase does not
mean the entire operation has finished.

## Maintenance and update problems

Use `status` to check current health. For an update already launched, keep using
the same status file:

```bash
SERVER_ID="<server-id>"
STATE_FILE="<status-file-of-the-update-attempt>"

bin/coriolis-stackit status --config config.local.yaml --server-id "$SERVER_ID"
bin/coriolis-stackit update-status --config config.local.yaml \
  --server-id "$SERVER_ID" --state-file "$STATE_FILE"
```

| Problem | Check or action |
|---|---|
| `status` reports failed checks | Resolve the causes listed in `errors` before a new update |
| Package manager is not ready | Let ongoing package operations finish or have an administrator investigate |
| Executions or schedules prevent planning | Let work finish across all tenants and disable every transfer schedule |
| Storage usage prevents planning | Free enough storage; inspected filesystems must be below 80% usage |
| Version discovery fails | Check release-note reachability, required images, and appliance access; do not treat failure as “up to date” |
| Snapshot creation fails | Check snapshot quota, permissions, and actual VM/snapshot state |
| Approval no longer matches | Generate and review a new plan, provided no update has started |
| Connection or API is temporarily unavailable | Retry a read-only status check later; do not resubmit a launched update |
| Maintenance lock or unclear update status | Inspect the existing attempt, status file, and cloud resources; do not remove a lock blindly |
| Verification fails | Keep maintenance access exclusive, investigate data and configuration, and do not resume use based on the displayed version alone |

**Never blindly repeat `update --apply` after a timeout or failure.** There is no
automatic rollback. Follow the [maintenance guide](operations.md#appliance-maintenance).

## Requesting help

Include installer version, affected function, time, error category, and
reproducible steps. Remove credentials, passwords, and confidential project
information from configurations, results, and logs before sharing them.
Do not attach OVA files or private keys.
