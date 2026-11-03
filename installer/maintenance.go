package installer

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

//go:embed maintenance_status.py
var maintenanceStatusPython string

//go:embed maintenance_inventory.py
var maintenanceInventoryPython string

//go:embed maintenance_packages.py
var maintenancePackagesPython string

const maintenanceGuestEnvironment = "export HOME=/root\n"

type ApplianceContainer struct {
	Name    string `json:"name"`
	Image   string `json:"image"`
	ImageID string `json:"image_id"`
	State   string `json:"state"`
	Health  string `json:"health"`
}

type ApplianceStatus struct {
	ProjectID           string               `json:"project_id"`
	Region              string               `json:"region"`
	ServerID            string               `json:"server_id"`
	ServerName          string               `json:"server_name"`
	PowerStatus         string               `json:"power_status"`
	Volumes             []string             `json:"volumes"`
	Release             string               `json:"release"`
	Healthy             bool                 `json:"healthy"`
	Errors              []string             `json:"errors"`
	Containers          []ApplianceContainer `json:"containers"`
	DiskUsedPercent     int                  `json:"disk_used_percent"`
	ActiveExecutions    int                  `json:"active_executions"`
	EnabledSchedules    int                  `json:"enabled_schedules"`
	Inventory           map[string]string    `json:"inventory"`
	TLSFingerprints     map[string]string    `json:"tls_fingerprints"`
	GitCommit           string               `json:"git_commit"`
	GitClean            bool                 `json:"git_clean"`
	UpdaterAvailable    bool                 `json:"updater_available"`
	UpdaterSHA256       string               `json:"updater_sha256"`
	ConfigSHA256        string               `json:"config_sha256"`
	ProtectedSHA256     string               `json:"protected_sha256"`
	UpdateLocked        bool                 `json:"update_locked"`
	PackageManagerReady bool                 `json:"package_manager_ready"`
}

type UpdatePlan struct {
	Before        ApplianceStatus `json:"before"`
	TargetVersion string          `json:"target_version"`
	Backup        UpdateBackup    `json:"backup"`
	Approval      string          `json:"approval"`
}

type UpdateBackup struct {
	Provider          string   `json:"provider"`
	Method            string   `json:"method"`
	Volumes           []string `json:"volumes"`
	Required          bool     `json:"required"`
	VMStopped         bool     `json:"vm_stopped"`
	RetainAfterUpdate bool     `json:"retain_after_update"`
}

type UpdateJournal struct {
	ID        string            `json:"id"`
	Plan      UpdatePlan        `json:"plan"`
	Stage     string            `json:"stage"`
	Snapshots map[string]string `json:"snapshots"`
	Error     string            `json:"error,omitempty"`
	UpdatedAt time.Time         `json:"updated_at"`
}

var releasePattern = regexp.MustCompile(`^[1-9][0-9]{3}\.(0|[1-9][0-9]*)(\.(0|[1-9][0-9]*))?$`)

func validateUpdateVersion(current, target string) error {
	old, err := parseCoriolisVersion(current)
	if err != nil {
		return fmt.Errorf("current version: %w", err)
	}
	next, err := parseCoriolisVersion(target)
	if err != nil {
		return fmt.Errorf("target version: %w", err)
	}
	if old[0] != next[0] {
		return errors.New("cross-stream upgrades require a separately reviewed migration; only the current YYMM release stream is supported")
	}
	if compareCoriolisVersions(next, old) > 0 {
		return nil
	}
	return errors.New("target version must be newer than the running release; downgrades and reinstalls are refused")
}

func makeUpdatePlan(before ApplianceStatus, target string) (UpdatePlan, error) {
	if err := validateUpdateVersion(before.Release, target); err != nil {
		return UpdatePlan{}, err
	}
	if !before.Healthy || len(before.Errors) != 0 || before.PowerStatus != "RUNNING" {
		return UpdatePlan{}, errors.New("appliance must be running and healthy before planning an update")
	}
	if !before.PackageManagerReady {
		return UpdatePlan{}, errors.New("package manager readiness must be verified before an update; repair dpkg/agent state outside the agent service")
	}
	if before.ActiveExecutions != 0 || before.EnabledSchedules != 0 {
		return UpdatePlan{}, errors.New("all tenant executions must be terminal and all transfer schedules disabled before maintenance")
	}
	if before.DiskUsedPercent >= 80 || before.DiskUsedPercent <= 0 {
		return UpdatePlan{}, errors.New("filesystem usage must be known and below 80 percent")
	}
	if !before.GitClean || !before.UpdaterAvailable || before.UpdateLocked {
		return UpdatePlan{}, errors.New("vendor working tree must be clean, updater present and no previous update lock may exist")
	}
	if before.ServerID == "" || len(before.Volumes) == 0 || len(before.Inventory) == 0 ||
		before.GitCommit == "" || before.UpdaterSHA256 == "" || before.ConfigSHA256 == "" || before.ProtectedSHA256 == "" {
		return UpdatePlan{}, errors.New("incomplete server, volume, inventory or configuration evidence")
	}
	before.Volumes = slices.Clone(before.Volumes)
	slices.Sort(before.Volumes)
	plan := UpdatePlan{Before: before, TargetVersion: target, Backup: UpdateBackup{
		Provider: "stackit", Method: "volume_snapshots", Volumes: slices.Clone(before.Volumes),
		Required: true, VMStopped: true, RetainAfterUpdate: true,
	}}
	data, err := json.Marshal(plan)
	if err != nil {
		return plan, err
	}
	sum := sha256.Sum256(data)
	plan.Approval = hex.EncodeToString(sum[:])
	return plan, nil
}

