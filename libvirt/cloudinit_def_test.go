package libvirt

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hooklift/iso9660"
)

func TestCloudInitTerraformKeyOps(t *testing.T) {
	ci := newCloudInitDef()

	volKey := "volume-key"

	terraformID := ci.buildTerraformKey(volKey)
	if terraformID == "" {
		t.Error("key should not be empty")
	}

	actualKey, _ := getCloudInitVolumeKeyFromTerraformID(terraformID)
	if actualKey != volKey {
		t.Error("wrong key returned")
	}
}

func TestCloudInitCreateISO(t *testing.T) {
	ci := defCloudInit{
		Name:          "test.iso",
		UserData:      "test user data",
		MetaData:      "test meta data",
		NetworkConfig: "test network config",
	}

	isoPath, err := ci.createISO()
	if err != nil {
		t.Fatalf("Unexpected error creating ISO: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(isoPath))

	check, err := exists(isoPath)
	if !check {
		t.Fatalf("ISO file not found: %v", err)
	}
}

// TestCloudInitCreateISOWithoutExternalTool verifies that ISO creation works
// without requiring any external tools (like mkisofs).
func TestCloudInitCreateISOWithoutExternalTool(t *testing.T) {
	origPath := os.Getenv("PATH")
	defer os.Setenv("PATH", origPath)

	// Restrict PATH so no external tools are accessible
	os.Setenv("PATH", "/")

	ci := defCloudInit{
		Name:          "test.iso",
		UserData:      "user data content",
		MetaData:      "meta data content",
		NetworkConfig: "network config content",
	}

	isoPath, err := ci.createISO()
	if err != nil {
		t.Fatalf("Expected ISO creation to succeed without external tools, got error: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(isoPath))

	if isoPath == "" {
		t.Error("Expected non-empty ISO path")
	}
}

func TestCloudInitISOContents(t *testing.T) {
	userData := "user data content"
	metaData := "meta data content"
	networkConfig := "network config content"

	ci := defCloudInit{
		Name:          "test.iso",
		UserData:      userData,
		MetaData:      metaData,
		NetworkConfig: networkConfig,
	}

	isoPath, err := ci.createISO()
	if err != nil {
		t.Fatalf("Unexpected error creating ISO: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(isoPath))

	isoFile, err := os.Open(isoPath)
	if err != nil {
		t.Fatalf("Cannot open ISO file: %v", err)
	}
	defer isoFile.Close()

	isoReader, err := iso9660.NewReader(isoFile)
	if err != nil {
		t.Fatalf("Error initializing ISO reader: %v", err)
	}

	found := map[string]string{}
	for {
		file, err := isoReader.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("Error reading ISO: %v", err)
		}
		dataBytes, err := io.ReadAll(file.Sys().(io.Reader))
		if err != nil {
			t.Fatalf("Error reading file %s: %v", file.Name(), err)
		}
		found[file.Name()] = string(dataBytes)
	}

	if got, ok := found["/user-data"]; !ok {
		t.Error("user-data not found in ISO")
	} else if got != userData {
		t.Errorf("user-data mismatch: got %q, want %q", got, userData)
	}

	if got, ok := found["/meta-data"]; !ok {
		t.Error("meta-data not found in ISO")
	} else if got != metaData {
		t.Errorf("meta-data mismatch: got %q, want %q", got, metaData)
	}

	if got, ok := found["/network-config"]; !ok {
		t.Error("network-config not found in ISO")
	} else if got != networkConfig {
		t.Errorf("network-config mismatch: got %q, want %q", got, networkConfig)
	}
}

// TestCloudInitISOVolumeLabel verifies the generated ISO has the correct
// volume label "cidata", which cloud-init uses to locate the NoCloud data source.
func TestCloudInitISOVolumeLabel(t *testing.T) {
	ci := defCloudInit{
		Name:          "test.iso",
		UserData:      "#cloud-config\nhostname: testhost\n",
		MetaData:      "instance-id: test-01\nlocal-hostname: testhost\n",
		NetworkConfig: "version: 2\n",
	}

	isoPath, err := ci.createISO()
	if err != nil {
		t.Fatalf("Unexpected error creating ISO: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(isoPath))

	isoFile, err := os.Open(isoPath)
	if err != nil {
		t.Fatalf("Cannot open ISO: %v", err)
	}
	defer isoFile.Close()

	// Read the Primary Volume Descriptor at sector 16 (byte 32768).
	// The Volume Identifier field is at PVD bytes 40-71 (32 bytes, space-padded).
	pvd := make([]byte, 2048)
	if _, err := isoFile.ReadAt(pvd, 16*2048); err != nil {
		t.Fatalf("Cannot read PVD: %v", err)
	}

	const wantLabel = "cidata"
	gotLabel := strings.TrimRight(string(pvd[40:72]), " ")
	if gotLabel != wantLabel {
		t.Errorf("ISO volume label = %q, want %q", gotLabel, wantLabel)
	}
}

// TestCloudInitISOReadback verifies the full round-trip: content written via
// createISO() can be read back correctly through the provider's own reading
// path (setCloudInitDataFromExistingCloudInitDisk).
func TestCloudInitISOReadback(t *testing.T) {
	wantUserData := "#cloud-config\nhostname: testhost\n"
	wantMetaData := "instance-id: test-01\nlocal-hostname: testhost\n"
	wantNetConfig := "version: 2\nethernets:\n  eth0:\n    dhcp4: true\n"

	ci := defCloudInit{
		Name:          "test.iso",
		UserData:      wantUserData,
		MetaData:      wantMetaData,
		NetworkConfig: wantNetConfig,
	}

	isoPath, err := ci.createISO()
	if err != nil {
		t.Fatalf("Unexpected error creating ISO: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(isoPath))

	isoFile, err := os.Open(isoPath)
	if err != nil {
		t.Fatalf("Cannot open ISO file: %v", err)
	}
	defer isoFile.Close()

	// Simulate what the provider does when refreshing state from an existing volume.
	var readBack defCloudInit
	if err := readBack.setCloudInitDataFromExistingCloudInitDisk(isoFile); err != nil {
		t.Fatalf("setCloudInitDataFromExistingCloudInitDisk error: %v", err)
	}

	if readBack.UserData != wantUserData {
		t.Errorf("UserData mismatch:\n got: %q\nwant: %q", readBack.UserData, wantUserData)
	}
	if readBack.MetaData != wantMetaData {
		t.Errorf("MetaData mismatch:\n got: %q\nwant: %q", readBack.MetaData, wantMetaData)
	}
	if readBack.NetworkConfig != wantNetConfig {
		t.Errorf("NetworkConfig mismatch:\n got: %q\nwant: %q", readBack.NetworkConfig, wantNetConfig)
	}
}

func exists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return true, err
}
