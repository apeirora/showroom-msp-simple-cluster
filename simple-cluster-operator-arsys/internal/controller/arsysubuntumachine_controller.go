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
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	arsysv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/arsys-ubuntu-machine-operator/api/v1alpha1"
	"github.com/apeirora/showroom-msp-simple-cluster/arsys-ubuntu-machine-operator/internal/arsys"
	umv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/ubuntu-machines/api/v1alpha1"
)

const (
	requeueReady        = 5 * time.Minute
	requeueFailed       = 10 * time.Minute
	requeueProvisioning = 20 * time.Second
	requeueWorking      = 20 * time.Second
	requeueTransient    = 1 * time.Minute
	requeueDeleting     = 10 * time.Second

	// transientFailureThreshold is the number of consecutive transient errors
	// after which the machine is moved to Failed rather than silently retried.
	transientFailureThreshold = 5
)

// ArsysUbuntuMachineReconciler orders dedicated servers from Arsys
// CloudBuilder and registers each as a cluster-scoped UbuntuMachine it owns.
type ArsysUbuntuMachineReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=arsys.apeirora.eu,resources=arsysubuntumachines,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=arsys.apeirora.eu,resources=arsysubuntumachines/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=arsys.apeirora.eu,resources=arsysubuntumachines/finalizers,verbs=update
// +kubebuilder:rbac:groups=ubuntumachines.apeirora.eu,resources=ubuntumachines,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

func (r *ArsysUbuntuMachineReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var aum arsysv1alpha1.ArsysUbuntuMachine
	if err := r.Get(ctx, req.NamespacedName, &aum); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	cl, cerr := r.arsysClient(ctx, &aum)
	if cerr != nil {
		if !aum.DeletionTimestamp.IsZero() {
			// Can't reach the API to delete the server — keep the finalizer
			// and retry so we don't leak it.
			log.Error(cerr, "delete: arsys token unreadable; retrying")
			return ctrl.Result{RequeueAfter: requeueWorking}, nil
		}
		return r.fail(ctx, &aum, "TokenError", cerr.Error())
	}

	if !aum.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &aum, cl)
	}

	if controllerutil.AddFinalizer(&aum, arsysv1alpha1.FinalizerName) {
		if err := r.Update(ctx, &aum); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Order the server once, then poll it to readiness.
	server, err := r.ensureServer(ctx, &aum, cl)
	if err != nil {
		if isTransientServerError(err) {
			patch := client.MergeFrom(aum.DeepCopy())
			aum.Status.TransientErrors++
			log.Info("transient server error", "err", err, "attempt", aum.Status.TransientErrors)
			r.Recorder.Eventf(&aum, corev1.EventTypeWarning, "TransientError",
				"attempt %d: %s", aum.Status.TransientErrors, err.Error())
			if aum.Status.TransientErrors >= transientFailureThreshold {
				return r.fail(ctx, &aum, "TooManyRetries",
					fmt.Sprintf("giving up after %d transient errors: %v", aum.Status.TransientErrors, err))
			}
			_ = r.Status().Patch(ctx, &aum, patch)
			return ctrl.Result{RequeueAfter: requeueTransient}, nil
		}
		return r.fail(ctx, &aum, "ServerError", err.Error())
	}

	if server.Status.State != arsys.StatePoweredOn || server.PrimaryIP() == "" {
		log.Info("server provisioning", "id", server.ID, "state", server.Status.State, "percent", server.Status.Percent)
		if err := r.setProvisioning(ctx, &aum, server); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: requeueProvisioning}, nil
	}

	// Server is up — register (or refresh) the owned UbuntuMachine.
	if err := r.ensureUbuntuMachine(ctx, &aum, server); err != nil {
		return r.fail(ctx, &aum, "MachineError", err.Error())
	}

	patch := client.MergeFrom(aum.DeepCopy())
	r.setPhase(&aum, arsysv1alpha1.PhaseReady)
	aum.Status.ServerID = server.ID
	aum.Status.Address = server.PrimaryIP()
	aum.Status.ProvisioningPercent = 0
	aum.Status.TransientErrors = 0
	aum.Status.MachineRef = machineName(&aum)
	aum.Status.ObservedGeneration = aum.Generation
	readyMsg := fmt.Sprintf("server %s up at %s; UbuntuMachine %q registered", server.ID, server.PrimaryIP(), machineName(&aum))
	if aum.Status.BaremetalModelID != "" {
		readyMsg += fmt.Sprintf("; baremetal model %s", aum.Status.BaremetalModelID)
	}
	apimeta.SetStatusCondition(&aum.Status.Conditions, metav1.Condition{
		Type:    arsysv1alpha1.ConditionReady,
		Status:  metav1.ConditionTrue,
		Reason:  "Ready",
		Message: readyMsg,
	})
	apimeta.RemoveStatusCondition(&aum.Status.Conditions, arsysv1alpha1.ConditionError)
	if err := r.Status().Patch(ctx, &aum, patch); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: requeueReady}, nil
}

