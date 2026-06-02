/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	ubuntuv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator-ubuntu/api/v1alpha1"
	"github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator-ubuntu/internal/k3s"
	sshpkg "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator-ubuntu/internal/ssh"
)

const (
	requeueReady   = 2 * time.Minute
	requeueWorking = 30 * time.Second

	hostPhasePending    = "Pending"
	hostPhaseInstalling = "Installing"
	hostPhaseReady      = "Ready"
	hostPhaseFailed     = "Failed"
)

// UbuntuClusterReconciler drives k3s install across the spec'd Ubuntu
// hosts and mirrors per-host progress back to status.
type UbuntuClusterReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder

	// DefaultK3sVersion is the operator-wide fallback k3s release tag
	// used when UbuntuCluster.spec.version is empty. Empty falls back
	// to the get.k3s.io installer default.
	DefaultK3sVersion string
}

// +kubebuilder:rbac:groups=ubuntu.apeirora.eu,resources=ubuntuclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ubuntu.apeirora.eu,resources=ubuntuclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ubuntu.apeirora.eu,resources=ubuntuclusters/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// nolint:gocyclo // sequential orchestration; splitting hides intent
func (r *UbuntuClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var cluster ubuntuv1alpha1.UbuntuCluster
	if err := r.Get(ctx, req.NamespacedName, &cluster); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !cluster.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &cluster)
	}

	if !controllerutil.ContainsFinalizer(&cluster, ubuntuv1alpha1.FinalizerName) {
		patch := client.MergeFrom(cluster.DeepCopy())
		controllerutil.AddFinalizer(&cluster, ubuntuv1alpha1.FinalizerName)
		if err := r.Patch(ctx, &cluster, patch); err != nil {
			return ctrl.Result{}, err
		}
	}

	if cluster.Status.Phase == "" {
		if err := r.setPhase(ctx, &cluster, ubuntuv1alpha1.PhaseInit, "Initialising", "operator picked up the CR"); err != nil {
			return ctrl.Result{}, err
		}
	}

	cred, err := r.readSSHCredential(ctx, &cluster)
	if err != nil {
		return r.fail(ctx, &cluster, "SSHKeyError", err.Error())
	}

	cps := cluster.Spec.ControlPlane.Hosts
	if len(cps) == 0 {
		return r.fail(ctx, &cluster, "InvalidSpec", "spec.controlPlane.hosts must not be empty")
	}

	k3sVer := cluster.Spec.Version
	if k3sVer == "" {
		k3sVer = r.DefaultK3sVersion
	}

	bootstrap := cps[0]
	// endpoint = external (kubeconfig URL, TLS SAN). Falls back to
	// bootstrap.ExternalAddress, then bootstrap.Address.
	endpoint := cluster.Spec.ControlPlaneEndpoint
	if endpoint == "" {
		endpoint = bootstrap.ExternalAddress
	}
	if endpoint == "" {
		endpoint = bootstrap.Address
	}
	// internalEndpoint = internal (k3s server-join + agent URL). Always
	// the bootstrap CP's internal Address.
	internalEndpoint := bootstrap.Address
	internalEndpointURL := apiURLFromEndpoint(internalEndpoint)

	hostStatuses := make([]ubuntuv1alpha1.HostStatus, 0, len(cps)+len(cluster.Spec.Workers.Hosts))
	ready := int32(0)
	allReady := true

	// Leave Init as soon as credentials + spec validate, so users see
	// CP_WKR / Phase=Provisioning while the (slow) install runs.
	if cluster.Status.Phase == ubuntuv1alpha1.PhaseInit {
		patch := client.MergeFrom(cluster.DeepCopy())
		cluster.Status.Phase = ubuntuv1alpha1.PhaseProvisioning
		setRoleCounts(&cluster, hostStatuses)
		if err := r.Status().Patch(ctx, &cluster, patch); err != nil {
			return ctrl.Result{}, err
		}
	}

	failHost := func(name, role, address, msg string) {
		hostStatuses = append(hostStatuses, ubuntuv1alpha1.HostStatus{
			Name: name, Role: role, Address: address,
			Phase: hostPhaseFailed, Message: msg,
		})
		allReady = false
	}

	// ----- bootstrap control-plane -----
	r.eventf(&cluster, corev1.EventTypeNormal, "PreparingHost",
		"control-plane bootstrap on %s (%s)", bootstrap.Name, bootstrap.Address)
	bootstrapClient, err := r.dialBootstrap(ctx, &cluster, bootstrap, cred)
	if err != nil {
		failHost(bootstrap.Name, ubuntuv1alpha1.RoleControlPlane, bootstrap.Address, err.Error())
		return r.partial(ctx, &cluster, endpoint, hostStatuses, ready, allReady)
	}
	defer bootstrapClient.Close()

	installed, err := k3s.IsInstalled(ctx, bootstrapClient)
	if err != nil {
		failHost(bootstrap.Name, ubuntuv1alpha1.RoleControlPlane, bootstrap.Address, "probe k3s: "+err.Error())
		return r.partial(ctx, &cluster, endpoint, hostStatuses, ready, allReady)
	}
	if !installed {
		r.eventf(&cluster, corev1.EventTypeNormal, "InstallingK3s",
			"installing k3s server (bootstrap) on %s", bootstrap.Name)
		log.Info("installing k3s server (bootstrap)", "host", bootstrap.Name)
		if err := k3s.InstallServerBootstrap(ctx, bootstrapClient, k3s.InstallOptions{
			Version:   k3sVer,
			ExtraArgs: cluster.Spec.K3sServerArgs,
			TLSSAN:    hostOnly(endpoint),
			NodeIP:    bootstrap.Address,
		}); err != nil {
			failHost(bootstrap.Name, ubuntuv1alpha1.RoleControlPlane, bootstrap.Address, err.Error())
			return r.partial(ctx, &cluster, endpoint, hostStatuses, ready, allReady)
		}
	}

	token, err := k3s.FetchToken(ctx, bootstrapClient)
	if err != nil {
		failHost(bootstrap.Name, ubuntuv1alpha1.RoleControlPlane, bootstrap.Address, "fetch token: "+err.Error())
		return r.partial(ctx, &cluster, endpoint, hostStatuses, ready, allReady)
	}

	kubeconfig, err := k3s.FetchKubeconfig(ctx, bootstrapClient, endpoint)
	if err != nil {
		failHost(bootstrap.Name, ubuntuv1alpha1.RoleControlPlane, bootstrap.Address, "fetch kubeconfig: "+err.Error())
		return r.partial(ctx, &cluster, endpoint, hostStatuses, ready, allReady)
	}
	secretName, err := r.ensureKubeconfigSecret(ctx, &cluster, []byte(kubeconfig))
	if err != nil {
		return r.fail(ctx, &cluster, "KubeconfigSyncError", err.Error())
	}

	k3sVersion, _ := k3s.ReportedVersion(ctx, bootstrapClient)
	hostStatuses = append(hostStatuses, ubuntuv1alpha1.HostStatus{
		Name:           bootstrap.Name,
		Role:           ubuntuv1alpha1.RoleControlPlane,
		Address:        bootstrap.Address,
		Phase:          hostPhaseReady,
		KubeletVersion: k3sVersion,
	})
	ready++

	// Bootstrap CP is up — flush K3s, kubeconfig ref and CP_WKR counts
	// to status so they don't stay empty while workers install.
	{
		patch := client.MergeFrom(cluster.DeepCopy())
		cluster.Status.ControlPlaneEndpoint = endpoint
		cluster.Status.K3sVersion = k3sVersion
		cluster.Status.KubeconfigSecretRef = &corev1.LocalObjectReference{Name: secretName}
		cluster.Status.HostStatuses = hostStatuses
		cluster.Status.ReadyHosts = ready
		cluster.Status.Hosts = int32(len(hostStatuses))
		setRoleCounts(&cluster, hostStatuses)
		if err := r.Status().Patch(ctx, &cluster, patch); err != nil {
			return ctrl.Result{}, err
		}
	}

	// ----- additional control-plane nodes -----
	for _, h := range cps[1:] {
		r.eventf(&cluster, corev1.EventTypeNormal, "PreparingHost",
			"control-plane join %s (%s)", h.Name, h.Address)
		ok, ver, msg := r.installRole(ctx, &cluster, bootstrapClient, h, cred, ubuntuv1alpha1.RoleControlPlane, func(c *sshpkg.Client) error {
			return k3s.InstallServerJoin(ctx, c, k3s.InstallOptions{
				Version:   k3sVer,
				ExtraArgs: cluster.Spec.K3sServerArgs,
				TLSSAN:    hostOnly(endpoint),
				NodeIP:    h.Address,
			}, internalEndpointURL, token)
		})
		hostStatuses = append(hostStatuses, ubuntuv1alpha1.HostStatus{
			Name: h.Name, Role: ubuntuv1alpha1.RoleControlPlane, Address: h.Address,
			Phase: phaseFromOK(ok), Message: msg, KubeletVersion: ver,
		})
		if ok {
			ready++
		} else {
			allReady = false
		}
	}

	// ----- workers -----
	for _, h := range cluster.Spec.Workers.Hosts {
		r.eventf(&cluster, corev1.EventTypeNormal, "PreparingHost",
			"worker join %s (%s)", h.Name, h.Address)
		ok, ver, msg := r.installRole(ctx, &cluster, bootstrapClient, h, cred, ubuntuv1alpha1.RoleWorker, func(c *sshpkg.Client) error {
			return k3s.InstallAgent(ctx, c, k3s.InstallOptions{
				Version:   k3sVer,
				ExtraArgs: cluster.Spec.K3sAgentArgs,
				NodeIP:    h.Address,
			}, internalEndpointURL, token)
		})
		hostStatuses = append(hostStatuses, ubuntuv1alpha1.HostStatus{
			Name: h.Name, Role: ubuntuv1alpha1.RoleWorker, Address: h.Address,
			Phase: phaseFromOK(ok), Message: msg, KubeletVersion: ver,
		})
		if ok {
			ready++
		} else {
			allReady = false
		}
	}

	// ----- terminal status patch -----
	prevPhase := cluster.Status.Phase
	patch := client.MergeFrom(cluster.DeepCopy())
	cluster.Status.ControlPlaneEndpoint = endpoint
	cluster.Status.K3sVersion = k3sVersion
	cluster.Status.KubeconfigSecretRef = &corev1.LocalObjectReference{Name: secretName}
	cluster.Status.HostStatuses = hostStatuses
	cluster.Status.ReadyHosts = ready
	cluster.Status.Hosts = int32(len(hostStatuses))
	setRoleCounts(&cluster, hostStatuses)
	cluster.Status.ObservedGeneration = cluster.Generation
	if allReady {
		cluster.Status.Phase = ubuntuv1alpha1.PhaseReady
		apimeta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
			Type:    ubuntuv1alpha1.ConditionReady,
			Status:  metav1.ConditionTrue,
			Reason:  "AllHostsReady",
			Message: "every host reached Ready",
		})
		apimeta.RemoveStatusCondition(&cluster.Status.Conditions, ubuntuv1alpha1.ConditionError)
	} else {
		cluster.Status.Phase = ubuntuv1alpha1.PhaseProvisioning
		apimeta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
			Type:    ubuntuv1alpha1.ConditionReady,
			Status:  metav1.ConditionFalse,
			Reason:  "HostsNotReady",
			Message: fmt.Sprintf("%d / %d hosts Ready", ready, len(hostStatuses)),
		})
	}
	if err := r.Status().Patch(ctx, &cluster, patch); err != nil {
		return ctrl.Result{}, err
	}

	if allReady && prevPhase != ubuntuv1alpha1.PhaseReady {
		r.eventf(&cluster, corev1.EventTypeNormal, "InstallationCompleted",
			"k3s installed on all %d hosts (CP %s, WKR %s)", len(hostStatuses), cluster.Status.CP, cluster.Status.WKR)
	}

	if allReady {
		return ctrl.Result{RequeueAfter: requeueReady}, nil
	}
	return ctrl.Result{RequeueAfter: requeueWorking}, nil
}

