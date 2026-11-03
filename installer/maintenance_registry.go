package installer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const releaseRegistryURL = "https://registry.cloudbase.it"
const releaseCatalogRepository = "appliance/coriolis-metal-hub"
const registryManifestTypes = "application/vnd.oci.image.index.v1+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.docker.distribution.manifest.v2+json"

var registryAuthParameter = regexp.MustCompile(`([A-Za-z][A-Za-z0-9_-]*)="([^"]*)"`)
var registryNextPage = regexp.MustCompile(`<([^>]+)>\s*;[^,]*\brel\s*=\s*"?next"?`)
var registryRepositoryPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._/-]*$`)

type releaseRegistry struct {
	base   *url.URL
	http   *http.Client
	tokens map[string]string
}

func newReleaseRegistry(endpoint string, client *http.Client) (*releaseRegistry, error) {
	base, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}
	if (base.Scheme != "https" && base.Scheme != "http") || base.Host == "" ||
		base.User != nil || base.RawQuery != "" || base.Fragment != "" || (base.Path != "" && base.Path != "/") {
		return nil, errors.New("invalid registry origin")
	}
	copy := *client
	copy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if copy.Timeout == 0 {
		copy.Timeout = 30 * time.Second
	}
	return &releaseRegistry{base: base, http: &copy, tokens: map[string]string{}}, nil
}

func (r *releaseRegistry) sameOrigin(target *url.URL) bool {
	return target.Scheme == r.base.Scheme && target.Host == r.base.Host && target.User == nil && target.Fragment == ""
}

func (r *releaseRegistry) request(ctx context.Context, target *url.URL, token, accept string) ([]byte, http.Header, int, error) {
	if !r.sameOrigin(target) {
		return nil, nil, 0, errors.New("registry attempted to leave its trusted origin")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, nil, 0, err
	}
	req.Header.Set("Accept", accept)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := r.http.Do(req)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("registry read request failed: %w", err)
	}
	defer response.Body.Close()
	const maximumResponse = 2 * 1024 * 1024
	data, err := io.ReadAll(io.LimitReader(response.Body, maximumResponse+1))
	if err != nil {
		return nil, nil, 0, err
	}
	if len(data) > maximumResponse {
		return nil, nil, 0, errors.New("registry response exceeds 2 MiB limit")
	}
	return data, response.Header, response.StatusCode, nil
}

func (r *releaseRegistry) anonymousToken(ctx context.Context, repository, challenge string) (string, error) {
	if !strings.HasPrefix(strings.ToLower(challenge), "bearer ") {
		return "", errors.New("registry refused anonymous pull access (HTTP 401); no usable Docker Bearer challenge")
	}
	parameters := map[string]string{}
	for _, match := range registryAuthParameter.FindAllStringSubmatch(challenge, -1) {
		parameters[strings.ToLower(match[1])] = match[2]
	}
	realm, err := url.Parse(parameters["realm"])
	if err != nil || realm.Host == "" || !r.sameOrigin(realm) {
		return "", errors.New("registry authentication realm is missing or outside its trusted origin")
	}
	query := realm.Query()
	query.Set("service", parameters["service"])
	query.Set("scope", "repository:"+repository+":pull")
	realm.RawQuery = query.Encode()
	data, _, status, err := r.request(ctx, realm, "", "application/json")
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("anonymous registry token request returned HTTP %d", status)
	}
	var result struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", errors.New("invalid anonymous registry token response")
	}
	token := result.Token
	if token == "" {
		token = result.AccessToken
	}
	if token == "" || strings.ContainsAny(token, "\r\n") {
		return "", errors.New("anonymous registry token response contains no usable token")
	}
	return token, nil
}

func (r *releaseRegistry) read(ctx context.Context, target *url.URL, repository, accept string) ([]byte, http.Header, int, error) {
	data, headers, status, err := r.request(ctx, target, r.tokens[repository], accept)
	if err != nil {
		return nil, nil, 0, err
	}
	if status == http.StatusUnauthorized {
		token, err := r.anonymousToken(ctx, repository, headers.Get("WWW-Authenticate"))
		if err != nil {
			return nil, nil, 0, err
		}
		r.tokens[repository] = token
		data, headers, status, err = r.request(ctx, target, token, accept)
		if err != nil {
			return nil, nil, 0, err
		}
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return nil, nil, 0, fmt.Errorf("registry refused anonymous pull access for %s (HTTP %d); verify the vendor's public update endpoint/access policy", repository, status)
	}
	if status != http.StatusOK && status != http.StatusNotFound {
		return nil, nil, 0, fmt.Errorf("registry read returned HTTP %d", status)
	}
	return data, headers, status, nil
}

func validRegistryRepository(repository string) bool {
	return registryRepositoryPattern.MatchString(repository) && !strings.Contains(repository, "..") &&
		!strings.Contains(repository, "//") && !strings.HasSuffix(repository, "/")
}

func (r *releaseRegistry) tags(ctx context.Context, repository string) ([]string, error) {
	if !validRegistryRepository(repository) {
		return nil, errors.New("invalid registry repository")
	}
	path := "/v2/" + repository + "/tags/list"
	target := r.base.ResolveReference(&url.URL{Path: path, RawQuery: "n=100"})
	seen := map[string]bool{}
	tags := []string{}
	for page := 0; target != nil; page++ {
		if page >= 256 || seen[target.String()] {
			return nil, errors.New("registry pagination loop or page limit exceeded")
		}
		if !r.sameOrigin(target) || target.Path != path {
			return nil, errors.New("registry next page leaves the requested repository or trusted origin")
		}
		seen[target.String()] = true
		data, headers, status, err := r.read(ctx, target, repository, "application/json")
		if err != nil {
			return nil, err
		}
		if status == http.StatusNotFound {
			return nil, fmt.Errorf("release catalog repository %s does not exist", repository)
		}
		var result struct {
			Name string   `json:"name"`
			Tags []string `json:"tags"`
		}
		if err := json.Unmarshal(data, &result); err != nil || result.Name != repository {
			return nil, errors.New("invalid registry tag catalog response")
		}
		tags = append(tags, result.Tags...)
		link := headers.Get("Link")
		if link == "" {
			target = nil
		} else if next := registryNextPage.FindStringSubmatch(link); next != nil {
			reference, err := url.Parse(next[1])
			if err != nil {
				return nil, errors.New("invalid registry next-page URL")
			}
			target = target.ResolveReference(reference)
		} else {
			return nil, errors.New("unrecognized registry pagination header; catalog may be incomplete")
		}
	}
	return tags, nil
}

func (r *releaseRegistry) hasManifest(ctx context.Context, repository, version string) (bool, error) {
	if !validRegistryRepository(repository) || !releasePattern.MatchString(version) {
		return false, errors.New("invalid image repository or release")
	}
	target := r.base.ResolveReference(&url.URL{Path: "/v2/" + repository + "/manifests/" + version})
	data, _, status, err := r.read(ctx, target, repository, registryManifestTypes)
	if err != nil || status == http.StatusNotFound {
		return false, err
	}
	var manifest struct {
		SchemaVersion int `json:"schemaVersion"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil || manifest.SchemaVersion != 2 {
		return false, errors.New("invalid or unsupported registry image manifest")
	}
	return true, nil
}
