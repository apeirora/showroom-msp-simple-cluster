package charttests

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"

	"sigs.k8s.io/yaml"
)

// TestSyncAgentVendoredDependency ensures Helm loads the declared dependency,
// rather than an obsolete archive that cannot receive the configured alias values.
// It reads local chart metadata only and fails on missing, duplicate, malformed,
// or mismatched packages; it does not download charts or alter the filesystem.
func TestSyncAgentVendoredDependency(t *testing.T) {
	chartDir := filepath.Join("..", "..", "..", "charts", "simple-cluster-sync-agent")
	manifest, err := os.ReadFile(filepath.Join(chartDir, "Chart.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var parent struct {
		Dependencies []struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"dependencies"`
	}
	if err := yaml.Unmarshal(manifest, &parent); err != nil {
		t.Fatal(err)
	}
	version := ""
	for _, dependency := range parent.Dependencies {
		if dependency.Name == "api-syncagent" {
			version = dependency.Version
		}
	}
	if version == "" {
		t.Fatal("api-syncagent dependency has no declared version")
	}
	archives, err := filepath.Glob(filepath.Join(chartDir, "charts", "api-syncagent-*.tgz"))
	if err != nil {
		t.Fatal(err)
	}
	expected := filepath.Join(chartDir, "charts", "api-syncagent-"+version+".tgz")
	if len(archives) != 1 || archives[0] != expected {
		t.Fatalf("vendored api-syncagent must match declared version %s: found %v; run helm dependency update", version, archives)
	}
	archive, err := os.Open(expected)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	compressed, err := gzip.NewReader(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer compressed.Close()
	reader := tar.NewReader(compressed)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			t.Fatal("vendored api-syncagent archive has no Chart.yaml")
		}
		if err != nil {
			t.Fatal(err)
		}
		if header.Name != "api-syncagent/Chart.yaml" {
			continue
		}
		metadata, err := io.ReadAll(io.LimitReader(reader, 1024*1024))
		if err != nil {
			t.Fatal(err)
		}
		var packaged struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		}
		if err := yaml.Unmarshal(metadata, &packaged); err != nil {
			t.Fatal(err)
		}
		if packaged.Name != "api-syncagent" || packaged.Version != version {
			t.Fatalf("archive metadata %s@%s does not match api-syncagent@%s", packaged.Name, packaged.Version, version)
		}
		return
	}
}
