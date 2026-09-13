package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var version = "dev"

type result struct {
	ProjectID, Region, ImageID, NetworkID, ServerID, AvailabilityZone, MachineType, OVAHash string `json:",omitempty"`
	SecurityGroupID, PublicIP, LoginURL, LoginUser, PasswordHint                            string `json:",omitempty"`
	LoadBalancerName, LoadBalancerIP, CertificateID                                         string `json:",omitempty"`
	GeneratedPassword                                                                       string `json:"generated_password,omitempty"`
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("coriolis-stackit", flag.ContinueOnError)
	configPath := fs.String("config", "", "YAML configuration file")
	project := fs.String("project-id", "", "STACKIT project ID")
	credentials := fs.String("credentials", "", "service account key JSON")
	ova := fs.String("ova", "", "Coriolis OVA file")
	region := fs.String("region", "", "STACKIT region")
	imageID := fs.String("image-id", "", "existing local or shared STACKIT image ID")
	serverID := fs.String("server-id", "", "adopt an existing STACKIT server without replacing its boot volume")
	imageOwnerProject := fs.String("image-owner-project-id", "", "project that owns or should receive the image")
	shareImageOrganization := fs.Bool("share-image-with-organization", false, "share the image with its parent organization")
	shareImageProjects := fs.String("share-image-with-projects", "", "comma-separated target project IDs for image sharing")
	zone := fs.String("availability-zone", "", "availability zone")
	machine := fs.String("machine-type", "", "machine type")
	storage := fs.String("performance-class", "", "boot volume performance class")
	diskSize := fs.Int64("boot-volume-size", 0, "boot volume size in GiB")
	normalizationStorage := fs.String("normalization-performance-class", "", "temporary normalization and scratch volume performance class")
	normalizationScratchSize := fs.Int64("normalization-scratch-size", 0, "temporary normalization scratch volume size in GiB")
	helperImageID := fs.String("normalization-helper-image-id", "", "public Ubuntu image ID for the temporary normalization helper")
	helperMachineType := fs.String("normalization-helper-machine-type", "", "machine type for the temporary normalization helper")
	helperBootSize := fs.Int64("normalization-helper-boot-size", 0, "temporary normalization helper boot volume size in GiB")
	networkID := fs.String("network-id", "", "existing network ID")
	publicIPID := fs.String("public-ip-id", "", "existing public IP ID")
	publicIPAddress := fs.String("public-ip-address", "", "existing public IP address")
	dnsZoneID := fs.String("dns-zone-id", "", "STACKIT DNS zone ID")
	dnsZoneName := fs.String("dns-zone-name", "", "STACKIT DNS zone name or DNS name")
	dnsName := fs.String("dns-name", "", "DNS record name; enables DNS")
	certificateEmail := fs.String("certificate-email", "", "ACME email; enables trusted direct appliance certificate when exposure mode is direct")
	adminPassword := fs.String("admin-password", "", "initial Coriolis admin password (prefer YAML/env-safe secret handling)")
	dry := fs.Bool("dry-run", false, "validate and print the resolved plan without cloud changes")
	checkCloud := fs.Bool("check-cloud", false, "validate credentials, project, zone and machine type without changes")
	showVersion := fs.Bool("version", false, "print version")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *showVersion {
		fmt.Println(version)
		return nil
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
	if *ova != "" {
		c.OVA = *ova
	}
	if *region != "" {
		c.Region = *region
	}
	if *imageID != "" {
		c.Image.ID = *imageID
	}
	if *serverID != "" {
		c.Server.ID = *serverID
	}
	if *imageOwnerProject != "" {
		c.Image.OwnerProjectID = *imageOwnerProject
	}
	if *shareImageOrganization {
		c.Image.Share.ParentOrganization = true
		c.Image.Share.ProjectIDs = nil
	}
	if *shareImageProjects != "" {
		c.Image.Share.ProjectIDs = splitCommaList(*shareImageProjects)
		c.Image.Share.ParentOrganization = false
	}
	if *zone != "" {
		c.Server.AvailabilityZone = *zone
	}
	if *machine != "" {
		c.Server.MachineType = *machine
	}
	if *storage != "" {
		c.Server.PerformanceClass = *storage
	}
	if *diskSize > 0 {
		c.Server.BootVolumeSize = *diskSize
	}
	if *normalizationStorage != "" {
		c.Normalization.PerformanceClass = *normalizationStorage
	}
	if *normalizationScratchSize > 0 {
		c.Normalization.ScratchSizeGiB = *normalizationScratchSize
	}
	if *helperImageID != "" {
		c.Normalization.HelperImageID = *helperImageID
	}
	if *helperMachineType != "" {
		c.Normalization.HelperMachineType = *helperMachineType
	}
	if *helperBootSize > 0 {
		c.Normalization.HelperBootSizeGiB = *helperBootSize
	}
	if *networkID != "" {
		c.Network.ID = *networkID
	}
	if *publicIPID != "" {
		c.PublicIPID = *publicIPID
		c.PublicIP = true
	}
	if *publicIPAddress != "" {
		c.PublicIPAddress = *publicIPAddress
		c.PublicIP = true
	}
	if *dnsZoneID != "" {
		c.DNS.ZoneID = *dnsZoneID
	}
	if *dnsZoneName != "" {
		c.DNS.ZoneName = *dnsZoneName
	}
	if *dnsName != "" {
		c.DNS.RecordName = *dnsName
		c.DNS.Enabled = true
	}
	if *certificateEmail != "" {
		c.Exposure.Certificate.Email = *certificateEmail
		if c.Exposure.Mode == "direct" {
			c.Exposure.Certificate.Enabled = true
		}
	}
	if *adminPassword != "" {
		c.Bootstrap.AdminPassword = *adminPassword
		c.Bootstrap.Enabled = true
	}
	if err := c.validate(); err != nil {
		return err
	}
	abs, err := filepath.Abs(c.Credentials)
	if err != nil {
		return err
	}
	c.Credentials = abs
	abs, err = filepath.Abs(c.OVA)
	if err != nil {
		return err
	}
	c.OVA = abs
	if st, err := os.Stat(c.Credentials); err != nil {
		return fmt.Errorf("credentials: %w", err)
	} else if st.Mode().Perm()&0077 != 0 {
		fmt.Fprintln(os.Stderr, "warning: credentials file is readable by group/others; chmod 600 is recommended")
	}
	info, err := inspectOVA(c.OVA)
	if err != nil {
		return err
	}
	if c.Server.BootVolumeSize < info.DiskGiB {
		return fmt.Errorf("boot volume %d GiB is smaller than OVA disk %d GiB", c.Server.BootVolumeSize, info.DiskGiB)
	}
	// Scratch holds the compressed vendor disk and, during final export, a
	// potentially fully allocated QCOW2 of the virtual disk plus filesystem
	// overhead. Reject undersized configurations before creating cloud resources.
	const gib = int64(1024 * 1024 * 1024)
	minimumScratchGiB := info.DiskGiB + (info.DiskFileBytes+gib-1)/gib + 2
	if c.Normalization.ScratchSizeGiB < minimumScratchGiB {
		return fmt.Errorf("normalization scratch volume %d GiB is too small; OVA requires at least %d GiB", c.Normalization.ScratchSizeGiB, minimumScratchGiB)
	}
	if *dry {
		return printJSON(struct {
			Config Config  `json:"config"`
			OVA    OVAInfo `json:"ova"`
		}{c, info})
	}
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()
	cloud, err := newCloud(c, ctx)
	if err != nil {
		return err
	}
	if *checkCloud {
		zoneResolved, err := cloud.chooseZone(ctx, c.Server.AvailabilityZone)
		if err != nil {
			return err
		}
		if c.DNS.Enabled {
			if err := cloud.checkDNSZone(ctx, c.DNS); err != nil {
				return err
			}
		}
		if c.Exposure.Mode == "application_load_balancer" {
			if err := cloud.checkALBAccess(ctx, c.Exposure.LoadBalancer); err != nil {
				return err
			}
		}
		machineResolved, err := cloud.validateMachineType(ctx, c.Server.MachineType, info.VCPUs, info.MemoryMiB)
		if err != nil {
			return err
		}
		return printJSON(result{ProjectID: c.ProjectID, Region: c.Region, AvailabilityZone: zoneResolved, MachineType: machineResolved, OVAHash: info.SHA256})
	}
	// Validate placement before spending time and quota on conversion/upload.
	zoneResolved, err := cloud.chooseZone(ctx, c.Server.AvailabilityZone)
	if err != nil {
		return err
	}
	machineResolved, err := cloud.validateMachineType(ctx, c.Server.MachineType, info.VCPUs, info.MemoryMiB)
	if err != nil {
		return err
	}
	// Provision DNS first so an unavailable name fails before the large image upload.
	if c.DNS.Enabled {
		dnsZone, err := cloud.ensureDNSZone(ctx, c.DNS)
		if err != nil {
			return fmt.Errorf("ensure DNS zone: %w", err)
		}
		c.DNS.ZoneID = dnsZone.Id
		c.DNS.ZoneName = dnsZone.DnsName
		c.DNS.CreateZone = false
	}
	netID, err := cloud.ensureNetwork(ctx, c.Network)
	if err != nil {
		return fmt.Errorf("ensure network: %w", err)
	}
	sgID, err := cloud.ensureSecurityGroup(ctx, c.SecurityGroup)
	if err != nil {
		return fmt.Errorf("ensure security group: %w", err)
	}
	image, err := cloud.ensureImage(ctx, c, info, zoneResolved, netID, sgID)
	if err != nil {
		return fmt.Errorf("ensure image: %w", err)
	}
	fqdn := ""
	if c.DNS.Enabled {
		fqdn = dnsRecordFQDN(c.DNS.RecordName, c.DNS.ZoneName)
	}
	passwordWasGenerated := c.Bootstrap.Enabled && c.Bootstrap.AdminPassword == ""
	server, _, err := cloud.ensureServer(ctx, c, image.GetId(), zoneResolved, machineResolved, netID, sgID, "")
	if err != nil {
		return fmt.Errorf("ensure server: %w", err)
	}
	appliancePassword, err := cloud.bootstrapAppliance(ctx, c, server.GetId(), fqdn)
	if err != nil {
		return fmt.Errorf("bootstrap appliance: %w", err)
	}
	res := result{ProjectID: c.ProjectID, Region: c.Region, ImageID: image.GetId(), NetworkID: netID, SecurityGroupID: sgID, ServerID: server.GetId(), AvailabilityZone: zoneResolved, MachineType: machineResolved, OVAHash: info.SHA256, LoginUser: "admin", PasswordHint: "configured automatically through the STACKIT Server Agent"}
	if passwordWasGenerated && appliancePassword != "" && c.Bootstrap.PrintGeneratedPassword {
		res.GeneratedPassword = appliancePassword
		res.PasswordHint = "store generated_password securely; reruns return the same appliance-specific password"
	}
	if c.Exposure.Mode == "application_load_balancer" {
		fqdn := dnsRecordFQDN(c.DNS.RecordName, c.DNS.ZoneName)
		certificateID, err := cloud.ensureCertificate(ctx, c.Exposure.Certificate, c.DNS.ZoneID, fqdn)
		if err != nil {
			return fmt.Errorf("ensure TLS certificate: %w", err)
		}
		targetIP, err := cloud.serverPrivateIP(ctx, server.GetId(), netID)
		if err != nil {
			return fmt.Errorf("get ALB target IP: %w", err)
		}
		loadBalancer, err := cloud.ensureALB(ctx, c.Exposure.LoadBalancer, netID, targetIP, fqdn, certificateID)
		if err != nil {
			return fmt.Errorf("ensure application load balancer: %w", err)
		}
		address := loadBalancer.GetExternalAddress()
		if address == "" {
			return fmt.Errorf("ALB %s is ready but has no external address", c.Exposure.LoadBalancer.Name)
		}
		if _, err := cloud.ensureDNS(ctx, c.DNS, address); err != nil {
			return fmt.Errorf("point DNS to ALB: %w", err)
		}
		res.CertificateID = certificateID
		res.LoadBalancerName = c.Exposure.LoadBalancer.Name
		res.LoadBalancerIP = address
		res.LoginURL = "https://" + fqdn
	} else if c.PublicIP {
		ip, err := cloud.ensurePublicIP(ctx, server.GetId(), netID, c.PublicIPID, c.PublicIPAddress)
		if err != nil {
			return fmt.Errorf("ensure public IP: %w", err)
		}
		res.PublicIP = ip
		res.LoginURL = "https://" + ip
		if c.DNS.Enabled {
			fqdn, err := cloud.ensureDNS(ctx, c.DNS, ip)
			if err != nil {
				return fmt.Errorf("ensure DNS: %w", err)
			}
			res.LoginURL = "https://" + fqdn
			if err := cloud.ensureDirectCertificate(ctx, c.Exposure.Certificate, c.DNS.ZoneID, server.GetId(), fqdn); err != nil {
				return fmt.Errorf("ensure direct appliance certificate: %w", err)
			}
		}
	}
	return printJSON(res)
}

func splitCommaList(value string) []string {
	var result []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			result = append(result, item)
		}
	}
	return result
}

func printJSON(v interface{}) error {
	e := json.NewEncoder(os.Stdout)
	e.SetIndent("", "  ")
	return e.Encode(v)
}