// ensureServer creates the dedicated server on first reconcile (capturing the
// one-shot first_password into the credentials Secret) and thereafter returns
// its current state.
func (r *ArsysUbuntuMachineReconciler) ensureServer(ctx context.Context, aum *arsysv1alpha1.ArsysUbuntuMachine, cl *arsys.Client) (*arsys.Server, error) {
	if aum.Status.ServerID != "" {
		server, err := cl.GetServer(ctx, aum.Status.ServerID)
		if err == nil {
			r.Recorder.Eventf(aum, corev1.EventTypeNormal, "GetServer",
				"server %s state=%s progress=%d%%", server.ID, server.Status.State, server.Status.Percent)
			return server, nil
		}
		r.Recorder.Eventf(aum, corev1.EventTypeWarning, "APIError", "GetServer %s: %v", aum.Status.ServerID, err)
		// If the server is gone in Arsys (404), clear the ID and fall through to
		// the name-based lookup so we can detect a re-ordered server or create
		// a new one rather than looping on 404 forever.
		if apiErr, ok := err.(*arsys.APIError); !ok || apiErr.StatusCode != 404 {
			return nil, err
		}
		log := logf.FromContext(ctx)
		log.Info("server not found by ID, clearing and retrying by name", "id", aum.Status.ServerID)
		patch := client.MergeFrom(aum.DeepCopy())
		aum.Status.ServerID = ""
		if err := r.Status().Patch(ctx, aum, patch); err != nil {
			return nil, fmt.Errorf("clear stale server id: %w", err)
		}
	}

	// Persist the intended server name once so it stays stable across retries.
	name := aum.Status.ServerName
	if name == "" {
		name = serverName(aum)
		patch := client.MergeFrom(aum.DeepCopy())
		aum.Status.ServerName = name
		if err := r.Status().Patch(ctx, aum, patch); err != nil {
			return nil, fmt.Errorf("record server name: %w", err)
		}
	}

	// Check whether a server with this name already exists before ordering.
	// This recovers from a prior reconcile that created the server but failed
	// to persist the status (avoiding REPEATED_PUBLIC_NAME on retry).
	existing, err := cl.ListServers(ctx)
	if err != nil {
		r.Recorder.Eventf(aum, corev1.EventTypeWarning, "APIError", "ListServers: %v", err)
		return nil, fmt.Errorf("list servers: %w", err)
	}
	log := logf.FromContext(ctx)
	r.Recorder.Eventf(aum, corev1.EventTypeNormal, "ListServers", "listed %d servers", len(existing))
	log.Info("pre-create server name check", "lookingFor", name, "crName", aum.Name, "found", len(existing))
	for i := range existing {
		// Primary match: the name we computed (includes UID suffix). Secondary
		// match: bare CR name, for servers created by an older operator version
		// that did not append the UID suffix.
		if existing[i].Name == name || (aum.Spec.Server.Name == "" && (existing[i].Name == aum.Name || strings.HasPrefix(existing[i].Name, aum.Name+"-"))) {
			log.Info("adopting existing server by name", "id", existing[i].ID, "serverName", existing[i].Name)
			r.Recorder.Eventf(aum, corev1.EventTypeNormal, "AdoptServer",
				"adopting existing server %s (id=%s)", existing[i].Name, existing[i].ID)
			patch := client.MergeFrom(aum.DeepCopy())
			aum.Status.ServerID = existing[i].ID
			aum.Status.ServerName = existing[i].Name
			r.setPhase(aum, arsysv1alpha1.PhaseProvisioning)
			if err := r.Status().Patch(ctx, aum, patch); err != nil {
				return nil, fmt.Errorf("adopt server %s: %w", existing[i].ID, err)
			}
			srv, err := cl.GetServer(ctx, existing[i].ID)
			if err != nil {
				r.Recorder.Eventf(aum, corev1.EventTypeWarning, "APIError",
					"GetServer %s: %v", existing[i].ID, err)
				return nil, err
			}
			r.Recorder.Eventf(aum, corev1.EventTypeNormal, "GetServer",
				"server %s state=%s progress=%d%%", srv.ID, srv.Status.State, srv.Status.Percent)
			return srv, nil
		}
	}

	rsaKey, err := r.collectSSHPublicKey(ctx, aum)
	if err != nil {
		return nil, err
	}

	r.Recorder.Eventf(aum, corev1.EventTypeNormal, "CreateServer", "ordering Arsys server %q", name)
	req := arsys.CreateRequest{
		Name:         name,
		ApplianceID:  aum.Spec.Server.ApplianceID,
		DatacenterID: aum.Spec.Server.DatacenterID,
		SSHPassword:  true,
		RSAKey:       rsaKey,
		PowerOn:      true,
	}
	// selected is non-nil only when we auto-pick a dedicated model below.
	var selected *arsys.BaremetalModel
	if aum.Spec.Server.Type == arsysv1alpha1.ServerTypeCloud {
		req.Hardware = arsys.Hardware{
			VCore:             aum.Spec.Server.VCore,
			CoresPerProcessor: aum.Spec.Server.CoresPerProcessor,
			RAM:               aum.Spec.Server.RAM,
			HDDs:              []arsys.HDD{{Size: aum.Spec.Server.Disk, IsMain: true}},
		}
	} else {
		// dedicated (default): bare-metal model, no vcore/ram/hdds. With no
		// model pinned, pick the smallest one meeting vcore/ram in the DC.
		req.ServerType = arsys.ServerTypeBaremetal
		modelID := aum.Spec.Server.BaremetalModelID
		if modelID == "" {
			models, err := cl.ListBaremetalModels(ctx)
			if err != nil {
				r.Recorder.Eventf(aum, corev1.EventTypeWarning, "APIError", "ListBaremetalModels: %v", err)
				return nil, fmt.Errorf("list baremetal models: %w", err)
			}
			r.Recorder.Eventf(aum, corev1.EventTypeNormal, "ListBaremetalModels", "listed %d models", len(models))
			selected = arsys.SmallestModel(models, aum.Spec.Server.DatacenterID, aum.Spec.Server.VCore, aum.Spec.Server.RAM)
			if selected == nil {
				return nil, fmt.Errorf("no enabled baremetal model with >=%d cores and >=%d GB RAM available in datacenter %s",
					aum.Spec.Server.VCore, aum.Spec.Server.RAM, aum.Spec.Server.DatacenterID)
			}
			modelID = selected.ID
			logf.FromContext(ctx).Info("auto-selected baremetal model",
				"model", selected.Name, "id", selected.ID,
				"cores", selected.Hardware.TotalCores(), "ramGB", selected.Hardware.RAMGB())
		}
		req.Hardware = arsys.Hardware{BaremetalModelID: modelID}
	}

	// Assign a firewall policy that opens the k3s ports. Without it the API
	// auto-creates an SSH-only default, which drops intra-cluster traffic on
	// 6443/10250/8472 and leaves workers unable to join the control plane.
	fwID, err := r.ensureK3sFirewallPolicy(ctx, cl)
	if err != nil {
		r.Recorder.Eventf(aum, corev1.EventTypeWarning, "APIError", "ensureK3sFirewallPolicy: %v", err)
		return nil, err
	}
	req.FirewallPolicyID = fwID

	server, err := cl.CreateServer(ctx, req)
	if err != nil {
		r.Recorder.Eventf(aum, corev1.EventTypeWarning, "APIError", "CreateServer %q: %v", name, err)
		return nil, fmt.Errorf("create server: %w", err)
	}
	r.Recorder.Eventf(aum, corev1.EventTypeNormal, "CreateServer", "server %q created: id=%s state=%s",
		name, server.ID, server.Status.State)

	// first_password is only returned on create — write it to the
	// password Secret now, before the status write, so a crash can't lose it.
	if server.FirstPassword != "" {
		if err := r.writePasswordSecret(ctx, aum, server.FirstPassword); err != nil {
			return nil, fmt.Errorf("store password: %w", err)
		}
	}

	patch := client.MergeFrom(aum.DeepCopy())
	aum.Status.ServerID = server.ID
	aum.Status.ServerName = server.Name
	r.setPhase(aum, arsysv1alpha1.PhaseProvisioning)
	if aum.Spec.Server.Type != arsysv1alpha1.ServerTypeCloud {
		aum.Status.BaremetalModelID = req.Hardware.BaremetalModelID
		if selected != nil {
			aum.Status.SelectedCores = selected.Hardware.TotalCores()
			aum.Status.SelectedRAMGB = selected.Hardware.RAMGB()
		}
	}
	if err := r.Status().Patch(ctx, aum, patch); err != nil {
		return nil, fmt.Errorf("record server id %s: %w", server.ID, err)
	}
	return server, nil
}

