package installer

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func healthyMaintenanceStatus() ApplianceStatus {
	return ApplianceStatus{
		ProjectID: "project", Region: "eu01", ServerID: "server", ServerName: "appliance",
		PowerStatus: "RUNNING", Volumes: []string{"boot"},
		Release: "2608.2", GitCommit: strings.Repeat("a", 40), GitClean: true,
		UpdaterSHA256: strings.Repeat("b", 64), ConfigSHA256: strings.Repeat("c", 64),
		ProtectedSHA256: strings.Repeat("e", 64),
		Inventory:       map[string]string{"endpoint": "digest"},
		DiskUsedPercent: 46, Healthy: true, UpdaterAvailable: true, PackageManagerReady: true,
	}
}

func TestMaintenanceVersionPolicy(t *testing.T) {
	for _, target := range []string{"2608.3", "2608.2.1"} {
		if err := validateUpdateVersion("2608.2", target); err != nil {
			t.Errorf("%s: %v", target, err)
		}
	}
	for _, target := range []string{"latest", "2608.2", "2608.1", "2610.0", "2608.3;id", "2608.03", "2608.3.99999999999999999", ""} {
		if validateUpdateVersion("2608.2", target) == nil {
			t.Errorf("accepted unsafe target %q", target)
		}
	}
}

func TestUpdatePlanFailsClosed(t *testing.T) {
	for name, change := range map[string]func(*ApplianceStatus){
		"unhealthy":             func(s *ApplianceStatus) { s.Healthy = false },
		"busy":                  func(s *ApplianceStatus) { s.ActiveExecutions = 1 },
		"scheduled":             func(s *ApplianceStatus) { s.EnabledSchedules = 1 },
		"disk threshold":        func(s *ApplianceStatus) { s.DiskUsedPercent = 80 },
		"dirty vendor tree":     func(s *ApplianceStatus) { s.GitClean = false },
		"no volumes":            func(s *ApplianceStatus) { s.Volumes = nil },
		"no inventory":          func(s *ApplianceStatus) { s.Inventory = nil },
		"no updater":            func(s *ApplianceStatus) { s.UpdaterAvailable = false },
		"package state unknown": func(s *ApplianceStatus) { s.PackageManagerReady = false },
		"pending update":        func(s *ApplianceStatus) { s.UpdateLocked = true },
		"stopped":               func(s *ApplianceStatus) { s.PowerStatus = "STOPPED" },
		"incomplete evidence":   func(s *ApplianceStatus) { s.ConfigSHA256 = "" },
	} {
		t.Run(name, func(t *testing.T) {
			s := healthyMaintenanceStatus()
			change(&s)
			if _, err := makeUpdatePlan(s, "2608.3"); err == nil {
				t.Fatal("unsafe update was accepted")
			}
		})
	}
}

func TestUpdateApprovalBindsTargetAndEvidence(t *testing.T) {
	s := healthyMaintenanceStatus()
	plan, err := makeUpdatePlan(s, "2608.3")
	if err != nil {
		t.Fatal(err)
	}
	changed, err := makeUpdatePlan(s, "2608.4")
	if err != nil {
		t.Fatal(err)
	}
	if changed.Approval == plan.Approval || len(plan.Approval) != 64 {
		t.Fatal("approval does not bind target")
	}
	s.ConfigSHA256 = strings.Repeat("d", 64)
	changed, err = makeUpdatePlan(s, "2608.3")
	if err != nil {
		t.Fatal(err)
	}
	if changed.Approval == plan.Approval {
		t.Fatal("approval does not bind appliance configuration")
	}
}

type fakeMaintenance struct {
	calls []string
	fail  string
}

