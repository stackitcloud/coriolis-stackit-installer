package installer

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	stackitcfg "github.com/stackitcloud/stackit-sdk-go/core/config"
	iaas "github.com/stackitcloud/stackit-sdk-go/services/iaas/v2api"
)

const maintenanceTestServer = "11111111-1111-4111-8111-111111111111"
const maintenanceTestBoot = "22222222-2222-4222-8222-222222222222"
const maintenanceTestData = "33333333-3333-4333-8333-333333333333"
const maintenanceTestSnapshot = "44444444-4444-4444-8444-444444444444"
const maintenanceTestProject = "55555555-5555-4555-8555-555555555555"
const maintenanceTestAPIPath = "/v2/projects/" + maintenanceTestProject + "/regions/eu01"

func snapshotTestClient(t *testing.T, handler http.HandlerFunc) cloudMaintenance {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	api, err := iaas.NewAPIClient(stackitcfg.WithEndpoint(server.URL),
		stackitcfg.WithToken("test-token"), stackitcfg.WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{}
	cfg.Server.ID = maintenanceTestServer
	return cloudMaintenance{
		cloud:  &Cloud{api: api, project: maintenanceTestProject, region: "eu01", poll: time.Millisecond},
		config: cfg,
	}
}

func TestNativeSTACKITSnapshotPayloadAndAttachmentChecks(t *testing.T) {
	for _, mode := range []string{"boot", "data", "running", "detached", "create-error", "missing-id"} {
		t.Run(mode, func(t *testing.T) {
			posts := 0
			calls := []string{}
			client := snapshotTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				calls = append(calls, r.Method)
				switch {
				case r.Method == http.MethodGet && r.URL.Path == maintenanceTestAPIPath+"/servers/"+maintenanceTestServer:
					power := "STOPPED"
					if mode == "running" {
						power = "RUNNING"
					}
					fmt.Fprintf(w, `{"id":%q,"machineType":"test","name":"appliance","powerStatus":%q,"bootVolume":{"id":%q},"volumes":[%q]}`,
						maintenanceTestServer, power, maintenanceTestBoot, maintenanceTestData)
				case r.Method == http.MethodPost && r.URL.Path == maintenanceTestAPIPath+"/snapshots":
					posts++
					var payload iaas.CreateSnapshotPayload
					if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
						t.Error(err)
					}
					wantVolume := maintenanceTestBoot
					if mode == "data" {
						wantVolume = maintenanceTestData
					}
					if payload.VolumeId != wantVolume || payload.GetName() != "coriolis-update-job" ||
						!reflect.DeepEqual(payload.Labels, map[string]interface{}{
							"managed-by": "coriolis-installer", "coriolis-update": "job",
						}) {
						t.Errorf("incorrect native snapshot request: %+v", payload)
					}
					if mode == "create-error" {
						w.WriteHeader(http.StatusBadRequest)
						fmt.Fprint(w, `{"message":"snapshot quota exceeded"}`)
					} else if mode == "missing-id" {
						fmt.Fprintf(w, `{"volumeId":%q,"status":"CREATING"}`, payload.VolumeId)
					} else {
						fmt.Fprintf(w, `{"id":%q,"volumeId":%q,"status":"CREATING"}`, maintenanceTestSnapshot, payload.VolumeId)
					}
				default:
					t.Errorf("unexpected SDK call: %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			})
			volume := maintenanceTestBoot
			if mode == "data" {
				volume = maintenanceTestData
			} else if mode == "detached" {
				volume = maintenanceTestSnapshot
			}
			id, err := client.Snapshot(context.Background(), volume, "job")
			if mode == "boot" || mode == "data" {
				if err != nil || id != maintenanceTestSnapshot || strings.Join(calls, ",") != "GET,POST" {
					t.Fatalf("snapshot=%s calls=%v error=%v", id, calls, err)
				}
			} else if err == nil || id != "" {
				t.Fatalf("unsafe snapshot success: id=%s error=%v", id, err)
			}
			if (mode == "running" || mode == "detached") && posts != 0 {
				t.Fatal("snapshot created without a stopped attached volume")
			}
			if posts > 1 {
				t.Fatal("mutation was retried")
			}
			if len(calls) == 0 {
				t.Fatal("test did not exercise the native SDK endpoint")
			}
		})
	}
}

func TestNativeSnapshotMustBecomeAvailable(t *testing.T) {
	for _, mode := range []string{"available", "error", "unknown", "timeout", "api-error"} {
		t.Run(mode, func(t *testing.T) {
			reads := 0
			client := snapshotTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				reads++
				if r.Method != http.MethodGet || r.URL.Path != maintenanceTestAPIPath+"/snapshots/"+maintenanceTestSnapshot {
					t.Errorf("unexpected SDK call: %s %s", r.Method, r.URL.Path)
				}
				status := "CREATING"
				if mode == "available" && reads > 1 {
					status = "AVAILABLE"
				} else if mode == "error" {
					status = "ERROR"
				} else if mode == "unknown" {
					status = "RESTORING"
				} else if mode == "api-error" {
					w.WriteHeader(http.StatusNotFound)
					fmt.Fprint(w, `{}`)
					return
				}
				fmt.Fprintf(w, `{"id":%q,"volumeId":%q,"status":%q}`, maintenanceTestSnapshot, maintenanceTestBoot, status)
			})
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if mode == "timeout" {
				client.cloud.poll = 2 * time.Second
			}
			err := client.WaitSnapshot(ctx, maintenanceTestSnapshot)
			if mode == "available" {
				if err != nil || reads != 2 {
					t.Fatalf("AVAILABLE not awaited: reads=%d error=%v", reads, err)
				}
			} else if err == nil {
				t.Fatal("unavailable snapshot accepted")
			}
			if reads == 0 {
				t.Fatal("test did not exercise the native snapshot status endpoint")
			}
		})
	}
}

