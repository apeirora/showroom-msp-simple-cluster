// crdcheck installs rendered CustomResourceDefinitions into a real
// kube-apiserver (envtest) and fails when the apiserver rejects one.
//
// Usage: crdcheck <envtest-binary-dir> <dir-with-rendered-yaml>...
//
// helm lint and helm template cannot see what the apiserver checks at
// install time. The CEL cost budget of x-kubernetes-validations is the
// prominent case: it depends on the Kubernetes version and on the bounds
// of every list and string the rule touches. Release 0.43.0 shipped an
// UbuntuCluster CRD that both MSP clusters rejected for exactly that
// reason while every lint and template step was green.
//
// The directories hold the output of helm template. envtest reads every
// YAML document in them and skips everything that is not a CRD.
package main

import (
	"fmt"
	"os"

	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: crdcheck <envtest-binary-dir> <rendered-dir>...")
		os.Exit(2)
	}
	env := &envtest.Environment{
		BinaryAssetsDirectory: os.Args[1],
		CRDDirectoryPaths:     os.Args[2:],
		ErrorIfCRDPathMissing: true,
	}
	if _, err := env.Start(); err != nil {
		fmt.Println("CRD INSTALL FAILED:", err)
		_ = env.Stop()
		os.Exit(1)
	}
	fmt.Printf("accepted %d CRDs\n", len(env.CRDs))
	for _, crd := range env.CRDs {
		fmt.Println("  ok", crd.GetName())
	}
	if err := env.Stop(); err != nil {
		fmt.Println("stop:", err)
		os.Exit(1)
	}
}
