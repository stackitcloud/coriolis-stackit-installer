package installer

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

func validUpdateID(id string) bool {
	parsed, err := uuid.Parse(id)
	return err == nil && parsed.String() == id
}

func updateDirectory(id string) string {
	return "/var/lib/coriolis-stackit/updates/" + id
}

func prepareUpdateScript(journal UpdateJournal) string {
	plan := journal.Plan
	return fmt.Sprintf(`#!/bin/bash
set -euo pipefail
%s
umask 077
base=/root/coriolis-docker
work=%s
lock=/var/lib/coriolis-stackit/update-lock
%s
test "$(git -C "$base" rev-parse HEAD)" = %s
test -z "$(git -C "$base" status --porcelain --untracked-files=no)"
test "$(sha256sum "$base/upgrade_coriolis.py" | cut -d' ' -f1)" = %s
mkdir -p /var/lib/coriolis-stackit/updates
mkdir "$lock"
printf '%%s' %s > "$lock/id"
mkdir "$work"
exec >"$work/prepare.log" 2>&1
git -C "$base" fetch origin
git -C "$base" rev-parse %s > "$work/target-commit"
docker manifest inspect %s >/dev/null
cp -a /etc/coriolis/coriolis.conf "$work/coriolis.conf"
cp -a /etc/kolla/passwords.yml "$work/passwords.yml"
tar -czf "$work/configuration.tar.gz" --exclude=var/lib/coriolis-stackit/updates -C / etc/coriolis etc/kolla root/coriolis-docker/config.yml root/coriolis-docker/docker-images-config.yml etc/hostname var/lib/coriolis-stackit
printf '%%s' %s | base64 -d > "$work/run.sh"
chmod 0700 "$work/run.sh"
sync
`, maintenanceGuestEnvironment, shellLiteral(updateDirectory(journal.ID)), packageManagerPreflightScript(), shellLiteral(plan.Before.GitCommit),
		shellLiteral(plan.Before.UpdaterSHA256), shellLiteral(journal.ID),
		shellLiteral("origin/stable/"+strings.Split(plan.TargetVersion, ".")[0]),
		shellLiteral("registry.cloudbase.it/appliance/coriolis-metal-hub:"+plan.TargetVersion),
		shellLiteral(encodedScript(vendorUpdateScript(journal))))
}

func quiesceUpdateScript(journal UpdateJournal) string {
	inventory, _ := json.Marshal(journal.Plan.Before.Inventory)
	return fmt.Sprintf(`#!/bin/bash
set -euo pipefail
umask 077
work=%s
test "$(cat /var/lib/coriolis-stackit/update-lock/id)" = %s
exec >>"$work/prepare.log" 2>&1
docker ps --format '{{.Names}}' > "$work/running-containers"
running=()
while IFS= read -r container || [ -n "$container" ]; do
  running+=("$container")
done < "$work/running-containers"
test "${#running[@]}" -gt 0
# Close local submission paths before checking all tenant workloads again.
docker stop --time 120 coriolis-api coriolis-scheduler coriolis-transfer-cron
ip=$(ip -4 route get 1.1.1.1 | awk '{for(i=1;i<=NF;i++) if($i=="src") {print $(i+1); exit}}')
docker exec -i coriolis-conductor python3 - "$ip" > "$work/quiesced-inventory.json" <<'QUIESCE_INVENTORY'
%s
QUIESCE_INVENTORY
python3 - "$work/quiesced-inventory.json" %s <<'CHECK_QUIESCED'
import json, sys
with open(sys.argv[1]) as stream:
    current = json.load(stream)
if current["active_executions"] != 0 or current["enabled_schedules"] != 0:
    raise SystemExit("Workloads are not quiescent; API/schedulers remain stopped for manual inspection")
if current["inventory"] != json.loads(sys.argv[2]):
    raise SystemExit("Tenant data changed after approval; inspect before continuing maintenance")
CHECK_QUIESCED
docker stop --time 120 -- "${running[@]}"
sync
`, shellLiteral(updateDirectory(journal.ID)), shellLiteral(journal.ID),
		maintenanceInventoryPython, shellLiteral(string(inventory)))
}

