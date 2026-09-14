package installer

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"
)

// buildAgentBootstrapScript configures the appliance from its base OS through
// the STACKIT Server Agent. No SSH user, key or sshd setting is touched.
func buildAgentBootstrapScript(cfg Config, fqdn string) string {
	configuredPassword := base64.StdEncoding.EncodeToString([]byte(cfg.Bootstrap.AdminPassword))
	if fqdn == "" {
		fqdn = cfg.Server.Name
	}
	return fmt.Sprintf(`#!/bin/bash
set -euo pipefail
state_dir=/var/lib/coriolis-stackit
password_file=$state_dir/admin-password
	marker=$state_dir/bootstrap-v3.sha256
mkdir -p "$state_dir"
chmod 0700 "$state_dir"
configured_password_b64=%q
if [ -n "$configured_password_b64" ]; then
  password=$(printf '%%s' "$configured_password_b64" | base64 -d)
elif [ -s "$password_file" ]; then
  password=$(cat "$password_file")
else
  password=$(openssl rand -base64 24 | tr -d '\n')
fi
printf '%%s' "$password" > "$password_file"
chmod 0600 "$password_file"
desired=$(printf '%%s\n%%s' %q "$password" | sha256sum | awk '{print $1}')
if [ "$(cat "$marker" 2>/dev/null || true)" != "$desired" ]; then
  hostnamectl set-hostname %q
  source /etc/kolla/admin-openrc.sh

  # A previous interrupted run may already have changed Keystone while the
  # local openrc still contains the old password. Accept either credential and
  # wait for the appliance services to become ready.
  openrc_password=$OS_PASSWORD
  authenticated_with_new_password=0
  for attempt in $(seq 1 120); do
    if openstack token issue >/dev/null 2>&1; then
      break
    fi
    export OS_PASSWORD="$password"
    if openstack token issue >/dev/null 2>&1; then
      authenticated_with_new_password=1
      break
    fi
    export OS_PASSWORD="$openrc_password"
    if [ "$attempt" = 120 ]; then
      echo "Coriolis identity service did not become ready" >&2
      exit 31
    fi
    sleep 10
  done
  if [ "$authenticated_with_new_password" = 0 ]; then
    openstack user set --password "$password" admin
    export OS_PASSWORD="$password"
  fi
  NEW_PASSWORD="$password" python3 - <<'PY'
import os, re, shlex
p = "/etc/kolla/admin-openrc.sh"
s = open(p).read()
s, n = re.subn(r"(?m)^export OS_PASSWORD=.*$", "export OS_PASSWORD=" + shlex.quote(os.environ["NEW_PASSWORD"]), s)
if n != 1:
    raise SystemExit("OS_PASSWORD entry not found or ambiguous")
open(p, "w").write(s)
PY
  unset NEW_PASSWORD OS_PASSWORD
  ip=$(ip -4 route get 1.1.1.1 | awk '{for(i=1;i<=NF;i++) if($i=="src") {print $(i+1); exit}}')
  python3 /root/coriolis-docker/expose_coriolis.py --use-address "$ip"
  printf '%%s' "$desired" > "$marker"
fi
printf 'CORIOLIS_ADMIN_PASSWORD_B64='
printf '%%s' "$password" | base64 -w0
printf '\nBOOTSTRAP_OK\n'
`, configuredPassword, fqdn, fqdn)
}

func (c *Cloud) bootstrapAppliance(ctx context.Context, cfg Config, serverID, fqdn string) (string, error) {
	if !cfg.Bootstrap.Enabled {
		return "", nil
	}
	if !cfg.Agent.Enabled {
		return "", fmt.Errorf("appliance bootstrap requires agent.enabled")
	}
	if err := c.waitServerAgent(ctx, c.project, serverID); err != nil {
		return "", err
	}
	// The command output contains the initial password; do not stream it to
	// stderr. It is returned only in the structured final result when requested.
	var output string
	var err error
	for attempt := 1; attempt <= 3; attempt++ {
		output, err = c.runShellScriptWithOutput(ctx, c.project, serverID, buildAgentBootstrapScript(cfg, fqdn), false)
		if err == nil {
			break
		}
		if !retryableCommandError(err) || attempt == 3 {
			return "", err
		}
		writeStatus("retrying idempotent appliance bootstrap after transient run-command failure (attempt %d/3)\n", attempt+1)
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(c.poll):
		}
	}
	for _, line := range strings.Split(output, "\n") {
		if !strings.HasPrefix(line, "CORIOLIS_ADMIN_PASSWORD_B64=") {
			continue
		}
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(line, "CORIOLIS_ADMIN_PASSWORD_B64="))
		if err != nil {
			return "", fmt.Errorf("decode generated appliance password: %w", err)
		}
		return string(decoded), nil
	}
	return "", fmt.Errorf("bootstrap completed without returning the appliance password")
}