// dialBootstrap dials the bootstrap CP directly using its external
// address (falling back to Address if no external is set). Emits
// success/failure events so operators can spot auth or network issues
// from `kubectl describe`.
func (r *UbuntuClusterReconciler) dialBootstrap(ctx context.Context, cluster *ubuntuv1alpha1.UbuntuCluster, h ubuntuv1alpha1.Host, cred sshpkg.Credential) (*sshpkg.Client, error) {
	target := h.ExternalAddress
	if target == "" {
		target = h.Address
	}
	c, err := sshpkg.Dial(ctx, target, int(cluster.Spec.SSH.Port), cluster.Spec.SSH.User, cred)
	if err != nil {
		r.eventf(cluster, corev1.EventTypeWarning, "SSHLoginFailed",
			"ssh %s@%s: %s", cluster.Spec.SSH.User, target, err.Error())
		return nil, err
	}
	r.eventf(cluster, corev1.EventTypeNormal, "SSHLoginSucceeded",
		"ssh %s@%s ok (host %s)", cluster.Spec.SSH.User, target, h.Name)
	return c, nil
}

// dialVia dials a non-bootstrap host through the bootstrap CP as a
// ProxyJump bastion — uses the host's internal Address.
func (r *UbuntuClusterReconciler) dialVia(ctx context.Context, cluster *ubuntuv1alpha1.UbuntuCluster, bastion *sshpkg.Client, h ubuntuv1alpha1.Host, cred sshpkg.Credential) (*sshpkg.Client, error) {
	c, err := sshpkg.DialVia(ctx, bastion, h.Address, int(cluster.Spec.SSH.Port), cluster.Spec.SSH.User, cred)
	if err != nil {
		r.eventf(cluster, corev1.EventTypeWarning, "SSHLoginFailed",
			"ssh %s@%s via bastion: %s", cluster.Spec.SSH.User, h.Address, err.Error())
		return nil, err
	}
	r.eventf(cluster, corev1.EventTypeNormal, "SSHLoginSucceeded",
		"ssh %s@%s ok via bastion (host %s)", cluster.Spec.SSH.User, h.Address, h.Name)
	return c, nil
}

