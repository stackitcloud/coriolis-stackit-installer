package installer

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	stackitcfg "github.com/stackitcloud/stackit-sdk-go/core/config"
	iaas "github.com/stackitcloud/stackit-sdk-go/services/iaas/v2api"
	runcommand "github.com/stackitcloud/stackit-sdk-go/services/runcommand/v2api"
	serviceenablement "github.com/stackitcloud/stackit-sdk-go/services/serviceenablement/v1api"
)

type Cloud struct {
	api             *iaas.APIClient
	run             *runcommand.APIClient
	enablement      serviceenablement.DefaultAPI
	http            *http.Client
	project, region string
	poll            time.Duration
	credentials     string
	refreshCtx      context.Context
}

func newCloud(c Config, refreshContexts ...context.Context) (*Cloud, error) {
	refreshCtx := context.Background()
	if len(refreshContexts) > 0 && refreshContexts[0] != nil {
		refreshCtx = refreshContexts[0]
	}
	apiHTTP := &http.Client{Timeout: 2 * time.Minute}
	api, err := iaas.NewAPIClient(stackitcfg.WithServiceAccountKeyPath(c.Credentials), stackitcfg.WithBackgroundTokenRefresh(refreshCtx), stackitcfg.WithHTTPClient(apiHTTP))
	if err != nil {
		return nil, err
	}
	runHTTP := &http.Client{Timeout: 2 * time.Minute}
	run, err := runcommand.NewAPIClient(stackitcfg.WithServiceAccountKeyPath(c.Credentials), stackitcfg.WithBackgroundTokenRefresh(refreshCtx), stackitcfg.WithHTTPClient(runHTTP))
	if err != nil {
		return nil, err
	}
	enablementHTTP := &http.Client{Timeout: 2 * time.Minute}
	enablement, err := serviceenablement.NewAPIClient(stackitcfg.WithRegion(c.Region), stackitcfg.WithServiceAccountKeyPath(c.Credentials), stackitcfg.WithBackgroundTokenRefresh(refreshCtx), stackitcfg.WithHTTPClient(enablementHTTP))
	if err != nil {
		return nil, err
	}
	return &Cloud{api: api, run: run, enablement: enablement.DefaultAPI, http: &http.Client{Timeout: 0}, project: c.ProjectID, region: c.Region, poll: c.PollInterval, credentials: c.Credentials, refreshCtx: refreshCtx}, nil
}

// runShellScript executes a script through the STACKIT Server Agent. This is
// the management channel used by the installer; it is independent from and
// makes no changes to the Coriolis support SSH service.
func (c *Cloud) runShellScript(ctx context.Context, projectID, serverID, script string) (string, error) {
	return c.runShellScriptWithOutput(ctx, projectID, serverID, script, true)
}

func (c *Cloud) runShellScriptWithOutput(ctx context.Context, projectID, serverID, script string, streamOutput bool) (string, error) {
	if len(script) > 10_000 {
		return "", fmt.Errorf("run-command script is %d bytes; maximum is 10000", len(script))
	}
	payload := runcommand.NewCreateCommandPayload("RunShellScript")
	payload.SetParameters(map[string]string{"script": script})
	created, err := c.run.DefaultAPI.CreateCommand(ctx, projectID, serverID, c.region).CreateCommandPayload(*payload).Execute()
	if err != nil {
		return "", fmt.Errorf("create run command for server %s: %w", serverID, err)
	}
	return c.waitRunCommand(ctx, projectID, serverID, fmt.Sprint(created.GetId()), streamOutput)
}

