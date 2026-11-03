package installer

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	iaas "github.com/stackitcloud/stackit-sdk-go/services/iaas/v2api"
)

func (c *Cloud) findMaintenanceServer(ctx context.Context, cfg Config) (string, error) {
	if cfg.Server.ID != "" {
		server, err := c.api.DefaultAPI.GetServer(ctx, c.project, c.region, cfg.Server.ID).Execute()
		if err != nil {
			return "", err
		}
		return server.GetId(), nil
	}
	servers, err := c.api.DefaultAPI.ListServers(ctx, c.project, c.region).Execute()
	if err != nil {
		return "", err
	}
	id := ""
	for _, server := range servers.Items {
		if server.GetName() == cfg.Server.Name {
			if id != "" {
				return "", errors.New("ambiguous appliance name; use --server-id")
			}
			id = server.GetId()
		}
	}
	if id == "" {
		return "", errors.New("existing appliance not found; maintenance never creates a server")
	}
	return id, nil
}

func maintenanceFQDN(cfg Config) string {
	if cfg.DNS.Enabled && cfg.Exposure.Mode == "direct" {
		return dnsRecordFQDN(cfg.DNS.RecordName, cfg.DNS.ZoneName)
	}
	return ""
}

func (c *Cloud) applianceStatus(ctx context.Context, cfg Config, id string) (ApplianceStatus, error) {
	var status ApplianceStatus
	server, err := c.api.DefaultAPI.GetServer(ctx, c.project, c.region, id).Execute()
	if err != nil {
		return status, err
	}
	status.ProjectID, status.Region = c.project, c.region
	status.ServerID, status.ServerName, status.PowerStatus = id, server.GetName(), server.GetPowerStatus()
	status.Volumes = server.GetVolumes()
	boot := server.GetBootVolume()
	if boot.GetId() != "" {
		status.Volumes = append(status.Volumes, boot.GetId())
	}
	status.Volumes = uniqueStrings(status.Volumes)
	if status.PowerStatus != "RUNNING" {
		status.Errors = []string{"appliance is not running; guest inspection unavailable"}
		return status, nil
	}
	if err := c.waitServerAgent(ctx, c.project, id); err != nil {
		return status, err
	}
	output, err := c.runShellScriptWithOutput(ctx, c.project, id, maintenanceStatusScript(maintenanceFQDN(cfg)), false)
	if err != nil {
		return status, fmt.Errorf("appliance status command: %w", err)
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &status); err != nil {
		return status, fmt.Errorf("invalid or truncated appliance status response: %w", err)
	}
	return status, nil
}

type cloudMaintenance struct {
	cloud  *Cloud
	config Config
}

func (m cloudMaintenance) Prepare(ctx context.Context, journal UpdateJournal) error {
	script := prepareUpdateScript(journal)
	if _, err := m.cloud.runShellScriptWithOutput(ctx, m.cloud.project, journal.Plan.Before.ServerID, script, false); err != nil {
		return err
	}
	_, err := m.cloud.runShellScriptWithOutput(ctx, m.cloud.project, journal.Plan.Before.ServerID, quiesceUpdateScript(journal), false)
	return err
}

func (m cloudMaintenance) waitPower(ctx context.Context, serverID, power string) error {
	for {
		server, err := m.cloud.api.DefaultAPI.GetServer(ctx, m.cloud.project, m.cloud.region, serverID).Execute()
		if err != nil {
			return err
		}
		if server.GetPowerStatus() == power && (power != "RUNNING" || server.GetStatus() == "ACTIVE") {
			return nil
		}
		if server.GetStatus() == "ERROR" || server.GetPowerStatus() == "ERROR" || server.GetPowerStatus() == "CRASHED" {
			return fmt.Errorf("server entered %s/%s", server.GetStatus(), server.GetPowerStatus())
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(m.cloud.poll):
		}
	}
}

func (m cloudMaintenance) Stop(ctx context.Context, id string) error {
	if err := m.cloud.api.DefaultAPI.StopServer(ctx, m.cloud.project, m.cloud.region, id).Execute(); err != nil {
		return err
	}
	return m.waitPower(ctx, id, "STOPPED")
}

