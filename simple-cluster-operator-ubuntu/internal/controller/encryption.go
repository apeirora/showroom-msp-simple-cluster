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
	"strings"
	"time"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	ubuntuv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator-ubuntu/api/v1alpha1"
	"github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator-ubuntu/internal/kms"
	"github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator-ubuntu/internal/openbao"
	sshpkg "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator-ubuntu/internal/ssh"
)

// Encryption at rest, SimpleCluster #470 step 3.
//
// The platform (showroom-infra) owns one OpenBao tenancy per environment:
// the namespace, the Transit and AppRole mounts, a JWT login for this
// operator's ServiceAccount and two fixed policies. This operator owns the
// per-cluster objects inside it: one Transit key and one AppRole role, both
// named after the cluster, plus one response-wrapped secret ID per
// control-plane host. The host unwraps it in install.sh; the operator only
// ever sees the accessor, which it records in status, replaces before the
// secret ID expires and destroys on delete.
//
// Fail-closed rules:
//   - no OpenBao configuration on the operator, or a cluster whose key is
//     soft-deleted or exportable: Phase=Failed, nothing is installed;
//   - a host that already runs k3s without the plugin, or with another
//     cluster's plugin configuration: Phase=Failed, never adopted;
//   - a host whose kube-apiserver does not report kms-providers ok after
//     the install: host Failed, cluster stays Provisioning and retries;
//   - Ready, and therefore the kubeconfig the SimpleCluster hands out, is
//     reached only when every control-plane host verified.
//
// Invariant for cleanup: status.encryption (with the key and role names) is
// persisted BEFORE the first OpenBao object is created. A cluster without
// status.encryption therefore owns nothing in OpenBao; a cluster with it
// must be cleaned up, and deletion fails loudly when that is impossible.
//
// The Transit key is kept when the cluster is deleted: etcd snapshots taken
// while the cluster lived can only be restored with it. Removing the key is
// an OpenBao admin action (crypto-shredding).

// errEncryptionMismatch marks a host state that retries cannot fix.
var errEncryptionMismatch = errors.New("encryption mismatch")

// verifyAttempts × verifyInterval bounds how long a freshly installed
// kube-apiserver may take to report kms-providers ok.
const (
	verifyAttempts = 24
	verifyInterval = 5 * time.Second
)

// renewalFraction of the secret ID TTL after which a host gets a new one.
const renewalFraction = 0.75

// encryptionSession is the OpenBao login and the per-cluster facts one
// reconcile needs. Close it when the reconcile ends.
type encryptionSession struct {
	client     *openbao.Client
	keyName    string
	roleName   string
	roleID     string
	keyVersion int64
	// hosts is status.encryption.hosts by name, updated as hosts progress.
	hosts map[string]ubuntuv1alpha1.HostEncryptionStatus
	// now is injectable for tests.
	now func() time.Time
}

func (s *encryptionSession) close(ctx context.Context) {
	if s == nil || s.client == nil {
		return
	}
	if err := s.client.RevokeSelf(ctx); err != nil {
		logf.FromContext(ctx).V(1).Info("openbao revoke-self failed (token expires by itself)", "err", err.Error())
	}
}

// kmsObjectName is the Transit key and AppRole role name for a cluster that
// has none recorded yet: namespace and name joined by "_", a character no
// Kubernetes namespace or object name may contain, so two different
// clusters can never map to one key (team-a/demo vs team/a-demo). Once
// recorded in status.encryption the stored names win, so a later change of
// this scheme cannot detach a cluster from its key.
func kmsObjectName(cluster *ubuntuv1alpha1.UbuntuCluster) string {
	return cluster.Namespace + "_" + cluster.Name
}

// kmsNames returns the key and role names for the cluster: the recorded
// ones when present, otherwise the deterministic default.
func kmsNames(cluster *ubuntuv1alpha1.UbuntuCluster) (key, role string) {
	key, role = kmsObjectName(cluster), kmsObjectName(cluster)
	if st := cluster.Status.Encryption; st != nil {
		if st.KeyName != "" {
			key = st.KeyName
		}
		if st.RoleName != "" {
			role = st.RoleName
		}
	}
	return key, role
}

