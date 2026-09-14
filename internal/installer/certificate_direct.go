package installer

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"strings"
)

func certificateProbeScript(fqdn string, renewBeforeDays int) string {
	fqdn64 := base64.StdEncoding.EncodeToString([]byte(fqdn))
	seconds := int64(renewBeforeDays) * 24 * 60 * 60
	return fmt.Sprintf(`#!/bin/bash
set -euo pipefail
fqdn=$(printf '%%s' %q | base64 -d)
source /root/coriolis-docker/utils/common.sh
cert=$(get_global_config_value coriolis_appliance_tls_certificate)
custom_mark=$(get_global_config_value coriolis_appliance_custom_cert)
active_fp=$(openssl x509 -in "$cert" -noout -fingerprint -sha256 2>/dev/null || true)
served_fp=$(timeout 15 openssl s_client -connect 127.0.0.1:443 -servername "$fqdn" </dev/null 2>/dev/null | openssl x509 -noout -fingerprint -sha256 2>/dev/null || true)
if [ -n "$custom_mark" ] && [ -e "$custom_mark" ] && [ -s "$cert" ] && [ -n "$active_fp" ] && [ "$active_fp" = "$served_fp" ] && openssl x509 -in "$cert" -noout -checkhost "$fqdn" >/dev/null 2>&1 && openssl x509 -in "$cert" -noout -checkend %d >/dev/null 2>&1; then
  echo CERTIFICATE_CURRENT
else
  echo CERTIFICATE_RENEWAL_REQUIRED
fi
`, fqdn64, seconds)
}

func certificateCSRScript(fqdn string) string {
	fqdn64 := base64.StdEncoding.EncodeToString([]byte(fqdn))
	return fmt.Sprintf(`#!/bin/bash
set -euo pipefail
fqdn=$(printf '%%s' %q | base64 -d)
stage=/var/lib/coriolis-stackit/tls-staging
install -d -m 0700 "$stage"
umask 077
if [ ! -s "$stage/server.key" ] || [ "$(cat "$stage/fqdn" 2>/dev/null || true)" != "$fqdn" ]; then
  rm -f "$stage/server.key" "$stage/server.csr" "$stage/server.pem" "$stage/issuer.pem"
  openssl genrsa -out "$stage/server.key" 3072 >/dev/null 2>&1
  openssl req -new -key "$stage/server.key" -out "$stage/server.csr" -subj "/CN=$fqdn" -addext "subjectAltName=DNS:$fqdn"
  printf '%%s' "$fqdn" > "$stage/fqdn"
fi
openssl req -in "$stage/server.csr" -noout -verify >/dev/null
printf 'CORIOLIS_CSR_B64='
base64 -w0 "$stage/server.csr"
printf '\nCSR_OK\n'
`, fqdn64)
}

