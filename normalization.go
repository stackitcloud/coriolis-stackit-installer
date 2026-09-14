package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"time"

	iaas "github.com/stackitcloud/stackit-sdk-go/services/iaas/v2api"
)

const normalizeApplianceScript = `#!/bin/bash
set -euo pipefail

source_disk=$(lsblk -dnpo NAME,SERIAL | awk '$2 ~ /^__SOURCE_SERIAL__/ {print $1}')
scratch_disk=$(lsblk -dnpo NAME,SERIAL | awk '$2 ~ /^__SCRATCH_SERIAL__/ {print $1}')
[ -n "$source_disk" ] || { echo "appliance source disk not found" >&2; exit 22; }
[ -n "$scratch_disk" ] || { echo "scratch disk not found" >&2; exit 23; }

target=/mnt/coriolis
mkdir -p "$target"
root_part=""
while read -r part; do
  mount -o ro "$part" "$target" 2>/dev/null || continue
  if [ -f "$target/etc/os-release" ] && [ -d "$target/opt" ]; then
    root_part="$part"
    umount "$target"
    break
  fi
  umount "$target"
done < <(lsblk -nrpo NAME,TYPE "$source_disk" | awk '$2 == "part" {print $1}')
[ -n "$root_part" ] || { echo "appliance root partition not found" >&2; exit 24; }

mount "$root_part" "$target"
trap 'umount "$target" 2>/dev/null || true' EXIT
test -s "$target/usr/bin/systemctl"
test -x "$target/usr/bin/systemctl"
package=/tmp/stackit-server-agent.deb
curl -fsS http://169.254.169.254/stackit/v1/server-agent/amd64/deb -o "$package"
dpkg-deb -x "$package" "$target"
test -x "$target/opt/stackit/server-agent/stackit-server-agent"
test -f "$target/etc/systemd/system/stackit-server-agent.service"
test -s "$target/usr/bin/systemctl"
mkdir -p "$target/etc/systemd/system/multi-user.target.wants"
ln -sfn /etc/systemd/system/stackit-server-agent.service \
  "$target/etc/systemd/system/multi-user.target.wants/stackit-server-agent.service"

# Remove only machine-specific agent and cloud-init state. Coriolis services,
# SSH/support configuration, appliance identity and application data stay intact.
rm -f "$target/opt/stackit/server-agent/local.log"
rm -f "$target/opt/stackit/server-agent/serviceToken.txt"
rm -rf "$target/var/lib/cloud/instances/"*
rm -f "$target/var/lib/cloud/instance"
rm -f "$target/var/lib/cloud/data/instance-id"
rm -f "$target/var/lib/cloud/data/previous-instance-id"
rm -f "$target/var/lib/cloud/data/previous-datasource"
rm -rf "$target/var/lib/cloud/seed/nocloud" "$target/var/lib/cloud/seed/nocloud-net"
printf '%s\n' 'datasource_list: [ OpenStack, ConfigDrive, NoCloud ]' > \
  "$target/etc/cloud/cloud.cfg.d/91-stackit.cfg"
sync
echo "NORMALIZATION_OK source=$source_disk scratch=$scratch_disk root=$root_part"
`

const importVMDKScript = `#!/bin/bash
set -euo pipefail
source_disk=$(lsblk -dnpo NAME,SERIAL | awk '$2 ~ /^__SOURCE_SERIAL__/ {print $1}')
scratch_disk=$(lsblk -dnpo NAME,SERIAL | awk '$2 ~ /^__SCRATCH_SERIAL__/ {print $1}')
[ -b "$source_disk" ] && [ -b "$scratch_disk" ]
scratch=/mnt/coriolis-scratch
mountpoint -q "$scratch" || mount "$scratch_disk" "$scratch"
input=$scratch/incoming/source.vmdk
test -s "$input"
echo "Converting vendor VMDK directly onto perf12 normalization volume..."
qemu-img convert -p -f vmdk -O raw "$input" "$source_disk"
sync
blockdev --rereadpt "$source_disk" || true
lsblk "$source_disk"
rm -f "$input"
echo VMDK_IMPORT_OK
`

