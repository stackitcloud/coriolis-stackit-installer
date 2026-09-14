package installer

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	iaas "github.com/stackitcloud/stackit-sdk-go/services/iaas/v2api"
	"golang.org/x/crypto/ssh"
)

type helperSSHAccess struct {
	keypairName string
	signer      ssh.Signer
	publicIPID  string
	publicIP    string
	securityID  string
}

func (c *Cloud) createHelperSSHAccess(ctx context.Context, projectID, sha string) (*helperSSHAccess, error) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		return nil, err
	}
	random := make([]byte, 4)
	if _, err := rand.Read(random); err != nil {
		return nil, err
	}
	name := fmt.Sprintf("coriolis-normalizer-%s-%x", sha[:8], random)
	publicKey := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey()))) + " " + name
	payload := iaas.NewCreateKeyPairPayload(publicKey)
	payload.SetName(name)
	payload.SetLabels(map[string]interface{}{
		"managed-by":      "coriolis-installer",
		"purpose":         "image-normalization",
		"project-id":      projectID,
		"coriolis-sha256": sha[:32],
	})
	if _, err := c.api.DefaultAPI.CreateKeyPair(ctx).CreateKeyPairPayload(*payload).Execute(); err != nil {
		return nil, fmt.Errorf("create temporary helper keypair: %w", err)
	}

	sg := SecurityGroupConfig{
		Name: "coriolis-normalizer-ssh-" + sha[:12],
		Ingress: []IngressRule{{
			Protocol:    "tcp",
			Port:        22,
			CIDR:        "0.0.0.0/0",
			Description: "Temporary installer SSH upload",
		}},
	}
	securityID, err := c.ensureSecurityGroupInProject(ctx, projectID, sg)
	if err != nil {
		_ = c.api.DefaultAPI.DeleteKeyPair(ctx, name).Execute()
		return nil, fmt.Errorf("create temporary helper security group: %w", err)
	}
	return &helperSSHAccess{keypairName: name, signer: signer, securityID: securityID}, nil
}

func (c *Cloud) cleanupStaleHelperAccess(ctx context.Context, projectID, sha string) error {
	for {
		pending := false
		ips, err := c.api.DefaultAPI.ListPublicIPs(ctx, projectID, c.region).Execute()
		if err != nil {
			return err
		}
		for _, publicIP := range ips.Items {
			if fmt.Sprint(publicIP.Labels["managed-by"]) != "coriolis-installer" ||
				fmt.Sprint(publicIP.Labels["purpose"]) != "image-normalization" ||
				fmt.Sprint(publicIP.Labels["coriolis-sha256"]) != sha[:32] {
				continue
			}
			if publicIP.GetNetworkInterface() != "" {
				pending = true
				continue
			}
			if err := c.api.DefaultAPI.DeletePublicIP(ctx, projectID, c.region, publicIP.GetId()).Execute(); err != nil {
				return fmt.Errorf("delete stale helper public IP %s: %w", publicIP.GetId(), err)
			}
		}
		if !pending {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(c.poll):
		}
	}
	keypairs, err := c.api.DefaultAPI.ListKeyPairs(ctx).Execute()
	if err != nil {
		return err
	}
	for _, keypair := range keypairs.Items {
		if fmt.Sprint(keypair.Labels["managed-by"]) == "coriolis-installer" &&
			fmt.Sprint(keypair.Labels["purpose"]) == "image-normalization" &&
			fmt.Sprint(keypair.Labels["project-id"]) == projectID &&
			fmt.Sprint(keypair.Labels["coriolis-sha256"]) == sha[:32] {
			if err := c.api.DefaultAPI.DeleteKeyPair(ctx, keypair.GetName()).Execute(); err != nil {
				return fmt.Errorf("delete stale helper keypair %s: %w", keypair.GetName(), err)
			}
		}
	}
	groups, err := c.api.DefaultAPI.ListSecurityGroups(ctx, projectID, c.region).Execute()
	if err != nil {
		return err
	}
	wantedName := "coriolis-normalizer-ssh-" + sha[:12]
	for _, group := range groups.Items {
		if group.GetName() == wantedName && fmt.Sprint(group.Labels["managed-by"]) == "coriolis-installer" {
			if err := c.api.DefaultAPI.DeleteSecurityGroup(ctx, projectID, c.region, group.GetId()).Execute(); err != nil {
				return fmt.Errorf("delete stale helper security group %s: %w", group.GetId(), err)
			}
		}
	}
	return nil
}

