package installer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

const releaseNotesURL = "https://cloudbase.github.io/coriolis-docs/reference/coriolis-release-notes.html"

var releaseHeading = regexp.MustCompile(`(?is)<h2\b[^>]*>\s*Version\s+([0-9]+(?:\.[0-9]+)+)\s*(?:<|$)`)

type ReleaseCandidate struct {
	Version       string   `json:"version"`
	Eligible      bool     `json:"eligible"`
	Reason        string   `json:"reason"`
	ImagesChecked bool     `json:"images_checked"`
	MissingImages []string `json:"missing_images,omitempty"`
}

type VersionCatalog struct {
	ProjectID        string             `json:"project_id"`
	Region           string             `json:"region"`
	ServerID         string             `json:"server_id"`
	CurrentVersion   string             `json:"current_version"`
	Source           string             `json:"source"`
	CheckedAt        time.Time          `json:"checked_at"`
	Versions         []ReleaseCandidate `json:"versions"`
	LatestCompatible string             `json:"latest_compatible,omitempty"`
	UpToDate         bool               `json:"up_to_date_within_stream"`
	IgnoredTags      int                `json:"ignored_non_release_tags"`
	Note             string             `json:"note"`
}

type releaseReader interface {
	tags(context.Context, string) ([]string, error)
	hasManifest(context.Context, string, string) (bool, error)
}

func parseCoriolisVersion(version string) ([3]uint64, error) {
	var parts [3]uint64
	if !releasePattern.MatchString(version) {
		return parts, errors.New("release must be an explicit numeric YYMM version, not latest")
	}
	for index, value := range strings.Split(version, ".") {
		part, err := strconv.ParseUint(value, 10, 32)
		if err != nil {
			return parts, fmt.Errorf("invalid release version: %w", err)
		}
		parts[index] = part
	}
	return parts, nil
}

func compareCoriolisVersions(a, b [3]uint64) int {
	for index := range a {
		if a[index] < b[index] {
			return -1
		}
		if a[index] > b[index] {
			return 1
		}
	}
	return 0
}

func applianceReleaseRepositories(status ApplianceStatus) ([]string, error) {
	repositories := []string{releaseCatalogRepository}
	components := 0
	for _, container := range status.Containers {
		if !strings.HasPrefix(container.Name, "coriolis-") {
			continue
		}
		components++
		if !strings.HasPrefix(container.Image, "registry.cloudbase.it/") {
			return nil, fmt.Errorf("component %s uses an unsupported registry", container.Name)
		}
		repository := strings.TrimPrefix(container.Image, "registry.cloudbase.it/")
		repository, _, _ = strings.Cut(repository, "@")
		if tag := strings.LastIndex(repository, ":"); tag >= 0 {
			repository = repository[:tag]
		}
		if !validRegistryRepository(repository) {
			return nil, fmt.Errorf("component %s has an invalid image repository", container.Name)
		}
		repositories = append(repositories, repository)
	}
	if components == 0 {
		return nil, errors.New("no installed Coriolis component images could be identified")
	}
	return uniqueStrings(repositories), nil
}

func discoverVersions(ctx context.Context, status ApplianceStatus, registry releaseReader) (VersionCatalog, error) {
	current, err := parseCoriolisVersion(status.Release)
	if err != nil {
		return VersionCatalog{}, fmt.Errorf("running appliance version: %w", err)
	}
	repositories, err := applianceReleaseRepositories(status)
	if err != nil {
		return VersionCatalog{}, err
	}
	tags, err := registry.tags(ctx, releaseCatalogRepository)
	if err != nil {
		return VersionCatalog{}, fmt.Errorf("cannot determine available Coriolis releases: %w", err)
	}
	catalog := VersionCatalog{
		ProjectID: status.ProjectID, Region: status.Region, ServerID: status.ServerID,
		CurrentVersion: status.Release, Source: releaseNotesURL,
		CheckedAt: time.Now().UTC(), Versions: []ReleaseCandidate{},
		Note: "Eligibility checks the local same-stream policy and installed component manifests, not vendor certification or fixes contained only in a newer OVA.",
	}
	versions := map[string][3]uint64{}
	for _, tag := range tags {
		parsed, err := parseCoriolisVersion(tag)
		if err != nil {
			catalog.IgnoredTags++
			continue
		}
		versions[tag] = parsed
	}
	if len(versions) == 0 {
		return VersionCatalog{}, errors.New("release catalog contains no numeric Coriolis releases; update availability is unknown")
	}
	ordered := make([]string, 0, len(versions))
	for version := range versions {
		ordered = append(ordered, version)
	}
	slices.SortFunc(ordered, func(a, b string) int {
		if compared := compareCoriolisVersions(versions[b], versions[a]); compared != 0 {
			return compared
		}
		return strings.Compare(a, b)
	})
	for _, version := range ordered {
		parsed := versions[version]
		candidate := ReleaseCandidate{Version: version}
		switch {
		case compareCoriolisVersions(parsed, current) == 0:
			candidate.Reason = "current"
		case compareCoriolisVersions(parsed, current) < 0:
			candidate.Reason = "older"
		case parsed[0] != current[0]:
			candidate.Reason = "requires_stream_review"
		default:
			candidate.ImagesChecked = true
			for _, repository := range repositories {
				present, err := registry.hasManifest(ctx, repository, version)
				if err != nil {
					return VersionCatalog{}, fmt.Errorf("cannot check %s:%s: %w", repository, version, err)
				}
				if !present {
					candidate.MissingImages = append(candidate.MissingImages, repository+":"+version)
				}
			}
			if len(candidate.MissingImages) == 0 {
				candidate.Eligible, candidate.Reason = true, "compatible"
				if catalog.LatestCompatible == "" {
					catalog.LatestCompatible = version
				}
			} else {
				candidate.Reason = "component_images_missing"
			}
		}
		catalog.Versions = append(catalog.Versions, candidate)
	}
	currentListed := false
	for _, candidate := range catalog.Versions {
		currentListed = currentListed || candidate.Reason == "current"
	}
	catalog.UpToDate = catalog.LatestCompatible == "" && currentListed
	for _, candidate := range catalog.Versions {
		if candidate.Reason == "component_images_missing" {
			catalog.UpToDate = false
		}
	}
	return catalog, nil
}

