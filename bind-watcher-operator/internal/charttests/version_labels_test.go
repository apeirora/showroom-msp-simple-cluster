package charttests

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/yaml"
)

func TestVersionLabels(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("Helm is required; CI installs Helm")
	}
	root := filepath.Join("..", "..", "..", "charts")
	charts := []string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Name() != "_helpers.tpl" {
			return nil
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(source, []byte("app.kubernetes.io/version:")) || bytes.Contains(source, []byte("helm.sh/chart:")) {
			charts = append(charts, filepath.Dir(filepath.Dir(path)))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(charts) == 0 {
		t.Fatal("no version label charts found")
	}
	for _, chart := range charts {
		t.Run(strings.TrimPrefix(chart, root+string(filepath.Separator)), func(t *testing.T) {
			for _, version := range []string{"0.37.0", "0.37.0+abcdef", "0.37.0+" + strings.Repeat("a", 55) + "-z"} {
				t.Run(version, func(t *testing.T) {
					dir := t.TempDir()
					output, err := exec.Command("helm", "package", chart, "--version", version, "--app-version", version, "--destination", dir).CombinedOutput()
					if err != nil {
						t.Fatalf("package: %v\n%s", err, output)
					}
					packages, err := filepath.Glob(filepath.Join(dir, "*.tgz"))
					if err != nil || len(packages) != 1 {
						t.Fatalf("packages: %v, %v", packages, err)
					}
					args := []string{"template", "test", packages[0], "--set", "global.imageTag=0.37.0"}
					if filepath.Base(chart) == "sapprovider-operator" {
						args = append(args, "-f", filepath.Join(chart, "examples", "garden-dev.yaml"))
					}
					output, err = exec.Command("helm", args...).CombinedOutput()
					if err != nil {
						t.Fatalf("render: %v\n%s", err, output)
					}
					expected := strings.ReplaceAll(version, "+", "_")
					if len(expected) > 63 {
						expected = expected[:63]
					}
					expected = strings.TrimSuffix(expected, "-")
					decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(output), 4096)
					count := 0
					for {
						var resource map[string]interface{}
						err := decoder.Decode(&resource)
						if err == io.EOF {
							break
						}
						if err != nil {
							t.Fatal(err)
						}
						count += checkVersionLabels(t, resource, expected)
					}
					// The PM chart defines the helper but does not use it in a resource.
					if count == 0 && filepath.Base(chart) != "simple-cluster-pm-integration" {
						t.Fatal("no version labels rendered")
					}
				})
			}
		})
	}
}

func checkVersionLabels(t *testing.T, node interface{}, expected string) int {
	t.Helper()
	count := 0
	switch value := node.(type) {
	case map[string]interface{}:
		for key, child := range value {
			if key == "labels" {
				labels, ok := child.(map[string]interface{})
				if !ok {
					t.Fatalf("labels must be a map: %v", child)
				}
				for name, raw := range labels {
					label, ok := raw.(string)
					if !ok {
						t.Errorf("label %s must be a string: %v", name, raw)
						continue
					}
					if errors := validation.IsValidLabelValue(label); len(errors) != 0 {
						t.Errorf("invalid label %s=%q: %v", name, label, errors)
					}
					if name == "helm.sh/chart" || name == "app.kubernetes.io/version" {
						count++
						if label != expected {
							t.Errorf("label %s=%q, want %q", name, label, expected)
						}
					}
				}
			} else {
				count += checkVersionLabels(t, child, expected)
			}
		}
	case []interface{}:
		for _, child := range value {
			count += checkVersionLabels(t, child, expected)
		}
	}
	return count
}