// partial writes whatever status we have so far (used when one host
// fails mid-loop) and requeues quickly to retry.
func (r *UbuntuClusterReconciler) partial(
	ctx context.Context,
	cluster *ubuntuv1alpha1.UbuntuCluster,
	endpoint string,
	hostStatuses []ubuntuv1alpha1.HostStatus,
	ready int32,
	_ bool,
) (ctrl.Result, error) {
	patch := client.MergeFrom(cluster.DeepCopy())
	cluster.Status.ControlPlaneEndpoint = endpoint
	cluster.Status.HostStatuses = hostStatuses
	cluster.Status.ReadyHosts = ready
	cluster.Status.Hosts = int32(len(hostStatuses))
	setRoleCounts(cluster, hostStatuses)
	cluster.Status.Phase = ubuntuv1alpha1.PhaseProvisioning
	apimeta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
		Type:    ubuntuv1alpha1.ConditionReady,
		Status:  metav1.ConditionFalse,
		Reason:  "HostFailure",
		Message: "at least one host failed — see status.hostStatuses",
	})
	if err := r.Status().Patch(ctx, cluster, patch); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: requeueWorking}, nil
}

func (r *UbuntuClusterReconciler) reconcileDelete(ctx context.Context, cluster *ubuntuv1alpha1.UbuntuCluster) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	if !controllerutil.ContainsFinalizer(cluster, ubuntuv1alpha1.FinalizerName) {
		return ctrl.Result{}, nil
	}

	cred, err := r.readSSHCredential(ctx, cluster)
	if err != nil {
		log.Error(err, "delete: SSH key unreadable — finalizer kept; user may need to remove it manually")
		return ctrl.Result{RequeueAfter: requeueWorking}, nil
	}

	cps := cluster.Spec.ControlPlane.Hosts
	if len(cps) == 0 {
		// Nothing to talk to — let finalizer fall off.
		patch := client.MergeFrom(cluster.DeepCopy())
		controllerutil.RemoveFinalizer(cluster, ubuntuv1alpha1.FinalizerName)
		return ctrl.Result{}, r.Patch(ctx, cluster, patch)
	}
	bootstrap := cps[0]

	// Open bastion to bootstrap CP; workers + extra CPs go via it.
	bastion, err := r.dialBootstrap(ctx, cluster, bootstrap, cred)
	if err != nil {
		log.Error(err, "delete: bastion dial failed; will retry")
		return ctrl.Result{RequeueAfter: requeueWorking}, nil
	}
	defer bastion.Close()

	// Best effort: agents first (so workers stop calling the CP),
	// then non-bootstrap servers, then bootstrap last. Failures are
	// logged but do not block finalizer removal forever — the next
	// reconcile retries.
	allOK := true
	for _, h := range cluster.Spec.Workers.Hosts {
		r.eventf(cluster, corev1.EventTypeNormal, "UninstallingK3s",
			"agent uninstall on %s", h.Name)
		if err := r.uninstallVia(ctx, cluster, bastion, h, cred, false); err != nil {
			log.Error(err, "uninstall agent failed", "host", h.Name)
			allOK = false
		}
	}
	for _, h := range cps[1:] {
		r.eventf(cluster, corev1.EventTypeNormal, "UninstallingK3s",
			"server uninstall on %s", h.Name)
		if err := r.uninstallVia(ctx, cluster, bastion, h, cred, true); err != nil {
			log.Error(err, "uninstall server failed", "host", h.Name)
			allOK = false
		}
	}
	// Bootstrap last — its bastion session is dropped right after.
	r.eventf(cluster, corev1.EventTypeNormal, "UninstallingK3s",
		"server uninstall on %s", bootstrap.Name)
	if err := k3s.UninstallServer(ctx, bastion); err != nil {
		log.Error(err, "uninstall bootstrap server failed", "host", bootstrap.Name)
		allOK = false
	}

	if !allOK {
		return ctrl.Result{RequeueAfter: requeueWorking}, nil
	}

	patch := client.MergeFrom(cluster.DeepCopy())
	controllerutil.RemoveFinalizer(cluster, ubuntuv1alpha1.FinalizerName)
	if err := r.Patch(ctx, cluster, patch); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *UbuntuClusterReconciler) uninstallVia(
	ctx context.Context,
	cluster *ubuntuv1alpha1.UbuntuCluster,
	bastion *sshpkg.Client,
	h ubuntuv1alpha1.Host,
	cred sshpkg.Credential,
	server bool,
) error {
	c, err := r.dialVia(ctx, cluster, bastion, h, cred)
	if err != nil {
		return err
	}
	defer c.Close()
	if server {
		return k3s.UninstallServer(ctx, c)
	}
	return k3s.UninstallAgent(ctx, c)
}

