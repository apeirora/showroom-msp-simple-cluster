/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package main

import (
	"flag"
	"os"

	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	ubuntuv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator-ubuntu/api/v1alpha1"
	"github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator-ubuntu/internal/controller"
	"github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator-ubuntu/internal/openbao"
	"github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator-ubuntu/internal/secretpolicy"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(ubuntuv1alpha1.AddToScheme(scheme))
}

func main() {
	var metricsAddr, probeAddr, defaultK3sVersion string
	var enableLeaderElection bool
	flag.StringVar(&metricsAddr, "metrics-bind-address", "0", "metrics endpoint")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "health probe endpoint")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false, "enable leader election")
	flag.StringVar(&defaultK3sVersion, "default-k3s-version", "",
		"k3s release tag used when UbuntuCluster.spec.version is empty (e.g. v1.31.4+k3s1). Empty falls back to the get.k3s.io installer default.")
	opts := zap.Options{Development: true}
	opts.BindFlags(flag.CommandLine)
	var secretReferenceAllowlist string
	flag.StringVar(&secretReferenceAllowlist, "secret-reference-allowlist", "[]",
		"Exact operator-approved reference exceptions as a JSON list.")
	// OpenBao KMS tenancy (SimpleCluster #470). Unset address = encryption
	// requests are refused, never silently ignored.
	var bao openbao.Config
	flag.StringVar(&bao.Address, "openbao-address", "",
		"OpenBao URL (https://...) of the SimpleCluster KMS tenancy. Empty refuses every UbuntuCluster with spec.encryption.")
	flag.StringVar(&bao.Namespace, "openbao-namespace", "",
		"OpenBao namespace of the tenancy (X-Vault-Namespace); empty is the root namespace.")
	flag.StringVar(&bao.CAFile, "openbao-ca-file", "",
		"PEM file with extra CA certificates for the OpenBao address; also handed to the hosts' plugins.")
	flag.StringVar(&bao.TokenFile, "openbao-token-file", openbao.DefaultTokenFile,
		"Projected ServiceAccount token (audience bound to the JWT mount) used to log in.")
	flag.StringVar(&bao.JWTMount, "openbao-jwt-mount", openbao.DefaultJWTMount, "JWT auth mount path in the tenancy.")
	flag.StringVar(&bao.JWTRole, "openbao-jwt-role", openbao.DefaultJWTRole, "JWT auth role this operator logs in with.")
	flag.StringVar(&bao.TransitMount, "openbao-transit-mount", openbao.DefaultTransitMount, "Transit mount holding the per-cluster keys.")
	flag.StringVar(&bao.ApproleMount, "openbao-approle-mount", openbao.DefaultApproleMount, "AppRole mount holding the per-cluster host roles.")
	flag.StringVar(&bao.PluginPolicy, "openbao-plugin-policy", openbao.DefaultPluginPolicy, "The only policy host roles may carry.")
	flag.DurationVar(&bao.TokenPeriod, "openbao-token-period", openbao.DefaultTokenPeriod, "Periodic token lifetime for the hosts' plugins.")
	flag.DurationVar(&bao.SecretIDTTL, "openbao-secret-id-ttl", openbao.DefaultSecretIDTTL, "Validity of a host's AppRole secret ID.")
	flag.DurationVar(&bao.WrapTTL, "openbao-wrap-ttl", openbao.DefaultWrapTTL, "How long a host has to unwrap its secret ID.")
	flag.Parse()
	if bao.Enabled() {
		if _, err := openbao.New(bao); err != nil {
			setupLog.Error(err, "invalid OpenBao configuration")
			os.Exit(1)
		}
	}
	secretPolicy, err := secretpolicy.Parse(secretReferenceAllowlist)
	if err != nil {
		setupLog.Error(err, "invalid Secret reference configuration")
		os.Exit(1)
	}

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsserver.Options{BindAddress: metricsAddr},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "ubuntucluster.apeirora.eu",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err := (&controller.UbuntuClusterReconciler{
		SecretPolicy:      secretPolicy,
		Client:            mgr.GetClient(),
		Scheme:            mgr.GetScheme(),
		Recorder:          mgr.GetEventRecorderFor("ubuntucluster"),
		DefaultK3sVersion: defaultK3sVersion,
		OpenBao:           bao,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "UbuntuCluster")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