func vendorUpdateScript(journal UpdateJournal) string {
	plan := journal.Plan
	return fmt.Sprintf(`#!/bin/bash
set -euo pipefail
%s
umask 077
work=%s
base=/root/coriolis-docker
exec >>"$work/update.log" 2>&1
finish() {
  code=$?
  trap - EXIT
  if [ "$code" != 0 ]; then
    echo "Update failed; keeping snapshots and maintenance lock. No automatic database rollback."
    if ! docker stop coriolis-api coriolis-scheduler coriolis-transfer-cron coriolis-worker coriolis-conductor coriolis-deployer-manager coriolis-minion-manager; then
      echo "ERROR: Could not stop every workload service after update failure."
    fi
  fi
  printf '%%s\n' "$code" > "$work/exit-code.tmp"
  mv "$work/exit-code.tmp" "$work/exit-code"
  exit "$code"
}
trap finish EXIT
test "$(cat /var/lib/coriolis-stackit/update-lock/id)" = %s
test "$(cat /etc/coriolis/coriolis.release)" = %s
%s
cd "$base"
git checkout --detach "$(cat "$work/target-commit")"
source /etc/kolla/admin-openrc.sh
python3 - %s <<'UPDATE_IMAGE_CONFIG'
import os, sys, tempfile, yaml
path = "/root/coriolis-docker/docker-images-config.yml"
with open(path) as stream:
    config = yaml.safe_load(stream)
config["docker_pull_images"] = True
config["default_coriolis_docker_images_tag"] = sys.argv[1]
fd, temporary = tempfile.mkstemp(dir=os.path.dirname(path), prefix=".coriolis-update-")
with os.fdopen(fd, "w") as stream:
    yaml.safe_dump(config, stream, sort_keys=False)
    stream.flush()
    os.fsync(stream.fileno())
os.replace(temporary, path)
UPDATE_IMAGE_CONFIG
python3 "$base/upgrade_coriolis.py" -u "$base/utils/upgrade_scripts" -c %s -n %s
# Same-stream updates retain the existing application settings; preserve the
# generated target file separately for inspection of new optional settings.
cp -a /etc/coriolis/coriolis.conf "$work/coriolis.conf.generated"
cp -a "$work/coriolis.conf" /etc/coriolis/coriolis.conf
cmp "$work/passwords.yml" /etc/kolla/passwords.yml
services=()
while IFS= read -r service || [ -n "$service" ]; do
  services+=("$service")
done < <(docker ps -a --format '{{.Names}}' | grep '^coriolis-')
test "${#services[@]}" -gt 0
docker restart "${services[@]}"
echo "Vendor updater completed; independent verification via update-status is still required."
`, maintenanceGuestEnvironment, shellLiteral(updateDirectory(journal.ID)), shellLiteral(journal.ID),
		shellLiteral(plan.Before.Release), packageManagerPreflightScript(), shellLiteral(plan.TargetVersion),
		shellLiteral(plan.Before.Release), shellLiteral(plan.TargetVersion))
}

func launchUpdateScript(journal UpdateJournal) string {
	return fmt.Sprintf(`#!/bin/bash
set -euo pipefail
test "$(cat /var/lib/coriolis-stackit/update-lock/id)" = %s
systemd-run --no-block --unit=%s --property=Type=exec --property=TimeoutStartSec=0 /bin/bash %s
`, shellLiteral(journal.ID), shellLiteral("coriolis-stackit-update-"+journal.ID),
		shellLiteral(updateDirectory(journal.ID)+"/run.sh"))
}

func updateJobStatusScript(id string) string {
	return fmt.Sprintf(`#!/bin/bash
set -euo pipefail
python3 - %s %s <<'JOB_STATUS'
import json, pathlib, subprocess, sys
work = pathlib.Path(sys.argv[1])
result = work / "exit-code"
if result.is_file():
    print(json.dumps({"state": "finished", "exit_code": int(result.read_text())}))
else:
    state = subprocess.run(["systemctl", "show", sys.argv[2], "--property=ActiveState", "--value"],
                           check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True).stdout.strip()
    print(json.dumps({"state": "running" if state in ("active", "activating") else "unknown", "exit_code": -1}))
JOB_STATUS
`, shellLiteral(updateDirectory(id)), shellLiteral("coriolis-stackit-update-"+id))
}