func (c *Cloud) assignHelperPublicIP(ctx context.Context, projectID, serverID, networkID, sha string, access *helperSSHAccess) error {
	server, err := c.api.DefaultAPI.GetServer(ctx, projectID, c.region, serverID).Execute()
	if err != nil {
		return err
	}
	nicID := ""
	for _, nic := range server.GetNics() {
		if nic.GetNetworkId() == networkID {
			nicID = nic.GetNicId()
			break
		}
	}
	if nicID == "" {
		nics, err := c.api.DefaultAPI.ListNics(ctx, projectID, c.region, networkID).Execute()
		if err != nil {
			return err
		}
		for _, nic := range nics.Items {
			if nic.GetDevice() == serverID {
				nicID = nic.GetId()
				break
			}
		}
	}
	if nicID == "" {
		return fmt.Errorf("temporary helper %s has no NIC in network %s", serverID, networkID)
	}
	payload := iaas.NewCreatePublicIPPayload()
	payload.SetNetworkInterface(nicID)
	payload.SetLabels(map[string]interface{}{
		"managed-by":      "coriolis-installer",
		"purpose":         "image-normalization",
		"coriolis-sha256": sha[:32],
	})
	publicIP, err := c.api.DefaultAPI.CreatePublicIP(ctx, projectID, c.region).CreatePublicIPPayload(*payload).Execute()
	if err != nil {
		return fmt.Errorf("create temporary helper public IP: %w", err)
	}
	access.publicIPID = publicIP.GetId()
	access.publicIP = publicIP.GetIp()
	return nil
}

func parseHelperHostKey(output string) (ssh.PublicKey, error) {
	for _, line := range strings.Split(output, "\n") {
		if !strings.HasPrefix(line, "HELPER_HOST_KEY_B64=") {
			continue
		}
		data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(line, "HELPER_HOST_KEY_B64="))
		if err != nil {
			return nil, err
		}
		key, _, _, _, err := ssh.ParseAuthorizedKey(data)
		if err != nil {
			return nil, fmt.Errorf("parse helper SSH host key: %w", err)
		}
		return key, nil
	}
	return nil, fmt.Errorf("helper did not return its SSH host key")
}

func (c *Cloud) prepareHelperTransfer(ctx context.Context, projectID, serverID, scratchVolumeID string) (ssh.PublicKey, error) {
	if len(scratchVolumeID) < 20 {
		return nil, fmt.Errorf("invalid scratch volume ID")
	}
	script := strings.ReplaceAll(`#!/bin/bash
set -euo pipefail
scratch_disk=$(lsblk -dnpo NAME,SERIAL | awk '$2 ~ /^__SCRATCH_SERIAL__/ {print $1}')
[ -b "$scratch_disk" ]
apt-get update -qq
DEBIAN_FRONTEND=noninteractive apt-get install -y -qq qemu-utils
if ! blkid "$scratch_disk" >/dev/null 2>&1; then mkfs.ext4 -F "$scratch_disk"; fi
mkdir -p /mnt/coriolis-scratch
mountpoint -q /mnt/coriolis-scratch || mount "$scratch_disk" /mnt/coriolis-scratch
install -d -o ubuntu -g ubuntu -m 0700 /mnt/coriolis-scratch/incoming
printf 'HELPER_HOST_KEY_B64='
base64 -w0 /etc/ssh/ssh_host_ed25519_key.pub
printf '\nHELPER_TRANSFER_READY\n'
`, "__SCRATCH_SERIAL__", scratchVolumeID[:20])
	// The command output contains the pinned host key used internally by the
	// installer. Keep that machine-readable value out of the user-facing log.
	output, err := c.runShellScriptWithOutput(ctx, projectID, serverID, script, false)
	if err != nil {
		return nil, err
	}
	hostKey, err := parseHelperHostKey(output)
	if err != nil {
		return nil, err
	}
	writeInfo("temporary helper transfer endpoint is ready and its SSH host key is pinned")
	return hostKey, nil
}

func dialPinnedSSH(ctx context.Context, address string, signer ssh.Signer, hostKey ssh.PublicKey) (*ssh.Client, error) {
	config := &ssh.ClientConfig{
		User:              "ubuntu",
		Auth:              []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyAlgorithms: []string{hostKey.Type()},
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			if !bytes.Equal(key.Marshal(), hostKey.Marshal()) {
				return fmt.Errorf("temporary helper SSH host key mismatch")
			}
			return nil
		},
		Timeout: 30 * time.Second,
	}
	var last error
	for {
		dialer := net.Dialer{Timeout: 30 * time.Second}
		conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(address, "22"))
		if err == nil {
			clientConn, channels, requests, handshakeErr := ssh.NewClientConn(conn, net.JoinHostPort(address, "22"), config)
			if handshakeErr == nil {
				return ssh.NewClient(clientConn, channels, requests), nil
			}
			conn.Close()
			err = handshakeErr
		}
		last = err
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("connect to temporary helper over SSH: %v: %w", last, ctx.Err())
		case <-time.After(10 * time.Second):
		}
	}
}