func (m cloudMaintenance) Snapshot(ctx context.Context, volume, job string) (string, error) {
	server, err := m.cloud.api.DefaultAPI.GetServer(ctx, m.cloud.project, m.cloud.region, m.config.Server.ID).Execute()
	if err != nil {
		return "", err
	}
	if server.GetPowerStatus() != "STOPPED" {
		return "", errors.New("server is no longer stopped; refusing an inconsistent snapshot")
	}
	attached := server.GetVolumes()
	boot := server.GetBootVolume()
	attached = append(attached, boot.GetId())
	found := false
	for _, id := range attached {
		if id == volume && volume != "" {
			found = true
			break
		}
	}
	if !found {
		return "", errors.New("snapshot volume is not attached to the approved appliance")
	}
	payload := iaas.NewCreateSnapshotPayload(volume)
	payload.SetName("coriolis-update-" + job)
	payload.SetLabels(map[string]interface{}{"managed-by": "coriolis-installer", "coriolis-update": job})
	snapshot, err := m.cloud.api.DefaultAPI.CreateSnapshot(ctx, m.cloud.project, m.cloud.region).CreateSnapshotPayload(*payload).Execute()
	if err != nil {
		return "", err
	}
	if snapshot.GetId() == "" {
		return "", errors.New("snapshot API returned no ID; inspect cloud resources before any retry")
	}
	return snapshot.GetId(), nil
}

func (m cloudMaintenance) WaitSnapshot(ctx context.Context, id string) error {
	for {
		snapshot, err := m.cloud.api.DefaultAPI.GetSnapshot(ctx, m.cloud.project, m.cloud.region, id).Execute()
		if err != nil {
			return err
		}
		switch snapshot.GetStatus() {
		case "AVAILABLE":
			return nil
		case "CREATING":
		default:
			return fmt.Errorf("snapshot %s entered unexpected state %s", id, snapshot.GetStatus())
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(m.cloud.poll):
		}
	}
}

func (m cloudMaintenance) Start(ctx context.Context, id string) error {
	if err := m.cloud.api.DefaultAPI.StartServer(ctx, m.cloud.project, m.cloud.region, id).Execute(); err != nil {
		return err
	}
	if err := m.waitPower(ctx, id, "RUNNING"); err != nil {
		return err
	}
	if err := m.cloud.waitServerAgent(ctx, m.cloud.project, id); err != nil {
		return err
	}
	script := `#!/bin/bash
set -euo pipefail
id=$(cat /var/lib/coriolis-stackit/update-lock/id)
work=/var/lib/coriolis-stackit/updates/$id
mapfile -t running < "$work/running-containers"
test "${#running[@]}" -gt 0
docker start -- "${running[@]}"
`
	if _, err := m.cloud.runShellScriptWithOutput(ctx, m.cloud.project, id, script, false); err != nil {
		return err
	}
	for {
		status, err := m.cloud.applianceStatus(ctx, m.config, id)
		if err != nil {
			return err
		}
		if status.Healthy {
			return nil
		}
		writeInfo("waiting for appliance recovery after cold backup: %s", strings.Join(status.Errors, "; "))
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(m.cloud.poll):
		}
	}
}

func (m cloudMaintenance) Launch(ctx context.Context, journal UpdateJournal) error {
	for _, volume := range journal.Plan.Before.Volumes {
		id := journal.Snapshots[volume]
		if id == "" {
			return fmt.Errorf("missing snapshot for volume %s", volume)
		}
		snapshot, err := m.cloud.api.DefaultAPI.GetSnapshot(ctx, m.cloud.project, m.cloud.region, id).Execute()
		if err != nil {
			return err
		}
		if snapshot.GetStatus() != "AVAILABLE" || snapshot.GetVolumeId() != volume {
			return fmt.Errorf("snapshot %s is not an available backup of volume %s", id, volume)
		}
	}
	status, err := m.cloud.applianceStatus(ctx, m.config, journal.Plan.Before.ServerID)
	if err != nil {
		return err
	}
	before := journal.Plan.Before
	if !status.Healthy || status.ActiveExecutions != 0 || status.EnabledSchedules != 0 ||
		status.Release != before.Release || status.GitCommit != before.GitCommit ||
		status.ConfigSHA256 != before.ConfigSHA256 || !status.GitClean ||
		status.UpdaterSHA256 != before.UpdaterSHA256 ||
		!reflect.DeepEqual(status.Inventory, before.Inventory) ||
		!reflect.DeepEqual(status.Volumes, before.Volumes) {
		return errors.New("appliance changed during cold backup or did not recover cleanly; updater was not started")
	}
	_, err = m.cloud.runShellScriptWithOutput(ctx, m.cloud.project, before.ServerID, launchUpdateScript(journal), false)
	return err
}

type updateJobStatus struct {
	State    string `json:"state"`
	ExitCode int    `json:"exit_code"`
}