// readSSHCredential loads the host login credential from the referenced
// Secret: either an SSH private key (`kubernetes.io/ssh-auth`) or a
// password (`kubernetes.io/basic-auth`), depending on which ref the spec
// carries. The SSHAuth CEL guarantees exactly one ref is set.
func (r *UbuntuClusterReconciler) readSSHCredential(ctx context.Context, cluster *ubuntuv1alpha1.UbuntuCluster) (sshpkg.Credential, error) {
	auth := cluster.Spec.SSH
	switch {
	case auth.PrivateKeySecretRef != nil:
		data, err := r.readSecretKey(ctx, cluster.Namespace, auth.PrivateKeySecretRef.Name, ubuntuv1alpha1.SSHSecretPrivateKeyKey)
		if err != nil {
			return sshpkg.Credential{}, err
		}
		return sshpkg.Credential{PrivateKeyPEM: data}, nil
	case auth.PasswordSecretRef != nil:
		data, err := r.readSecretKey(ctx, cluster.Namespace, auth.PasswordSecretRef.Name, ubuntuv1alpha1.SSHSecretPasswordKey)
		if err != nil {
			return sshpkg.Credential{}, err
		}
		return sshpkg.Credential{Password: string(data)}, nil
	default:
		return sshpkg.Credential{}, fmt.Errorf("spec.ssh has neither privateKeySecretRef nor passwordSecretRef")
	}
}

