// Package controller reflects KCP APIBindings onto MSP namespace labels.
package controller

import (
	apisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	"github.com/m-szalik/goutils"

	"k8s.io/client-go/kubernetes/scheme"
)

// Scheme is shared by the KCP source clients (APIBinding) and the MSP target
// client (core Namespace, already registered in the client-go scheme).
var Scheme = scheme.Scheme

func init() {
	goutils.ExitOnError(apisv1alpha1.AddToScheme(Scheme), 1)
}