func maintenanceStatusScript(fqdn string) string {
	return "#!/bin/bash\nset -euo pipefail\n" + maintenanceGuestEnvironment + "source /etc/kolla/admin-openrc.sh\npython3 - " + shellLiteral(fqdn) + " <<'CORIOLIS_STATUS_PY'\n" + statusCollectorSource() + "\nCORIOLIS_STATUS_PY\n"
}

func statusCollectorSource() string {
	return "CORIOLIS_DATABASE_SCRIPT = " + strconv.Quote(maintenanceInventoryPython) + "\n" + maintenancePackagesPython + "\n" + maintenanceStatusPython
}

func packageManagerPreflightScript() string {
	return "python3 - <<'PACKAGE_PREFLIGHT'\n" + maintenancePackagesPython + "\ncheck_package_manager()\nPACKAGE_PREFLIGHT\n"
}

func shellLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func runMaintenance(action string, args []string) error {
	fs := flag.NewFlagSet(action, flag.ContinueOnError)
	configPath := fs.String("config", "", "YAML configuration (no OVA needed)")
	project := fs.String("project-id", "", "STACKIT project ID")
	credentials := fs.String("credentials", "", "service account key JSON")
	region := fs.String("region", "", "STACKIT region")
	serverID := fs.String("server-id", "", "exact existing appliance server ID")
	target := fs.String("target-version", "", "explicit newer Coriolis release in the current YYMM stream")
	latest := fs.Bool("latest-compatible", false, "resolve the newest same-stream release with all installed component images")
	apply := fs.Bool("apply", false, "perform approved maintenance; default only produces a plan")
	approval := fs.String("approve-plan", "", "SHA-256 approval from an unchanged update plan")
	confirmed := fs.String("confirm-server-id", "", "confirm downtime and exclusive maintenance access to this server")
	statePath := fs.String("state-file", "", "new local recovery journal for update, existing journal for update-status")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected positional maintenance arguments")
	}
	if action != "update" && (*apply || *approval != "" || *confirmed != "" || *target != "" || *latest) {
		return errors.New("update options are only valid for the update command")
	}
	if (action == "status" || action == "versions") && *statePath != "" {
		return errors.New("--state-file is only valid for update and update-status")
	}
	if action == "update" {
		if (*latest && *target != "") || (!*latest && !releasePattern.MatchString(*target)) {
			return errors.New("choose either an explicit numeric --target-version or --latest-compatible")
		}
		if *apply && (*serverID == "" || *confirmed != *serverID || *approval == "" || *statePath == "") {
			return errors.New("--apply requires --server-id, matching --confirm-server-id, --approve-plan and a new --state-file; confirmation asserts exclusive maintenance access")
		}
	}
	c, err := loadConfig(*configPath)
	if err != nil {
		return err
	}
	if *project != "" {
		c.ProjectID = *project
	}
	if *credentials != "" {
		c.Credentials = *credentials
	}
	if *region != "" {
		c.Region = *region
	}
	if *serverID != "" {
		c.Server.ID = *serverID
	}
	if c.ProjectID == "" || c.Credentials == "" || c.Region == "" || !c.Agent.Enabled ||
		c.Timeout <= 0 || c.PollInterval <= 0 {
		return errors.New("maintenance requires project, credentials, region, enabled agent and positive timeouts")
	}
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()
	cloud, err := newCloud(c, ctx)
	if err != nil {
		return err
	}
	if action == "update-status" {
		return cloud.reportUpdate(ctx, c, *statePath)
	}
	id, err := cloud.findMaintenanceServer(ctx, c)
	if err != nil {
		return err
	}
	status, err := cloud.applianceStatus(ctx, c, id)
	if err != nil {
		return err
	}
	if action == "status" {
		if err := printJSON(status); err != nil {
			return err
		}
		if !status.Healthy {
			return errors.New("appliance health checks failed; see errors in status JSON")
		}
		return nil
	}
	if action == "versions" || *latest {
		catalog, err := cloud.availableVersions(ctx, status)
		if err != nil {
			return err
		}
		if action == "versions" {
			return printJSON(catalog)
		}
		if catalog.LatestCompatible == "" {
			if err := printJSON(catalog); err != nil {
				return err
			}
			if *apply || !catalog.UpToDate {
				return errors.New("no complete newer same-stream release is available to apply")
			}
			return nil
		}
		*target = catalog.LatestCompatible
	}
	plan, err := makeUpdatePlan(status, *target)
	if err != nil {
		if printErr := printJSON(status); printErr != nil {
			return errors.Join(err, printErr)
		}
		return err
	}
	if !*apply {
		return printJSON(plan)
	}
	if *approval != plan.Approval {
		return errors.New("approval does not match the current appliance and target; generate and review a fresh plan")
	}
	journal := UpdateJournal{ID: uuid.NewString(), Plan: plan, Stage: "approved", Snapshots: map[string]string{}}
	if err := createUpdateJournal(*statePath, journal); err != nil {
		return err
	}
	writeWarning("exclusive maintenance starts for server %s; keep other clients away until verification completes; recovery journal: %s", id, *statePath)
	client := cloudMaintenance{cloud: cloud, config: c}
	if err := executeUpdate(ctx, client, *statePath, &journal); err != nil {
		return err
	}
	writeInfo("update launched as a persistent systemd job; use update-status --state-file %s to observe and verify it", *statePath)
	return printJSON(journal)
}