const exportApplianceScript = `#!/bin/bash
set -euo pipefail
upload_url=$(printf '%s' '__UPLOAD_URL_B64__' | base64 -d)
source_disk=$(lsblk -dnpo NAME,SERIAL | awk '$2 ~ /^__SOURCE_SERIAL__/ {print $1}')
scratch_disk=$(lsblk -dnpo NAME,SERIAL | awk '$2 ~ /^__SCRATCH_SERIAL__/ {print $1}')
[ -n "$source_disk" ] && [ -n "$scratch_disk" ]

apt-get update -qq
DEBIAN_FRONTEND=noninteractive apt-get install -y -qq qemu-utils curl
scratch=/mnt/coriolis-scratch
output=$scratch/coriolis-normalized.qcow2
if ! blkid "$scratch_disk" >/dev/null 2>&1; then mkfs.ext4 -F "$scratch_disk"; fi
mkdir -p "$scratch"
mountpoint -q "$scratch" || mount "$scratch_disk" "$scratch"
trap 'umount "$scratch" 2>/dev/null || true' EXIT
rm -f "$output"
echo "Converting normalized appliance disk..."
qemu-img convert -p -f raw -O qcow2 -o compat=1.1,lazy_refcounts=on "$source_disk" "$output"
qemu-img check -q "$output"
echo "Uploading normalized image..."
curl --fail --show-error --progress-bar --upload-file "$output" "$upload_url"
rm -f "$output"
sync
umount "$scratch"
trap - EXIT
echo IMAGE_UPLOAD_OK
`

const probeNormalizedApplianceScript = `#!/bin/bash
set -euo pipefail
source_disk=$(lsblk -dnpo NAME,SERIAL | awk '$2 ~ /^__SOURCE_SERIAL__/ {print $1}')
[ -b "$source_disk" ]
target=/mnt/coriolis-probe
mkdir -p "$target"
root_part=""
while read -r part; do
  mount -o ro "$part" "$target" 2>/dev/null || continue
  if [ -f "$target/etc/os-release" ] && [ -d "$target/opt" ]; then
    root_part="$part"
    break
  fi
  umount "$target"
done < <(lsblk -nrpo NAME,TYPE "$source_disk" | awk '$2 == "part" {print $1}')
trap 'umount "$target" 2>/dev/null || true' EXIT
[ -n "$root_part" ]
test -s "$target/usr/bin/systemctl"
test -x "$target/usr/bin/systemctl"
test -x "$target/opt/stackit/server-agent/stackit-server-agent"
test -f "$target/etc/systemd/system/stackit-server-agent.service"
test -L "$target/etc/systemd/system/multi-user.target.wants/stackit-server-agent.service"
echo NORMALIZATION_READY
`