func certificateStagedProbeScript(fqdn string, renewBeforeDays int) string {
	fqdn64 := base64.StdEncoding.EncodeToString([]byte(fqdn))
	seconds := int64(renewBeforeDays) * 24 * 60 * 60
	return fmt.Sprintf(`#!/bin/bash
set -euo pipefail
fqdn=$(printf '%%s' %q | base64 -d)
stage=/var/lib/coriolis-stackit/tls-staging
source /root/coriolis-docker/utils/common.sh
active_cert=$(get_global_config_value coriolis_appliance_tls_certificate)
active_key=$(get_global_config_value coriolis_appliance_tls_key)
custom_mark=$(get_global_config_value coriolis_appliance_custom_cert)
if [ ! -s "$stage/server.pem" ] || [ ! -s "$stage/issuer.pem" ] || [ ! -s "$stage/server.key" ]; then
  if [ -n "$custom_mark" ] && [ -e "$custom_mark" ] && [ -s "$active_cert" ] && [ -s "$active_key" ] &&
     openssl x509 -in "$active_cert" -noout -checkhost "$fqdn" >/dev/null 2>&1 &&
     openssl x509 -in "$active_cert" -noout -checkend %d >/dev/null 2>&1; then
    install -d -m 0700 "$stage"
    umask 077
    : > "$stage/server.pem"
    : > "$stage/issuer.pem"
    awk -v leaf="$stage/server.pem" -v issuers="$stage/issuer.pem" '
      /-----BEGIN CERTIFICATE-----/ {n++}
      n == 1 {print > leaf}
      n > 1 {print > issuers}
    ' "$active_cert"
    install -m 0600 "$active_key" "$stage/server.key"
    printf '%%s' "$fqdn" > "$stage/fqdn"
  fi
fi
if [ -s "$stage/server.pem" ] && [ -s "$stage/issuer.pem" ] && [ -s "$stage/server.key" ] &&
   openssl x509 -in "$stage/server.pem" -noout -checkhost "$fqdn" >/dev/null 2>&1 &&
   openssl x509 -in "$stage/server.pem" -noout -checkend %d >/dev/null 2>&1; then
  cert_pub=$(openssl x509 -in "$stage/server.pem" -pubkey -noout | openssl pkey -pubin -outform DER 2>/dev/null | sha256sum | awk '{print $1}')
  key_pub=$(openssl pkey -in "$stage/server.key" -pubout -outform DER 2>/dev/null | sha256sum | awk '{print $1}')
  if [ "$cert_pub" = "$key_pub" ] && openssl verify -partial_chain -CAfile "$stage/issuer.pem" "$stage/server.pem" >/dev/null 2>&1; then
    echo STAGED_CERTIFICATE_READY
    exit 0
  fi
fi
echo STAGED_CERTIFICATE_MISSING
`, fqdn64, seconds, seconds)
}

func certificateStageFileScript(name string, contents []byte) string {
	encoded := base64.StdEncoding.EncodeToString(contents)
	return fmt.Sprintf(`#!/bin/bash
set -euo pipefail
stage=/var/lib/coriolis-stackit/tls-staging
install -d -m 0700 "$stage"
umask 077
printf '%%s' %q | base64 -d > "$stage/%s"
test -s "$stage/%s"
echo CERTIFICATE_MATERIAL_STAGED
`, encoded, name, name)
}