// k3sFirewallPolicyName is the shared policy the operator find-or-creates so
// every ordered server opens the ports k3s needs between nodes.
const k3sFirewallPolicyName = "simple-cluster-k3s"

// k3sFirewallRules opens SSH plus the k3s control-plane/data-plane ports.
// Source is left empty (any): 6443 must stay reachable from the management
// cluster and kubeconfig users, and sibling node IPs aren't known at order
// time. The API server is still protected by TLS + node token.
func k3sFirewallRules() []arsys.FirewallRule {
	return []arsys.FirewallRule{
		{Protocol: "TCP", PortFrom: 22, PortTo: 22},       // SSH (operator access)
		{Protocol: "TCP", PortFrom: 6443, PortTo: 6443},   // kube-apiserver
		{Protocol: "TCP", PortFrom: 10250, PortTo: 10250}, // kubelet
		{Protocol: "TCP", PortFrom: 2379, PortTo: 2380},   // etcd (HA control plane)
		{Protocol: "UDP", PortFrom: 8472, PortTo: 8472},   // flannel VXLAN
	}
}

// ensureK3sFirewallPolicy returns the ID of the shared k3s firewall policy,
// creating it on first use. Idempotent: matches by name.
func (r *ArsysUbuntuMachineReconciler) ensureK3sFirewallPolicy(ctx context.Context, cl *arsys.Client) (string, error) {
	policies, err := cl.ListFirewallPolicies(ctx)
	if err != nil {
		return "", fmt.Errorf("list firewall policies: %w", err)
	}
	for _, p := range policies {
		if p.Name == k3sFirewallPolicyName {
			return p.ID, nil
		}
	}
	created, err := cl.CreateFirewallPolicy(ctx, arsys.FirewallPolicyCreate{
		Name:        k3sFirewallPolicyName,
		Description: "SSH + k3s ports for simple-cluster nodes",
		Rules:       k3sFirewallRules(),
	})
	if err != nil {
		return "", fmt.Errorf("create firewall policy: %w", err)
	}
	return created.ID, nil
}