// readSecretKey fetches one data key out of a Secret in the given namespace.
func (r *UbuntuClusterReconciler) readSecretKey(ctx context.Context, namespace, name, dataKey string) ([]byte, error) {
	var secret corev1.Secret
	key := types.NamespacedName{Name: name, Namespace: namespace}
	if err := r.Get(ctx, key, &secret); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("ssh Secret %s not found", key.String())
		}
		return nil, fmt.Errorf("get ssh Secret %s: %w", key.String(), err)
	}
	v, ok := secret.Data[dataKey]
	if !ok || len(v) == 0 {
		return nil, fmt.Errorf("Secret %s has no %q", key.String(), dataKey)
	}
	return v, nil
}

// ensureKubeconfigSecret writes the rendered kubeconfig into
// `<cluster>-kubeconfig`, owner-referenced for cascade cleanup.
func (r *UbuntuClusterReconciler) ensureKubeconfigSecret(ctx context.Context, cluster *ubuntuv1alpha1.UbuntuCluster, kubeconfig []byte) (string, error) {
	name := cluster.Name + ubuntuv1alpha1.KubeconfigSecretSuffix
	desired := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: cluster.Namespace,
			Labels: map[string]string{
				"ubuntucluster.apeirora.eu/cluster": cluster.Name,
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{ubuntuv1alpha1.KubeconfigSecretKey: kubeconfig},
	}
	if err := controllerutil.SetControllerReference(cluster, desired, r.Scheme); err != nil {
		return "", fmt.Errorf("set owner ref: %w", err)
	}

	var existing corev1.Secret
	err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: cluster.Namespace}, &existing)
	if apierrors.IsNotFound(err) {
		if cerr := r.Create(ctx, desired); cerr != nil {
			return "", fmt.Errorf("create Secret %s/%s: %w", cluster.Namespace, name, cerr)
		}
		return name, nil
	}
	if err != nil {
		return "", fmt.Errorf("get Secret %s/%s: %w", cluster.Namespace, name, err)
	}
	if string(existing.Data[ubuntuv1alpha1.KubeconfigSecretKey]) == string(kubeconfig) {
		return name, nil
	}
	existing.Data = desired.Data
	existing.Type = desired.Type
	if existing.Labels == nil {
		existing.Labels = map[string]string{}
	}
	for k, v := range desired.Labels {
		existing.Labels[k] = v
	}
	if err := controllerutil.SetControllerReference(cluster, &existing, r.Scheme); err != nil {
		return "", fmt.Errorf("set owner ref: %w", err)
	}
	if err := r.Update(ctx, &existing); err != nil {
		return "", fmt.Errorf("update Secret %s/%s: %w", cluster.Namespace, name, err)
	}
	return name, nil
}