// certificateChainValidationScript validates staged public certificate
// material in a temporary directory. It never writes active appliance files.
func certificateChainValidationScript(fqdn string) string {
	fqdn64 := base64.StdEncoding.EncodeToString([]byte(fqdn))
	return fmt.Sprintf(`#!/bin/bash
set -euo pipefail
fqdn=$(printf '%%s' %q | base64 -d)
stage=/var/lib/coriolis-stackit/tls-staging
work=$(mktemp -d /tmp/coriolis-certificate-validation.XXXXXX)
trap 'rm -rf "$work"' EXIT
for f in "$stage/server.pem" "$stage/issuer.pem"; do test -s "$f"; done
openssl x509 -in "$stage/server.pem" -noout -checkhost "$fqdn" >/dev/null
awk -v dir="$work" '
  /-----BEGIN CERTIFICATE-----/ {n++; file=sprintf("%%s/issuer-%%03d.pem", dir, n); inside=1}
  inside {print > file}
  /-----END CERTIFICATE-----/ {close(file); inside=0}
' "$stage/issuer.pem"
mapfile -t issuer_files < <(find "$work" -name 'issuer-*.pem' -type f | sort)
[ "${#issuer_files[@]}" -gt 0 ]
for f in "${issuer_files[@]}"; do openssl x509 -in "$f" -noout >/dev/null; done
openssl verify -partial_chain -CAfile "${issuer_files[0]}" "$stage/server.pem" >/dev/null
for ((i=0; i+1<${#issuer_files[@]}; i++)); do
  openssl verify -partial_chain -CAfile "${issuer_files[$((i+1))]}" "${issuer_files[$i]}" >/dev/null
done
last=${issuer_files[$((${#issuer_files[@]}-1))]}
last_subject=$(openssl x509 -in "$last" -noout -subject -nameopt RFC2253 | sed 's/^subject=//')
last_issuer=$(openssl x509 -in "$last" -noout -issuer -nameopt RFC2253 | sed 's/^issuer=//')
echo "terminal_subject=$last_subject"
echo "terminal_issuer=$last_issuer"
trust_anchor=""
intermediate_count=${#issuer_files[@]}
if [ "$last_subject" = "$last_issuer" ] && openssl verify -CAfile "$last" "$last" >/dev/null 2>&1; then
  trust_anchor=$last
  intermediate_count=$((intermediate_count-1))
else
  while IFS= read -r candidate; do
    [ -s "$candidate" ] || continue
    candidate_subject=$(openssl x509 -in "$candidate" -noout -subject -nameopt RFC2253 2>/dev/null | sed 's/^subject=//' || true)
    candidate_issuer=$(openssl x509 -in "$candidate" -noout -issuer -nameopt RFC2253 2>/dev/null | sed 's/^issuer=//' || true)
    [ "$candidate_subject" = "$last_issuer" ] || continue
    echo "matching_system_subject=$candidate_subject"
    [ "$candidate_subject" = "$candidate_issuer" ] || continue
    if openssl verify -CAfile "$candidate" "$candidate" >/dev/null 2>&1 && openssl verify -CAfile "$candidate" "$last" >/dev/null 2>&1; then
      trust_anchor=$candidate
      break
    fi
  done < <(find -L /etc/ssl/certs -maxdepth 1 -type f \( -name '*.pem' -o -name '*.crt' \) | sort -u)
fi
if [ -z "$trust_anchor" ]; then echo TRUST_ANCHOR_NOT_FOUND; exit 1; fi
cat "${issuer_files[@]:0:$intermediate_count}" > "$work/intermediates.pem"
openssl verify -CAfile "$trust_anchor" -untrusted "$work/intermediates.pem" "$stage/server.pem" >/dev/null
echo "intermediates=$(grep -c '^-----BEGIN CERTIFICATE-----$' "$work/intermediates.pem")"
echo "trust_anchors=1"
openssl x509 -in "$trust_anchor" -noout -subject -issuer
echo CERTIFICATE_CHAIN_PREPARATION_OK
`, fqdn64)
}