func imagePayload(cfg Config, info OVAInfo, name string, labels map[string]interface{}, withAgent bool) *iaas.CreateImagePayload {
	p := iaas.NewCreateImagePayload("qcow2", name)
	p.SetLabels(labels)
	p.SetMinDiskSize(maxInt64(info.DiskGiB, cfg.Server.BootVolumeSize))
	p.SetMinRam(info.MemoryMiB)
	ic := iaas.NewImageConfig()
	ic.SetArchitecture("x86")
	ic.SetOperatingSystem("linux")
	ic.SetDiskBus(cfg.Image.DiskBus)
	ic.SetNicModel(cfg.Image.NICModel)
	if cfg.Image.UEFI != nil {
		ic.SetUefi(*cfg.Image.UEFI)
	}
	if cfg.Image.SecureBoot != nil {
		ic.SetSecureBoot(*cfg.Image.SecureBoot)
	}
	p.SetConfig(*ic)
	if withAgent {
		agent := iaas.NewImageAgent()
		agent.SetSupported(true)
		agent.SetProvisionByDefault(true)
		p.SetAgent(*agent)
	}
	return p
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func (c *Cloud) waitVolume(ctx context.Context, projectID, volumeID string) (*iaas.Volume, error) {
	for {
		volume, err := c.api.DefaultAPI.GetVolume(ctx, projectID, c.region, volumeID).Execute()
		if err != nil {
			return nil, err
		}
		switch strings.ToUpper(volume.GetStatus()) {
		case "AVAILABLE", "ATTACHED":
			return volume, nil
		case "ERROR", "ERROR_DELETING", "ERROR_RESTORING-BACKUP":
			return nil, fmt.Errorf("volume %s entered status %s", volumeID, volume.GetStatus())
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(c.poll):
		}
	}
}

func (c *Cloud) createNormalizationVolume(ctx context.Context, projectID, zone, name, sourceImageID, performanceClass string, size int64, purpose, sha string) (*iaas.Volume, error) {
	p := iaas.NewCreateVolumePayload(zone)
	p.SetName(name)
	p.SetPerformanceClass(performanceClass)
	p.SetSize(size)
	if sourceImageID != "" {
		p.SetSource(*iaas.NewVolumeSource(sourceImageID, "image"))
	}
	p.SetLabels(map[string]interface{}{"managed-by": "coriolis-installer", "purpose": purpose, "coriolis-sha256": sha[:32]})
	volume, err := c.api.DefaultAPI.CreateVolume(ctx, projectID, c.region).CreateVolumePayload(*p).Execute()
	if err != nil {
		return nil, err
	}
	return c.waitVolume(ctx, projectID, volume.GetId())
}

func (c *Cloud) findNormalizationVolume(ctx context.Context, projectID, sha, purpose, performanceClass string) (*iaas.Volume, error) {
	list, err := c.api.DefaultAPI.ListVolumes(ctx, projectID, c.region).Execute()
	if err != nil {
		return nil, err
	}
	for i := range list.Items {
		volume := &list.Items[i]
		if fmt.Sprint(volume.Labels["managed-by"]) != "coriolis-installer" ||
			fmt.Sprint(volume.Labels["purpose"]) != purpose ||
			fmt.Sprint(volume.Labels["coriolis-sha256"]) != sha[:32] ||
			volume.GetPerformanceClass() != performanceClass {
			continue
		}
		switch strings.ToUpper(volume.GetStatus()) {
		case "AVAILABLE", "ATTACHED", "CREATING", "DOWNLOADING":
			return volume, nil
		}
	}
	return nil, nil
}

func (c *Cloud) removeStaleNormalizationHelpers(ctx context.Context, projectID, sha string) error {
	list, err := c.api.DefaultAPI.ListServers(ctx, projectID, c.region).Execute()
	if err != nil {
		return err
	}
	for i := range list.Items {
		server := &list.Items[i]
		serverSHA := ""
		if value, ok := server.Labels["coriolis-sha256"]; ok && value != nil {
			serverSHA = fmt.Sprint(value)
		}
		if fmt.Sprint(server.Labels["managed-by"]) != "coriolis-installer" ||
			fmt.Sprint(server.Labels["purpose"]) != "image-normalization" ||
			(serverSHA != "" && serverSHA != sha[:12]) {
			continue
		}
		fmt.Fprintln(os.Stderr, "replacing stale normalization helper", server.GetId())
		if err := c.api.DefaultAPI.DeleteServer(ctx, projectID, c.region, server.GetId()).Execute(); err != nil {
			return fmt.Errorf("delete stale normalization helper %s: %w", server.GetId(), err)
		}
	}
	return nil
}

func (c *Cloud) findNormalizationHelper(ctx context.Context, projectID, sha string) (*iaas.Server, error) {
	list, err := c.api.DefaultAPI.ListServers(ctx, projectID, c.region).Execute()
	if err != nil {
		return nil, err
	}
	for i := range list.Items {
		server := &list.Items[i]
		if fmt.Sprint(server.Labels["managed-by"]) == "coriolis-installer" &&
			fmt.Sprint(server.Labels["purpose"]) == "image-normalization" &&
			fmt.Sprint(server.Labels["coriolis-sha256"]) == sha[:12] &&
			!strings.EqualFold(server.GetStatus(), "DELETED") {
			return server, nil
		}
	}
	return nil, nil
}

func (c *Cloud) normalizedVolumeReady(ctx context.Context, projectID, helperID, sourceVolumeID, scratchVolumeID string) bool {
	script, err := volumeScript(probeNormalizedApplianceScript, sourceVolumeID, scratchVolumeID)
	if err != nil {
		return false
	}
	output, err := c.runShellScript(ctx, projectID, helperID, script)
	return err == nil && strings.Contains(output, "NORMALIZATION_READY")
}

func (c *Cloud) cleanupNormalizationResources(ctx context.Context, projectID, helperID, sha string, access *helperSSHAccess, volumeIDs ...string) {
	if err := c.api.DefaultAPI.DeleteServer(ctx, projectID, c.region, helperID).Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "warning: delete normalization helper:", err)
	}
	for _, volumeID := range volumeIDs {
		if err := c.waitVolumeDetachedAndDelete(ctx, projectID, volumeID); err != nil {
			fmt.Fprintln(os.Stderr, "warning:", err)
		}
	}
	c.cleanupHelperSSHAccess(ctx, projectID, access)
	if err := c.cleanupStaleHelperAccess(ctx, projectID, sha); err != nil {
		fmt.Fprintln(os.Stderr, "warning: clean up normalization access:", err)
	}
}