func (c *Cloud) waitRunCommand(ctx context.Context, projectID, serverID, id string, streamOutput bool) (string, error) {
	lastOutput := ""
	consecutiveErrors := 0
	for {
		command, err := c.run.DefaultAPI.GetCommand(ctx, projectID, c.region, serverID, id).Execute()
		if err != nil {
			if !transientAPIError(err) {
				return lastOutput, fmt.Errorf("get run command %s: %w", id, err)
			}
			consecutiveErrors++
			if consecutiveErrors == 1 || consecutiveErrors%6 == 0 {
				fmt.Fprintf(os.Stderr, "warning: transient error polling run command %s (attempt %d): %v\n", id, consecutiveErrors, err)
			}
			select {
			case <-ctx.Done():
				return lastOutput, fmt.Errorf("wait for run command %s after transient API errors: %w", id, ctx.Err())
			case <-time.After(c.poll):
			}
			continue
		}
		consecutiveErrors = 0
		if output := command.GetOutput(); output != lastOutput {
			if streamOutput && output != "" {
				if strings.HasPrefix(output, lastOutput) {
					fmt.Fprint(os.Stderr, output[len(lastOutput):])
				} else {
					fmt.Fprint(os.Stderr, output)
				}
			}
			lastOutput = output
		}
		switch command.GetStatus() {
		case runcommand.COMMANDDETAILSSTATUS_COMPLETED:
			if command.GetExitCode() != 0 {
				return lastOutput, fmt.Errorf("run command %s exited with code %d", id, command.GetExitCode())
			}
			return lastOutput, nil
		case runcommand.COMMANDDETAILSSTATUS_FAILED:
			return lastOutput, fmt.Errorf("run command %s failed with exit code %d", id, command.GetExitCode())
		case runcommand.COMMANDDETAILSSTATUS_UNKNOWN_DEFAULT_OPEN_API:
			if command.GetFinishedAt() != "" {
				if command.GetExitCode() == 0 {
					return lastOutput, nil
				}
				return lastOutput, fmt.Errorf("run command %s ended in an unsupported status with exit code %d", id, command.GetExitCode())
			}
		}
		select {
		case <-ctx.Done():
			return lastOutput, fmt.Errorf("wait for run command %s: %w", id, ctx.Err())
		case <-time.After(c.poll):
		}
	}
}

func transientAPIError(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) && (netErr.Timeout() || netErr.Temporary()) {
		return true
	}
	message := strings.ToLower(err.Error())
	for _, fragment := range []string{
		"connection reset", "connection refused", "connection closed", "unexpected eof", " eof",
		"timeout", "gateway timeout", "status code 502", "status code 503", "status code 504",
		"temporary", "no such host", "server misbehaving", "tls handshake timeout",
		"get new access token", "refresh token",
	} {
		if strings.Contains(message, fragment) {
			return true
		}
	}
	return false
}

func retryableCommandError(err error) bool {
	return err != nil && (transientAPIError(err) || strings.Contains(strings.ToLower(err.Error()), "unsupported status"))
}

func labelsMatch(got map[string]interface{}, sha string) bool {
	return fmt.Sprint(got["coriolis-sha256"]) == sha[:32] && fmt.Sprint(got["coriolis-normalized"]) == "agent-v2"
}

func (c *Cloud) resolveImage(ctx context.Context, projectID, imageID, sha string) (*iaas.Image, error) {
	if imageID == "" {
		return c.findImage(ctx, projectID, sha)
	}
	im, err := c.api.DefaultAPI.GetImage(ctx, projectID, c.region, imageID).Execute()
	if err != nil {
		return nil, fmt.Errorf("get configured image %s: %w", imageID, err)
	}
	status := strings.ToUpper(im.GetStatus())
	if status != "AVAILABLE" && status != "CREATING" {
		return nil, fmt.Errorf("configured image %s has unusable status %s", imageID, status)
	}
	if label := fmt.Sprint(im.Labels["coriolis-sha256"]); label != "" && label != sha[:32] {
		return nil, fmt.Errorf("configured image %s belongs to a different OVA (label coriolis-sha256=%s)", imageID, label)
	}
	if !labelsMatch(im.Labels, sha) {
		fmt.Fprintln(os.Stderr, "warning: configured image has no matching coriolis-sha256 label; using it because image.id was explicitly set")
	}
	return im, nil
}