func certificateApplyScript(fqdn string) string {
	fqdn64 := base64.StdEncoding.EncodeToString([]byte(fqdn))
	return fmt.Sprintf(`#!/bin/bash
set -Eeuo pipefail
phase=initialization
preparation_failure() {
  trap - ERR
  echo "CERTIFICATE_APPLY_FAILED phase=$phase task=not_started categories=preparation rollback=not_required"
}
trap preparation_failure ERR
fqdn=$(printf '%%s' %q | base64 -d)
base=/root/coriolis-docker
stage=/var/lib/coriolis-stackit/tls-staging
backup=/var/lib/coriolis-stackit/tls-rollback
source "$base/utils/common.sh"

cert_store=$(get_global_config_value coriolis_certificate_store)
api_ca=$(get_global_config_value coriolis_appliance_tls_cacert)
api_cert=$(get_global_config_value coriolis_appliance_tls_certificate)
api_key=$(get_global_config_value coriolis_appliance_tls_key)
api_bundle=$(get_global_config_value coriolis_appliance_tls_cert_bundle)
api_combined=$(get_global_config_value coriolis_appliance_tls_combined)
custom_mark=$(get_global_config_value coriolis_appliance_custom_cert)
custom_local_ca=/usr/local/share/ca-certificates/coriolis-custom-ca.crt
config=$base/config.yml
kolla=/etc/kolla/globals.yml
work=$stage/chain-work
reconfigure_log=/var/log/coriolis-stackit-certificate.log
phase=certificate_preparation

for f in "$stage/server.pem" "$stage/issuer.pem" "$stage/server.key"; do test -s "$f"; done
openssl x509 -in "$stage/server.pem" -noout -checkhost "$fqdn" >/dev/null
openssl x509 -in "$stage/server.pem" -noout -checkend 86400 >/dev/null
openssl pkey -in "$stage/server.key" -check -noout >/dev/null
cert_pub=$(openssl x509 -in "$stage/server.pem" -pubkey -noout | openssl pkey -pubin -outform DER 2>/dev/null | sha256sum | awk '{print $1}')
key_pub=$(openssl pkey -in "$stage/server.key" -pubout -outform DER 2>/dev/null | sha256sum | awk '{print $1}')
test "$cert_pub" = "$key_pub"

rm -rf "$work"
install -d -m 0700 "$work"
awk -v dir="$work" '
  /-----BEGIN CERTIFICATE-----/ {n++; file=sprintf("%%s/issuer-%%03d.pem", dir, n); inside=1}
  inside {print > file}
  /-----END CERTIFICATE-----/ {close(file); inside=0}
' "$stage/issuer.pem"
mapfile -t issuer_files < <(find "$work" -name 'issuer-*.pem' -type f | sort)
[ "${#issuer_files[@]}" -gt 0 ]
for f in "${issuer_files[@]}"; do openssl x509 -in "$f" -noout >/dev/null; done
openssl verify -partial_chain -CAfile "${issuer_files[0]}" "$stage/server.pem" >/dev/null
for ((i=0; i+1<${#issuer_files[@]}; i++)); do
  openssl verify -partial_chain -CAfile "${issuer_files[$((i+1))]}" "${issuer_files[$i]}" >/dev/null
done

last=${issuer_files[$((${#issuer_files[@]}-1))]}
last_subject=$(openssl x509 -in "$last" -noout -subject -nameopt RFC2253 | sed 's/^subject=//')
last_issuer=$(openssl x509 -in "$last" -noout -issuer -nameopt RFC2253 | sed 's/^issuer=//')
trust_anchor=""
intermediate_count=${#issuer_files[@]}
if [ "$last_subject" = "$last_issuer" ] && openssl verify -CAfile "$last" "$last" >/dev/null 2>&1; then
  trust_anchor=$last
  intermediate_count=$((intermediate_count-1))
else
  while IFS= read -r candidate; do
    [ -s "$candidate" ] || continue
    candidate_subject=$(openssl x509 -in "$candidate" -noout -subject -nameopt RFC2253 2>/dev/null | sed 's/^subject=//' || true)
    candidate_issuer=$(openssl x509 -in "$candidate" -noout -issuer -nameopt RFC2253 2>/dev/null | sed 's/^issuer=//' || true)
    [ "$candidate_subject" = "$last_issuer" ] || continue
    [ "$candidate_subject" = "$candidate_issuer" ] || continue
    if openssl verify -CAfile "$candidate" "$candidate" >/dev/null 2>&1 && openssl verify -CAfile "$candidate" "$last" >/dev/null 2>&1; then
      trust_anchor=$candidate
      break
    fi
  done < <(find -L /etc/ssl/certs -maxdepth 1 -type f \( -name '*.pem' -o -name '*.crt' \) | sort -u)
fi
[ -n "$trust_anchor" ]
cat "${issuer_files[@]:0:$intermediate_count}" > "$work/intermediates.pem"
install -m 0644 "$trust_anchor" "$work/trust-anchor.pem"
openssl x509 -in "$work/trust-anchor.pem" -noout >/dev/null
openssl verify -CAfile "$work/trust-anchor.pem" -untrusted "$work/intermediates.pem" "$stage/server.pem" >/dev/null

rm -rf "$backup"
install -d -m 0700 "$backup"
backup_one() { if [ -e "$1" ]; then cp -a "$1" "$backup/$2"; else touch "$backup/$2.absent"; fi; }
restore_one() { if [ -e "$backup/$2" ]; then mkdir -p "$(dirname "$1")"; cp -a "$backup/$2" "$1"; elif [ -e "$backup/$2.absent" ]; then rm -f "$1"; fi; }
backup_one "$api_ca" api-ca
backup_one "$api_cert" api-cert
backup_one "$api_key" api-key
backup_one "$api_bundle" api-bundle
backup_one "$api_combined" api-combined
backup_one "$custom_local_ca" local-ca
backup_one "$custom_mark" custom-mark
backup_one "$config" config
backup_one "$kolla" kolla
hostname > "$backup/hostname"

transition_ca=$work/transition-ca.pem
: > "$transition_ca"
if [ -s "$backup/api-ca" ]; then
  old_fp=$(openssl x509 -in "$backup/api-ca" -noout -fingerprint -sha256 2>/dev/null || true)
  new_fp=$(openssl x509 -in "$work/trust-anchor.pem" -noout -fingerprint -sha256)
  if [ -n "$old_fp" ] && [ "$old_fp" != "$new_fp" ]; then
    cat "$backup/api-ca" >> "$transition_ca"
  fi
fi
cat "$work/trust-anchor.pem" >> "$transition_ca"
[ "$(grep -c '^-----BEGIN CERTIFICATE-----$' "$transition_ca")" -ge 1 ]

rollback() {
  trap - ERR
  categories=""
  grep -qiE 'certificate verify failed|unable to get (local )?issuer certificate' "$reconfigure_log" 2>/dev/null && categories="${categories},certificate_verify"
  grep -qiE 'connection refused|failed to establish a new connection' "$reconfigure_log" 2>/dev/null && categories="${categories},connection_refused"
  grep -qiE 'timed out|timeout' "$reconfigure_log" 2>/dev/null && categories="${categories},timeout"
  grep -qiE 'unauthorized|authentication failed|invalid credentials' "$reconfigure_log" 2>/dev/null && categories="${categories},authentication"
  grep -qiE 'no route to host|network is unreachable|name or service not known' "$reconfigure_log" 2>/dev/null && categories="${categories},network"
  grep -qiE 'status[^0-9]*500|http[^0-9]*500|internal server error' "$reconfigure_log" 2>/dev/null && categories="${categories},server_error"
  grep -qiE 'already exists|conflict' "$reconfigure_log" 2>/dev/null && categories="${categories},conflict"
  grep -qiE 'traceback' "$reconfigure_log" 2>/dev/null && categories="${categories},traceback"
  [ -n "$categories" ] || categories=unknown
  failed_task=unknown
  restore_one "$api_ca" api-ca
  restore_one "$api_cert" api-cert
  restore_one "$api_key" api-key
  restore_one "$api_bundle" api-bundle
  restore_one "$api_combined" api-combined
  restore_one "$custom_local_ca" local-ca
  restore_one "$custom_mark" custom-mark
  restore_one "$config" config
  restore_one "$kolla" kolla
  hostnamectl set-hostname "$(cat "$backup/hostname")" || true
  update-ca-certificates >/dev/null 2>&1 || true
  ip=$(ip -4 route get 1.1.1.1 | awk '{for(i=1;i<=NF;i++) if($i=="src") {print $(i+1); exit}}')
  python3 "$base/expose_coriolis.py" --use-address "$ip" >>"$reconfigure_log" 2>&1 || true
  rm -f "$reconfigure_log"
  echo "CERTIFICATE_APPLY_FAILED phase=$phase task=$failed_task categories=${categories#,} rollback=completed"
}
trap rollback ERR

phase=install_certificate_files
install -d -m 0755 "$cert_store" "$(dirname "$api_ca")" "$(dirname "$api_cert")" "$(dirname "$api_key")"
install -m 0644 "$transition_ca" "$api_ca"
cat "$stage/server.pem" "$work/intermediates.pem" > "$api_cert"
chmod 0644 "$api_cert"
install -m 0600 "$stage/server.key" "$api_key"
cat "$stage/server.pem" "$work/intermediates.pem" > "$api_bundle"
chmod 0644 "$api_bundle"
cat "$stage/server.key" "$stage/server.pem" "$work/intermediates.pem" > "$api_combined"
chmod 0600 "$api_combined"
install -m 0644 "$work/trust-anchor.pem" "$custom_local_ca"
update-ca-certificates >/dev/null
install -D -m 0644 /dev/null "$custom_mark"

metal_enabled=$(get_global_config_value coriolis_export_providers | grep metal || true)
if [ -n "$metal_enabled" ]; then
  install -m 0644 "$work/trust-anchor.pem" "$(get_global_config_value coriolis_metal_hub_ca_cert_path)"
  install -m 0644 "$api_cert" "$(get_global_config_value coriolis_metal_hub_client_cert_path)"
  install -m 0644 "$api_cert" "$(get_global_config_value coriolis_metal_hub_server_cert_path)"
  install -m 0600 "$stage/server.key" "$(get_global_config_value coriolis_metal_hub_server_key_path)"
  install -m 0600 "$stage/server.key" "$(get_global_config_value coriolis_metal_hub_client_key_path)"
fi

hostnamectl set-hostname "$fqdn"
python3 "$base/utils/set_config_value.py" -c "$config" -n reject_step_ca -v true
umask 077
phase=coriolis_reconfigure
ip=$(ip -4 route get 1.1.1.1 | awk '{for(i=1;i<=NF;i++) if($i=="src") {print $(i+1); exit}}')
test -n "$ip"
python3 "$base/expose_coriolis.py" --use-address "$ip" >"$reconfigure_log" 2>&1
phase=reload_tls_services
docker restart coriolis-web-proxy coriolis-api >>"$reconfigure_log" 2>&1
phase=verify_tls_services
active_fp=$(openssl x509 -in "$api_cert" -noout -fingerprint -sha256)
for port in 443 5000; do
  served_fp=""
  for _ in $(seq 1 30); do
    served_fp=$(timeout 10 openssl s_client -connect "$ip:$port" -servername "$fqdn" </dev/null 2>/dev/null | openssl x509 -noout -fingerprint -sha256 2>/dev/null || true)
    [ "$served_fp" = "$active_fp" ] && break
    sleep 2
  done
  test "$served_fp" = "$active_fp"
done
trap - ERR
fingerprint=$(openssl x509 -in "$api_cert" -noout -fingerprint -sha256 | cut -d= -f2)
rm -rf "$stage" "$backup"
rm -f "$reconfigure_log"
printf 'CERTIFICATE_INSTALLED fingerprint=%%s\n' "$fingerprint"
`, fqdn64)
}