// collectSSHPublicKey returns the single SSH public key to inject at order
// time via the Arsys rsa_key field. Candidates are spec.server.publicKeys (in
// order) then the publicKeySecretRef Secret values (ordered by data key, for
// determinism). Blank and private-key entries are skipped. Arsys' inline
// rsa_key accepts only one key; if several are found the first is used and a
// warning event records the rest were dropped.
func (r *ArsysUbuntuMachineReconciler) collectSSHPublicKey(ctx context.Context, aum *arsysv1alpha1.ArsysUbuntuMachine) (string, error) {
	var keys []string
	add := func(v string) {
		if v = strings.TrimSpace(v); v != "" && !strings.Contains(v, "PRIVATE KEY") {
			keys = append(keys, v)
		}
	}
	for _, k := range aum.Spec.Server.PublicKeys {
		add(k)
	}
	if ref := aum.Spec.Server.PublicKeySecretRef; ref != nil {
		var sec corev1.Secret
		if err := r.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: ref.Namespace}, &sec); err != nil {
			r.Recorder.Eventf(aum, corev1.EventTypeWarning, "APIError", "publicKeySecretRef %s/%s: %v", ref.Namespace, ref.Name, err)
			return "", fmt.Errorf("get publicKeySecretRef %s/%s: %w", ref.Namespace, ref.Name, err)
		}
		names := make([]string, 0, len(sec.Data))
		for name := range sec.Data {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			add(string(sec.Data[name]))
		}
	}
	if len(keys) == 0 {
		return "", nil
	}
	if len(keys) > 1 {
		r.Recorder.Eventf(aum, corev1.EventTypeWarning, "MultipleSSHKeys",
			"%d SSH public keys provided but Arsys rsa_key accepts only one; using the first", len(keys))
	}
	return keys[0], nil
}

