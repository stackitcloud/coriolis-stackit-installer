package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"strings"
	"testing"
)

func TestParseCSR(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject:  pkix.Name{CommonName: "coriolis.example.test"},
		DNSNames: []string{"coriolis.example.test"},
	}, key)
	if err != nil {
		t.Fatal(err)
	}
	pemCSR := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der})
	output := "CORIOLIS_CSR_B64=" + base64.StdEncoding.EncodeToString(pemCSR) + "\nCSR_OK\n"
	csr, err := parseCSR(output)
	if err != nil {
		t.Fatal(err)
	}
	if len(csr.DNSNames) != 1 || csr.DNSNames[0] != "coriolis.example.test" {
		t.Fatalf("unexpected CSR DNS names: %#v", csr.DNSNames)
	}
}

func TestCertificateScriptsDoNotEmbedFQDNLiterally(t *testing.T) {
	fqdn := "coriolis.example.test"
	for name, script := range map[string]string{
		"probe":        certificateProbeScript(fqdn, 30),
		"staged-probe": certificateStagedProbeScript(fqdn, 30),
		"csr":          certificateCSRScript(fqdn),
		"apply":        certificateApplyScript(fqdn),
	} {
		if strings.Contains(script, fqdn) {
			t.Fatalf("%s script embeds FQDN without encoding", name)
		}
		if len(script) > 10_000 {
			t.Fatalf("%s script exceeds Run Command limit: %d bytes", name, len(script))
		}
	}
}

func TestCertificateApplyUsesLocalIPAddressForVendorExpose(t *testing.T) {
	script := certificateApplyScript("coriolis.example.test")
	hostnamePos := strings.Index(script, `hostnamectl set-hostname "$fqdn"`)
	exposePos := strings.LastIndex(script, `expose_coriolis.py" --use-address "$ip"`)
	if hostnamePos < 0 || exposePos < 0 || hostnamePos > exposePos {
		t.Fatalf("certificate apply must set the FQDN hostname before exposing through the local interface IP (hostname=%d expose=%d)", hostnamePos, exposePos)
	}
	if strings.Contains(script, `expose_coriolis.py" --use-address "$fqdn"`) {
		t.Fatal("vendor expose command does not accept an FQDN as --use-address")
	}
}

func TestCertificateStageScriptFitsRunCommand(t *testing.T) {
	script := certificateStageFileScript("server.pem", make([]byte, 4000))
	if len(script) > 10_000 {
		t.Fatalf("stage script exceeds Run Command limit: %d bytes", len(script))
	}
}

func TestCertificateFailureSummaryAllowsOnlyFixedMetadata(t *testing.T) {
	output := "noise secret=value\nCERTIFICATE_APPLY_FAILED phase=coriolis_reconfigure categories=certificate_verify,timeout rollback=completed\nmore"
	got := certificateFailureSummary(output)
	want := "CERTIFICATE_APPLY_FAILED phase=coriolis_reconfigure categories=certificate_verify,timeout rollback=completed"
	if got != want {
		t.Fatalf("unexpected summary %q", got)
	}
	if strings.Contains(got, "secret") {
		t.Fatal("summary leaked unrelated output")
	}
}
