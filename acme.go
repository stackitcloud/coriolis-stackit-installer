package main

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-acme/lego/v4/certcrypto"
	"github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/challenge/dns01"
	"github.com/go-acme/lego/v4/lego"
	"github.com/go-acme/lego/v4/registration"
	stackitcfg "github.com/stackitcloud/stackit-sdk-go/core/config"
	certapi "github.com/stackitcloud/stackit-sdk-go/services/certificates/v2api"
	dns "github.com/stackitcloud/stackit-sdk-go/services/dns/v1api"
)

type acmeUser struct {
	email string
	key   crypto.PrivateKey
	reg   *registration.Resource
}

func (u *acmeUser) GetEmail() string                        { return u.email }
func (u *acmeUser) GetRegistration() *registration.Resource { return u.reg }
func (u *acmeUser) GetPrivateKey() crypto.PrivateKey        { return u.key }

type stackitDNSProvider struct {
	ctx       context.Context
	client    *dns.APIClient
	projectID string
	zoneID    string
	mu        sync.Mutex
	recordIDs map[string]string
}

func (p *stackitDNSProvider) Present(domain, token, keyAuth string) error {
	fqdn, value := dns01.GetRecord(domain, keyAuth)
	fqdn = strings.TrimSuffix(fqdn, ".")
	records, err := p.client.DefaultAPI.ListRecordSets(p.ctx, p.projectID, p.zoneID).PageSize(100).Execute()
	if err != nil {
		return err
	}
	record := *dns.NewRecordPayload(value)
	for _, rr := range records.RrSets {
		if equalDNS(rr.Name, fqdn) && string(rr.Type) == "TXT" {
			payload := dns.NewPartialUpdateRecordSetPayload()
			payload.SetRecords([]dns.RecordPayload{record})
			payload.SetTtl(60)
			_, err := p.client.DefaultAPI.PartialUpdateRecordSet(p.ctx, p.projectID, p.zoneID, rr.Id).PartialUpdateRecordSetPayload(*payload).Execute()
			if err != nil {
				return err
			}
			p.remember(domain, rr.Id)
			return nil
		}
	}
	payload := dns.NewCreateRecordSetPayload(fqdn, []dns.RecordPayload{record}, dns.CREATERECORDSETPAYLOADTYPE_TXT)
	payload.SetTtl(60)
	payload.SetComment("Temporary ACME DNS-01 challenge managed by coriolis-stackit")
	created, err := p.client.DefaultAPI.CreateRecordSet(p.ctx, p.projectID, p.zoneID).CreateRecordSetPayload(*payload).Execute()
	if err != nil {
		return err
	}
	p.remember(domain, created.Rrset.Id)
	return nil
}

func (p *stackitDNSProvider) CleanUp(domain, token, keyAuth string) error {
	p.mu.Lock()
	id := p.recordIDs[domain]
	delete(p.recordIDs, domain)
	p.mu.Unlock()
	if id == "" {
		return nil
	}
	_, err := p.client.DefaultAPI.DeleteRecordSet(p.ctx, p.projectID, p.zoneID, id).Execute()
	return err
}

func (p *stackitDNSProvider) Timeout() (time.Duration, time.Duration) {
	return 12 * time.Minute, 15 * time.Second
}

func (p *stackitDNSProvider) remember(domain, id string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.recordIDs[domain] = id
}