func (r *UbuntuClusterReconciler) setPhase(ctx context.Context, cluster *ubuntuv1alpha1.UbuntuCluster, phase ubuntuv1alpha1.Phase, reason, msg string) error {
	if cluster.Status.Phase == phase {
		return nil
	}
	patch := client.MergeFrom(cluster.DeepCopy())
	cluster.Status.Phase = phase
	apimeta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
		Type:    ubuntuv1alpha1.ConditionReady,
		Status:  metav1.ConditionFalse,
		Reason:  reason,
		Message: msg,
	})
	return r.Status().Patch(ctx, cluster, patch)
}

func (r *UbuntuClusterReconciler) fail(ctx context.Context, cluster *ubuntuv1alpha1.UbuntuCluster, reason, msg string) (ctrl.Result, error) {
	patch := client.MergeFrom(cluster.DeepCopy())
	cluster.Status.Phase = ubuntuv1alpha1.PhaseFailed
	apimeta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
		Type:    ubuntuv1alpha1.ConditionError,
		Status:  metav1.ConditionTrue,
		Reason:  reason,
		Message: msg,
	})
	if err := r.Status().Patch(ctx, cluster, patch); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: requeueWorking}, nil
}

func (r *UbuntuClusterReconciler) eventf(cluster *ubuntuv1alpha1.UbuntuCluster, eventType, reason, format string, args ...any) {
	if r.Recorder == nil {
		return
	}
	r.Recorder.Eventf(cluster, eventType, reason, format, args...)
}

