package main

import (
	"errors"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	ProjectID       string              `yaml:"project_id" json:"project_id"`
	Credentials     string              `yaml:"credentials" json:"-"`
	OVA             string              `yaml:"ova" json:"ova"`
	Region          string              `yaml:"region" json:"region"`
	Image           ImageConfig         `yaml:"image" json:"image"`
	Server          ServerConfig        `yaml:"server" json:"server"`
	Network         NetworkConfig       `yaml:"network" json:"network"`
	SecurityGroup   SecurityGroupConfig `yaml:"security_group" json:"security_group"`
	PublicIP        bool                `yaml:"public_ip" json:"public_ip"`
	PublicIPID      string              `yaml:"public_ip_id" json:"public_ip_id"`
	PublicIPAddress string              `yaml:"public_ip_address" json:"public_ip_address"`
	DNS             DNSConfig           `yaml:"dns" json:"dns"`
	Exposure        ExposureConfig      `yaml:"exposure" json:"exposure"`
	Bootstrap       BootstrapConfig     `yaml:"bootstrap" json:"bootstrap"`
	Agent           AgentConfig         `yaml:"agent" json:"agent"`
	Normalization   NormalizationConfig `yaml:"normalization" json:"normalization"`
	Timeout         time.Duration       `yaml:"-" json:"-"`
	TimeoutText     string              `yaml:"timeout" json:"timeout"`
	PollInterval    time.Duration       `yaml:"-" json:"-"`
	PollText        string              `yaml:"poll_interval" json:"poll_interval"`
	UploadAttempts  int                 `yaml:"upload_attempts" json:"upload_attempts"`
}

type ImageConfig struct {
	ID             string           `yaml:"id" json:"id"`
	OwnerProjectID string           `yaml:"owner_project_id" json:"owner_project_id"`
	NamePrefix     string           `yaml:"name_prefix" json:"name_prefix"`
	DiskBus        string           `yaml:"disk_bus" json:"disk_bus"`
	NICModel       string           `yaml:"nic_model" json:"nic_model"`
	UEFI           *bool            `yaml:"uefi" json:"uefi"`
	SecureBoot     *bool            `yaml:"secure_boot" json:"secure_boot"`
	Share          ImageShareConfig `yaml:"share" json:"share"`
}

type BootstrapConfig struct {
	Enabled                bool   `yaml:"enabled" json:"enabled"`
	AdminPassword          string `yaml:"admin_password" json:"-"`
	PrintGeneratedPassword bool   `yaml:"print_generated_password" json:"print_generated_password"`
}

type AgentConfig struct {
	Enabled       bool `yaml:"enabled" json:"enabled"`
	EnableService bool `yaml:"enable_service" json:"enable_service"`
}

// NormalizationConfig controls the temporary block storage used while turning
// a vendor OVA into the reusable, Server-Agent-enabled image. These volumes do
// sustained reads and writes, so they deliberately do not inherit a low-cost
// default intended for an otherwise idle helper VM.
type NormalizationConfig struct {
	PerformanceClass  string `yaml:"performance_class" json:"performance_class"`
	ScratchSizeGiB    int64  `yaml:"scratch_size_gib" json:"scratch_size_gib"`
	HelperImageID     string `yaml:"helper_image_id" json:"helper_image_id"`
	HelperMachineType string `yaml:"helper_machine_type" json:"helper_machine_type"`
	HelperBootSizeGiB int64  `yaml:"helper_boot_size_gib" json:"helper_boot_size_gib"`
}

type ImageShareConfig struct {
	ParentOrganization bool     `yaml:"parent_organization" json:"parent_organization"`
	ProjectIDs         []string `yaml:"project_ids" json:"project_ids"`
}

type ServerConfig struct {
	ID                    string `yaml:"id" json:"id"`
	Name                  string `yaml:"name" json:"name"`
	MachineType           string `yaml:"machine_type" json:"machine_type"`
	AvailabilityZone      string `yaml:"availability_zone" json:"availability_zone"`
	BootVolumeSize        int64  `yaml:"boot_volume_size_gib" json:"boot_volume_size_gib"`
	PerformanceClass      string `yaml:"performance_class" json:"performance_class"`
	KeypairName           string `yaml:"keypair_name" json:"keypair_name"`
	DeleteBootOnTerminate bool   `yaml:"delete_boot_volume_on_termination" json:"delete_boot_volume_on_termination"`
}