func parseCSR(output string) (*x509.CertificateRequest, error) {
	for _, line := range strings.Split(output, "\n") {
		if !strings.HasPrefix(line, "CORIOLIS_CSR_B64=") {
			continue
		}
		data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(line, "CORIOLIS_CSR_B64="))
		if err != nil {
			return nil, fmt.Errorf("decode appliance CSR: %w", err)
		}
		block, _ := pem.Decode(data)
		if block == nil || block.Type != "CERTIFICATE REQUEST" {
			return nil, fmt.Errorf("appliance returned an invalid CSR PEM")
		}
		csr, err := x509.ParseCertificateRequest(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse appliance CSR: %w", err)
		}
		if err := csr.CheckSignature(); err != nil {
			return nil, fmt.Errorf("verify appliance CSR: %w", err)
		}
		return csr, nil
	}
	return nil, fmt.Errorf("appliance did not return a CSR")
}

func certificateFailureSummary(output string) string {
	const marker = "CERTIFICATE_APPLY_FAILED"
	start := strings.Index(output, marker)
	if start < 0 {
		return ""
	}
	end := start
	for end < len(output) && end-start < 480 {
		ch := output[end]
		if ch == '\n' || ch == '\r' || !(ch == ' ' || ch == ',' || ch == '-' || ch == '=' || ch == '_' || ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9') {
			break
		}
		end++
	}
	return strings.TrimSpace(output[start:end])
}

