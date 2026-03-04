package libvirt

import (
	"errors"
	"io"
	"os"
	"path/filepath"
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
