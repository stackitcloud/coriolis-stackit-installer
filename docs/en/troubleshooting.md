# Troubleshooting

[← Overview](../../README.md) | [Deutsch](../de/troubleshooting.md)

Recommended order:

1. Run `--dry-run` and resolve OVA or sizing errors.
2. Run `--check-cloud` to validate permissions, region, zone, and machine type.
3. Check quotas for servers, images, volumes, and public IPs.
4. For `Service not enabled`, verify `agent.enable_service: true` and the service
   account's `Project Editor` role.
5. After a transfer error, rerun the same installer command.
6. For DNS or ACME errors, verify zone, record name, and service account rights.
7. When adopting a server, verify that its STACKIT Server Agent is active.
8. Do not blindly delete a VM or normalization volumes: labelled resources may
   contain recoverable intermediate state.

Errors identify the affected phase. Certificate installation reports sanitized
error categories only; internal vendor secrets are not copied into console output.