func (c *Cloud) uploadOVAToHelper(ctx context.Context, access *helperSSHAccess, ovaPath string, info OVAInfo, hostKey ssh.PublicKey, attempts int) error {
	var last error
	for attempt := 1; attempt <= attempts; attempt++ {
		client, err := dialPinnedSSH(ctx, access.publicIP, access.signer, hostKey)
		if err != nil {
			return err
		}
		statusSession, err := client.NewSession()
		if err != nil {
			client.Close()
			return err
		}
		statusOutput, err := statusSession.CombinedOutput("final=/mnt/coriolis-scratch/incoming/source.vmdk; partial=$final.tmp; expected=" + strconv.FormatInt(info.DiskFileBytes, 10) + "; if [ -f \"$final\" ] && [ \"$(stat -c %s \"$final\")\" = \"$expected\" ]; then echo COMPLETE; elif [ -f \"$partial\" ]; then stat -c %s \"$partial\"; else echo 0; fi")
		statusSession.Close()
		if err != nil {
			client.Close()
			return fmt.Errorf("inspect partial helper upload: %w", err)
		}
		status := strings.TrimSpace(string(statusOutput))
		if status == "COMPLETE" {
			client.Close()
			writeInfo("VMDK is already completely staged on temporary helper")
			return nil
		}
		offset, err := strconv.ParseInt(status, 10, 64)
		if err != nil || offset < 0 || offset > info.DiskFileBytes {
			resetSession, sessionErr := client.NewSession()
			if sessionErr == nil {
				sessionErr = resetSession.Run("rm -f /mnt/coriolis-scratch/incoming/source.vmdk.tmp")
				resetSession.Close()
			}
			if sessionErr != nil {
				client.Close()
				return fmt.Errorf("reset invalid partial helper upload: %w", sessionErr)
			}
			offset = 0
		}
		disk, size, err := openOVADisk(ovaPath, info)
		if err != nil {
			client.Close()
			return err
		}
		if offset > 0 {
			writeInfo("resuming helper upload at %.2f GiB", float64(offset)/(1024*1024*1024))
			if _, err := io.CopyN(io.Discard, disk, offset); err != nil {
				disk.Close()
				client.Close()
				return fmt.Errorf("seek OVA stream to resume offset: %w", err)
			}
		}
		session, err := client.NewSession()
		if err != nil {
			client.Close()
			disk.Close()
			return err
		}
		progress := &progressReader{reader: disk, total: size, transferred: offset, started: time.Now(), lastReport: time.Now(), attempt: attempt, attempts: attempts}
		session.Stdin = progress
		redirect := ">"
		if offset > 0 {
			redirect = ">>"
		}
		err = session.Run("set -e; umask 077; cat " + redirect + " /mnt/coriolis-scratch/incoming/source.vmdk.tmp")
		progress.finish(err)
		session.Close()
		disk.Close()
		if err == nil {
			verify, verifyErr := client.NewSession()
			if verifyErr == nil {
				verifyErr = verify.Run("set -e; partial=/mnt/coriolis-scratch/incoming/source.vmdk.tmp; test \"$(stat -c %s \"$partial\")\" = " + strconv.FormatInt(size, 10) + "; mv \"$partial\" /mnt/coriolis-scratch/incoming/source.vmdk")
				verify.Close()
			}
			client.Close()
			if verifyErr == nil {
				return nil
			}
			err = verifyErr
		} else {
			client.Close()
		}
		last = err
		if attempt < attempts {
			writeWarning("temporary helper upload failed; retrying from the beginning: %v", err)
		}
	}
	return fmt.Errorf("upload OVA disk to temporary helper: %w", last)
}

func (c *Cloud) cleanupHelperSSHAccess(ctx context.Context, projectID string, access *helperSSHAccess) {
	if access == nil {
		return
	}
	if access.publicIPID != "" {
		if err := c.api.DefaultAPI.DeletePublicIP(ctx, projectID, c.region, access.publicIPID).Execute(); err != nil {
			writeWarning("delete temporary helper public IP: %v", err)
		}
	}
	if access.keypairName != "" {
		if err := c.api.DefaultAPI.DeleteKeyPair(ctx, access.keypairName).Execute(); err != nil {
			writeWarning("delete temporary helper keypair: %v", err)
		}
	}
	if access.securityID != "" {
		if err := c.api.DefaultAPI.DeleteSecurityGroup(ctx, projectID, c.region, access.securityID).Execute(); err != nil {
			writeWarning("delete temporary helper security group: %v", err)
		}
	}
}