type NetworkConfig struct {
	ID     string `yaml:"id" json:"id"`
	Name   string `yaml:"name" json:"name"`
	Prefix string `yaml:"ipv4_prefix" json:"ipv4_prefix"`
	Routed bool   `yaml:"routed" json:"routed"`
}

type SecurityGroupConfig struct {
	Name    string        `yaml:"name" json:"name"`
	Ingress []IngressRule `yaml:"ingress" json:"ingress"`
}

type IngressRule struct {
	Protocol    string `yaml:"protocol" json:"protocol"`
	Port        int64  `yaml:"port" json:"port"`
	CIDR        string `yaml:"cidr" json:"cidr"`
	Description string `yaml:"description" json:"description"`
}

type DNSConfig struct {
	Enabled    bool   `yaml:"enabled" json:"enabled"`
	CreateZone bool   `yaml:"create_zone" json:"create_zone"`
	ZoneID     string `yaml:"zone_id" json:"zone_id"`
	ZoneName   string `yaml:"zone_name" json:"zone_name"`
	RecordName string `yaml:"record_name" json:"record_name"`
	TTL        int32  `yaml:"ttl" json:"ttl"`
}

type ExposureConfig struct {
	Mode         string            `yaml:"mode" json:"mode"`
	Certificate  CertificateConfig `yaml:"certificate" json:"certificate"`
	LoadBalancer ALBConfig         `yaml:"load_balancer" json:"load_balancer"`
}

type CertificateConfig struct {
	Enabled         bool   `yaml:"enabled" json:"enabled"`
	Email           string `yaml:"email" json:"email"`
	Staging         bool   `yaml:"staging" json:"staging"`
	RenewBeforeDays int    `yaml:"renew_before_days" json:"renew_before_days"`
	NamePrefix      string `yaml:"name_prefix" json:"name_prefix"`
}

type ALBConfig struct {
	Name            string `yaml:"name" json:"name"`
	PlanID          string `yaml:"plan_id" json:"plan_id"`
	BackendPort     int32  `yaml:"backend_port" json:"backend_port"`
	HealthCheckPath string `yaml:"health_check_path" json:"health_check_path"`
}

func defaultConfig() Config {
	f := false
	return Config{
		Region: "eu01", TimeoutText: "90m", PollText: "15s", UploadAttempts: 3,
		Image:         ImageConfig{NamePrefix: "coriolis-appliance", DiskBus: "virtio", NICModel: "virtio", UEFI: &f, SecureBoot: &f},
		Server:        ServerConfig{Name: "coriolis-appliance", MachineType: "c1a.4d", BootVolumeSize: 48, PerformanceClass: "storage_premium_perf12"},
		Network:       NetworkConfig{Name: "coriolis-network", Prefix: "10.1.100.0/24", Routed: true},
		SecurityGroup: SecurityGroupConfig{Name: "coriolis-security", Ingress: []IngressRule{{Protocol: "tcp", Port: 443, CIDR: "0.0.0.0/0", Description: "Coriolis Web UI"}}},
		PublicIP:      true,
		DNS:           DNSConfig{TTL: 300},
		Exposure:      ExposureConfig{Mode: "direct", Certificate: CertificateConfig{RenewBeforeDays: 30, NamePrefix: "coriolis-tls"}, LoadBalancer: ALBConfig{Name: "coriolis-alb", PlanID: "p10", BackendPort: 80, HealthCheckPath: "/"}},
		// Appliance configuration is performed through the STACKIT Server Agent;
		// the stock appliance does not execute STACKIT user-data reliably.
		Bootstrap: BootstrapConfig{Enabled: true, PrintGeneratedPassword: true},
		Agent:     AgentConfig{Enabled: true, EnableService: true},
		Normalization: NormalizationConfig{
			PerformanceClass:  "storage_premium_perf12",
			ScratchSizeGiB:    64,
			HelperImageID:     "95d77000-d977-4ed1-8639-70ca359a5d1a",
			HelperMachineType: "g1a.1d",
			HelperBootSizeGiB: 16,
		},
	}
}

