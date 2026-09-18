package installer

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
		"chain":        certificateChainValidationScript(fqdn),
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

func TestCertificateApplyScriptHasRunCommandSizeMargin(t *testing.T) {
	script := certificateApplyScript("coriolis.example.test")
	if len(script) > 8_000 {
		t.Fatalf("certificate apply script has insufficient Run Command size margin: %d bytes", len(script))
	}
	if !strings.Contains(script, `for f in "$work/intermediates.pem" "$work/trust-anchor.pem"`) {
		t.Fatal("certificate apply script does not require prepared chain files")
	}
}

func TestCertificateChainPreparationIsPersistent(t *testing.T) {
	script := certificateChainValidationScript("coriolis.example.test")
	if !strings.Contains(script, `work=$stage/chain-work`) {
		t.Fatal("certificate chain preparation must persist output for the apply command")
	}
	if strings.Contains(script, "mktemp") {
		t.Fatal("certificate chain preparation must not delete output before the apply command")
	}
	if !strings.Contains(script, `install -m 0644 "$trust_anchor" "$work/trust-anchor.pem"`) {
		t.Fatal("certificate chain preparation must persist the trust anchor for the apply command")
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

func TestCertificateStageFilesHaveDeterministicOrder(t *testing.T) {
	files := certificateStageFiles([]byte("leaf"), []byte("issuer"))
	if len(files) != 2 {
		t.Fatalf("unexpected number of staged files: %d", len(files))
	}
	if files[0].name != "server.pem" || string(files[0].contents) != "leaf" {
		t.Fatalf("unexpected first staged file: %#v", files[0])
	}
	if files[1].name != "issuer.pem" || string(files[1].contents) != "issuer" {
		t.Fatalf("unexpected second staged file: %#v", files[1])
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