func (f *fakeMaintenance) action(name string) error {
	f.calls = append(f.calls, name)
	if f.fail == name {
		return errors.New("injected " + name)
	}
	return nil
}
func (f *fakeMaintenance) Prepare(context.Context, UpdateJournal) error { return f.action("prepare") }
func (f *fakeMaintenance) Stop(context.Context, string) error           { return f.action("stop") }
func (f *fakeMaintenance) Snapshot(_ context.Context, volume, job string) (string, error) {
	return "snapshot-" + volume, f.action("snapshot:" + volume)
}
func (f *fakeMaintenance) WaitSnapshot(_ context.Context, id string) error {
	return f.action("wait:" + id)
}
func (f *fakeMaintenance) Start(context.Context, string) error { return f.action("start") }
func (f *fakeMaintenance) Launch(context.Context, UpdateJournal) error {
	return f.action("launch")
}

func TestColdBackupPrecedesUpdate(t *testing.T) {
	plan, err := makeUpdatePlan(healthyMaintenanceStatus(), "2608.3")
	if err != nil {
		t.Fatal(err)
	}
	journal := UpdateJournal{ID: "test-job", Plan: plan, Stage: "approved"}
	path := filepath.Join(t.TempDir(), "update.json")
	if err := createUpdateJournal(path, journal); err != nil {
		t.Fatal(err)
	}
	fake := &fakeMaintenance{}
	if err := executeUpdate(context.Background(), fake, path, &journal); err != nil {
		t.Fatal(err)
	}
	want := "prepare,stop,snapshot:boot,wait:snapshot-boot,start,launch"
	if got := strings.Join(fake.calls, ","); got != want {
		t.Fatalf("calls=%s, want %s", got, want)
	}
	stored, err := readUpdateJournal(path)
	if err != nil || stored.Snapshots["boot"] != "snapshot-boot" || stored.Stage != "running" {
		t.Fatalf("journal=%+v error=%v", stored, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("unsafe journal permissions: %v, %v", info, err)
	}
}

func TestBackupFailureNeverStartsUpdater(t *testing.T) {
	for _, failure := range []string{"prepare", "stop", "snapshot:boot", "wait:snapshot-boot", "start", "launch"} {
		t.Run(failure, func(t *testing.T) {
			plan, _ := makeUpdatePlan(healthyMaintenanceStatus(), "2608.3")
			journal := UpdateJournal{ID: "test-job", Plan: plan}
			path := filepath.Join(t.TempDir(), "update.json")
			if err := createUpdateJournal(path, journal); err != nil {
				t.Fatal(err)
			}
			fake := &fakeMaintenance{fail: failure}
			if executeUpdate(context.Background(), fake, path, &journal) == nil {
				t.Fatal("failure ignored")
			}
			if failure != "launch" && strings.Contains(strings.Join(fake.calls, ","), "launch") {
				t.Fatal("updater launched without a verified backup and running server")
			}
			if failure == "snapshot:boot" || failure == "wait:snapshot-boot" {
				if strings.Contains(strings.Join(fake.calls, ","), "start") {
					t.Fatal("VM restarted after an incomplete backup")
				}
			}
		})
	}
}

func TestAllVolumesMustBeBackedUpBeforeRestart(t *testing.T) {
	before := healthyMaintenanceStatus()
	before.Volumes = []string{"boot", "data"}
	plan, _ := makeUpdatePlan(before, "2608.3")
	journal := UpdateJournal{ID: "test-job", Plan: plan}
	path := filepath.Join(t.TempDir(), "update.json")
	if err := createUpdateJournal(path, journal); err != nil {
		t.Fatal(err)
	}
	fake := &fakeMaintenance{fail: "wait:snapshot-data"}
	if executeUpdate(context.Background(), fake, path, &journal) == nil {
		t.Fatal("second snapshot failure ignored")
	}
	calls := strings.Join(fake.calls, ",")
	if strings.Contains(calls, "start") || strings.Contains(calls, "launch") {
		t.Fatalf("update advanced despite an incomplete backup: %s", calls)
	}
	stored, err := readUpdateJournal(path)
	if err != nil || len(stored.Snapshots) != 2 || stored.Error == "" || stored.UpdatedAt.IsZero() {
		t.Fatalf("incomplete recovery evidence: %+v, %v", stored, err)
	}
}

func TestJournalFailurePreventsAllCloudChanges(t *testing.T) {
	plan, _ := makeUpdatePlan(healthyMaintenanceStatus(), "2608.3")
	journal := UpdateJournal{ID: "test-job", Plan: plan}
	fake := &fakeMaintenance{}
	path := filepath.Join(t.TempDir(), "absent", "update.json")
	if executeUpdate(context.Background(), fake, path, &journal) == nil || len(fake.calls) != 0 {
		t.Fatal("cloud mutation occurred without a durable journal")
	}
}

func TestJournalNeverOverwritesPreviousAttempt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "update.json")
	j := UpdateJournal{ID: "first"}
	if err := createUpdateJournal(path, j); err != nil {
		t.Fatal(err)
	}
	if createUpdateJournal(path, UpdateJournal{ID: "second"}) == nil {
		t.Fatal("existing attempt overwritten")
	}
}