func (c *Cloud) waitVolumeAvailable(ctx context.Context, projectID, volumeID string) (*iaas.Volume, error) {
	for {
		volume, err := c.api.DefaultAPI.GetVolume(ctx, projectID, c.region, volumeID).Execute()
		if err != nil {
			return nil, err
		}
		switch strings.ToUpper(volume.GetStatus()) {
		case "AVAILABLE":
			return volume, nil
		case "ERROR", "ERROR_DELETING", "ERROR_RESTORING-BACKUP":
			return nil, fmt.Errorf("volume %s entered status %s", volumeID, volume.GetStatus())
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(c.poll):
		}
	}
}

func (c *Cloud) waitServerAgent(ctx context.Context, projectID, serverID string) error {
	for {
		_, err := c.run.DefaultAPI.GetCommandTemplate(ctx, projectID, serverID, "RunShellScript", c.region).Execute()
		if err == nil {
			return nil
		}
		if strings.Contains(strings.ToLower(err.Error()), "service not enabled") {
			return fmt.Errorf("wait for STACKIT Server Agent on %s: Run Command service is not enabled in project %s", serverID, projectID)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for STACKIT Server Agent on %s: %w", serverID, ctx.Err())
		case <-time.After(c.poll):
		}
	}
}

func (c *Cloud) createNormalizationHelper(ctx context.Context, cfg Config, projectID, zone, networkID, securityGroupID, keypairName, sourceVolumeID, scratchVolumeID, suffix string) (*iaas.Server, error) {
	net := iaas.NewCreateServerNetworking()
	net.SetNetworkId(networkID)
	union := iaas.CreateServerNetworkingAsCreateServerPayloadAllOfNetworking(net)
	p := iaas.NewCreateServerPayload(cfg.Normalization.HelperMachineType, "coriolis-normalizer-"+suffix, union)
	p.SetAvailabilityZone(zone)
	p.SetLabels(map[string]interface{}{"managed-by": "coriolis-installer", "purpose": "image-normalization", "coriolis-sha256": suffix})
	p.SetSecurityGroups([]string{securityGroupID})
	p.SetKeypairName(keypairName)
	p.SetVolumes([]string{sourceVolumeID, scratchVolumeID})
	b := iaas.NewBootVolume()
	b.SetSource(*iaas.NewBootVolumeSource(cfg.Normalization.HelperImageID, "image"))
	b.SetSize(cfg.Normalization.HelperBootSizeGiB)
	b.SetPerformanceClass("storage_premium_perf1")
	b.SetDeleteOnTermination(true)
	p.SetBootVolume(*b)
	agent := iaas.NewServerAgent()
	agent.SetProvisioned(true)
	p.SetAgent(*agent)
	server, err := c.api.DefaultAPI.CreateServer(ctx, projectID, c.region).CreateServerPayload(*p).Execute()
	if err != nil {
		return nil, err
	}
	server, err = c.waitServerInProject(ctx, projectID, server.GetId())
	if err != nil {
		return nil, err
	}
	if err := c.waitServerAgent(ctx, projectID, server.GetId()); err != nil {
		return nil, err
	}
	return server, nil
}

func (c *Cloud) waitServerInProject(ctx context.Context, projectID, id string) (*iaas.Server, error) {
	for {
		server, err := c.api.DefaultAPI.GetServer(ctx, projectID, c.region, id).Execute()
		if err != nil {
			return nil, err
		}
		switch strings.ToUpper(server.GetStatus()) {
		case "ACTIVE":
			return server, nil
		case "ERROR", "DELETED":
			return nil, fmt.Errorf("server %s entered status %s", id, server.GetStatus())
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(c.poll):
		}
	}
}