func (c *Cloud) ensureDirectCertificate(ctx context.Context, cfg CertificateConfig, zoneID, serverID, fqdn string) error {
	if !cfg.Enabled {
		return nil
	}
	if err := c.waitServerAgent(ctx, c.project, serverID); err != nil {
		return err
	}
	probe, err := c.runShellScriptWithOutput(ctx, c.project, serverID, certificateProbeScript(fqdn, cfg.RenewBeforeDays), false)
	if err != nil {
		return fmt.Errorf("inspect appliance certificate: %w", err)
	}
	if strings.Contains(probe, "CERTIFICATE_CURRENT") {
		writeInfo("appliance certificate is current")
		return nil
	}
	staged, err := c.runShellScriptWithOutput(ctx, c.project, serverID, certificateStagedProbeScript(fqdn, cfg.RenewBeforeDays), false)
	if err != nil {
		return fmt.Errorf("inspect staged appliance certificate: %w", err)
	}
	if strings.Contains(staged, "STAGED_CERTIFICATE_READY") {
		writeInfo("reusing valid staged appliance certificate")
	} else {
		csrOutput, err := c.runShellScriptWithOutput(ctx, c.project, serverID, certificateCSRScript(fqdn), false)
		if err != nil {
			return fmt.Errorf("create appliance certificate request: %w", err)
		}
		csr, err := parseCSR(csrOutput)
		if err != nil {
			return err
		}
		coversFQDN := false
		for _, name := range csr.DNSNames {
			if strings.EqualFold(strings.TrimSuffix(name, "."), strings.TrimSuffix(fqdn, ".")) {
				coversFQDN = true
				break
			}
		}
		if !coversFQDN {
			return fmt.Errorf("appliance CSR does not cover %s", fqdn)
		}
		var certificate, issuerCertificate []byte
		if err := progressAction(ctx, "Obtaining trusted certificate through ACME DNS-01", func() error {
			issued, issueErr := c.obtainCertificateForCSR(ctx, cfg, zoneID, fqdn, csr)
			if issueErr != nil {
				return issueErr
			}
			if len(issued.Certificate) == 0 || len(issued.IssuerCertificate) == 0 {
				return fmt.Errorf("ACME returned incomplete certificate material")
			}
			certificate = issued.Certificate
			issuerCertificate = issued.IssuerCertificate
			return nil
		}); err != nil {
			return err
		}
		if err := progressAction(ctx, "Staging trusted certificate on appliance", func() error {
			for name, contents := range map[string][]byte{"server.pem": certificate, "issuer.pem": issuerCertificate} {
				if _, stageErr := c.runShellScriptWithOutput(ctx, c.project, serverID, certificateStageFileScript(name, contents), false); stageErr != nil {
					return fmt.Errorf("stage %s on appliance: %w", name, stageErr)
				}
			}
			return nil
		}); err != nil {
			return err
		}
	}
	return progressAction(ctx, "Applying trusted certificate and reconfiguring Coriolis services", func() error {
		applyOutput, applyErr := c.runShellScriptWithOutput(ctx, c.project, serverID, certificateApplyScript(fqdn), false)
		if applyErr == nil {
			return nil
		}
		// The control plane can report an unknown terminal status although the
		// script completed. Confirm actual state before declaring failure.
		if retryableCommandError(applyErr) {
			confirmed, confirmErr := c.runShellScriptWithOutput(ctx, c.project, serverID, certificateProbeScript(fqdn, cfg.RenewBeforeDays), false)
			if confirmErr == nil && strings.Contains(confirmed, "CERTIFICATE_CURRENT") {
				writeInfo("appliance certificate installation confirmed after an ambiguous run-command status")
				return nil
			}
		}
		if summary := certificateFailureSummary(applyOutput); summary != "" {
			return fmt.Errorf("install appliance certificate (%s): %w", summary, applyErr)
		}
		return fmt.Errorf("install appliance certificate: %w", applyErr)
	})
}