func TestMaintenanceDispatchDoesNotRequireOVA(t *testing.T) {
	if err := Run([]string{"update", "--target-version", "latest"}, "test"); err == nil || !strings.Contains(err.Error(), "target") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMaintenanceRejectsUnapprovedApplyBeforeCloudAccess(t *testing.T) {
	for _, args := range [][]string{
		{"update", "--target-version", "2608.3", "--apply"},
		{"status", "--apply"},
		{"status", "--target-version", "2608.3"},
		{"update", "--target-version", "2608.3", "unexpected"},
	} {
		if err := Run(args, "test"); err == nil {
			t.Errorf("accepted %v", args)
		}
	}
}

func TestMaintenanceScriptsFitRunCommandAndParse(t *testing.T) {
	before := healthyMaintenanceStatus()
	plan, _ := makeUpdatePlan(before, "2608.3")
	journal := UpdateJournal{ID: "c66380ee-a322-4fa2-bb29-df5f22274c12", Plan: plan}
	scripts := map[string]string{
		"status":     maintenanceStatusScript("appliance.example.com"),
		"prepare":    prepareUpdateScript(journal),
		"quiesce":    quiesceUpdateScript(journal),
		"launch":     launchUpdateScript(journal),
		"job-status": updateJobStatusScript(journal.ID),
		"vendor":     vendorUpdateScript(journal),
	}
	bash, bashErr := exec.LookPath("bash")
	for name, script := range scripts {
		t.Run(name, func(t *testing.T) {
			if len(script) > 10_000 {
				t.Fatalf("script exceeds API limit: %d bytes", len(script))
			}
			if bashErr == nil {
				cmd := exec.Command(bash, "-n")
				cmd.Stdin = strings.NewReader(script)
				if output, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("bash syntax: %v: %s", err, output)
				}
			}
		})
	}
	prepare := scripts["prepare"]
	if strings.Index(scripts["quiesce"], "docker stop") > strings.LastIndex(scripts["quiesce"], "sync") {
		t.Fatal("data is not flushed after stopping application writers")
	}
	if !strings.Contains(prepare, "--exclude=var/lib/coriolis-stackit/updates") {
		t.Fatal("configuration archive would contain itself")
	}
}

func TestVerifiedUpdateStatusIsHistoricalAndNeedsNoGuestAccess(t *testing.T) {
	plan, _ := makeUpdatePlan(healthyMaintenanceStatus(), "2608.3")
	journal := UpdateJournal{ID: "c66380ee-a322-4fa2-bb29-df5f22274c12", Plan: plan, Stage: "verified"}
	path := filepath.Join(t.TempDir(), "update.json")
	if err := createUpdateJournal(path, journal); err != nil {
		t.Fatal(err)
	}
	cloud := &Cloud{project: "project", region: "eu01"}
	if err := cloud.reportUpdate(context.Background(), Config{}, path); err != nil {
		t.Fatalf("historical success must not depend on later tenant edits or guest availability: %v", err)
	}
}

