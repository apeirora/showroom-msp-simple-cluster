package charttests

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
)

// TestKCPCredentialMounts validates the vendored charts' credential mounts with
// isolated Helm settings. It renders local archives without relying on a user's
// registered repositories or downloading dependencies into the source checkout.
func TestKCPCredentialMounts(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("Helm is required; the chart CI job runs this test with Helm installed")
	}
	helmHome := t.TempDir()
	for key, path := range map[string]string{
		"HELM_CONFIG_HOME":       filepath.Join(helmHome, "config"),
		"HELM_CACHE_HOME":        filepath.Join(helmHome, "cache"),
		"HELM_DATA_HOME":         filepath.Join(helmHome, "data"),
		"HELM_REPOSITORY_CONFIG": filepath.Join(helmHome, "repositories.yaml"),
		"HELM_REPOSITORY_CACHE":  filepath.Join(helmHome, "repository-cache"),
	} {
		t.Setenv(key, path)
	}
	lockPath := filepath.Join("..", "..", "..", "charts", "simple-cluster-sync-agent", "Chart.lock")
	originalLock, originalErr := os.ReadFile(lockPath)
	if originalErr != nil && !os.IsNotExist(originalErr) {
		t.Fatal(originalErr)
	}
	t.Cleanup(func() {
		currentLock, currentErr := os.ReadFile(lockPath)
		if currentErr != nil && !os.IsNotExist(currentErr) {
			t.Error(currentErr)
			return
		}
		if os.IsNotExist(originalErr) != os.IsNotExist(currentErr) || !bytes.Equal(originalLock, currentLock) {
			t.Error("chart rendering must not create or modify Chart.lock; refresh dependencies explicitly")
		}
	})
	for _, chart := range []string{"bind-watcher-operator", "simple-cluster-sync-agent"} {
		t.Run(chart, func(t *testing.T) {
			path := filepath.Join("..", "..", "..", "charts", chart)
			for _, custom := range []bool{false, true} {
				args := []string{"template", "test", path}
				secret := "kcp-kubeconfig"
				if chart == "simple-cluster-sync-agent" {
					secret = "pm-kubeconfig"
				}
				if custom {
					secret = "custom-kcp"
					if chart == "bind-watcher-operator" {
						args = append(args, "--set", "KCP.kubeconfig.secretName="+secret, "--set", "imagePullSecrets[0].name=registry-auth")
					} else {
						args = append(args, "--set", "syncAgentOperator.kcpKubeconfig="+secret, "--set", "syncAgentOperator.extraVolumes[0].secret.secretName="+secret, "--set", "syncAgentOperator.extraVolumes[0].name=kcp-credentials", "--set", "syncAgentOperator.extraVolumes[0].secret.defaultMode=292", "--set", "global.imagePullSecrets[0]=registry-auth")
					}
				}
				output, err := exec.Command("helm", args...).CombinedOutput()
				if err != nil {
					t.Fatalf("render: %v\n%s", err, output)
				}
				decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(output), 4096)
				found := false
				for {
					var deployment appsv1.Deployment
					err := decoder.Decode(&deployment)
					if err == io.EOF {
						break
					}
					if err != nil {
						t.Fatal(err)
					}
					if deployment.Kind != "Deployment" {
						continue
					}
					found = true
					pod := deployment.Spec.Template.Spec
					if custom && (len(pod.ImagePullSecrets) != 1 || pod.ImagePullSecrets[0].Name != "registry-auth") {
						t.Fatal("imagePullSecrets lost")
					}
					mounted := false
					configured := false
					for _, container := range pod.Containers {
						for _, env := range container.Env {
							if env.Name == "KCP_KUBECONFIG" {
								configured = env.Value == "/etc/kcp/kubeconfig"
							}
						}
						for _, arg := range container.Args {
							if strings.HasPrefix(arg, "--kcp-kubeconfig=") {
								configured = arg == "--kcp-kubeconfig=/etc/kcp/kubeconfig"
							}
						}
						for _, mount := range container.VolumeMounts {
							if mount.MountPath != "/etc/kcp" {
								continue
							}
							mounted = true
							if !mount.ReadOnly || mount.SubPath != "" || mount.SubPathExpr != "" {
								t.Fatal("credentials require a read-only whole mount")
							}
							volumeFound := false
							for _, volume := range pod.Volumes {
								if volume.Name != mount.Name {
									continue
								}
								volumeFound = true
								s := volume.Secret
								if s == nil || s.SecretName != secret || len(s.Items) != 0 || s.DefaultMode == nil || *s.DefaultMode != 0444 {
									t.Fatalf("invalid Secret volume: %+v", s)
								}
							}
							if !volumeFound {
								t.Fatal("missing Secret volume")
							}
						}
					}
					if !mounted || !configured {
						t.Fatalf("mount=%t kubeconfig=%t", mounted, configured)
					}
				}
				if !found {
					t.Fatal("missing Deployment")
				}
			}
		})
	}
}
