package installer

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	stackitcfg "github.com/stackitcloud/stackit-sdk-go/core/config"
	dns "github.com/stackitcloud/stackit-sdk-go/services/dns/v1api"
)

func (c *Cloud) ensureDNS(ctx context.Context, cfg DNSConfig, ip string) (string, error) {
	if cfg.RecordName == "" {
		return "", fmt.Errorf("dns.record_name is required")
	}
	client, err := c.newDNSClient()
	if err != nil {
		return "", err
	}
	zone, err := c.ensureDNSZoneWithClient(ctx, client, cfg)
	if err != nil {
		return "", err
	}
	fqdn := dnsRecordFQDN(cfg.RecordName, zone.DnsName)
	records, err := client.DefaultAPI.ListRecordSets(ctx, c.project, zone.Id).PageSize(100).Execute()
	if err != nil {
		return "", err
	}
	value := *dns.NewRecordPayload(ip)
	for _, rr := range records.RrSets {
		if equalDNS(rr.Name, fqdn) && string(rr.Type) == "A" {
			p := dns.NewPartialUpdateRecordSetPayload()
			p.SetRecords([]dns.RecordPayload{value})
			p.SetTtl(cfg.TTL)
			if _, err := client.DefaultAPI.PartialUpdateRecordSet(ctx, c.project, zone.Id, rr.Id).PartialUpdateRecordSetPayload(*p).Execute(); err != nil {
				return "", err
			}
			return fqdn, nil
		}
	}
	p := dns.NewCreateRecordSetPayload(fqdn, []dns.RecordPayload{value}, dns.CREATERECORDSETPAYLOADTYPE_A)
	p.SetTtl(cfg.TTL)
	p.SetComment("Managed by coriolis-stackit")
	if _, err := client.DefaultAPI.CreateRecordSet(ctx, c.project, zone.Id).CreateRecordSetPayload(*p).Execute(); err != nil {
		return "", err
	}
	return fqdn, nil
}

func dnsRecordFQDN(recordName, zoneName string) string {
	fqdn := strings.TrimSuffix(recordName, ".")
	zoneName = strings.TrimSuffix(zoneName, ".")
	if !equalDNS(fqdn, zoneName) && !strings.HasSuffix(strings.ToLower(fqdn), "."+strings.ToLower(zoneName)) {
		fqdn += "." + zoneName
	}
	return fqdn
}

func (c *Cloud) newDNSClient() (*dns.APIClient, error) {
	return dns.NewAPIClient(stackitcfg.WithServiceAccountKeyPath(c.credentials), stackitcfg.WithBackgroundTokenRefresh(c.refreshCtx), stackitcfg.WithHTTPClient(&http.Client{Timeout: 2 * time.Minute}))
}

func (c *Cloud) findDNSZone(ctx context.Context, client *dns.APIClient, cfg DNSConfig) (*dns.Zone, []dns.Zone, error) {
	zones, err := client.DefaultAPI.ListZones(ctx, c.project).PageSize(100).Execute()
	if err != nil {
		return nil, nil, err
	}
	for i := range zones.Zones {
		z := &zones.Zones[i]
		if cfg.ZoneID != "" && z.Id == cfg.ZoneID {
			return z, zones.Zones, nil
		}
		if cfg.ZoneID == "" && cfg.ZoneName != "" && (strings.EqualFold(z.Name, cfg.ZoneName) || equalDNS(z.DnsName, cfg.ZoneName)) {
			return z, zones.Zones, nil
		}
	}
	return nil, zones.Zones, nil
}

func (c *Cloud) ensureDNSZone(ctx context.Context, cfg DNSConfig) (*dns.Zone, error) {
	client, err := c.newDNSClient()
	if err != nil {
		return nil, err
	}
	return c.ensureDNSZoneWithClient(ctx, client, cfg)
}

func (c *Cloud) ensureDNSZoneWithClient(ctx context.Context, client *dns.APIClient, cfg DNSConfig) (*dns.Zone, error) {
	zone, zones, err := c.findDNSZone(ctx, client, cfg)
	if err != nil {
		return nil, err
	}
	if zone == nil && cfg.CreateZone {
		payload := dns.NewCreateZonePayload(strings.TrimSuffix(cfg.ZoneName, "."), "coriolis-appliance")
		payload.SetDescription("Managed by coriolis-stackit")
		created, err := client.DefaultAPI.CreateZone(ctx, c.project).CreateZonePayload(*payload).Execute()
		if err != nil {
			return nil, fmt.Errorf("create DNS zone %q: %w", cfg.ZoneName, err)
		}
		z := created.GetZone()
		zone = &z
		fmt.Fprintln(os.Stderr, "created DNS zone", zone.DnsName, zone.Id)
	}
	if zone == nil {
		var available []string
		for _, z := range zones {
			available = append(available, fmt.Sprintf("%s (%s)", z.Name, z.DnsName))
		}
		return nil, fmt.Errorf("DNS zone %q not found; available zones: %s", cfg.ZoneName, strings.Join(available, ", "))
	}
	for zone.State == dns.ZONESTATE_CREATING || zone.State == dns.ZONESTATE_UPDATING {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(5 * time.Second):
		}
		response, err := client.DefaultAPI.GetZone(ctx, c.project, zone.Id).Execute()
		if err != nil {
			return nil, err
		}
		z := response.GetZone()
		zone = &z
	}
	if zone.State == dns.ZONESTATE_CREATE_FAILED || zone.State == dns.ZONESTATE_UPDATE_FAILED {
		return nil, fmt.Errorf("DNS zone %q is in state %s: %s", zone.DnsName, zone.State, zone.GetError())
	}
	return zone, nil
}

func (c *Cloud) checkDNSZone(ctx context.Context, cfg DNSConfig) error {
	client, err := c.newDNSClient()
	if err != nil {
		return err
	}
	zones, err := client.DefaultAPI.ListZones(ctx, c.project).PageSize(100).Execute()
	if err != nil {
		return err
	}
	for _, z := range zones.Zones {
		if cfg.ZoneID != "" && z.Id == cfg.ZoneID {
			return nil
		}
		if cfg.ZoneID == "" && (strings.EqualFold(z.Name, cfg.ZoneName) || equalDNS(z.DnsName, cfg.ZoneName)) {
			return nil
		}
	}
	if cfg.CreateZone {
		return nil
	}
	var available []string
	for _, z := range zones.Zones {
		available = append(available, fmt.Sprintf("%s (%s)", z.Name, z.DnsName))
	}
	return fmt.Errorf("DNS zone %q not found; available zones: %s", cfg.ZoneName, strings.Join(available, ", "))
}

func equalDNS(a, b string) bool {
	return strings.EqualFold(strings.TrimSuffix(a, "."), strings.TrimSuffix(b, "."))
}