func TestPostUpdateVerificationFailsClosed(t *testing.T) {
	before := healthyMaintenanceStatus()
	before.Containers = []ApplianceContainer{{Name: "coriolis-api", Image: "registry/coriolis-api:2608.2"}}
	before.TLSFingerprints = map[string]string{"443": "certificate"}
	plan, _ := makeUpdatePlan(before, "2608.3")
	for name, change := range map[string]func(*ApplianceStatus){
		"healthy":             func(s *ApplianceStatus) {},
		"wrong release":       func(s *ApplianceStatus) { s.Release = "2608.2" },
		"lost tenant data":    func(s *ApplianceStatus) { s.Inventory = map[string]string{"endpoint": "changed"} },
		"certificate changed": func(s *ApplianceStatus) { s.TLSFingerprints = nil },
		"settings changed":    func(s *ApplianceStatus) { s.ProtectedSHA256 = "changed" },
		"component unchanged": func(s *ApplianceStatus) { s.Containers = before.Containers },
		"container lost":      func(s *ApplianceStatus) { s.Containers = nil },
		"unhealthy":           func(s *ApplianceStatus) { s.Healthy = false },
	} {
		t.Run(name, func(t *testing.T) {
			after := before
			after.Release = plan.TargetVersion
			after.Containers = []ApplianceContainer{{Name: "coriolis-api", Image: "registry/coriolis-api:2608.3"}}
			change(&after)
			err := verifyUpdatedAppliance(plan, after)
			if (err == nil) != (name == "healthy") {
				t.Fatalf("unexpected verification result: %v", err)
			}
		})
	}
}

func TestQuiesceRechecksJobsBeforeStoppingWorkers(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not available")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is not available")
	}
	for _, mode := range []string{"idle", "active", "data-changed"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			id := "c66380ee-a322-4fa2-bb29-df5f22274c12"
			work := filepath.Join(root, "updates", id)
			bin := filepath.Join(root, "bin")
			for _, dir := range []string{work, bin, filepath.Join(root, "update-lock")} {
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
			}
			before := healthyMaintenanceStatus()
			plan, _ := makeUpdatePlan(before, "2608.3")
			active := 0
			inventory := before.Inventory
			if mode == "active" {
				active = 1
			}
			if mode == "data-changed" {
				inventory = map[string]string{"endpoint": "changed"}
			}
			data, err := json.Marshal(map[string]interface{}{
				"active_executions": active, "enabled_schedules": 0, "inventory": inventory,
			})
			if err != nil {
				t.Fatal(err)
			}
			files := map[string]string{
				filepath.Join(root, "inventory.json"):    string(data),
				filepath.Join(root, "update-lock", "id"): id,
				filepath.Join(bin, "ip"):                 "#!/bin/sh\nprintf '1.1.1.1 dev eth0 src 10.1.0.2\\n'\n",
				filepath.Join(bin, "sync"):               "#!/bin/sh\nexit 0\n",
				filepath.Join(bin, "docker"): `#!/bin/sh
printf '%s\n' "$*" >> "$CORIOLIS_FIXTURE_ROOT/docker-calls"
case "$1" in
  ps) printf 'coriolis-api\ncoriolis-worker\ncoriolis-conductor\n' ;;
  exec) cat "$CORIOLIS_FIXTURE_ROOT/inventory.json" ;;
esac
`,
			}
			for path, content := range files {
				if err := os.WriteFile(path, []byte(content), 0700); err != nil {
					t.Fatal(err)
				}
			}
			script := strings.ReplaceAll(quiesceUpdateScript(UpdateJournal{ID: id, Plan: plan}),
				"/var/lib/coriolis-stackit", root)
			cmd := exec.Command(bash, "-c", script)
			cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "CORIOLIS_FIXTURE_ROOT="+root)
			output, err := cmd.CombinedOutput()
			if (err == nil) != (mode == "idle") {
				t.Fatalf("unexpected quiesce result: %v: %s", err, output)
			}
			calls, err := os.ReadFile(filepath.Join(root, "docker-calls"))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(calls), "-- coriolis-api coriolis-worker") != (mode == "idle") {
				t.Fatalf("worker stopped despite active jobs or data drift: %s", calls)
			}
		})
	}
}