func (c *Cloud) normalizeAndUploadImage(ctx context.Context, projectID string, cfg Config, info OVAInfo, zone, networkID string) (*iaas.Image, error) {
	short := info.SHA256[:12]
	diskSize := maxInt64(info.DiskGiB, cfg.Server.BootVolumeSize)
	source, err := c.findNormalizationVolume(ctx, projectID, info.SHA256, "image-normalization", cfg.Normalization.PerformanceClass)
	if err != nil {
		return nil, err
	}
	if source == nil {
		source, err = c.createNormalizationVolume(ctx, projectID, zone, "coriolis-normalization-source-"+short, "", cfg.Normalization.PerformanceClass, diskSize, "image-normalization", info.SHA256)
		if err != nil {
			return nil, fmt.Errorf("create normalization source volume: %w", err)
		}
	}
	scratch, err := c.findNormalizationVolume(ctx, projectID, info.SHA256, "image-normalization-scratch", cfg.Normalization.PerformanceClass)
	if err != nil {
		return nil, err
	}
	if scratch == nil {
		scratch, err = c.createNormalizationVolume(ctx, projectID, zone, "coriolis-normalization-scratch-"+short, "", cfg.Normalization.PerformanceClass, cfg.Normalization.ScratchSizeGiB, "image-normalization-scratch", info.SHA256)
		if err != nil {
			return nil, fmt.Errorf("create normalization scratch volume: %w", err)
		}
	}

	existingHelper, err := c.findNormalizationHelper(ctx, projectID, info.SHA256)
	if err != nil {
		return nil, err
	}
	if existingHelper != nil {
		existingHelper, err = c.waitServerInProject(ctx, projectID, existingHelper.GetId())
		if err == nil {
			err = c.waitServerAgent(ctx, projectID, existingHelper.GetId())
		}
		if err == nil && c.normalizedVolumeReady(ctx, projectID, existingHelper.GetId(), source.GetId(), scratch.GetId()) {
			fmt.Fprintln(os.Stderr, "resuming completed normalization on helper", existingHelper.GetId())
			image, exportErr := c.exportNormalizedVolume(ctx, projectID, existingHelper.GetId(), source.GetId(), scratch.GetId(), cfg, info)
			if exportErr != nil {
				return nil, exportErr
			}
			c.cleanupNormalizationResources(ctx, projectID, existingHelper.GetId(), info.SHA256, nil, source.GetId(), scratch.GetId())
			return image, nil
		}
	}
	if err := c.removeStaleNormalizationHelpers(ctx, projectID, info.SHA256); err != nil {
		return nil, err
	}
	if err := c.cleanupStaleHelperAccess(ctx, projectID, info.SHA256); err != nil {
		return nil, fmt.Errorf("clean up stale normalization access: %w", err)
	}
	for _, volume := range []*iaas.Volume{source, scratch} {
		if _, err := c.waitVolumeAvailable(ctx, projectID, volume.GetId()); err != nil {
			return nil, err
		}
	}
	access, err := c.createHelperSSHAccess(ctx, projectID, info.SHA256)
	if err != nil {
		return nil, err
	}
	helper, err := c.createNormalizationHelper(ctx, cfg, projectID, zone, networkID, access.securityID, access.keypairName, source.GetId(), scratch.GetId(), short)
	if err != nil {
		c.cleanupHelperSSHAccess(ctx, projectID, access)
		return nil, fmt.Errorf("create normalization helper: %w", err)
	}
	if err := c.assignHelperPublicIP(ctx, projectID, helper.GetId(), networkID, info.SHA256, access); err != nil {
		return nil, err
	}
	hostKey, err := c.prepareHelperTransfer(ctx, projectID, helper.GetId(), scratch.GetId())
	if err != nil {
		return nil, fmt.Errorf("prepare temporary helper transfer: %w", err)
	}
	fmt.Fprintln(os.Stderr, "streaming vendor VMDK from OVA to temporary helper", access.publicIP)
	if err := c.uploadOVAToHelper(ctx, access, cfg.OVA, info, hostKey, cfg.UploadAttempts); err != nil {
		return nil, err
	}
	importScript, err := volumeScript(importVMDKScript, source.GetId(), scratch.GetId())
	if err != nil {
		return nil, err
	}
	if _, err := c.runShellScript(ctx, projectID, helper.GetId(), importScript); err != nil {
		return nil, fmt.Errorf("convert VMDK on temporary helper: %w", err)
	}
	fmt.Fprintln(os.Stderr, "normalizing appliance on temporary helper", helper.GetId())
	normalizeScript, err := volumeScript(normalizeApplianceScript, source.GetId(), scratch.GetId())
	if err != nil {
		return nil, err
	}
	if _, err := c.runShellScript(ctx, projectID, helper.GetId(), normalizeScript); err != nil {
		return nil, fmt.Errorf("normalize appliance: %w", err)
	}

	image, err := c.exportNormalizedVolume(ctx, projectID, helper.GetId(), source.GetId(), scratch.GetId(), cfg, info)
	if err != nil {
		return nil, err
	}

	// Cleanup is intentionally performed only after the reusable image is
	// AVAILABLE. On an earlier failure, labelled resources remain resumable and
	// preserve evidence for diagnosis.
	c.cleanupNormalizationResources(ctx, projectID, helper.GetId(), info.SHA256, access, source.GetId(), scratch.GetId())
	return image, nil
}