// beginEncryption validates the operator configuration, logs in, records
// the cluster's object names in status and then makes sure its key and role
// exist. A nil result means proceed; a non-nil result is the reconcile's
// answer (the cluster was marked Failed or Pending).
func (r *UbuntuClusterReconciler) beginEncryption(ctx context.Context, cluster *ubuntuv1alpha1.UbuntuCluster) (*encryptionSession, *ctrl.Result, error) {
	if cluster.Spec.Encryption.Provider.Type != ubuntuv1alpha1.EncryptionProviderOpenBao {
		res, err := r.failEncryption(ctx, cluster, "EncryptionUnsupported",
			fmt.Sprintf("encryption provider %q is not supported by this operator", cluster.Spec.Encryption.Provider.Type))
		return nil, &res, err
	}
	if !r.OpenBao.Enabled() {
		res, err := r.failEncryption(ctx, cluster, "EncryptionUnsupported",
			"this operator has no OpenBao configuration (--openbao-address); refusing to provision an unencrypted cluster")
		return nil, &res, err
	}
	cl, err := openbao.New(r.OpenBao)
	if err != nil {
		res, ferr := r.failEncryption(ctx, cluster, "EncryptionConfigError", err.Error())
		return nil, &res, ferr
	}
	if err := cl.Login(ctx); err != nil {
		// Transient (OpenBao down, token not yet projected): keep retrying,
		// but say so. Nothing is installed meanwhile.
		res, perr := r.encryptionPending(ctx, cluster, "OpenBaoLoginFailed", err.Error())
		return nil, &res, perr
	}
	key, role := kmsNames(cluster)
	s := &encryptionSession{client: cl, keyName: key, roleName: role,
		hosts: map[string]ubuntuv1alpha1.HostEncryptionStatus{}, now: time.Now}
	if cluster.Status.Encryption != nil {
		s.keyVersion = cluster.Status.Encryption.KeyVersion
		for _, h := range cluster.Status.Encryption.Hosts {
			s.hosts[h.Name] = h
		}
	}
	// Persist the names before anything exists in OpenBao (cleanup invariant).
	if cluster.Status.Encryption == nil || cluster.Status.Encryption.KeyName != key || cluster.Status.Encryption.RoleName != role {
		patch := client.MergeFrom(cluster.DeepCopy())
		s.applyStatus(cluster, false)
		if err := r.Status().Patch(ctx, cluster, patch); err != nil {
			s.close(ctx)
			return nil, &ctrl.Result{}, err
		}
	}
	var rotate time.Duration
	if p := cluster.Spec.Encryption.Provider.KeyRotationPeriod; p != nil {
		rotate = p.Duration
	}
	ver, err := cl.EnsureTransitKey(ctx, s.keyName, rotate)
	if err != nil {
		s.close(ctx)
		if isPermanentKeyError(err) {
			res, ferr := r.failEncryption(ctx, cluster, "EncryptionKeyError", err.Error())
			return nil, &res, ferr
		}
		res, perr := r.encryptionPending(ctx, cluster, "OpenBaoError", err.Error())
		return nil, &res, perr
	}
	s.keyVersion = ver
	if err := cl.EnsureRole(ctx, s.roleName); err != nil {
		s.close(ctx)
		res, perr := r.encryptionPending(ctx, cluster, "OpenBaoError", err.Error())
		return nil, &res, perr
	}
	rid, err := cl.RoleID(ctx, s.roleName)
	if err != nil {
		s.close(ctx)
		res, perr := r.encryptionPending(ctx, cluster, "OpenBaoError", err.Error())
		return nil, &res, perr
	}
	s.roleID = rid
	patch := client.MergeFrom(cluster.DeepCopy())
	s.applyStatus(cluster, false)
	if err := r.Status().Patch(ctx, cluster, patch); err != nil {
		s.close(ctx)
		return nil, &ctrl.Result{}, err
	}
	return s, nil, nil
}

// isPermanentKeyError tells key states no retry fixes from transient faults.
func isPermanentKeyError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "soft-deleted") || strings.Contains(msg, "exportable")
}

// failEncryption is fail with the EncryptionReady condition set to False
// for the same reason, so no stale "Encrypted" survives a failure.
func (r *UbuntuClusterReconciler) failEncryption(ctx context.Context, cluster *ubuntuv1alpha1.UbuntuCluster, reason, msg string) (ctrl.Result, error) {
	apimeta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
		Type: ubuntuv1alpha1.ConditionEncryptionReady, Status: metav1.ConditionFalse, Reason: reason, Message: msg,
	})
	return r.fail(ctx, cluster, reason, msg)
}

// encryptionPending keeps the cluster Provisioning with an explicit
// EncryptionReady=False reason and retries; nothing is installed.
func (r *UbuntuClusterReconciler) encryptionPending(ctx context.Context, cluster *ubuntuv1alpha1.UbuntuCluster, reason, msg string) (ctrl.Result, error) {
	patch := client.MergeFrom(cluster.DeepCopy())
	cluster.Status.Phase = ubuntuv1alpha1.PhaseProvisioning
	cluster.Status.Message = "Provisioning: " + reason + ": " + msg
	apimeta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
		Type: ubuntuv1alpha1.ConditionEncryptionReady, Status: metav1.ConditionFalse, Reason: reason, Message: msg,
	})
	apimeta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
		Type: ubuntuv1alpha1.ConditionReady, Status: metav1.ConditionFalse, Reason: reason, Message: "waiting for the KMS provider",
	})
	if err := r.Status().Patch(ctx, cluster, patch); err != nil {
		return ctrl.Result{}, err
	}
	r.eventf(cluster, "Warning", reason, "%s", msg)
	return ctrl.Result{RequeueAfter: requeueWorking}, nil
}