// writePasswordSecret writes the server's root password into the Secret named
// by spec.passwordSecretRef (kubernetes.io/basic-auth), creating it if absent.
// Owned by the CR so it is garbage-collected on deletion.
func (r *ArsysUbuntuMachineReconciler) writePasswordSecret(ctx context.Context, aum *arsysv1alpha1.ArsysUbuntuMachine, password string) error {
	ref := aum.Spec.PasswordSecretRef
	desired := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: ref.Name, Namespace: ref.Namespace},
		Type:       corev1.SecretTypeBasicAuth,
		Data: map[string][]byte{
			corev1.BasicAuthUsernameKey: []byte(aum.Spec.SSH.User),
			corev1.BasicAuthPasswordKey: []byte(password),
		},
	}
	if err := controllerutil.SetControllerReference(aum, desired, r.Scheme); err != nil {
		return fmt.Errorf("set owner ref: %w", err)
	}

	var existing corev1.Secret
	err := r.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: ref.Namespace}, &existing)
	if apierrors.IsNotFound(err) {
		return r.Create(ctx, desired)
	}
	if err != nil {
		return err
	}
	existing.Type = desired.Type
	existing.Data = desired.Data
	if err := controllerutil.SetControllerReference(aum, &existing, r.Scheme); err != nil {
		return fmt.Errorf("set owner ref: %w", err)
	}
	return r.Update(ctx, &existing)
}