func (c *Cloud) exportNormalizedVolume(ctx context.Context, projectID, helperServerID, sourceVolumeID, scratchVolumeID string, cfg Config, info OVAInfo) (*iaas.Image, error) {
	short := info.SHA256[:12]
	labels := map[string]interface{}{
		"managed-by":          "coriolis-installer",
		"coriolis-sha256":     info.SHA256[:32],
		"coriolis-normalized": "agent-v2",
	}
	payload := imagePayload(cfg, info, cfg.Image.NamePrefix+"-agent-v2-"+short, labels, true)
	imageImport, err := c.api.DefaultAPI.CreateImage(ctx, projectID, c.region).CreateImagePayload(*payload).Execute()
	if err != nil {
		return nil, fmt.Errorf("create normalized image: %w", err)
	}
	uploadB64 := base64.StdEncoding.EncodeToString([]byte(imageImport.GetUploadUrl()))
	script, err := volumeScript(exportApplianceScript, sourceVolumeID, scratchVolumeID)
	if err != nil {
		return nil, err
	}
	script = strings.Replace(script, "__UPLOAD_URL_B64__", uploadB64, 1)
	if _, err := c.runShellScript(ctx, projectID, helperServerID, script); err != nil {
		if deleteErr := c.api.DefaultAPI.DeleteImage(ctx, projectID, c.region, imageImport.GetId()).Execute(); deleteErr != nil {
			fmt.Fprintln(os.Stderr, "warning: delete failed normalized image import:", deleteErr)
		}
		return nil, fmt.Errorf("export normalized image: %w", err)
	}
	return c.waitImage(ctx, projectID, imageImport.GetId())
}

func volumeScript(script, sourceVolumeID, scratchVolumeID string) (string, error) {
	if len(sourceVolumeID) < 20 || len(scratchVolumeID) < 20 || sourceVolumeID == scratchVolumeID {
		return "", fmt.Errorf("invalid normalization volume IDs")
	}
	script = strings.ReplaceAll(script, "__SOURCE_SERIAL__", sourceVolumeID[:20])
	script = strings.ReplaceAll(script, "__SCRATCH_SERIAL__", scratchVolumeID[:20])
	return script, nil
}

func (c *Cloud) waitVolumeDetachedAndDelete(ctx context.Context, projectID, volumeID string) error {
	for {
		volume, err := c.api.DefaultAPI.GetVolume(ctx, projectID, c.region, volumeID).Execute()
		if err != nil {
			return err
		}
		if strings.EqualFold(volume.GetStatus(), "AVAILABLE") {
			if err := c.api.DefaultAPI.DeleteVolume(ctx, projectID, c.region, volumeID).Execute(); err != nil {
				return fmt.Errorf("delete temporary volume %s: %w", volumeID, err)
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(c.poll):
		}
	}
}