// hostInput is the connection data for one host of this cluster.
func (s *encryptionSession) hostInput(wrapped string) kms.Input {
	cfg := s.client.Config()
	return kms.Input{
		Address:         cfg.Address,
		Namespace:       cfg.Namespace,
		TransitMount:    cfg.TransitMount,
		TransitKey:      s.keyName,
		ApproleMount:    cfg.ApproleMount,
		ApproleRole:     s.roleName,
		RoleID:          s.roleID,
		SecretIDWrapped: wrapped,
		CACert:          s.client.CAPEM(),
	}
}

// renewalDue reports whether the host's secret ID is old enough to replace.
// A recorded accessor without an issue time (older status) is stamped now
// rather than renewed at once.
func (s *encryptionSession) renewalDue(h *ubuntuv1alpha1.HostEncryptionStatus) bool {
	if h.SecretIDAccessor == "" {
		return false
	}
	if h.SecretIDIssuedAt == nil {
		t := metav1.NewTime(s.now())
		h.SecretIDIssuedAt = &t
		return false
	}
	ttl := s.client.Config().SecretIDTTL
	if ttl <= 0 {
		return false
	}
	return s.now().After(h.SecretIDIssuedAt.Time.Add(time.Duration(float64(ttl) * renewalFraction)))
}

// prepareHost gets one control-plane host ready for an encrypted k3s
// install, or keeps it ready. running tells whether k3s already runs there.
//
//   - not prepared, running: errEncryptionMismatch (its Secrets would be
//     plaintext);
//   - prepared for ANOTHER cluster (reused host): running ->
//     errEncryptionMismatch; not running -> re-prepare for this cluster;
//   - prepared for this cluster: nothing, unless the secret ID is due for
//     renewal, then install.sh runs again with a fresh wrapped secret ID
//     (it restarts the plugin only) and the old accessor is destroyed;
//   - not prepared, not running: mint a wrapped secret ID for this host and
//     run install.sh, which unwraps it and installs the plugin.
func (s *encryptionSession) prepareHost(ctx context.Context, c *sshpkg.Client, host ubuntuv1alpha1.Host, running bool) error {
	prepared, err := kms.IsPrepared(ctx, c)
	if err != nil {
		return fmt.Errorf("probe KMS plugin on %s: %w", host.Name, err)
	}
	h := s.hosts[host.Name]
	h.Name = host.Name
	defer func() { s.hosts[host.Name] = h }()

	if prepared {
		mine, merr := kms.MatchesCluster(ctx, c, s.hostInput(""))
		if merr != nil {
			return fmt.Errorf("probe KMS configuration on %s: %w", host.Name, merr)
		}
		switch {
		case !mine && running:
			return fmt.Errorf("%w: k3s already runs on %s with another cluster's KMS configuration; use a fresh host", errEncryptionMismatch, host.Name)
		case mine && !s.renewalDue(&h):
			return nil
		}
		// !mine && !running: reinstall for this cluster. mine && due: renew.
	} else if running {
		return fmt.Errorf("%w: k3s already runs on %s without the KMS plugin; its Secrets would be plaintext. Use a fresh host", errEncryptionMismatch, host.Name)
	}

	wrapped, err := s.client.MintWrappedSecretID(ctx, s.roleName, map[string]string{"host": host.Name, "cluster": s.roleName})
	if err != nil {
		return err
	}
	previous := h.SecretIDAccessor
	accessor, _, perr := kms.Prepare(ctx, c, s.hostInput(wrapped))
	// The accessor is known as soon as the host unwrapped, even if the
	// script failed afterwards: record it so it is never orphaned.
	if accessor != "" {
		h.SecretIDAccessor = accessor
		t := metav1.NewTime(s.now())
		h.SecretIDIssuedAt = &t
		h.Verified = false
	}
	if perr != nil {
		return fmt.Errorf("prepare KMS plugin on %s: %w", host.Name, perr)
	}
	if previous != "" && previous != h.SecretIDAccessor {
		if derr := s.client.DestroySecretIDAccessor(ctx, s.roleName, previous); derr != nil {
			// The new secret ID is in use; the old one expires by TTL.
			logf.FromContext(ctx).Info("could not destroy the replaced secret ID", "host", host.Name, "err", derr.Error())
		}
	}
	h.Verified = false
	return nil
}

