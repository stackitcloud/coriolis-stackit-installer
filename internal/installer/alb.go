package installer

import (
	"context"
	"fmt"
	"net/http"
	"time"

	stackitcfg "github.com/stackitcloud/stackit-sdk-go/core/config"
	alb "github.com/stackitcloud/stackit-sdk-go/services/alb/v2api"
	certapi "github.com/stackitcloud/stackit-sdk-go/services/certificates/v2api"
)

func (c *Cloud) checkALBAccess(ctx context.Context, cfg ALBConfig) error {
	httpClient := &http.Client{Timeout: 2 * time.Minute}
	albClient, err := alb.NewAPIClient(stackitcfg.WithServiceAccountKeyPath(c.credentials), stackitcfg.WithBackgroundTokenRefresh(c.refreshCtx), stackitcfg.WithHTTPClient(httpClient))
	if err != nil {
		return err
	}
	if _, err := albClient.DefaultAPI.ListLoadBalancers(ctx, c.project, c.region).PageSize("1").Execute(); err != nil {
		return fmt.Errorf("list ALBs: %w", err)
	}
	plans, err := albClient.DefaultAPI.ListPlans(ctx, c.region).Execute()
	if err != nil {
		return fmt.Errorf("list ALB plans: %w", err)
	}
	found := false
	for _, plan := range plans.ValidPlans {
		if plan.GetPlanId() == cfg.PlanID {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("ALB plan %q is not available", cfg.PlanID)
	}
	certificateClient, err := certapi.NewAPIClient(stackitcfg.WithServiceAccountKeyPath(c.credentials), stackitcfg.WithBackgroundTokenRefresh(c.refreshCtx), stackitcfg.WithHTTPClient(httpClient))
	if err != nil {
		return err
	}
	if _, err := certificateClient.DefaultAPI.ListCertificates(ctx, c.project, c.region).PageSize("1").Execute(); err != nil {
		return fmt.Errorf("list certificates: %w", err)
	}
	return nil
}

func (c *Cloud) serverPrivateIP(ctx context.Context, serverID, networkID string) (string, error) {
	server, err := c.api.DefaultAPI.GetServer(ctx, c.project, c.region, serverID).Execute()
	if err != nil {
		return "", err
	}
	for _, nic := range server.GetNics() {
		if nic.GetNetworkId() == networkID && nic.GetIpv4() != "" {
			return nic.GetIpv4(), nil
		}
	}
	return "", fmt.Errorf("server %s has no private IPv4 address in network %s", serverID, networkID)
}

func (c *Cloud) ensureALB(ctx context.Context, cfg ALBConfig, networkID, targetIP, fqdn, certificateID string) (*alb.LoadBalancer, error) {
	client, err := alb.NewAPIClient(stackitcfg.WithServiceAccountKeyPath(c.credentials), stackitcfg.WithBackgroundTokenRefresh(c.refreshCtx), stackitcfg.WithHTTPClient(&http.Client{Timeout: 2 * time.Minute}))
	if err != nil {
		return nil, err
	}
	desired := buildALBPayload(cfg, networkID, targetIP, fqdn, certificateID)
	list, err := client.DefaultAPI.ListLoadBalancers(ctx, c.project, c.region).PageSize("100").Execute()
	if err != nil {
		return nil, err
	}
	for i := range list.LoadBalancers {
		existing := &list.LoadBalancers[i]
		if existing.GetName() != cfg.Name {
			continue
		}
		if albUsesCertificate(existing, certificateID) {
			return waitALB(ctx, client, c.project, c.region, cfg.Name, existing, c.poll)
		}
		update := alb.NewUpdateLoadBalancerPayload()
		update.SetName(cfg.Name)
		update.SetVersion(existing.GetVersion())
		update.SetListeners(desired.GetListeners())
		update.SetTargetPools(desired.GetTargetPools())
		updated, err := client.DefaultAPI.UpdateLoadBalancer(ctx, c.project, c.region, cfg.Name).UpdateLoadBalancerPayload(*update).Execute()
		if err != nil {
			return nil, err
		}
		return waitALB(ctx, client, c.project, c.region, cfg.Name, updated, c.poll)
	}
	created, err := client.DefaultAPI.CreateLoadBalancer(ctx, c.project, c.region).CreateLoadBalancerPayload(*desired).Execute()
	if err != nil {
		return nil, err
	}
	return waitALB(ctx, client, c.project, c.region, cfg.Name, created, c.poll)
}

func buildALBPayload(cfg ALBConfig, networkID, targetIP, fqdn, certificateID string) *alb.CreateLoadBalancerPayload {
	certificate := alb.NewCertificateConfig()
	certificate.SetCertificateIds([]string{certificateID})
	https := alb.NewProtocolOptionsHTTPS()
	https.SetCertificateConfig(*certificate)
	rule := alb.NewRule()
	rule.SetTargetPool("coriolis-backend")
	rule.SetWebSocket(true)
	path := alb.NewPath()
	path.SetPrefix("/")
	rule.SetPath(*path)
	host := alb.NewHostConfig()
	host.SetHost(fqdn)
	host.SetRules([]alb.Rule{*rule})
	httpOptions := alb.NewProtocolOptionsHTTP()
	httpOptions.SetHosts([]alb.HostConfig{*host})
	listener := alb.NewListener()
	listener.SetName("https")
	listener.SetPort(443)
	listener.SetProtocol(alb.LISTENERPROTOCOL_PROTOCOL_HTTPS)
	listener.SetHttp(*httpOptions)
	listener.SetHttps(*https)
	target := alb.NewTarget()
	target.SetDisplayName("coriolis-appliance")
	target.SetIp(targetIP)
	pool := alb.NewTargetPool()
	pool.SetName("coriolis-backend")
	pool.SetTargetPort(cfg.BackendPort)
	pool.SetTargets([]alb.Target{*target})
	if cfg.HealthCheckPath != "" {
		httpCheck := alb.NewHttpHealthChecks()
		httpCheck.SetPath(cfg.HealthCheckPath)
		httpCheck.SetOkStatuses([]string{"200", "301", "302"})
		check := alb.NewActiveHealthCheck()
		check.SetHttpHealthChecks(*httpCheck)
		check.SetInterval("10s")
		check.SetTimeout("5s")
		check.SetHealthyThreshold(2)
		check.SetUnhealthyThreshold(3)
		pool.SetActiveHealthCheck(*check)
	}
	network := alb.NewNetwork()
	network.SetNetworkId(networkID)
	network.SetRole(alb.NETWORKROLE_ROLE_LISTENERS_AND_TARGETS)
	options := alb.NewLoadBalancerOptions()
	options.SetEphemeralAddress(true)
	options.SetPrivateNetworkOnly(false)
	payload := alb.NewCreateLoadBalancerPayload()
	payload.SetName(cfg.Name)
	payload.SetPlanId(cfg.PlanID)
	payload.SetLabels(map[string]string{"managed-by": "coriolis-installer"})
	payload.SetNetworks([]alb.Network{*network})
	payload.SetOptions(*options)
	payload.SetListeners([]alb.Listener{*listener})
	payload.SetTargetPools([]alb.TargetPool{*pool})
	return payload
}

func albUsesCertificate(lb *alb.LoadBalancer, certificateID string) bool {
	for _, listener := range lb.GetListeners() {
		if listener.Https == nil || listener.Https.CertificateConfig == nil {
			continue
		}
		for _, id := range listener.Https.CertificateConfig.GetCertificateIds() {
			if id == certificateID {
				return true
			}
		}
	}
	return false
}

func waitALB(ctx context.Context, client *alb.APIClient, project, region, name string, current *alb.LoadBalancer, poll time.Duration) (*alb.LoadBalancer, error) {
	var ready *alb.LoadBalancer
	err := progressActionWithUpdates(ctx, "Waiting for Application Load Balancer "+name+" to become READY", func(update func(string)) error {
		for {
			status := current.GetStatus()
			if status == alb.LOADBALANCERSTATUS_STATUS_READY {
				ready = current
				return nil
			}
			if status == alb.LOADBALANCERSTATUS_STATUS_ERROR {
				return fmt.Errorf("ALB %s entered error state: %v", name, current.GetErrors())
			}
			update("current status " + string(status))
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(poll):
			}
			var err error
			current, err = client.DefaultAPI.GetLoadBalancer(ctx, project, region, name).Execute()
			if err != nil {
				return err
			}
		}
	})
	return ready, err
}
