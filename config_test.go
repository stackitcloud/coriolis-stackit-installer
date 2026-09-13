package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigMergesDefaults(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "config.yaml")
	if err := os.WriteFile(p, []byte("project_id: p\ncredentials: c\nova: a\nserver:\n  machine_type: custom\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := loadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Server.MachineType != "custom" {
		t.Fatalf("override not applied: %q", c.Server.MachineType)
	}
	if c.Server.BootVolumeSize != 48 {
		t.Fatalf("default lost: %d", c.Server.BootVolumeSize)
	}
	if c.Image.UEFI == nil || *c.Image.UEFI {
		t.Fatal("expected BIOS default")
	}
	if c.Server.PerformanceClass != "storage_premium_perf12" || c.Normalization.PerformanceClass != "storage_premium_perf12" {
		t.Fatalf("performance defaults must use perf12: server=%q normalization=%q", c.Server.PerformanceClass, c.Normalization.PerformanceClass)
	}
}

func TestNormalizationScratchMustFitApplianceDisk(t *testing.T) {
	c := defaultConfig()
	c.ProjectID, c.Credentials, c.OVA = "project", "credentials.json", "image.ova"
	c.Normalization.ScratchSizeGiB = c.Server.BootVolumeSize - 1
	if err := c.validate(); err == nil {
		t.Fatal("expected undersized normalization scratch validation error")
	}
}

func TestDNSRequiresZoneAndPublicIP(t *testing.T) {
	c := defaultConfig()
	c.ProjectID = "p"
	c.Credentials = "c"
	c.OVA = "o"
	c.DNS.Enabled = true
	c.DNS.RecordName = "coriolis"
	if err := c.validate(); err == nil {
		t.Fatal("expected missing zone error")
	}
	c.DNS.ZoneName = "example.test"
	c.PublicIP = false
	if err := c.validate(); err == nil {
		t.Fatal("expected public IP error")
	}
}

func TestALBRequiresCertificateEmail(t *testing.T) {
	c := defaultConfig()
	c.ProjectID, c.Credentials, c.OVA = "project", "credentials.json", "image.ova"
	c.DNS.Enabled, c.DNS.ZoneName, c.DNS.RecordName = true, "example.test", "coriolis"
	c.Exposure.Mode = "application_load_balancer"
	if err := c.validate(); err == nil {
		t.Fatal("expected missing certificate email validation error")
	}
	c.Exposure.Certificate.Email = "admin@example.test"
	if err := c.validate(); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestDirectCertificateRequiresDNSAndEmail(t *testing.T) {
	c := defaultConfig()
	c.ProjectID, c.Credentials, c.OVA = "project", "credentials.json", "image.ova"
	c.Exposure.Certificate.Enabled = true
	if err := c.validate(); err == nil {
		t.Fatal("expected direct certificate DNS validation error")
	}
	c.DNS.Enabled, c.DNS.ZoneName, c.DNS.RecordName = true, "example.test", "coriolis"
	if err := c.validate(); err == nil {
		t.Fatal("expected direct certificate email validation error")
	}
	c.Exposure.Certificate.Email = "admin@example.test"
	if err := c.validate(); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestImageShareModesAreMutuallyExclusive(t *testing.T) {
	c := defaultConfig()
	c.ProjectID, c.Credentials, c.OVA = "project", "credentials.json", "image.ova"
	c.Image.Share.ParentOrganization = true
	c.Image.Share.ProjectIDs = []string{"11111111-1111-1111-1111-111111111111"}
	if err := c.validate(); err == nil {
		t.Fatal("expected mutually exclusive image share validation error")
	}
}

func TestImageShareProjectIDValidation(t *testing.T) {
	c := defaultConfig()
	c.ProjectID, c.Credentials, c.OVA = "project", "credentials.json", "image.ova"
	c.Image.Share.ProjectIDs = []string{"not-a-project-id"}
	if err := c.validate(); err == nil {
		t.Fatal("expected invalid image share project ID error")
	}
}
