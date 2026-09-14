package installer

import (
	"archive/tar"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

type OVAInfo struct {
	Name, OVFName, DiskName, SHA256, Firmware, OS string
	VCPUs, MemoryMiB, DiskGiB, DiskFileBytes      int64
}

type ovfEnvelope struct {
	Disks []struct {
		Capacity string `xml:"capacity,attr"`
		FileRef  string `xml:"fileRef,attr"`
	} `xml:"DiskSection>Disk"`
	Files []struct {
		ID   string `xml:"id,attr"`
		Href string `xml:"href,attr"`
	} `xml:"References>File"`
	System struct {
		Name string `xml:"Name"`
		OS   struct {
			Description string `xml:"Description"`
		} `xml:"OperatingSystemSection"`
		Items []struct {
			ResourceType string `xml:"ResourceType"`
			Quantity     string `xml:"VirtualQuantity"`
		} `xml:"VirtualHardwareSection>Item"`
		Configs []struct {
			Key   string `xml:"key,attr"`
			Value string `xml:"value,attr"`
		} `xml:"VirtualHardwareSection>Config"`
	} `xml:"VirtualSystem"`
}

func inspectOVA(path string) (OVAInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return OVAInfo{}, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return OVAInfo{}, fmt.Errorf("hash ova: %w", err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return OVAInfo{}, err
	}
	tr := tar.NewReader(f)
	var env ovfEnvelope
	var info OVAInfo
	for {
		hdr, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			return info, fmt.Errorf("read ova: %w", e)
		}
		if strings.HasSuffix(strings.ToLower(hdr.Name), ".ovf") {
			if err := xml.NewDecoder(io.LimitReader(tr, 16<<20)).Decode(&env); err != nil {
				return info, fmt.Errorf("parse ovf: %w", err)
			}
			info.OVFName = hdr.Name
		}
		if hdr.Name == info.DiskName {
			info.DiskFileBytes = hdr.Size
		}
	}
	info.SHA256 = hex.EncodeToString(h.Sum(nil))
	info.Name = env.System.Name
	info.OS = env.System.OS.Description
	for _, c := range env.System.Configs {
		if c.Key == "firmware" {
			info.Firmware = c.Value
		}
	}
	for _, i := range env.System.Items {
		q, _ := strconv.ParseInt(strings.TrimSpace(i.Quantity), 10, 64)
		if i.ResourceType == "3" {
			info.VCPUs = q
		}
		if i.ResourceType == "4" {
			info.MemoryMiB = q
		}
	}
	if len(env.Disks) != 1 {
		return info, fmt.Errorf("exactly one disk is required, found %d", len(env.Disks))
	}
	info.DiskGiB, _ = strconv.ParseInt(env.Disks[0].Capacity, 10, 64)
	for _, x := range env.Files {
		if x.ID == env.Disks[0].FileRef {
			info.DiskName = x.Href
		}
	}
	if info.OVFName == "" || info.DiskName == "" {
		return info, fmt.Errorf("OVA is missing OVF or referenced disk")
	}
	// The disk name is learned from the OVF, which may occur before or after the
	// disk member. Make a second lightweight TAR pass when its size was not seen.
	if info.DiskFileBytes == 0 {
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return info, err
		}
		tr = tar.NewReader(f)
		for {
			hdr, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return info, fmt.Errorf("read OVA disk metadata: %w", err)
			}
			if hdr.Name == info.DiskName {
				info.DiskFileBytes = hdr.Size
				break
			}
		}
	}
	if info.DiskFileBytes < 1 {
		return info, fmt.Errorf("OVA disk %q has no usable size", info.DiskName)
	}
	return info, nil
}

type ovaDiskReader struct {
	io.Reader
	file *os.File
}

func (r *ovaDiskReader) Close() error { return r.file.Close() }

// openOVADisk streams the vendor disk directly from the OVA. It does not
// extract or convert anything on the operator's machine.
func openOVADisk(path string, info OVAInfo) (io.ReadCloser, int64, error) {
	in, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	tr := tar.NewReader(in)
	for {
		hdr, e := tr.Next()
		if e == io.EOF {
			in.Close()
			return nil, 0, fmt.Errorf("disk %q not found", info.DiskName)
		}
		if e != nil {
			in.Close()
			return nil, 0, e
		}
		if hdr.Name != info.DiskName {
			continue
		}
		return &ovaDiskReader{Reader: io.LimitReader(tr, hdr.Size), file: in}, hdr.Size, nil
	}
}
