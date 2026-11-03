package installer

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPublishedReleasesReadOnlyVersionHeadings(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<h2>Version 2608.3<a href="#version-2608-3">#</a></h2>
<h2 class="release">Version 2608.2 </h2>
<h2>Version 2608.3<a>#</a></h2>
<p>For example, upgrade to 2608.99</p><h3>Version 2608.90</h3>
<h2>Version 2608.4-rc1</h2><h2>Version 2608.03</h2>`)
	}))
	defer server.Close()
	got, err := readPublishedVersions(context.Background(), server.Client(), server.URL)
	if err != nil || !reflect.DeepEqual(got, []string{"2608.2", "2608.3"}) {
		t.Fatalf("versions=%v, error=%v", got, err)
	}
}

func TestPublishedReleaseReadFailuresRemainErrors(t *testing.T) {
	for _, mode := range []string{"denied", "redirect", "empty", "large"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch mode {
				case "denied":
					w.WriteHeader(http.StatusForbidden)
				case "redirect":
					http.Redirect(w, r, "/elsewhere", http.StatusFound)
				case "empty":
					fmt.Fprint(w, "<h1>Documentation</h1>")
				case "large":
					fmt.Fprint(w, strings.Repeat("x", 2*1024*1024+1))
				}
			}))
			defer server.Close()
			if _, err := readPublishedVersions(context.Background(), server.Client(), server.URL); err == nil {
				t.Fatal("unreadable release list was accepted")
			}
		})
	}
}

func TestPublishedManifestResponsesFailClosed(t *testing.T) {
	for _, output := range []string{"", "{}", `{"manifests":null}`, `{"manifests":{"appliance/coriolis-metal-hub:2608.3":null}}`, `{"manifests":{"wrong:image":true}}`} {
		reader := publishedReleaseReader{repositories: []string{releaseCatalogRepository}, run: func(context.Context, string) (string, error) {
			return output, nil
		}}
		if _, err := reader.hasManifest(context.Background(), releaseCatalogRepository, "2608.3"); err == nil {
			t.Errorf("accepted incomplete response %q", output)
		}
	}
	reader := publishedReleaseReader{repositories: []string{releaseCatalogRepository}, run: func(context.Context, string) (string, error) {
		return "", errors.New("agent unavailable")
	}}
	if _, err := reader.hasManifest(context.Background(), releaseCatalogRepository, "2608.3"); err == nil {
		t.Fatal("agent failure hidden")
	}
	for _, repository := range []string{"../other", "appliance/image;id"} {
		if _, err := manifestInspectionScript([]string{repository}, "2608.3"); err == nil {
			t.Fatal("unsafe repository accepted")
		}
	}
}

func TestManifestInspectionUsesDockerCredentialsWithoutExposingThem(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not available")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is not available")
	}
	for _, mode := range []string{"present", "missing", "denied", "helper", "transport", "invalid", "array", "schema"} {
		t.Run(mode, func(t *testing.T) {
			bin := t.TempDir()
			docker := `#!/bin/sh
test "$HOME" = /root || exit 70
test "$1 $2 $3" = "manifest inspect registry.cloudbase.it/appliance/coriolis-metal-hub:2608.3" || exit 71
case "$MANIFEST_MODE" in
 present) printf '{"schemaVersion":2}' ;;
 missing) echo 'no such manifest: image:2608.3' >&2; exit 1 ;;
 denied) echo 'unauthorized: SUPER_SECRET' >&2; exit 1 ;;
 helper) echo 'error getting credentials: SUPER_SECRET' >&2; exit 1 ;;
 transport) echo 'connection refused SUPER_SECRET' >&2; exit 1 ;;
 invalid) printf 'SUPER_SECRET' ;;
 array) printf '[]' ;;
 schema) printf '{"schemaVersion":1}' ;;
esac
`
			if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(docker), 0700); err != nil {
				t.Fatal(err)
			}
			reader := publishedReleaseReader{repositories: []string{releaseCatalogRepository}, run: func(_ context.Context, script string) (string, error) {
				if len(script) > 10000 {
					t.Fatal("manifest script exceeds Run Command limit")
				}

				cmd := exec.Command(bash, "-c", script)
				cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "HOME="+bin, "MANIFEST_MODE="+mode)
				output, err := cmd.CombinedOutput()
				if strings.Contains(string(output), "SUPER_SECRET") {
					t.Fatal("Docker/helper output leaked credentials")
				}
				return string(output), err
			}}
			present, err := reader.hasManifest(context.Background(), releaseCatalogRepository, "2608.3")
			if (err == nil) != (mode == "present" || mode == "missing") || present != (mode == "present") {
				t.Fatalf("present=%t, error=%v", present, err)
			}
		})
	}
}

func TestPublishedManifestChecksBatchEveryComponentAndRequireCompleteEvidence(t *testing.T) {
	for _, complete := range []bool{true, false} {
		t.Run(fmt.Sprint(complete), func(t *testing.T) {
			calls := 0
			reader := publishedReleaseReader{
				repositories: []string{"appliance/coriolis-metal-hub", "appliance/coriolis-api"},
				run: func(_ context.Context, script string) (string, error) {
					calls++
					for _, image := range []string{"appliance/coriolis-metal-hub:2608.3", "appliance/coriolis-api:2608.3"} {
						if !strings.Contains(script, shellLiteral(image)) {
							t.Fatalf("batch omitted %s", image)
						}
					}
					if !complete {
						return `{"manifests":{"appliance/coriolis-metal-hub:2608.3":true}}`, nil
					}
					return `{"manifests":{"appliance/coriolis-metal-hub:2608.3":true,"appliance/coriolis-api:2608.3":false}}`, nil
				},
			}
			for _, repo := range reader.repositories {
				present, err := reader.hasManifest(context.Background(), repo, "2608.3")
				if complete {
					if err != nil || present != (repo == releaseCatalogRepository) {
						t.Fatalf("present=%t, error=%v", present, err)
					}
				} else if err == nil {
					t.Fatal("partial batch response was accepted")
				}
			}
			if complete && calls != 1 {
				t.Fatalf("expected one Run Command per target, got %d", calls)
			}
		})
	}
}
