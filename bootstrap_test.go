package main

import (
	"strings"
	"testing"
)

func TestBuildAgentBootstrapScript(t *testing.T) {
	c := defaultConfig()
	c.Bootstrap.AdminPassword = "unique test password with spaces"
	script := buildAgentBootstrapScript(c, "coriolis.example.test")
	if strings.Contains(script, c.Bootstrap.AdminPassword) {
		t.Fatal("password must not occur as plaintext in the Run Command script")
	}
	for _, expected := range []string{"bootstrap-v3.sha256", "openstack token issue", "shlex.quote", "expose_coriolis.py", "CORIOLIS_ADMIN_PASSWORD_B64="} {
		if !strings.Contains(script, expected) {
			t.Fatalf("bootstrap script is missing %q", expected)
		}
	}
	if len(script) > 10_000 {
		t.Fatalf("bootstrap script exceeds Run Command limit: %d bytes", len(script))
	}
}