type publishedReleaseReader struct {
	versions     []string
	repositories []string
	run          func(context.Context, string) (string, error)
	manifests    map[string]bool
}

func (r publishedReleaseReader) tags(context.Context, string) ([]string, error) {
	return r.versions, nil
}

func (r *publishedReleaseReader) hasManifest(ctx context.Context, repository, version string) (bool, error) {
	if !slices.Contains(r.repositories, repository) {
		return false, errors.New("manifest repository is outside the installed component set")
	}
	key := repository + ":" + version
	if present, ok := r.manifests[key]; ok {
		return present, nil
	}
	script, err := manifestInspectionScript(r.repositories, version)
	if err != nil {
		return false, err
	}
	output, err := r.run(ctx, script)
	if err != nil {
		return false, err
	}
	var result struct {
		Manifests map[string]*bool `json:"manifests"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil || len(result.Manifests) != len(r.repositories) {
		return false, errors.New("invalid or incomplete appliance manifest response")
	}
	for _, repo := range r.repositories {
		if result.Manifests[repo+":"+version] == nil {
			return false, errors.New("appliance manifest response lacks installed component evidence")
		}
	}
	if r.manifests == nil {
		r.manifests = map[string]bool{}
	}
	for image, present := range result.Manifests {
		r.manifests[image] = *present
	}
	return r.manifests[key], nil
}

func manifestInspectionScript(repositories []string, version string) (string, error) {
	if len(repositories) == 0 || !releasePattern.MatchString(version) {
		return "", errors.New("missing image repositories or invalid release")
	}
	var images []string
	for _, repository := range repositories {
		if !validRegistryRepository(repository) {
			return "", errors.New("invalid image repository")
		}
		images = append(images, shellLiteral(repository+":"+version))
	}
	return "#!/bin/bash\nset -euo pipefail\n" + maintenanceGuestEnvironment +
		"python3 - " + strings.Join(images, " ") + ` <<'MANIFEST'
import json, subprocess, sys
def inspect(image):
    result = subprocess.run(["docker", "manifest", "inspect", "registry.cloudbase.it/" + image],
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, timeout=90)
    if result.returncode == 0:
        manifest = json.loads(result.stdout)
        if not isinstance(manifest, dict) or manifest.get("schemaVersion") != 2:
            raise ValueError("unsupported manifest")
        present = True
    elif any(value in result.stderr.lower() for value in ("unauthorized", "denied", "credentials")):
        raise SystemExit("Appliance Docker registry access denied; check the console credential-helper setup")
    elif any(value in result.stderr.lower() for value in ("no such manifest", "manifest unknown")):
        present = False
    else:
        raise SystemExit("Appliance Docker manifest inspection failed; details withheld to protect credentials")
    return present
try:
    print(json.dumps({"manifests": {image: inspect(image) for image in sys.argv[1:]}}))
except (OSError, ValueError, subprocess.SubprocessError):
    raise SystemExit("Appliance Docker manifest inspection failed; details withheld to protect credentials")
MANIFEST
`, nil
}

func readPublishedVersions(ctx context.Context, client *http.Client, endpoint string) ([]string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	copy := *client
	copy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := copy.Do(request)
	if err != nil {
		return nil, fmt.Errorf("read official release notes: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("official release notes returned HTTP %d", response.StatusCode)
	}
	const limit = 2 * 1024 * 1024
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, errors.New("official release notes exceed 2 MiB limit")
	}
	var versions []string
	for _, match := range releaseHeading.FindAllSubmatch(data, -1) {
		version := string(match[1])
		if _, err := parseCoriolisVersion(version); err == nil {
			versions = append(versions, version)
		}
	}
	if len(versions) == 0 {
		return nil, errors.New("official release notes contain no recognized release headings; update availability is unknown")
	}
	return uniqueStrings(versions), nil
}

func (c *Cloud) availableVersions(ctx context.Context, status ApplianceStatus) (VersionCatalog, error) {
	repositories, err := applianceReleaseRepositories(status)
	if err != nil {
		return VersionCatalog{}, err
	}
	versions, err := readPublishedVersions(ctx, &http.Client{Timeout: 30 * time.Second}, releaseNotesURL)
	if err != nil {
		return VersionCatalog{}, err
	}
	return discoverVersions(ctx, status, &publishedReleaseReader{
		versions: versions, repositories: repositories,
		run: func(ctx context.Context, script string) (string, error) {
			return c.runShellScriptWithOutput(ctx, c.project, status.ServerID, script, false)
		},
	})
}