func (c *Cloud) findImage(ctx context.Context, projectID, sha string) (*iaas.Image, error) {
	// all=true includes images shared with this project in addition to its local images.
	r, err := c.api.DefaultAPI.ListImages(ctx, projectID, c.region).All(true).Execute()
	if err != nil {
		return nil, err
	}
	var availableLocal, availableShared, creating *iaas.Image
	for i := range r.Items {
		if !labelsMatch(r.Items[i].Labels, sha) {
			continue
		}
		switch strings.ToUpper(r.Items[i].GetStatus()) {
		case "AVAILABLE":
			if r.Items[i].GetOwner() == projectID {
				availableLocal = &r.Items[i]
			} else if availableShared == nil {
				availableShared = &r.Items[i]
			}
		case "CREATING":
			// A consumer cannot complete an import in another project. Only wait
			// for imports owned by the current target project.
			if r.Items[i].GetOwner() == projectID {
				creating = &r.Items[i]
			}
		}
	}
	if availableLocal != nil {
		return availableLocal, nil
	}
	if availableShared != nil {
		return availableShared, nil
	}
	if creating != nil {
		return creating, nil
	}
	return nil, nil
}

func (c *Cloud) ensureImage(ctx context.Context, cfg Config, info OVAInfo, zone, networkID, securityGroupID string) (*iaas.Image, error) {
	ownerProject := cfg.Image.OwnerProjectID
	if ownerProject == "" {
		ownerProject = c.project
	}
	lookupProject := c.project
	if cfg.Image.ID != "" && cfg.Image.OwnerProjectID != "" {
		lookupProject = ownerProject
	}
	image, err := c.resolveImage(ctx, lookupProject, cfg.Image.ID, info.SHA256)
	if err != nil {
		return nil, err
	}
	if image == nil && ownerProject != c.project {
		image, err = c.findImage(ctx, ownerProject, info.SHA256)
		if err != nil {
			return nil, fmt.Errorf("search image owner project %s: %w", ownerProject, err)
		}
	}
	if image != nil && strings.EqualFold(image.GetStatus(), "CREATING") {
		if updated := image.GetUpdatedAt(); !updated.IsZero() && time.Since(updated) > 30*time.Minute && image.GetOwner() == ownerProject {
			fmt.Fprintln(os.Stderr, "removing stale incomplete image import", image.GetId())
			if err := c.api.DefaultAPI.DeleteImage(ctx, ownerProject, c.region, image.GetId()).Execute(); err != nil {
				return nil, fmt.Errorf("delete stale image import %s: %w", image.GetId(), err)
			}
			image = nil
		} else {
			fmt.Fprintln(os.Stderr, "waiting for existing image", image.GetId())
			image, err = c.waitImage(ctx, image.GetOwner(), image.GetId())
			if err != nil {
				return nil, err
			}
		}
	}
	if image == nil || !strings.EqualFold(image.GetStatus(), "AVAILABLE") {
		helperNetworkID := networkID
		if ownerProject != c.project {
			ownerNetwork := cfg.Network
			ownerNetwork.ID = ""
			helperNetworkID, err = c.ensureNetworkInProject(ctx, ownerProject, ownerNetwork)
			if err != nil {
				return nil, fmt.Errorf("ensure normalization network in image owner project: %w", err)
			}
		}
		fmt.Fprintln(os.Stderr, "streaming and normalizing OVA with a temporary STACKIT helper in project", ownerProject)
		image, err = c.normalizeAndUploadImage(ctx, ownerProject, cfg, info, zone, helperNetworkID)
		if err != nil {
			return nil, err
		}
	} else {
		fmt.Fprintln(os.Stderr, "reusing image", image.GetId(), "owned by", image.GetOwner())
	}
	if cfg.Agent.Enabled && fmt.Sprint(image.Labels["coriolis-normalized"]) != "agent-v2" {
		return nil, fmt.Errorf("image %s is not normalized for automated management (expected coriolis-normalized=agent-v2)", image.GetId())
	}
	if cfg.Agent.Enabled && image.GetOwner() == ownerProject {
		agent := image.GetAgent()
		if !agent.GetSupported() || !agent.GetProvisionByDefault() {
			desired := iaas.NewImageAgent()
			desired.SetSupported(true)
			desired.SetProvisionByDefault(true)
			payload := iaas.NewUpdateImagePayload()
			payload.SetAgent(*desired)
			updated, err := c.api.DefaultAPI.UpdateImage(ctx, ownerProject, c.region, image.GetId()).UpdateImagePayload(*payload).Execute()
			if err != nil {
				return nil, fmt.Errorf("mark image as STACKIT server-agent compatible: %w", err)
			}
			image = updated
		}
	}
	if err := c.ensureImageShare(ctx, image.GetOwner(), image.GetId(), cfg.Image.Share, ownerProject != c.project); err != nil {
		return nil, err
	}
	if image.GetOwner() != c.project {
		return c.waitImageVisible(ctx, image.GetId())
	}
	return image, nil
}

