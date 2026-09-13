package main

import (
	"archive/tar"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestInspectOVA(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "test.ova")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(f)
	ovf := `<Envelope xmlns="http://schemas.dmtf.org/ovf/envelope/1" xmlns:ovf="http://schemas.dmtf.org/ovf/envelope/1" xmlns:rasd="http://schemas.dmtf.org/wbem/wscim/1/cim-schema/2/CIM_ResourceAllocationSettingData" xmlns:vmw="http://www.vmware.com/schema/ovf"><References><File ovf:id="f1" ovf:href="disk.vmdk"/></References><DiskSection><Disk ovf:diskId="d1" ovf:fileRef="f1" ovf:capacity="40"/></DiskSection><VirtualSystem><Name>test</Name><OperatingSystemSection><Description>Ubuntu</Description></OperatingSystemSection><VirtualHardwareSection><Item><rasd:ResourceType>3</rasd:ResourceType><rasd:VirtualQuantity>4</rasd:VirtualQuantity></Item><Item><rasd:ResourceType>4</rasd:ResourceType><rasd:VirtualQuantity>8192</rasd:VirtualQuantity></Item><vmw:Config vmw:key="firmware" vmw:value="bios"/></VirtualHardwareSection></VirtualSystem></Envelope>`
	for n, b := range map[string][]byte{"test.ovf": []byte(ovf), "disk.vmdk": []byte("disk")} {
		if err := tw.WriteHeader(&tar.Header{Name: n, Mode: 0600, Size: int64(len(b))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(b); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	i, err := inspectOVA(p)
	if err != nil {
		t.Fatal(err)
	}
	if i.VCPUs != 4 || i.MemoryMiB != 8192 || i.DiskGiB != 40 || i.Firmware != "bios" || i.DiskName != "disk.vmdk" {
		t.Fatalf("unexpected info: %+v", i)
	}
	if i.DiskFileBytes != 4 {
		t.Fatalf("unexpected disk file size: %d", i.DiskFileBytes)
	}
	disk, size, err := openOVADisk(p, i)
	if err != nil {
		t.Fatal(err)
	}
	defer disk.Close()
	contents, err := io.ReadAll(disk)
	if err != nil {
		t.Fatal(err)
	}
	if size != 4 || string(contents) != "disk" {
		t.Fatalf("unexpected streamed disk: size=%d contents=%q", size, contents)
	}
}
