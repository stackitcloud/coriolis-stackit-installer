# Coriolis STACKIT Installer

**English** | [Deutsch](README.de.md)

Deploy and maintain a Cloudbase Coriolis appliance on STACKIT using one binary and
a YAML configuration. Terraform, the STACKIT CLI, and manual VM console setup are
not required.

For deployment, you need a STACKIT service account key, project ID, region, and a
Coriolis OVA. Maintenance of an existing appliance does not require the OVA.

## Features

- Deploy a new appliance or explicitly adopt an existing server.
- Reuse existing networks, images, and public IPs, or create the required resources.
- Share images across projects or manage them in a central image project.
- Configure the appliance hostname and admin password.
- Provide HTTPS with a directly installed certificate or an optional STACKIT
  Application Load Balancer.
- Inspect appliance health and discover suitable update targets.
- Perform explicitly approved, same-stream service updates with mandatory cold
  snapshots of all appliance volumes.
- Reuse matching resources on repeated deployment runs instead of replacing the
  appliance.

Deployment never upgrades an existing appliance by replacing its VM or boot
volume. Use the dedicated maintenance commands for service updates.

## Getting started

Read the [prerequisites](docs/en/prerequisites.md), then follow the
[getting started guide](docs/en/getting-started.md).

With a suitable binary and your own configuration in place:

```bash
# Inspect the configuration and OVA without changing cloud resources.
bin/coriolis-stackit --config config.local.yaml --dry-run

# Check cloud access and placement.
bin/coriolis-stackit --config config.local.yaml --check-cloud

# Deploy.
bin/coriolis-stackit --config config.local.yaml
```

Use [examples/config.yaml](examples/config.yaml) as a starting point and replace
its example values. Selected command-line flags override YAML settings. See the
[configuration reference](docs/en/configuration.md) for all options.

**Protect credentials and deployment output.** The result can include the
generated admin password. Keep local configurations containing secrets, service
account keys, OVAs, and result files out of version control and public build logs.

## Maintain an existing appliance

```bash
SERVER_ID="<server-id>"

bin/coriolis-stackit status --config config.local.yaml --server-id "$SERVER_ID"
bin/coriolis-stackit versions --config config.local.yaml --server-id "$SERVER_ID"

# Plan only; does not execute an update.
bin/coriolis-stackit update --config config.local.yaml \
  --server-id "$SERVER_ID" --latest-compatible
```

An update requires a reviewed plan, explicit approval, and a maintenance window.
The CLI stops services and the VM, creates fresh native STACKIT snapshots, restarts
the same appliance, and launches the service update. Only successful verification
with `update-status` confirms completion.

See the [maintenance guide](docs/en/operations.md#appliance-maintenance) for the
complete approval, backup, execution, and verification procedure.

## What to expect during deployment

The first deployment imports the OVA and needs temporary cloud resources.
Subsequent deployments can reuse the prepared image. Typical durations vary with
image size, upload speed, storage performance, and STACKIT availability:

| Scenario | Indicative duration |
|---|---|
| First deployment including image import | 40–100 minutes |
| Deployment using an existing prepared image | 8–25 minutes |
| Repeated deployment without material changes | 2–10 minutes |
| Optional load balancer | Usually adds 5–15 minutes |

These are estimates, not deadlines. Set `timeout` high enough for the longest
individual deployment phase. The recommended storage performance class is
`storage_premium_perf12`.

Progress messages appear on stderr; the final JSON result appears on stdout.
`[DONE]` for an individual phase does not mean the complete operation has finished.
Allow the command to return, and use the checks described in the appropriate guide.

## Documentation

### User guides

| Guide | Contents |
|---|---|
| [Prerequisites](docs/en/prerequisites.md) | Workstation, permissions, quotas, and connectivity |
| [Getting started](docs/en/getting-started.md) | Prepare, validate, and deploy |
| [Configuration](docs/en/configuration.md) | YAML settings and command-line options |
| [Operations](docs/en/operations.md) | Existing resources, appliance status, and safe updates |
| [Network and security](docs/en/security.md) | Access rules, credentials, backups, and limitations |
| [Troubleshooting](docs/en/troubleshooting.md) | Resolve deployment and maintenance problems |

### For contributors and maintainers

Architecture, implementation details, and development instructions are kept
separately in [developer documentation](docs/developer/README.md).

## Contributing

Contributions are welcome. Follow the [contribution guidelines](CONTRIBUTING.md)
and [Code of Conduct](CODE_OF_CONDUCT.md). Update both language versions of user
guides when user-visible behavior changes.

## License

The installer, source code, documentation, and examples are licensed under the
[Apache License, Version 2.0](LICENSE) (`Apache-2.0`).

The Cloudbase Coriolis appliance and third-party components remain subject to
their own licenses and terms.