func createUpdateJournal(path string, journal UpdateJournal) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("create new recovery journal: %w", err)
	}
	return writeJournalFile(file, &journal)
}

func writeJournalFile(file *os.File, journal *UpdateJournal) error {
	journal.UpdatedAt = time.Now().UTC()
	err := json.NewEncoder(file).Encode(journal)
	if err == nil {
		err = file.Sync()
	}
	return errors.Join(err, file.Close())
}

func saveUpdateJournal(path string, journal *UpdateJournal) error {
	dir := filepath.Dir(path)
	file, err := os.CreateTemp(dir, ".coriolis-update-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err := writeJournalFile(file, journal); err != nil {
		return err
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return err
	}
	directory, err := os.Open(dir)
	if err != nil {
		return err
	}
	return errors.Join(directory.Sync(), directory.Close())
}

func readUpdateJournal(path string) (UpdateJournal, error) {
	var journal UpdateJournal
	data, err := os.ReadFile(path)
	if err != nil {
		return journal, fmt.Errorf("read recovery journal: %w", err)
	}
	err = json.Unmarshal(data, &journal)
	return journal, err
}

type maintenanceOperations interface {
	Prepare(context.Context, UpdateJournal) error
	Stop(context.Context, string) error
	Snapshot(context.Context, string, string) (string, error)
	WaitSnapshot(context.Context, string) error
	Start(context.Context, string) error
	Launch(context.Context, UpdateJournal) error
}

func executeUpdate(ctx context.Context, client maintenanceOperations, path string, journal *UpdateJournal) (runErr error) {
	if journal.Plan.Backup.Provider != "stackit" || journal.Plan.Backup.Method != "volume_snapshots" ||
		!journal.Plan.Backup.Required || !journal.Plan.Backup.VMStopped || !journal.Plan.Backup.RetainAfterUpdate ||
		!slices.Equal(journal.Plan.Backup.Volumes, journal.Plan.Before.Volumes) {
		return errors.New("update requires the approved native STACKIT snapshot plan for every attached volume")
	}
	defer func() {
		if runErr != nil {
			journal.Error = runErr.Error()
			runErr = errors.Join(runErr, saveUpdateJournal(path, journal))
		}
	}()
	stage := func(name string, action func() error) error {
		journal.Stage = name
		if err := saveUpdateJournal(path, journal); err != nil {
			return err
		}
		if err := action(); err != nil {
			return fmt.Errorf("%s: %w; no automatic retry or rollback; inspect recovery journal %s", name, err, path)
		}
		return nil
	}
	if err := stage("preparing", func() error { return client.Prepare(ctx, *journal) }); err != nil {
		return err
	}
	if err := stage("stopping", func() error { return client.Stop(ctx, journal.Plan.Before.ServerID) }); err != nil {
		return err
	}
	if journal.Snapshots == nil {
		journal.Snapshots = map[string]string{}
	}
	for _, volume := range journal.Plan.Before.Volumes {
		if err := stage("snapshotting", func() error {
			id, err := client.Snapshot(ctx, volume, journal.ID)
			if err != nil {
				return err
			}
			journal.Snapshots[volume] = id
			if err := saveUpdateJournal(path, journal); err != nil {
				return err
			}
			return client.WaitSnapshot(ctx, id)
		}); err != nil {
			return err
		}
	}
	if err := stage("starting", func() error { return client.Start(ctx, journal.Plan.Before.ServerID) }); err != nil {
		return err
	}
	if err := stage("launching", func() error { return client.Launch(ctx, *journal) }); err != nil {
		return err
	}
	journal.Stage = "running"
	return saveUpdateJournal(path, journal)
}