// ensureUbuntuMachine creates (or refreshes) the cluster-scoped UbuntuMachine
// for the provisioned server: labels copied from the CR, owner set to the CR.
func (r *ArsysUbuntuMachineReconciler) ensureUbuntuMachine(ctx context.Context, aum *arsysv1alpha1.ArsysUbuntuMachine, server *arsys.Server) error {
	ip := server.PrimaryIP()
	ssh := umv1alpha1.SSHAuth{
		User: aum.Spec.SSH.User,
		PasswordSecretRef: &corev1.SecretReference{
			Name:      aum.Spec.PasswordSecretRef.Name,
			Namespace: aum.Spec.PasswordSecretRef.Namespace,
		},
	}

	desired := &umv1alpha1.UbuntuMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:   machineName(aum),
			Labels: copyLabels(aum.Labels),
		},
		Spec: umv1alpha1.UbuntuMachineSpec{
			Address:         ip,
			ExternalAddress: ip,
			SSH:             ssh,
		},
	}
	// Advertise capacity from the requested cloud size, or from an
	// auto-selected dedicated model's real size (GB). An explicitly pinned
	// dedicated model leaves it unconstrained.
	switch {
	case aum.Spec.Server.Type == arsysv1alpha1.ServerTypeCloud:
		desired.Spec.Capacity = umv1alpha1.NodeResources{
			CPU:    resource.MustParse(fmt.Sprintf("%d", aum.Spec.Server.VCore)),
			Memory: resource.MustParse(fmt.Sprintf("%dGi", aum.Spec.Server.RAM)),
		}
	case aum.Status.SelectedCores > 0:
		desired.Spec.Capacity = umv1alpha1.NodeResources{
			CPU:    resource.MustParse(fmt.Sprintf("%d", aum.Status.SelectedCores)),
			Memory: resource.MustParse(fmt.Sprintf("%dG", aum.Status.SelectedRAMGB)),
		}
	}
	if err := controllerutil.SetControllerReference(aum, desired, r.Scheme); err != nil {
		return fmt.Errorf("set owner ref: %w", err)
	}

	var existing umv1alpha1.UbuntuMachine
	err := r.Get(ctx, types.NamespacedName{Name: machineName(aum)}, &existing)
	if apierrors.IsNotFound(err) {
		return r.Create(ctx, desired)
	}
	if err != nil {
		return err
	}
	existing.Labels = copyLabels(aum.Labels)
	existing.Spec = desired.Spec
	existing.OwnerReferences = desired.OwnerReferences
	return r.Update(ctx, &existing)
}