func TestVendorRunnerPreservesConfigurationAndRecordsFailures(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not available")
	}
	for _, mode := range []string{"success", "vendor-failure", "password-change"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			id := "c66380ee-a322-4fa2-bb29-df5f22274c12"
			state := filepath.Join(root, "state")
			work := filepath.Join(state, "updates", id)
			base := filepath.Join(root, "vendor")
			config := filepath.Join(root, "etc", "coriolis")
			kolla := filepath.Join(root, "etc", "kolla")
			bin := filepath.Join(root, "bin")
			for _, dir := range []string{work, base, config, kolla, bin, filepath.Join(state, "update-lock")} {
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
			}
			files := map[string]string{
				filepath.Join(work, "target-commit"):      "target-commit",
				filepath.Join(work, "coriolis.conf"):      "custom settings",
				filepath.Join(work, "passwords.yml"):      "existing credentials",
				filepath.Join(config, "coriolis.conf"):    "custom settings",
				filepath.Join(config, "coriolis.release"): "2608.2",
				filepath.Join(kolla, "passwords.yml"):     "existing credentials",
				filepath.Join(kolla, "admin-openrc.sh"):   "export OS_PASSWORD=SUPER_SECRET_PASSWORD\n",
				filepath.Join(state, "update-lock", "id"): id,
				filepath.Join(bin, "git"):                 "#!/bin/sh\nexit 0\n",
				filepath.Join(bin, "docker"): `#!/bin/sh
	printf '%s\n' "$*" >> "$CORIOLIS_FIXTURE_ROOT/docker-calls"
	if [ "$1" = ps ]; then printf 'coriolis-api\ncoriolis-worker\n'; fi
	`,
				filepath.Join(bin, "python3"): `#!/bin/sh
	if [ "$1" = "-" ]; then cat >/dev/null; exit 0; fi
	printf '%s\n' "$*" > "$CORIOLIS_FIXTURE_ROOT/vendor-call"
	printf 'generated defaults' > "$CORIOLIS_FIXTURE_ROOT/etc/coriolis/coriolis.conf"
	if [ "$CORIOLIS_FIXTURE_MODE" = vendor-failure ]; then exit 23; fi
	if [ "$CORIOLIS_FIXTURE_MODE" = password-change ]; then
	  printf 'changed credentials' > "$CORIOLIS_FIXTURE_ROOT/etc/kolla/passwords.yml"
	fi
	`,
			}
			for path, content := range files {
				if err := os.WriteFile(path, []byte(content), 0700); err != nil {
					t.Fatal(err)
				}
			}
			plan, _ := makeUpdatePlan(healthyMaintenanceStatus(), "2608.3")
			script := vendorUpdateScript(UpdateJournal{ID: id, Plan: plan})
			script = strings.NewReplacer(
				"/var/lib/coriolis-stackit", state,
				"/root/coriolis-docker", base,
				"/etc/coriolis", config,
				"/etc/kolla", kolla,
			).Replace(script)
			cmd := exec.Command(bash, "-c", script)
			cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"),
				"CORIOLIS_FIXTURE_ROOT="+root, "CORIOLIS_FIXTURE_MODE="+mode)
			output, runErr := cmd.CombinedOutput()
			if (runErr == nil) != (mode == "success") {
				t.Fatalf("unexpected runner result: %v: %s", runErr, output)
			}
			result, err := os.ReadFile(filepath.Join(work, "exit-code"))
			if err != nil || (strings.TrimSpace(string(result)) == "0") != (mode == "success") {
				t.Fatalf("incorrect persistent result %q: %v", result, err)
			}
			if mode == "vendor-failure" && strings.TrimSpace(string(result)) != "23" {
				t.Fatal("vendor failure exit code was lost")
			}
			if mode == "success" {
				retained, err := os.ReadFile(filepath.Join(config, "coriolis.conf"))
				if err != nil || string(retained) != "custom settings" {
					t.Fatal("custom configuration was not preserved")
				}
			}
			calls, err := os.ReadFile(filepath.Join(root, "vendor-call"))
			if err != nil || !strings.Contains(string(calls), "-c 2608.2 -n 2608.3") {
				t.Fatalf("incorrect manufacturer invocation %q: %v", calls, err)
			}
			if strings.Contains(string(output), "SUPER_SECRET_PASSWORD") {
				t.Fatal("runner leaked credentials")
			}
			if _, err := os.Stat(filepath.Join(state, "update-lock", "id")); err != nil {
				t.Fatal("runner released the lock before independent verification")
			}
		})
	}
}
func TestStatusCollectorHealthAndSecretRedaction(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is not available")
	}
	harness := `
	import json, pathlib, subprocess, sys, types
	from unittest.mock import patch, mock_open
	namespace = {}
	exec(sys.stdin.read().rsplit("\ntry:", 1)[0], namespace)
	scenario = sys.argv[1]
	def command(args):
	    if args[0] == "git":
	        return "a" * 40 if args[-1] == "HEAD" else ""
	    if args[:2] == ["docker", "ps"]:
	        return "one"
	    if args[:2] == ["docker", "inspect"]:
	        return json.dumps([{"Name": "/coriolis-api", "Image": "sha256:image",
	            "Config": {"Image": "registry/coriolis-api:2608.2"}, "Mounts": [],
	            "State": {"Status": "running", "Health": {"Status": "unhealthy" if scenario == "container" else "healthy"}}}])
	    if args[0] == "ip":
	        return "1.1.1.1 via 10.1.0.1 dev eth0 src 10.1.0.2"
	    if scenario == "identity" and args[0] == "openstack":
	        raise subprocess.CalledProcessError(1, args, stderr="SUPER_SECRET_PASSWORD")
	    return ""
	def database(*args, **kwargs):
	    if args[0][0] == "dpkg":
	        return types.SimpleNamespace(stdout="half configured" if scenario == "packages" else "")
	    if scenario == "database":
	        raise subprocess.CalledProcessError(1, args, stderr="SUPER_SECRET_PASSWORD")
	    return types.SimpleNamespace(stdout=json.dumps({"inventory": {"endpoint": "hash"},
	        "active_executions": 2 if scenario == "busy" else 0, "enabled_schedules": 0}))
	namespace["command"] = command
	namespace["sys"].argv = ["status", ""]
	with patch.object(pathlib.Path, "read_text", return_value="2608.2"), \
	     patch.object(pathlib.Path, "read_bytes", return_value=b"secret config"), \
	     patch.object(pathlib.Path, "is_file", return_value=True), \
	     patch.object(pathlib.Path, "exists", return_value=False), \
	     patch.object(pathlib.Path, "iterdir", return_value=[]), \
	     patch("builtins.open", mock_open()), \
	     patch("fcntl.lockf"), \
	     patch("os.statvfs", return_value=types.SimpleNamespace(f_bavail=70, f_bfree=70, f_blocks=100)), \
	     patch("subprocess.run", side_effect=database):
	    namespace["collect"]()
	`
	for _, scenario := range []string{"healthy", "container", "identity", "database", "busy", "packages"} {
		t.Run(scenario, func(t *testing.T) {
			cmd := exec.Command(python, "-c", harness, scenario)
			cmd.Stdin = strings.NewReader(statusCollectorSource())
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("collector failed: %v: %s", err, output)
			}
			if strings.Contains(string(output), "SUPER_SECRET") || strings.Contains(string(output), "secret config") {
				t.Fatal("collector leaked credentials")
			}
			var status ApplianceStatus
			if err := json.Unmarshal(output, &status); err != nil {
				t.Fatal(err)
			}
			wantHealthy := scenario == "healthy" || scenario == "busy"
			if status.Healthy != wantHealthy {
				t.Fatalf("healthy=%t, errors=%v", status.Healthy, status.Errors)
			}
			if status.PackageManagerReady != (scenario != "packages") {
				t.Fatalf("incorrect package readiness: %+v", status)
			}
			if scenario == "busy" && status.ActiveExecutions != 2 {
				t.Fatal("global active executions were not reported")
			}
		})
	}
}

