package installer

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestRegistryAnonymousAuthenticationAndPagination(t *testing.T) {
	var server *httptest.Server
	pages, authentications := 0, 0
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/auth" {
			authentications++
			if r.URL.Query().Get("scope") != "repository:appliance/coriolis-metal-hub:pull" {
				t.Error("incorrect pull scope")
			}
			fmt.Fprint(w, `{"token":"anonymous-test-token"}`)
			return
		}
		if r.Header.Get("Authorization") != "Bearer anonymous-test-token" {
			w.Header().Set("WWW-Authenticate", `Bearer realm="`+server.URL+`/auth",service="Docker registry"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		pages++
		if r.URL.Query().Get("last") == "" {
			w.Header().Set("Link", `</v2/appliance/coriolis-metal-hub/tags/list?n=100&last=2608.2>; rel="next"`)
			fmt.Fprint(w, `{"name":"appliance/coriolis-metal-hub","tags":["2608.2","latest"]}`)
		} else {
			fmt.Fprint(w, `{"name":"appliance/coriolis-metal-hub","tags":["2608.3","2608.10"]}`)
		}
	}))
	defer server.Close()
	client, err := newReleaseRegistry(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	tags, err := client.tags(context.Background(), "appliance/coriolis-metal-hub")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tags, []string{"2608.2", "latest", "2608.3", "2608.10"}) || pages != 2 || authentications != 1 {
		t.Fatalf("tags=%v pages=%d authentications=%d", tags, pages, authentications)
	}
}

func TestRegistryFailuresAreNotAnEmptyUpdateCatalog(t *testing.T) {
	for _, mode := range []string{"denied", "invalid-json", "foreign-page", "loop", "missing-token", "foreign-auth", "redirect"} {
		t.Run(mode, func(t *testing.T) {
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if mode == "redirect" {
					http.Redirect(w, r, "https://example.invalid/", http.StatusFound)
					return
				}
				if r.URL.Path == "/auth" {
					fmt.Fprint(w, `{}`)
					return
				}
				if mode == "denied" || mode == "missing-token" || mode == "foreign-auth" {
					if mode != "denied" {
						host := server.URL
						if mode == "foreign-auth" {
							host = "https://example.invalid"
						}
						w.Header().Set("WWW-Authenticate", `Bearer realm="`+host+`/auth",service="Docker registry"`)
					}
					w.WriteHeader(http.StatusUnauthorized)
					fmt.Fprint(w, `{"secret":"must-not-be-printed"}`)
					return
				}
				if mode == "invalid-json" {
					fmt.Fprint(w, `<html>login required</html>`)
					return
				}
				if mode == "foreign-page" {
					w.Header().Set("Link", `<https://example.invalid/v2/appliance/coriolis-metal-hub/tags/list>; rel="next"`)
				} else {
					w.Header().Set("Link", `<`+r.URL.String()+`>; rel="next"`)
				}
				fmt.Fprint(w, `{"name":"appliance/coriolis-metal-hub","tags":["2608.3"]}`)
			}))
			defer server.Close()
			client, err := newReleaseRegistry(server.URL, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.tags(context.Background(), "appliance/coriolis-metal-hub")
			if err == nil || strings.Contains(err.Error(), "must-not-be-printed") {
				t.Fatalf("unsafe registry result: %v", err)
			}
		})
	}
}

type fakeReleaseRegistry struct {
	values    []string
	missing   string
	denied    bool
	manifests []string
}

func (f *fakeReleaseRegistry) tags(context.Context, string) ([]string, error) {
	return f.values, nil
}
func (f *fakeReleaseRegistry) hasManifest(_ context.Context, repository, version string) (bool, error) {
	ref := repository + ":" + version
	f.manifests = append(f.manifests, ref)
	if f.denied {
		return false, fmt.Errorf("registry read denied")
	}
	return ref != f.missing, nil
}

func versionDiscoveryStatus() ApplianceStatus {
	s := healthyMaintenanceStatus()
	s.Containers = []ApplianceContainer{
		{Name: "coriolis-api", Image: "registry.cloudbase.it/appliance/coriolis-api:2608.2"},
		{Name: "coriolis-worker", Image: "registry.cloudbase.it/appliance/coriolis-worker:2608.2"},
		{Name: "mariadb", Image: "registry.cloudbase.it/appliance/mariadb-server:2023.1-ubuntu-jammy"},
	}
	return s
}

func TestDiscoverVersionsUsesNumericOrderAndCompleteImages(t *testing.T) {
	registry := &fakeReleaseRegistry{
		values:  []string{"latest", "2608.3", "2608.10", "2608.2", "2608.1", "2610.0", "2608.4-rc1", "1.3.2", "2608.3"},
		missing: "appliance/coriolis-worker:2608.10",
	}
	catalog, err := discoverVersions(context.Background(), versionDiscoveryStatus(), registry)
	if err != nil {
		t.Fatal(err)
	}
	if catalog.LatestCompatible != "2608.3" || catalog.UpToDate || len(catalog.Versions) != 5 {
		t.Fatalf("unexpected catalog: %+v", catalog)
	}
	if catalog.Versions[0].Version != "2610.0" || catalog.Versions[1].Version != "2608.10" ||
		catalog.Versions[1].Reason != "component_images_missing" {
		t.Fatalf("incorrect ordering or completeness: %+v", catalog.Versions)
	}
	for _, manifest := range registry.manifests {
		if strings.Contains(manifest, "mariadb") || strings.HasSuffix(manifest, ":2610.0") {
			t.Fatalf("unrelated or cross-stream image inspected: %s", manifest)
		}
	}
}