func (c *Cloud) ensureImageShare(ctx context.Context, ownerProject, imageID string, share ImageShareConfig, includeTarget bool) error {
	projects := append([]string(nil), share.ProjectIDs...)
	if includeTarget && !share.ParentOrganization {
		projects = append(projects, c.project)
	}
	projects = uniqueStrings(projects)
	if !share.ParentOrganization && len(projects) == 0 {
		return nil
	}
	current, err := c.api.DefaultAPI.GetImageShare(ctx, ownerProject, c.region, imageID).Execute()
	if err != nil {
		return fmt.Errorf("get image share: %w", err)
	}
	if share.ParentOrganization {
		if current.GetParentOrganization() {
			return nil
		}
		payload := iaas.NewUpdateImageSharePayload()
		payload.SetParentOrganization(true)
		if _, err := c.api.DefaultAPI.UpdateImageShare(ctx, ownerProject, c.region, imageID).UpdateImageSharePayload(*payload).Execute(); err != nil {
			return fmt.Errorf("share image with parent organization: %w", err)
		}
		fmt.Fprintln(os.Stderr, "shared image", imageID, "with parent organization")
		return nil
	}
	existing := make(map[string]bool, len(current.GetProjects()))
	for _, projectID := range current.GetProjects() {
		existing[projectID] = true
	}
	var missing []string
	for _, projectID := range projects {
		if !existing[projectID] {
			missing = append(missing, projectID)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	payload := iaas.NewUpdateImageSharePayload()
	payload.SetProjects(missing)
	if _, err := c.api.DefaultAPI.UpdateImageShare(ctx, ownerProject, c.region, imageID).UpdateImageSharePayload(*payload).Execute(); err != nil {
		return fmt.Errorf("share image with projects: %w", err)
	}
	fmt.Fprintln(os.Stderr, "shared image", imageID, "with projects", strings.Join(missing, ","))
	return nil
}

func (c *Cloud) waitImageVisible(ctx context.Context, imageID string) (*iaas.Image, error) {
	for {
		im, err := c.api.DefaultAPI.GetImage(ctx, c.project, c.region, imageID).Execute()
		if err == nil && strings.EqualFold(im.GetStatus(), "AVAILABLE") {
			return im, nil
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("wait for shared image %s in target project: %w", imageID, ctx.Err())
		case <-time.After(c.poll):
		}
	}
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}

type progressReader struct {
	reader              io.Reader
	total, transferred  int64
	started, lastReport time.Time
	attempt, attempts   int
	finished            bool
}

func (r *progressReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.transferred += int64(n)
	if time.Since(r.lastReport) >= 5*time.Second || (err == io.EOF && !r.finished) {
		r.report(false)
	}
	return n, err
}

func (r *progressReader) finish(uploadErr error) {
	if r.finished {
		return
	}
	r.report(uploadErr == nil)
	fmt.Fprintln(os.Stderr)
	r.finished = true
}

func (r *progressReader) report(done bool) {
	percent := float64(r.transferred) * 100 / float64(r.total)
	if done {
		percent = 100
	}
	elapsed := time.Since(r.started).Seconds()
	rateMiB := float64(r.transferred) / (1024 * 1024) / elapsed
	fmt.Fprintf(os.Stderr, "\rupload attempt %d/%d: %6.2f%% (%.2f/%.2f GiB, %.1f MiB/s)", r.attempt, r.attempts, percent, float64(r.transferred)/(1024*1024*1024), float64(r.total)/(1024*1024*1024), rateMiB)
	r.lastReport = time.Now()
}

func (c *Cloud) waitImage(ctx context.Context, projectID, id string) (*iaas.Image, error) {
	for {
		im, err := c.api.DefaultAPI.GetImage(ctx, projectID, c.region, id).Execute()
		if err != nil {
			return nil, err
		}
		s := strings.ToUpper(im.GetStatus())
		if s == "AVAILABLE" {
			return im, nil
		}
		if s == "ERROR" || s == "DELETED" {
			return nil, fmt.Errorf("image %s entered status %s", id, s)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(c.poll):
		}
	}
}

func (c *Cloud) chooseZone(ctx context.Context, preferred string) (string, error) {
	r, err := c.api.DefaultAPI.ListAvailabilityZones(ctx, c.region).Execute()
	if err != nil {
		return "", err
	}
	if preferred != "" {
		for _, z := range r.Items {
			if z == preferred {
				return z, nil
			}
		}
		return "", fmt.Errorf("availability zone %q is not available; choices: %s", preferred, strings.Join(r.Items, ", "))
	}
	if len(r.Items) == 0 {
		return "", fmt.Errorf("no availability zones returned")
	}
	sort.Strings(r.Items)
	for _, z := range r.Items {
		if strings.HasSuffix(z, "-m") {
			return z, nil
		}
	}
	return r.Items[0], nil
}

func (c *Cloud) validateMachineType(ctx context.Context, requested string, vcpus, ram int64) (string, error) {
	r, err := c.api.DefaultAPI.ListMachineTypes(ctx, c.project, c.region).Execute()
	if err != nil {
		return "", err
	}
	if requested != "" {
		for _, m := range r.Items {
			if m.Name == requested {
				if m.Vcpus < vcpus || m.Ram < ram {
					return "", fmt.Errorf("machine type %s has %d vCPU/%d MiB, needs at least %d/%d", requested, m.Vcpus, m.Ram, vcpus, ram)
				}
				return requested, nil
			}
		}
		return "", fmt.Errorf("machine type %q not available", requested)
	}
	var candidates []iaas.MachineType
	for _, m := range r.Items {
		if m.Vcpus >= vcpus && m.Ram >= ram {
			candidates = append(candidates, m)
		}
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("no machine type satisfies %d vCPU/%d MiB", vcpus, ram)
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Vcpus == candidates[j].Vcpus {
			return candidates[i].Ram < candidates[j].Ram
		}
		return candidates[i].Vcpus < candidates[j].Vcpus
	})
	return candidates[0].Name, nil
}

func (c *Cloud) ensureNetwork(ctx context.Context, nc NetworkConfig) (string, error) {
	return c.ensureNetworkInProject(ctx, c.project, nc)
}

func (c *Cloud) ensureNetworkInProject(ctx context.Context, projectID string, nc NetworkConfig) (string, error) {
	if nc.ID != "" {
		return nc.ID, nil
	}
	r, err := c.api.DefaultAPI.ListNetworks(ctx, projectID, c.region).Execute()
	if err != nil {
		return "", err
	}
	for _, n := range r.Items {
		if n.Name == nc.Name {
			return n.Id, nil
		}
	}
	p := iaas.NewCreateNetworkPayload(nc.Name)
	p.SetRouted(nc.Routed)
	p.SetLabels(map[string]interface{}{"managed-by": "coriolis-installer"})
	if nc.Prefix != "" {
		ip := iaas.CreateNetworkIPv4WithPrefixAsCreateNetworkIPv4(iaas.NewCreateNetworkIPv4WithPrefix(nc.Prefix))
		p.SetIpv4(ip)
	}
	n, err := c.api.DefaultAPI.CreateNetwork(ctx, projectID, c.region).CreateNetworkPayload(*p).Execute()
	if err != nil {
		return "", err
	}
	return n.Id, nil
}

func (c *Cloud) ensureSecurityGroup(ctx context.Context, cfg SecurityGroupConfig) (string, error) {
	return c.ensureSecurityGroupInProject(ctx, c.project, cfg)
}

func (c *Cloud) ensureSecurityGroupInProject(ctx context.Context, projectID string, cfg SecurityGroupConfig) (string, error) {
	r, err := c.api.DefaultAPI.ListSecurityGroups(ctx, projectID, c.region).Execute()
	if err != nil {
		return "", err
	}
	for _, g := range r.Items {
		if g.Name == cfg.Name {
			if err := c.ensureSecurityGroupRules(ctx, projectID, g.GetId(), g.Rules, cfg.Ingress); err != nil {
				return "", err
			}
			return g.GetId(), nil
		}
	}
	p := iaas.NewCreateSecurityGroupPayload(cfg.Name)
	p.SetDescription("Managed by coriolis-stackit")
	p.SetLabels(map[string]interface{}{"managed-by": "coriolis-installer"})
	g, err := c.api.DefaultAPI.CreateSecurityGroup(ctx, projectID, c.region).CreateSecurityGroupPayload(*p).Execute()
	if err != nil {
		return "", err
	}
	if err := c.ensureSecurityGroupRules(ctx, projectID, g.GetId(), nil, cfg.Ingress); err != nil {
		return "", err
	}
	return g.GetId(), nil
}

func (c *Cloud) ensureSecurityGroupRules(ctx context.Context, projectID, groupID string, existing []iaas.SecurityGroupRule, wanted []IngressRule) error {
	for _, rule := range wanted {
		found := false
		for _, have := range existing {
			if have.GetDirection() == "ingress" && have.GetDescription() == rule.Description {
				found = true
				break
			}
		}
		if found {
			continue
		}
		rp := iaas.NewCreateSecurityGroupRulePayload("ingress")
		rp.SetIpRange(rule.CIDR)
		rp.SetDescription(rule.Description)
		proto := rule.Protocol
		rp.SetProtocol(iaas.StringAsCreateProtocol(&proto))
		if rule.Port > 0 {
			rp.SetPortRange(*iaas.NewPortRange(rule.Port, rule.Port))
		}
		if _, err := c.api.DefaultAPI.CreateSecurityGroupRule(ctx, projectID, c.region, groupID).CreateSecurityGroupRulePayload(*rp).Execute(); err != nil {
			return err
		}
	}
	return nil
}

func (c *Cloud) ensureServer(ctx context.Context, cfg Config, imageID, zone, machine, networkID, securityGroupID, userData string) (*iaas.Server, bool, error) {
	if cfg.Server.ID != "" {
		s, err := c.api.DefaultAPI.GetServer(ctx, c.project, c.region, cfg.Server.ID).Execute()
		if err != nil {
			return nil, false, fmt.Errorf("get configured server %s: %w", cfg.Server.ID, err)
		}
		if cfg.Server.Name != "" && s.GetName() != cfg.Server.Name {
			return nil, false, fmt.Errorf("configured server %s is named %q, expected %q", cfg.Server.ID, s.GetName(), cfg.Server.Name)
		}
		st := strings.ToUpper(s.GetStatus())
		if st == "ERROR" || st == "DELETED" {
			return nil, false, fmt.Errorf("configured server %s is in status %s", s.GetId(), st)
		}
		foundNetwork := false
		for _, nic := range s.GetNics() {
			if nic.GetNetworkId() == networkID {
				foundNetwork = true
				break
			}
		}
		if !foundNetwork {
			return nil, false, fmt.Errorf("configured server %s is not attached to configured network %s; refusing implicit network modification", s.GetId(), networkID)
		}
		if !containsString(s.GetSecurityGroups(), securityGroupID) {
			if err := c.api.DefaultAPI.AddSecurityGroupToServer(ctx, c.project, c.region, s.GetId(), securityGroupID).Execute(); err != nil {
				return nil, false, fmt.Errorf("attach security group %s to adopted server %s: %w", securityGroupID, s.GetId(), err)
			}
			s, err = c.api.DefaultAPI.GetServer(ctx, c.project, c.region, s.GetId()).Execute()
			if err != nil {
				return nil, false, err
			}
		}
		fmt.Fprintln(os.Stderr, "adopting existing server", s.GetId(), "without changing its boot volume")
		return s, false, nil
	}
	r, err := c.api.DefaultAPI.ListServers(ctx, c.project, c.region).Execute()
	if err != nil {
		return nil, false, err
	}
	for i := range r.Items {
		if r.Items[i].Name == cfg.Server.Name {
			s, err := c.api.DefaultAPI.GetServer(ctx, c.project, c.region, r.Items[i].GetId()).Execute()
			if err != nil {
				return nil, false, err
			}
			st := strings.ToUpper(s.GetStatus())
			if st == "CREATING" {
				ready, err := c.waitServer(ctx, s.GetId())
				return ready, false, err
			}
			if st == "ERROR" || st == "DELETED" {
				return nil, false, fmt.Errorf("existing server %s is in status %s", s.GetId(), st)
			}
			if label := fmt.Sprint(s.Labels["coriolis-image-id"]); label != "" && label != imageID {
				return nil, false, fmt.Errorf("existing server %s uses image %s, requested %s; refusing implicit replacement", s.GetId(), label, imageID)
			}
			return s, false, nil
		}
	}
	net := iaas.NewCreateServerNetworking()
	net.SetNetworkId(networkID)
	union := iaas.CreateServerNetworkingAsCreateServerPayloadAllOfNetworking(net)
	p := iaas.NewCreateServerPayload(machine, cfg.Server.Name, union)
	p.SetAvailabilityZone(zone)
	p.SetLabels(map[string]interface{}{"managed-by": "coriolis-installer", "coriolis-image-id": imageID})
	p.SetSecurityGroups([]string{securityGroupID})
	b := iaas.NewBootVolume()
	b.SetSource(*iaas.NewBootVolumeSource(imageID, "image"))
	b.SetSize(cfg.Server.BootVolumeSize)
	b.SetPerformanceClass(cfg.Server.PerformanceClass)
	b.SetDeleteOnTermination(cfg.Server.DeleteBootOnTerminate)
	p.SetBootVolume(*b)
	if cfg.Server.KeypairName != "" {
		p.SetKeypairName(cfg.Server.KeypairName)
	}
	if userData != "" {
		p.SetUserData(base64.StdEncoding.EncodeToString([]byte(userData)))
	}
	if cfg.Agent.Enabled {
		agent := iaas.NewServerAgent()
		agent.SetProvisioned(true)
		p.SetAgent(*agent)
	}
	s, err := c.api.DefaultAPI.CreateServer(ctx, c.project, c.region).CreateServerPayload(*p).Execute()
	if err != nil {
		return nil, false, err
	}
	ready, err := c.waitServer(ctx, s.GetId())
	return ready, true, err
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func (c *Cloud) ensurePublicIP(ctx context.Context, serverID, networkID, requestedID, requestedAddress string) (string, error) {
	s, err := c.api.DefaultAPI.GetServer(ctx, c.project, c.region, serverID).Execute()
	if err != nil {
		return "", err
	}
	nicID := ""
	for _, n := range s.GetNics() {
		if n.GetNetworkId() == networkID {
			nicID = n.GetNicId()
			if n.GetPublicIp() != "" {
				return n.GetPublicIp(), nil
			}
			break
		}
	}
	if nicID == "" {
		nics, err := c.api.DefaultAPI.ListNics(ctx, c.project, c.region, networkID).Execute()
		if err != nil {
			return "", err
		}
		for _, n := range nics.Items {
			if n.GetDevice() == serverID {
				nicID = n.GetId()
				break
			}
		}
	}
	if nicID == "" {
		return "", fmt.Errorf("no network interface found for server %s", serverID)
	}
	ips, err := c.api.DefaultAPI.ListPublicIPs(ctx, c.project, c.region).Execute()
	if err != nil {
		return "", err
	}
	for _, p := range ips.Items {
		if p.GetNetworkInterface() == nicID {
			return p.GetIp(), nil
		}
	}
	if requestedID != "" || requestedAddress != "" {
		for _, p := range ips.Items {
			if (requestedID != "" && p.GetId() != requestedID) || (requestedAddress != "" && p.GetIp() != requestedAddress) {
				continue
			}
			attached := p.GetNetworkInterface()
			if attached != "" && attached != nicID {
				return "", fmt.Errorf("public IP %s is already attached to NIC %s", requestedID, attached)
			}
			if attached == nicID {
				return p.GetIp(), nil
			}
			u := iaas.NewUpdatePublicIPPayload()
			u.SetNetworkInterface(nicID)
			u.SetLabels(map[string]interface{}{"managed-by": "coriolis-installer", "coriolis-server-id": serverID})
			assigned, err := c.api.DefaultAPI.UpdatePublicIP(ctx, c.project, c.region, p.GetId()).UpdatePublicIPPayload(*u).Execute()
			if err != nil {
				return "", err
			}
			return assigned.GetIp(), nil
		}
		return "", fmt.Errorf("requested public IP not found in project (id=%q, address=%q)", requestedID, requestedAddress)
	}
	for _, p := range ips.Items {
		if fmt.Sprint(p.Labels["managed-by"]) == "coriolis-installer" && p.GetNetworkInterface() == "" {
			u := iaas.NewUpdatePublicIPPayload()
			u.SetNetworkInterface(nicID)
			u.SetLabels(map[string]interface{}{"managed-by": "coriolis-installer", "coriolis-server-id": serverID})
			assigned, err := c.api.DefaultAPI.UpdatePublicIP(ctx, c.project, c.region, p.GetId()).UpdatePublicIPPayload(*u).Execute()
			if err != nil {
				return "", err
			}
			return assigned.GetIp(), nil
		}
	}
	p := iaas.NewCreatePublicIPPayload()
	p.SetNetworkInterface(nicID)
	p.SetLabels(map[string]interface{}{"managed-by": "coriolis-installer", "coriolis-server-id": serverID})
	ip, err := c.api.DefaultAPI.CreatePublicIP(ctx, c.project, c.region).CreatePublicIPPayload(*p).Execute()
	if err != nil {
		return "", err
	}
	return ip.GetIp(), nil
}

func (c *Cloud) waitServer(ctx context.Context, id string) (*iaas.Server, error) {
	for {
		s, err := c.api.DefaultAPI.GetServer(ctx, c.project, c.region, id).Execute()
		if err != nil {
			return nil, err
		}
		st := strings.ToUpper(s.GetStatus())
		if st == "ACTIVE" {
			return s, nil
		}
		if st == "ERROR" || st == "DELETED" {
			return nil, fmt.Errorf("server %s entered status %s", id, st)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(c.poll):
		}
	}
}