func (r *ArsysUbuntuMachineReconciler) reconcileDelete(ctx context.Context, aum *arsysv1alpha1.ArsysUbuntuMachine, cl *arsys.Client) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	if !controllerutil.ContainsFinalizer(aum, arsysv1alpha1.FinalizerName) {
		return ctrl.Result{}, nil
	}
	if aum.Status.Phase != arsysv1alpha1.PhaseDeleting {
		patch := client.MergeFrom(aum.DeepCopy())
		r.setPhase(aum, arsysv1alpha1.PhaseDeleting)
		_ = r.Status().Patch(ctx, aum, patch)
	}

	// Delete the UbuntuMachine first (the credentials Secret is GC'd via its
	// owner ref) and wait until it is confirmed gone.
	machine := &umv1alpha1.UbuntuMachine{ObjectMeta: metav1.ObjectMeta{Name: machineName(aum)}}
	if err := r.Delete(ctx, machine); err != nil && !apierrors.IsNotFound(err) {
		log.Error(err, "delete UbuntuMachine; retrying")
		return ctrl.Result{RequeueAfter: requeueDeleting}, nil
	}
	if err := r.Get(ctx, types.NamespacedName{Name: machineName(aum)}, machine); err == nil {
		return ctrl.Result{RequeueAfter: requeueDeleting}, nil
	} else if !apierrors.IsNotFound(err) {
		log.Error(err, "get UbuntuMachine during delete; retrying")
		return ctrl.Result{RequeueAfter: requeueDeleting}, nil
	}

	// Cancel the dedicated server and wait until it is confirmed removed from Arsys.
	// If ServerID is missing but ServerName is known, resolve the ID via ListServers.
	serverID := aum.Status.ServerID
	if serverID == "" && aum.Status.ServerName != "" {
		servers, err := cl.ListServers(ctx)
		if err != nil {
			r.Recorder.Eventf(aum, corev1.EventTypeWarning, "APIError", "ListServers: %v", err)
			log.Error(err, "list servers during delete; retrying")
			return ctrl.Result{RequeueAfter: requeueDeleting}, nil
		}
		r.Recorder.Eventf(aum, corev1.EventTypeNormal, "ListServers", "listed %d servers", len(servers))
		for i := range servers {
			if servers[i].Name == aum.Status.ServerName {
				serverID = servers[i].ID
				break
			}
		}
	}
	if serverID != "" {
		server, err := cl.GetServer(ctx, serverID)
		if err != nil {
			if apiErr, ok := err.(*arsys.APIError); !ok || apiErr.StatusCode != 404 {
				r.Recorder.Eventf(aum, corev1.EventTypeWarning, "APIError", "GetServer %s: %v", serverID, err)
				log.Error(err, "get arsys server during delete; retrying", "id", serverID)
				return ctrl.Result{RequeueAfter: requeueDeleting}, nil
			}
			// 404 — server is already gone, proceed
		} else {
			r.Recorder.Eventf(aum, corev1.EventTypeNormal, "GetServer",
				"server %s state=%s progress=%d%%", server.ID, server.Status.State, server.Status.Percent)
			if server.Status.State == arsys.StateRemoving {
				// Server is being removed (by us or externally) — proceed to finalizer removal.
				r.Recorder.Eventf(aum, corev1.EventTypeNormal, "DeleteServer", "Arsys server %s is removing", serverID)
			} else {
				r.Recorder.Eventf(aum, corev1.EventTypeNormal, "DeleteServer", "deleting Arsys server %s", serverID)
				if err := cl.DeleteServer(ctx, serverID); err != nil {
					r.Recorder.Eventf(aum, corev1.EventTypeWarning, "APIError", "DeleteServer %s: %v", serverID, err)
					if apiErr, ok := err.(*arsys.APIError); !ok || apiErr.StatusCode != 404 {
						log.Error(err, "delete arsys server; retrying", "id", serverID)
						return ctrl.Result{RequeueAfter: requeueDeleting}, nil
					}
					// 404 — already gone, fall through to finalizer removal
				} else {
					r.Recorder.Eventf(aum, corev1.EventTypeNormal, "DeleteServer", "Arsys server %s delete requested", serverID)
					return ctrl.Result{RequeueAfter: requeueDeleting}, nil
				}
			}
		}
	}

	patch := client.MergeFrom(aum.DeepCopy())
	controllerutil.RemoveFinalizer(aum, arsysv1alpha1.FinalizerName)
	if err := r.Patch(ctx, aum, patch); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	return ctrl.Result{}, nil
}

// arsysClient builds an API client from the token Secret referenced by the CR.
func (r *ArsysUbuntuMachineReconciler) arsysClient(ctx context.Context, aum *arsysv1alpha1.ArsysUbuntuMachine) (*arsys.Client, error) {
	var sec corev1.Secret
	key := types.NamespacedName{Name: aum.Spec.TokenSecretRef.Name, Namespace: aum.Spec.TokenSecretRef.Namespace}
	if err := r.Get(ctx, key, &sec); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("token Secret %s not found", key.String())
		}
		return nil, fmt.Errorf("get token Secret %s: %w", key.String(), err)
	}
	token := sec.Data[arsysv1alpha1.TokenSecretKey]
	if len(token) == 0 {
		return nil, fmt.Errorf("token Secret %s has no %q key", key.String(), arsysv1alpha1.TokenSecretKey)
	}
	return arsys.New(string(token)), nil
}