func TestDiscoverVersionsUpToDateIsDifferentFromReadFailure(t *testing.T) {
	registry := &fakeReleaseRegistry{values: []string{"2608.2", "latest"}}
	catalog, err := discoverVersions(context.Background(), versionDiscoveryStatus(), registry)
	if err != nil || !catalog.UpToDate || catalog.LatestCompatible != "" {
		t.Fatalf("unexpected up-to-date result: %+v, %v", catalog, err)
	}
	registry.values, registry.denied = []string{"2608.3"}, true
	if _, err := discoverVersions(context.Background(), versionDiscoveryStatus(), registry); err == nil {
		t.Fatal("registry failure reported as no updates")
	}
	registry.values, registry.denied = []string{"latest", "development"}, false
	if _, err := discoverVersions(context.Background(), versionDiscoveryStatus(), registry); err == nil {
		t.Fatal("unknown release catalog reported as up to date")
	}
}

func TestLatestCompatibleFlagValidation(t *testing.T) {
	for _, args := range [][]string{
		{"update", "--latest-compatible", "--target-version", "2608.3"},
		{"status", "--latest-compatible"},
		{"versions", "--apply"},
		{"versions", "--state-file", "not-used.json"},
		{"update", "--latest-compatible", "--apply"},
	} {
		if err := Run(args, "test"); err == nil {
			t.Fatalf("invalid options accepted: %v", args)
		}
	}
}

func TestUpdatePlanShowsMandatoryNativeSnapshots(t *testing.T) {
	status := healthyMaintenanceStatus()
	status.Volumes = []string{"data", "boot"}
	plan, err := makeUpdatePlan(status, "2608.3")
	if err != nil {
		t.Fatal(err)
	}

	encoded, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Backup struct {
			Provider  string   `json:"provider"`
			Method    string   `json:"method"`
			Volumes   []string `json:"volumes"`
			Required  bool     `json:"required"`
			VMStopped bool     `json:"vm_stopped"`
		} `json:"backup"`
	}
	if err := json.Unmarshal(encoded, &result); err != nil {
		t.Fatal(err)
	}
	if result.Backup.Provider != "stackit" || result.Backup.Method != "volume_snapshots" ||
		!result.Backup.Required || !result.Backup.VMStopped ||
		!reflect.DeepEqual(result.Backup.Volumes, []string{"boot", "data"}) {
		t.Fatalf("backup scope is not explicit: %s", encoded)
	}
}

func TestAnonymousTokenWithoutPullGrantIsAnExplicitError(t *testing.T) {
	var server *httptest.Server
	authentications, attempts := 0, 0
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth" {
			authentications++
			fmt.Fprint(w, `{"token":"anonymous-token-without-pull-grant"}`)
			return
		}
		attempts++
		w.Header().Set("WWW-Authenticate", fmt.Sprintf("Bearer realm=%q,service=%q", server.URL+"/auth", "Docker registry"))
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"message":"private-response-body"}`)
	}))
	defer server.Close()
	registry, err := newReleaseRegistry(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	tags, err := registry.tags(context.Background(), releaseCatalogRepository)
	if err == nil || !strings.Contains(err.Error(), "HTTP 401") || len(tags) != 0 ||
		strings.Contains(err.Error(), "private-response-body") ||
		strings.Contains(err.Error(), "anonymous-token-without-pull-grant") ||
		authentications != 1 || attempts != 2 {
		t.Fatalf("anonymous denial not reported safely: tags=%v auth=%d attempts=%d error=%v", tags, authentications, attempts, err)
	}
}

func TestManifestAvailabilityDoesNotHideRegistryErrors(t *testing.T) {
	for _, mode := range []string{"present", "missing", "denied", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v2/appliance/coriolis-api/manifests/2608.3" ||
					!strings.Contains(r.Header.Get("Accept"), "application/vnd.oci.image.manifest.v1+json") {
					t.Errorf("incorrect manifest request: %s", r.URL)
				}
				switch mode {
				case "present":
					fmt.Fprint(w, `{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json"}`)
				case "missing":
					w.WriteHeader(http.StatusNotFound)
				case "denied":
					w.WriteHeader(http.StatusForbidden)
				case "invalid":
					fmt.Fprint(w, `{}`)
				}
			}))
			defer server.Close()
			registry, err := newReleaseRegistry(server.URL, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			present, err := registry.hasManifest(context.Background(), "appliance/coriolis-api", "2608.3")
			if mode == "present" && (!present || err != nil) {
				t.Fatalf("available manifest not recognized: %v", err)
			}
			if mode == "missing" && (present || err != nil) {
				t.Fatalf("missing manifest not recognized: %v", err)
			}
			if (mode == "denied" || mode == "invalid") && (present || err == nil) {
				t.Fatalf("failed manifest check silently ignored: present=%v error=%v", present, err)
			}
		})
	}
}

func TestIncompleteOrUnrecognizedCatalogIsNotUpToDate(t *testing.T) {
	for _, values := range [][]string{{"2608.3"}, {"2608.1"}} {
		catalog, err := discoverVersions(context.Background(), versionDiscoveryStatus(),
			&fakeReleaseRegistry{values: values, missing: "appliance/coriolis-worker:2608.3"})
		if err != nil || catalog.UpToDate || catalog.LatestCompatible != "" {
			t.Fatalf("uncertain catalog reported as up to date: %+v, %v", catalog, err)
		}
	}
}