func TestLaunchRejectsIncompleteOrMismatchedSnapshotSets(t *testing.T) {
	for _, mode := range []string{"missing-data", "wrong-volume", "not-available"} {
		t.Run(mode, func(t *testing.T) {
			reads := 0
			client := snapshotTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				reads++
				w.Header().Set("Content-Type", "application/json")
				if r.Method != http.MethodGet || !strings.Contains(r.URL.Path, "/snapshots/") {
					t.Errorf("advanced beyond snapshot verification: %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				volume, status := maintenanceTestBoot, "AVAILABLE"
				if mode == "wrong-volume" {
					volume = maintenanceTestData
				} else if mode == "not-available" {
					status = "CREATING"
				}
				fmt.Fprintf(w, `{"id":%q,"volumeId":%q,"status":%q}`, maintenanceTestSnapshot, volume, status)
			})
			before := healthyMaintenanceStatus()
			before.Volumes = []string{maintenanceTestBoot, maintenanceTestData}
			plan, err := makeUpdatePlan(before, "2608.3")
			if err != nil {
				t.Fatal(err)
			}
			journal := UpdateJournal{Plan: plan, Snapshots: map[string]string{maintenanceTestBoot: maintenanceTestSnapshot}}
			if err := client.Launch(context.Background(), journal); err == nil {
				t.Fatal("updater accepted an incomplete or mismatched native backup")
			}
			if reads != 1 {
				t.Fatalf("snapshot verification not exercised exactly once: %d", reads)
			}
		})
	}
}

func TestExecutionRejectsModifiedMandatoryBackupScope(t *testing.T) {
	for _, mode := range []string{"provider", "method", "required", "vm-running", "retain", "volumes"} {
		t.Run(mode, func(t *testing.T) {
			plan, err := makeUpdatePlan(healthyMaintenanceStatus(), "2608.3")
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "provider":
				plan.Backup.Provider = "other"
			case "method":
				plan.Backup.Method = "export"
			case "required":
				plan.Backup.Required = false
			case "vm-running":
				plan.Backup.VMStopped = false
			case "retain":
				plan.Backup.RetainAfterUpdate = false
			case "volumes":
				plan.Backup.Volumes = nil
			}
			journal := UpdateJournal{Plan: plan}
			fake := &fakeMaintenance{}
			err = executeUpdate(context.Background(), fake, filepath.Join(t.TempDir(), "not-created.json"), &journal)
			if err == nil || !strings.Contains(err.Error(), "snapshot plan") || len(fake.calls) != 0 {
				t.Fatalf("invalid backup scope accepted: calls=%v error=%v", fake.calls, err)
			}
		})
	}
}