func (r *ArsysUbuntuMachineReconciler) setProvisioning(ctx context.Context, aum *arsysv1alpha1.ArsysUbuntuMachine, server *arsys.Server) error {
	patch := client.MergeFrom(aum.DeepCopy())
	r.setPhase(aum, arsysv1alpha1.PhaseProvisioning)
	aum.Status.ServerID = server.ID
	aum.Status.Address = server.PrimaryIP()
	aum.Status.ProvisioningPercent = server.Status.Percent
	apimeta.SetStatusCondition(&aum.Status.Conditions, metav1.Condition{
		Type:    arsysv1alpha1.ConditionReady,
		Status:  metav1.ConditionFalse,
		Reason:  "Provisioning",
		Message: fmt.Sprintf("server %s state=%s (%d%%)", server.ID, server.Status.State, server.Status.Percent),
	})
	return r.Status().Patch(ctx, aum, patch)
}

// isTransientServerError returns true for API errors that should be retried
// without transitioning to Failed: 429 rate-limit and 400 REPEATED_PUBLIC_NAME
// (name collision from a prior reconcile that lost its status write).
func isTransientServerError(err error) bool {
	var apiErr *arsys.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	if apiErr.StatusCode == http.StatusTooManyRequests {
		return true
	}
	return apiErr.StatusCode == http.StatusBadRequest && strings.Contains(apiErr.Body, "REPEATED_PUBLIC_NAME")
}

func (r *ArsysUbuntuMachineReconciler) fail(ctx context.Context, aum *arsysv1alpha1.ArsysUbuntuMachine, reason, msg string) (ctrl.Result, error) {
	patch := client.MergeFrom(aum.DeepCopy())
	r.setPhase(aum, arsysv1alpha1.PhaseFailed)
	apimeta.SetStatusCondition(&aum.Status.Conditions, metav1.Condition{
		Type:    arsysv1alpha1.ConditionError,
		Status:  metav1.ConditionTrue,
		Reason:  reason,
		Message: msg,
	})
	if err := r.Status().Patch(ctx, aum, patch); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: requeueFailed}, nil
}

// setPhase updates aum.Status.Phase and emits a PhaseTransition event if the
// phase actually changed. Call before the status patch that persists the change.
func (r *ArsysUbuntuMachineReconciler) setPhase(aum *arsysv1alpha1.ArsysUbuntuMachine, p arsysv1alpha1.Phase) {
	if aum.Status.Phase == p {
		return
	}
	eventType := corev1.EventTypeNormal
	if p == arsysv1alpha1.PhaseFailed {
		eventType = corev1.EventTypeWarning
	}
	r.Recorder.Eventf(aum, eventType, "PhaseTransition", "%s -> %s", aum.Status.Phase, p)
	aum.Status.Phase = p
}

// machineName is the 1:1 name of the UbuntuMachine for this CR.
func machineName(aum *arsysv1alpha1.ArsysUbuntuMachine) string {
	return aum.Name
}

// serverName is the Arsys server name. When spec.server.name is set the caller
// controls uniqueness; otherwise the CR name is combined with the first 8
// characters of the UID so that recreating a CR with the same name produces a
// distinct Arsys server name.
func serverName(aum *arsysv1alpha1.ArsysUbuntuMachine) string {
	if aum.Spec.Server.Name != "" {
		return aum.Spec.Server.Name
	}
	base := aum.Name
	if len(base) > 41 {
		base = base[:41]
	}
	uid := string(aum.UID)
	if len(uid) > 8 {
		uid = uid[:8]
	}
	return base + "-" + uid
}

// copyLabels duplicates the CR's labels onto the UbuntuMachine.
func copyLabels(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func (r *ArsysUbuntuMachineReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Recorder = mgr.GetEventRecorderFor("arsysubuntumachine-controller")
	return ctrl.NewControllerManagedBy(mgr).
		For(&arsysv1alpha1.ArsysUbuntuMachine{}).
		Owns(&umv1alpha1.UbuntuMachine{}).
		Named("arsysubuntumachine").
		Complete(r)
}