func TestPackageReadinessFailsClosed(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is not available")
	}
	harness := `
import pathlib, subprocess, sys, types
from unittest.mock import patch, mock_open
namespace = {}
exec(sys.stdin.read(), namespace)
scenario = sys.argv[1]
def run(args, **kwargs):
    if scenario == "command":
        raise subprocess.CalledProcessError(1, args, stderr="SUPER_SECRET")
    if args[0] != "dpkg":
        raise AssertionError("package readiness must not require dpkg registration of an extracted agent")
    return types.SimpleNamespace(stdout="stackit-server-agent half configured" if scenario == "agent" else "other package half configured" if scenario == "audit" else "")
def lock(*args):
    if scenario == "locked":
        raise BlockingIOError("locked")
with patch("subprocess.run", side_effect=run), \
     patch("builtins.open", mock_open()), \
     patch("fcntl.lockf", side_effect=lock), \
     patch.object(pathlib.Path, "iterdir", return_value=[pathlib.Path("0001")] if scenario == "pending" else []):
    try:
        namespace["check_package_manager"]()
    except (OSError, RuntimeError, subprocess.SubprocessError):
        sys.exit(1)
`
	for _, scenario := range []string{"ready", "audit", "agent", "pending", "locked", "command"} {
		t.Run(scenario, func(t *testing.T) {
			cmd := exec.Command(python, "-c", harness, scenario)
			cmd.Stdin = strings.NewReader(maintenancePackagesPython)
			output, err := cmd.CombinedOutput()
			if (err == nil) != (scenario == "ready") {
				t.Fatalf("readiness result: %v: %s", err, output)
			}
		})
	}
}