// verifyHost is the fail-closed check once k3s runs on the host: prepared
// for THIS cluster, plugin unit active and kms-providers ok.
func (s *encryptionSession) verifyHost(ctx context.Context, c *sshpkg.Client, host ubuntuv1alpha1.Host) error {
	prepared, err := kms.IsPrepared(ctx, c)
	if err != nil {
		return fmt.Errorf("probe KMS plugin on %s: %w", host.Name, err)
	}
	if !prepared {
		return fmt.Errorf("%w: k3s runs on %s without the KMS plugin", errEncryptionMismatch, host.Name)
	}
	mine, err := kms.MatchesCluster(ctx, c, s.hostInput(""))
	if err != nil {
		return fmt.Errorf("probe KMS configuration on %s: %w", host.Name, err)
	}
	if !mine {
		return fmt.Errorf("%w: k3s runs on %s with another cluster's KMS configuration", errEncryptionMismatch, host.Name)
	}
	var last error
	for i := 0; i < verifyAttempts; i++ {
		if last = kms.Verify(ctx, c); last == nil {
			h := s.hosts[host.Name]
			h.Name = host.Name
			h.Verified = true
			s.hosts[host.Name] = h
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(verifyInterval):
		}
	}
	return fmt.Errorf("KMS not healthy on %s: %w", host.Name, last)
}

// applyStatus writes status.encryption and the EncryptionReady condition.
// ready says whether every control-plane host verified.
func (s *encryptionSession) applyStatus(cluster *ubuntuv1alpha1.UbuntuCluster, ready bool) {
	st := &ubuntuv1alpha1.EncryptionStatus{
		Provider:   cluster.Spec.Encryption.Provider.Type,
		KeyName:    s.keyName,
		KeyVersion: s.keyVersion,
		RoleName:   s.roleName,
	}
	for _, h := range cluster.Spec.ControlPlane.Hosts {
		if hs, ok := s.hosts[h.Name]; ok {
			st.Hosts = append(st.Hosts, hs)
		}
	}
	cluster.Status.Encryption = st
	cond := metav1.Condition{
		Type:    ubuntuv1alpha1.ConditionEncryptionReady,
		Status:  metav1.ConditionFalse,
		Reason:  "Provisioning",
		Message: fmt.Sprintf("waiting for every control-plane host to run the KMS plugin (%d/%d verified)", verifiedCount(st), len(cluster.Spec.ControlPlane.Hosts)),
	}
	if ready {
		cond.Status = metav1.ConditionTrue
		cond.Reason = "Encrypted"
		cond.Message = fmt.Sprintf("Secrets are encrypted at rest with %s key %s (version %d)", st.Provider, st.KeyName, st.KeyVersion)
	}
	apimeta.SetStatusCondition(&cluster.Status.Conditions, cond)
}

func verifiedCount(st *ubuntuv1alpha1.EncryptionStatus) int {
	n := 0
	for _, h := range st.Hosts {
		if h.Verified {
			n++
		}
	}
	return n
}

// cleanupEncryption runs on delete, before and independently of any SSH:
// destroy the hosts' secret IDs and the role, keep the key, remember that
// it is done. Returns an error (so the finalizer retries) whenever objects
// may exist and cannot be removed. A cluster without status.encryption
// never got past the names-first patch, so it owns nothing (see the
// invariant above).
func (r *UbuntuClusterReconciler) cleanupEncryption(ctx context.Context, cluster *ubuntuv1alpha1.UbuntuCluster) error {
	if !cluster.Spec.EncryptionEnabled() {
		return nil
	}
	st := cluster.Status.Encryption
	if st == nil || st.CleanupDone {
		return nil
	}
	if !r.OpenBao.Enabled() {
		return errors.New("cluster has KMS objects but the operator has no OpenBao configuration; cannot clean up")
	}
	cl, err := openbao.New(r.OpenBao)
	if err != nil {
		return err
	}
	if err := cl.Login(ctx); err != nil {
		return err
	}
	defer func() { _ = cl.RevokeSelf(ctx) }()
	_, role := kmsNames(cluster)
	for _, h := range st.Hosts {
		if h.SecretIDAccessor == "" {
			continue
		}
		if err := cl.DestroySecretIDAccessor(ctx, role, h.SecretIDAccessor); err != nil {
			return err
		}
	}
	if err := cl.DeleteRole(ctx, role); err != nil {
		return err
	}
	patch := client.MergeFrom(cluster.DeepCopy())
	cluster.Status.Encryption.CleanupDone = true
	if err := r.Status().Patch(ctx, cluster, patch); err != nil {
		return err
	}
	r.eventf(cluster, "Normal", "EncryptionCleanedUp",
		"destroyed %d secret ID(s) and role %s; Transit key %s is kept for snapshot restores", len(st.Hosts), role, st.KeyName)
	return nil
}