func loadConfig(path string) (Config, error) {
	c := defaultConfig()
	if path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return c, fmt.Errorf("read config: %w", err)
		}
		if err := yaml.Unmarshal(b, &c); err != nil {
			return c, fmt.Errorf("parse config: %w", err)
		}
	}
	var err error
	c.Timeout, err = time.ParseDuration(c.TimeoutText)
	if err != nil {
		return c, fmt.Errorf("invalid timeout: %w", err)
	}
	c.PollInterval, err = time.ParseDuration(c.PollText)
	if err != nil {
		return c, fmt.Errorf("invalid poll_interval: %w", err)
	}
	if c.UploadAttempts < 1 {
		return c, errors.New("upload_attempts must be at least 1")
	}
	return c, nil
}

func (c Config) validate() error {
	if c.ProjectID == "" {
		return errors.New("project-id is required")
	}
	if c.Credentials == "" {
		return errors.New("credentials is required")
	}
	if c.OVA == "" {
		return errors.New("ova is required")
	}
	if c.Region == "" {
		return errors.New("region is required (defaults to eu01)")
	}
	if c.Image.Share.ParentOrganization && len(c.Image.Share.ProjectIDs) > 0 {
		return errors.New("image.share.parent_organization and image.share.project_ids are mutually exclusive")
	}
	for _, projectID := range c.Image.Share.ProjectIDs {
		if len(projectID) != 36 {
			return fmt.Errorf("invalid image share project ID %q", projectID)
		}
	}
	if c.Server.BootVolumeSize < 1 {
		return errors.New("boot-volume-size must be positive")
	}
	if c.Server.PerformanceClass == "" {
		return errors.New("server.performance_class is required")
	}
	if c.Normalization.PerformanceClass == "" {
		return errors.New("normalization.performance_class is required")
	}
	if c.Normalization.ScratchSizeGiB < c.Server.BootVolumeSize {
		return errors.New("normalization.scratch_size_gib must be at least server.boot_volume_size_gib")
	}
	if c.Agent.Enabled && (c.Normalization.HelperImageID == "" || c.Normalization.HelperMachineType == "" || c.Normalization.HelperBootSizeGiB < 1) {
		return errors.New("agent-enabled image publishing requires normalization.helper_image_id, helper_machine_type and a positive helper_boot_size_gib")
	}
	if c.DNS.Enabled && !c.PublicIP && c.Exposure.Mode == "direct" {
		return errors.New("dns.enabled requires public_ip")
	}
	if c.DNS.Enabled && c.DNS.ZoneID == "" && c.DNS.ZoneName == "" {
		return errors.New("dns.enabled requires dns.zone_id or dns.zone_name")
	}
	if c.DNS.Enabled && c.DNS.RecordName == "" {
		return errors.New("dns.enabled requires dns.record_name")
	}
	if c.DNS.TTL < 1 {
		return errors.New("dns.ttl must be positive")
	}
	if c.Exposure.Mode != "direct" && c.Exposure.Mode != "application_load_balancer" {
		return errors.New("exposure.mode must be direct or application_load_balancer")
	}
	if c.Exposure.Mode == "application_load_balancer" {
		if !c.DNS.Enabled {
			return errors.New("application_load_balancer exposure requires dns.enabled")
		}
		if c.Exposure.Certificate.Email == "" {
			return errors.New("application_load_balancer exposure requires exposure.certificate.email")
		}
		if c.Exposure.Certificate.RenewBeforeDays < 1 || c.Exposure.LoadBalancer.BackendPort < 1 {
			return errors.New("certificate renew_before_days and ALB backend_port must be positive")
		}
	}
	if c.Exposure.Mode == "direct" && c.Exposure.Certificate.Enabled {
		if !c.DNS.Enabled {
			return errors.New("direct certificate installation requires dns.enabled")
		}
		if c.Exposure.Certificate.Email == "" {
			return errors.New("direct certificate installation requires exposure.certificate.email")
		}
	}
	return nil
}