func (c *Cloud) reportUpdate(ctx context.Context, cfg Config, path string) error {
	journal, err := readUpdateJournal(path)
	if err != nil {
		return err
	}
	if journal.Plan.Before.ProjectID != c.project || journal.Plan.Before.Region != c.region ||
		(cfg.Server.ID != "" && cfg.Server.ID != journal.Plan.Before.ServerID) {
		return errors.New("recovery journal does not belong to the configured project, region or server")
	}
	if !validUpdateID(journal.ID) {
		return errors.New("invalid update ID in recovery journal")
	}
	if journal.Stage == "verified" {
		return printJSON(journal)
	}
	if journal.Stage != "running" && journal.Stage != "launching" && journal.Stage != "verifying" && journal.Stage != "verified" && journal.Stage != "failed" {
		if err := printJSON(journal); err != nil {
			return err
		}
		return errors.New("update interrupted before a confirmed launch; inspect journal and cloud resources, do not rerun --apply")
	}
	output, err := c.runShellScriptWithOutput(ctx, c.project, journal.Plan.Before.ServerID, updateJobStatusScript(journal.ID), false)
	if err != nil {
		return err
	}
	var job updateJobStatus
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &job); err != nil {
		return fmt.Errorf("invalid update job status: %w", err)
	}
	if job.State != "finished" {
		if err := printJSON(struct {
			Journal UpdateJournal   `json:"journal"`
			Job     updateJobStatus `json:"job"`
		}{journal, job}); err != nil {
			return err
		}
		if job.State != "running" {
			return errors.New("remote update result is unknown; inspect the systemd unit and private log, never restart blindly")
		}
		return nil
	}
	if job.ExitCode != 0 {
		journal.Stage = "failed"
		journal.Error = fmt.Sprintf("vendor update job exited with code %d; private log retained on appliance", job.ExitCode)
		if err := saveUpdateJournal(path, &journal); err != nil {
			return err
		}
		if err := printJSON(journal); err != nil {
			return err
		}
		return errors.New(journal.Error)
	}
	status, err := c.applianceStatus(ctx, cfg, journal.Plan.Before.ServerID)
	if err != nil {
		return err
	}
	if err := verifyUpdatedAppliance(journal.Plan, status); err != nil {
		journal.Stage, journal.Error = "verifying", err.Error()
		return errors.Join(err, saveUpdateJournal(path, &journal))
	}
	// Release the guest lock only after independent post-update verification.
	script := "#!/bin/bash\nset -euo pipefail\nlock=/var/lib/coriolis-stackit/update-lock\nif [ -d \"$lock\" ]; then\n  test \"$(cat \"$lock/id\")\" = " + shellLiteral(journal.ID) + "\n  rm \"$lock/id\"\n  rmdir \"$lock\"\nfi\n"
	if journal.Stage != "verified" {
		if _, err := c.runShellScriptWithOutput(ctx, c.project, status.ServerID, script, false); err != nil {
			return fmt.Errorf("update verified but releasing maintenance lock failed: %w", err)
		}
	}
	journal.Stage, journal.Error = "verified", ""
	if err := saveUpdateJournal(path, &journal); err != nil {
		return err
	}
	return printJSON(journal)
}

func verifyUpdatedAppliance(plan UpdatePlan, status ApplianceStatus) error {
	if !status.Healthy || status.Release != plan.TargetVersion || len(status.Containers) == 0 {
		return errors.New("updated appliance has not reached healthy target release")
	}
	if !reflect.DeepEqual(status.Inventory, plan.Before.Inventory) {
		return errors.New("tenant data inventory changed; keep maintenance access exclusive and inspect before resuming use")
	}
	if !reflect.DeepEqual(status.TLSFingerprints, plan.Before.TLSFingerprints) {
		return errors.New("served TLS certificates changed during update")
	}
	if status.ProtectedSHA256 != plan.Before.ProtectedSHA256 || status.ProtectedSHA256 == "" {
		return errors.New("application configuration or identity passwords changed during update")
	}
	for _, previous := range plan.Before.Containers {
		found := false
		for _, current := range status.Containers {
			if current.Name == previous.Name {
				found = true
				if strings.HasPrefix(current.Name, "coriolis-") && !strings.HasSuffix(current.Image, ":"+plan.TargetVersion) {
					return fmt.Errorf("component %s did not reach target image tag", current.Name)
				}
				break
			}
		}
		if !found {
			return fmt.Errorf("previous container %s is missing after update", previous.Name)
		}
	}
	return nil
}

func encodedScript(script string) string {
	return base64.StdEncoding.EncodeToString([]byte(script))
}