func (c *Cloud) ensureCertificate(ctx context.Context, cfg CertificateConfig, zoneID, fqdn string) (string, error) {
	client, err := certapi.NewAPIClient(stackitcfg.WithServiceAccountKeyPath(c.credentials), stackitcfg.WithBackgroundTokenRefresh(c.refreshCtx), stackitcfg.WithHTTPClient(&http.Client{Timeout: 2 * time.Minute}))
	if err != nil {
		return "", err
	}
	list, err := client.DefaultAPI.ListCertificates(ctx, c.project, c.region).PageSize("100").Execute()
	if err != nil {
		return "", err
	}
	environment := "production"
	if cfg.Staging {
		environment = "staging"
	}
	for _, item := range list.Items {
		if !(item.GetName() == cfg.NamePrefix || strings.HasPrefix(item.GetName(), cfg.NamePrefix+"-")) || item.Data == nil || !strings.Contains(item.Data.GetDnsNames(), fqdn) || item.GetLabels()["acme-environment"] != environment {
			continue
		}
		expiry, err := time.Parse(time.RFC3339, item.Data.GetNotAfter())
		if err == nil && time.Until(expiry) > time.Duration(cfg.RenewBeforeDays)*24*time.Hour {
			return item.GetId(), nil
		}
	}
	privateKey, err := rsa.GenerateKey(rand.Reader, 3072)
	if err != nil {
		return "", err
	}
	user := &acmeUser{email: cfg.Email, key: privateKey}
	legoCfg := lego.NewConfig(user)
	if cfg.Staging {
		legoCfg.CADirURL = lego.LEDirectoryStaging
	}
	legoCfg.Certificate.KeyType = certcrypto.RSA3072
	acmeClient, err := lego.NewClient(legoCfg)
	if err != nil {
		return "", err
	}
	dnsClient, err := c.newDNSClient()
	if err != nil {
		return "", err
	}
	provider := &stackitDNSProvider{ctx: ctx, client: dnsClient, projectID: c.project, zoneID: zoneID, recordIDs: map[string]string{}}
	if err := acmeClient.Challenge.SetDNS01Provider(provider, dns01.DisableCompletePropagationRequirement()); err != nil {
		return "", err
	}
	reg, err := acmeClient.Registration.Register(registration.RegisterOptions{TermsOfServiceAgreed: true})
	if err != nil {
		return "", fmt.Errorf("register ACME account: %w", err)
	}
	user.reg = reg
	issued, err := acmeClient.Certificate.Obtain(certificate.ObtainRequest{Domains: []string{fqdn}, Bundle: true})
	if err != nil {
		return "", fmt.Errorf("obtain certificate for %s: %w", fqdn, err)
	}
	payload := certapi.NewCreateCertificatePayload()
	fingerprint := fmt.Sprintf("%x", sha256.Sum256(issued.Certificate))[:12]
	payload.SetName(cfg.NamePrefix + "-" + fingerprint)
	payload.SetPublicKey(string(issued.Certificate))
	payload.SetPrivateKey(string(issued.PrivateKey))
	payload.SetLabels(map[string]string{"managed-by": "coriolis-installer", "dns-name": strings.ReplaceAll(fqdn, ".", "-"), "acme-environment": environment})
	created, err := client.DefaultAPI.CreateCertificate(ctx, c.project, c.region).CreateCertificatePayload(*payload).Execute()
	if err != nil {
		return "", fmt.Errorf("store certificate: %w", err)
	}
	return created.GetId(), nil
}

// obtainCertificateForCSR completes ACME DNS-01 issuance for a key generated
// on the appliance. The private key therefore never leaves the VM.
func (c *Cloud) obtainCertificateForCSR(ctx context.Context, cfg CertificateConfig, zoneID, fqdn string, csr *x509.CertificateRequest) (*certificate.Resource, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 3072)
	if err != nil {
		return nil, err
	}
	user := &acmeUser{email: cfg.Email, key: privateKey}
	legoCfg := lego.NewConfig(user)
	if cfg.Staging {
		legoCfg.CADirURL = lego.LEDirectoryStaging
	}
	legoCfg.Certificate.KeyType = certcrypto.RSA3072
	acmeClient, err := lego.NewClient(legoCfg)
	if err != nil {
		return nil, err
	}
	dnsClient, err := c.newDNSClient()
	if err != nil {
		return nil, err
	}
	provider := &stackitDNSProvider{ctx: ctx, client: dnsClient, projectID: c.project, zoneID: zoneID, recordIDs: map[string]string{}}
	if err := acmeClient.Challenge.SetDNS01Provider(provider, dns01.DisableCompletePropagationRequirement()); err != nil {
		return nil, err
	}
	reg, err := acmeClient.Registration.Register(registration.RegisterOptions{TermsOfServiceAgreed: true})
	if err != nil {
		return nil, fmt.Errorf("register ACME account: %w", err)
	}
	user.reg = reg
	issued, err := acmeClient.Certificate.ObtainForCSR(certificate.ObtainForCSRRequest{CSR: csr, Bundle: false})
	if err != nil {
		return nil, fmt.Errorf("obtain certificate for %s: %w", fqdn, err)
	}
	return issued, nil
}
