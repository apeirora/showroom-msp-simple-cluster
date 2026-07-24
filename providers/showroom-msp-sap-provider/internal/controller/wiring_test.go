/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	sapv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/providers/sap-provider/api/v1alpha1"
)

// Verifies the manager wiring — in particular that the generic ShootReconciler
// registers via For(r.New()) with an interface-typed object, and that the
// NamespaceReconciler's two-flavor Watches build. Reconcile() unit specs
// exercise behaviour but never touch SetupWithManager, where a bad For() or
// duplicate controller name only surfaces at Complete().
var _ = Describe("Manager wiring", func() {
	It("registers both flavor reconcilers and the namespace reconciler", func() {
		mgr, err := ctrl.NewManager(cfg, ctrl.Options{
			Scheme:  scheme.Scheme,
			Metrics: metricsserver.Options{BindAddress: "0"},
		})
		Expect(err).NotTo(HaveOccurred())

		Expect((&ShootReconciler{
			Client: mgr.GetClient(), Scheme: mgr.GetScheme(),
			SCPCName:       openstackSCPCName,
			New:            func() FlavorShoot { return &sapv1alpha1.OpenStackShoot{} },
			ControllerName: "openstackshoot",
		}).SetupWithManager(mgr)).To(Succeed())

		Expect((&ShootReconciler{
			Client: mgr.GetClient(), Scheme: mgr.GetScheme(),
			SCPCName:       ironcoreSCPCName,
			New:            func() FlavorShoot { return &sapv1alpha1.IronCoreShoot{} },
			ControllerName: "ironcoreshoot",
		}).SetupWithManager(mgr)).To(Succeed())

		Expect((&NamespaceReconciler{
			Client: mgr.GetClient(), Scheme: mgr.GetScheme(),
			KubeconfigSource:        KubeconfigSourceGlobal,
			SimpleClusterExportName: DefaultSimpleClusterExportName,
			SapExportName:           DefaultSapExportName,
		}).SetupWithManager(mgr)).To(Succeed())
	})
})