// installRole drives one secondary host (additional CP or worker): dial
// via the bootstrap bastion, probe for an existing k3s install, install
// if missing, return current reported version. apply receives an open
// SSH client and runs the installer (server-join or agent).
func (r *UbuntuClusterReconciler) installRole(
	ctx context.Context,
	cluster *ubuntuv1alpha1.UbuntuCluster,
	bastion *sshpkg.Client,
	h ubuntuv1alpha1.Host,
	cred sshpkg.Credential,
	role string,
	apply func(*sshpkg.Client) error,
) (ok bool, version, message string) {
	c, err := r.dialVia(ctx, cluster, bastion, h, cred)
	if err != nil {
		return false, "", err.Error()
	}
	defer c.Close()

	installed, err := k3s.IsInstalled(ctx, c)
	if err != nil {
		return false, "", "probe k3s: " + err.Error()
	}
	if !installed {
		r.eventf(cluster, corev1.EventTypeNormal, "InstallingK3s",
			"installing k3s %s on %s", role, h.Name)
		if err := apply(c); err != nil {
			return false, "", err.Error()
		}
	}
	ver, _ := k3s.ReportedVersion(ctx, c)
	return true, ver, ""
}

// setRoleCounts populates the CP / WKR printer columns as
// "<ready>/<declared>" strings, computed from the spec (declared) and
// observed hostStatuses (ready).
func setRoleCounts(cluster *ubuntuv1alpha1.UbuntuCluster, hostStatuses []ubuntuv1alpha1.HostStatus) {
	cpDeclared := len(cluster.Spec.ControlPlane.Hosts)
	wkrDeclared := len(cluster.Spec.Workers.Hosts)
	var cpReady, wkrReady int
	for _, h := range hostStatuses {
		if h.Phase != hostPhaseReady {
			continue
		}
		switch h.Role {
		case ubuntuv1alpha1.RoleControlPlane:
			cpReady++
		case ubuntuv1alpha1.RoleWorker:
			wkrReady++
		}
	}
	cluster.Status.CP = fmt.Sprintf("%d/%d", cpReady, cpDeclared)
	cluster.Status.WKR = fmt.Sprintf("%d/%d", wkrReady, wkrDeclared)
}

func phaseFromOK(ok bool) string {
	if ok {
		return hostPhaseReady
	}
	return hostPhaseFailed
}

// hostOnly strips the :port suffix from a host:port endpoint. k3s
// `--tls-san` wants a bare hostname or IP.
func hostOnly(addr string) string {
	if i := strings.LastIndex(addr, ":"); i > 0 {
		return addr[:i]
	}
	return addr
}

// apiURLFromEndpoint returns "https://host:port" with port 6443 default.
func apiURLFromEndpoint(addr string) string {
	if !strings.Contains(addr, ":") {
		addr = addr + ":6443"
	}
	return "https://" + addr
}

func (r *UbuntuClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&ubuntuv1alpha1.UbuntuCluster{}).
		Named("ubuntucluster").
		Complete(r)
}