func TestMaintenanceUsesRootDockerContextAndEarlyPackageChecks(t *testing.T) {
	plan, err := makeUpdatePlan(healthyMaintenanceStatus(), "2608.3")
	if err != nil {
		t.Fatal(err)
	}
	journal := UpdateJournal{ID: "test-job", Plan: plan}
	for name, script := range map[string]string{
		"status": maintenanceStatusScript(""), "prepare": prepareUpdateScript(journal),
		"vendor": vendorUpdateScript(journal),
	} {
		if !strings.Contains(script, "export HOME=/root\n") {
			t.Errorf("%s lacks the console Docker/credential-helper HOME", name)
		}
	}
	prepare := prepareUpdateScript(journal)
	check := strings.Index(prepare, "check_package_manager()")
	if check < 0 || check > strings.Index(prepare, `mkdir "$lock"`) {
		t.Fatal("package preflight must precede acquiring the maintenance lock")
	}
}

func TestPackageDriftDuringPreparationHasNoMaintenanceSideEffects(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not available")
	}
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "python3"), []byte("#!/bin/sh\nexit 17\n"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"git", "docker"} {
		if err := os.WriteFile(filepath.Join(bin, tool), []byte("#!/bin/sh\ntouch \"$UNEXPECTED_CALL\"\nexit 0\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	plan, err := makeUpdatePlan(healthyMaintenanceStatus(), "2608.3")
	if err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(root, "state")
	script := strings.ReplaceAll(prepareUpdateScript(UpdateJournal{ID: "test-job", Plan: plan}),
		"/var/lib/coriolis-stackit", state)
	cmd := exec.Command(bash, "-c", script)
	cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "UNEXPECTED_CALL="+filepath.Join(root, "unexpected"))
	output, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 17 {
		t.Fatalf("preflight failure was lost: %v: %s", err, output)
	}
	for _, path := range []string{state, filepath.Join(root, "unexpected")} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("package preflight mutated maintenance state or called git/docker: %s: %v", path, err)
		}
	}
}
