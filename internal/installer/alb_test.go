package installer

import "testing"

func TestBuildALBPayload(t *testing.T) {
	p := buildALBPayload(ALBConfig{Name: "coriolis-alb", PlanID: "p10", BackendPort: 80, HealthCheckPath: "/"}, "11111111-1111-4111-8111-111111111111", "10.1.100.95", "coriolis.example.test", "cert-1")
	if p.GetName() != "coriolis-alb" || len(p.GetListeners()) != 1 || len(p.GetTargetPools()) != 1 {
		t.Fatalf("unexpected ALB payload: %#v", p)
	}
	if got := p.GetTargetPools()[0].GetTargets()[0].GetIp(); got != "10.1.100.95" {
		t.Fatalf("target IP = %q", got)
	}
	https := p.GetListeners()[0].GetHttps()
	certConfig := https.GetCertificateConfig()
	ids := certConfig.GetCertificateIds()
	if len(ids) != 1 || ids[0] != "cert-1" {
		t.Fatalf("certificate IDs = %v", ids)
	}
}

func TestDNSRecordFQDN(t *testing.T) {
	for _, tc := range []struct{ record, zone, want string }{
		{"coriolis", "example.runs.onstackit.cloud", "coriolis.example.runs.onstackit.cloud"},
		{"coriolis.example.runs.onstackit.cloud.", "example.runs.onstackit.cloud.", "coriolis.example.runs.onstackit.cloud"},
	} {
		if got := dnsRecordFQDN(tc.record, tc.zone); got != tc.want {
			t.Fatalf("dnsRecordFQDN(%q, %q) = %q, want %q", tc.record, tc.zone, got, tc.want)
		}
	}
}
