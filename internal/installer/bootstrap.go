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
marker=$state_dir/bootstrap-v4.sha256
kolla_passwords=/etc/kolla/passwords.yml
openrc=/etc/kolla/admin-openrc.sh
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

runtime_current=0
persistent_current=0
if [ -s "$kolla_passwords" ]; then
  if NEW_PASSWORD="$password" python3 - "$kolla_passwords" <<'PY'
import json, os, re, sys

text = open(sys.argv[1]).read()
matches = re.findall(r"(?m)^\s*keystone_admin_password\s*:\s*(.*?)\s*$", text)
if len(matches) != 1:
    raise SystemExit(1)
try:
    value = json.loads(matches[0])
except Exception:
    value = matches[0].strip().strip("'").strip('"')
raise SystemExit(0 if value == os.environ["NEW_PASSWORD"] else 1)
PY
  then
    persistent_current=1
  fi
fi
if [ "$(cat "$marker" 2>/dev/null || true)" = "$desired" ] &&
   [ "$(hostname)" = %q ] && [ "$persistent_current" = 1 ] && [ -s "$openrc" ]; then
  source "$openrc"
  if [ "${OS_PASSWORD:-}" = "$password" ] && openstack token issue >/dev/null 2>&1; then
    runtime_current=1
  fi
fi

if [ "$runtime_current" = 0 ]; then
  hostnamectl set-hostname %q
  source "$openrc"

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
    openstack user set --domain default --password "$password" admin
    export OS_PASSWORD="$password"
    openstack token issue >/dev/null
  fi

  # expose_coriolis.py regenerates Keystone and admin-openrc.sh from the
  # persistent Kolla password file. Update that source of truth first, or the
  # vendor reconfiguration silently restores the original appliance password.
  NEW_PASSWORD="$password" python3 - "$kolla_passwords" "$openrc" <<'PY'
import json, os, re, shlex, stat, sys, tempfile

passwords_path, openrc_path = sys.argv[1:]
password = os.environ["NEW_PASSWORD"]

def atomic_write(path, contents):
    current = os.stat(path)
    fd, temporary = tempfile.mkstemp(prefix=".coriolis-stackit-", dir=os.path.dirname(path))
    try:
        os.fchmod(fd, stat.S_IMODE(current.st_mode))
        os.fchown(fd, current.st_uid, current.st_gid)
        with os.fdopen(fd, "w") as stream:
            fd = -1
            stream.write(contents)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, path)
    finally:
        if fd >= 0:
            os.close(fd)
        if os.path.exists(temporary):
            os.unlink(temporary)

contents = open(passwords_path).read()
contents, count = re.subn(
    r"(?m)^(\s*keystone_admin_password\s*:\s*).*$",
    lambda match: match.group(1) + json.dumps(password),
    contents,
)
if count != 1:
    raise SystemExit("keystone_admin_password entry not found or ambiguous")
atomic_write(passwords_path, contents)

contents = open(openrc_path).read()
contents, count = re.subn(
    r"(?m)^export OS_PASSWORD=.*$",
    "export OS_PASSWORD=" + shlex.quote(password),
    contents,
)
if count != 1:
    raise SystemExit("OS_PASSWORD entry not found or ambiguous")
atomic_write(openrc_path, contents)
PY
  unset NEW_PASSWORD OS_PASSWORD
  ip=$(ip -4 route get 1.1.1.1 | awk '{for(i=1;i<=NF;i++) if($i=="src") {print $(i+1); exit}}')
  reconfigure_log=$state_dir/bootstrap-reconfigure.log
  reconfigure_rc=0
  umask 077
  python3 /root/coriolis-docker/expose_coriolis.py --use-address "$ip" >"$reconfigure_log" 2>&1 || reconfigure_rc=$?

  # Do not report success merely because the vendor command returned zero.
  # It may restart Keystone and can return non-zero despite reaching the desired
  # state. Verify its regenerated OpenRC, the real login, and the web service.
  verified=0
  for attempt in $(seq 1 120); do
    source "$openrc"
    if [ "${OS_PASSWORD:-}" = "$password" ]; then
      export OS_PASSWORD="$password"
      if openstack token issue >/dev/null 2>&1 &&
         curl -fsSk --connect-timeout 5 --max-time 10 "https://$ip/api/config" >/dev/null 2>&1; then
        verified=1
        break
      fi
    fi
    sleep 10
  done
  if [ "$verified" = 0 ]; then
    echo "Coriolis services did not become healthy with the configured password after vendor reconfiguration (exit $reconfigure_rc)" >&2
    exit 32
  fi
  if [ "$reconfigure_rc" != 0 ]; then
    echo "BOOTSTRAP_RECONFIGURE_WARNING exit=$reconfigure_rc final_state=healthy"
  fi
  rm -f "$reconfigure_log"
  printf '%%s' "$desired" > "$marker"
fi
printf 'CORIOLIS_ADMIN_PASSWORD_B64='
printf '%%s' "$password" | base64 -w0
printf '\nBOOTSTRAP_OK\n'
`, configuredPassword, fqdn, fqdn, fqdn)
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
	err := progressAction(ctx, "Waiting for Coriolis services and applying appliance configuration", func() error {
		var commandErr error
		for attempt := 1; attempt <= 3; attempt++ {
			output, commandErr = c.runShellScriptWithOutput(ctx, c.project, serverID, buildAgentBootstrapScript(cfg, fqdn), false)
			if commandErr == nil {
				return nil
			}
			if !retryableCommandError(commandErr) || attempt == 3 {
				return commandErr
			}
			writeInfo("retrying idempotent appliance bootstrap after transient run-command failure (attempt %d/3)", attempt+1)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(c.poll):
			}
		}
		return commandErr
	})
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "BOOTSTRAP_RECONFIGURE_WARNING ") {
			writeWarning("vendor appliance reconfiguration returned non-zero, but Keystone and the web service reached the verified desired state (%s)", strings.TrimPrefix(line, "BOOTSTRAP_RECONFIGURE_WARNING "))
		}
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
