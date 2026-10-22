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
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	sshkeysv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/sshkey-operator/api/v1alpha1"
	"github.com/apeirora/showroom-msp-simple-cluster/sshkey-operator/internal/secretpolicy"
)

// rotateBefore is how long before expiration an auto-rotating key is regenerated.
const rotateBefore = 24 * time.Hour

// SSHKeyReconciler reconciles a SSHKey object
type SSHKeyReconciler struct {
	client.Client
	SecretPolicy secretpolicy.Policy
	Scheme       *runtime.Scheme
}

// +kubebuilder:rbac:groups=sshkeys.apeirora.eu,resources=sshkeys,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=sshkeys.apeirora.eu,resources=sshkeys/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=sshkeys.apeirora.eu,resources=sshkeys/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch

// Reconcile generates the SSH key pair, encrypts it with the optional passphrase,
// exports it to a secret when requested, and maintains the resource conditions.
func (r *SSHKeyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var key sshkeysv1alpha1.SSHKey
	if err := r.Get(ctx, req.NamespacedName, &key); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Resolve the passphrase and reflect its validity as a condition.
	passphrase, err := r.resolvePassphrase(ctx, &key)
	if hasRef(key.Spec.PassPhaseSecret.Name) || key.Spec.PassPhaseSecret.Namespace != "" {
		if !formatSupportsPassphrase(key.Spec.KeyFormat) {
			msg := fmt.Sprintf("key format %s does not support a passphrase", key.Spec.KeyFormat)
			meta.SetStatusCondition(&key.Status.Conditions, condition(
				sshkeysv1alpha1.ConditionPassPhaseSecret, metav1.ConditionFalse, "FormatUnsupported", msg, key.Generation))
			return r.markNotReady(ctx, &key, msg)
		}
		if err != nil {
			meta.SetStatusCondition(&key.Status.Conditions, condition(
				sshkeysv1alpha1.ConditionPassPhaseSecret, metav1.ConditionFalse, "Invalid", err.Error(), key.Generation))
			return r.markNotReady(ctx, &key, "passphrase secret invalid: "+err.Error())
		}
		meta.SetStatusCondition(&key.Status.Conditions, condition(
			sshkeysv1alpha1.ConditionPassPhaseSecret, metav1.ConditionTrue, "Valid", "passphrase secret is valid", key.Generation))
	} else {
		meta.RemoveStatusCondition(&key.Status.Conditions, sshkeysv1alpha1.ConditionPassPhaseSecret)
	}

	// (Re)generate the key pair when missing, when the key-defining spec changed, or when due for rotation.
	hash := specHash(&key)
	expires := nextExpires(&key)
	if key.PrivateKey == "" || key.PublicKey == "" || key.Status.KeyHash != hash || rotationDue(&key) {
		if err := generateKeyPair(&key, passphrase); err != nil {
			return r.markNotReady(ctx, &key, "key generation failed: "+err.Error())
		}
		expires = nextExpires(&key)
		if err := r.Update(ctx, &key); err != nil {
			return ctrl.Result{}, err
		}
		log.Info("generated ssh key", "type", effectiveKeyType(&key))
	}

	// Export to a secret when requested and report whether it is in sync.
	if hasRef(key.Spec.ExportToSecret.Name) {
		if err := r.exportToSecret(ctx, &key); err != nil {
			meta.SetStatusCondition(&key.Status.Conditions, condition(
				sshkeysv1alpha1.ConditionExportSynced, metav1.ConditionFalse, "ExportFailed", err.Error(), key.Generation))
			return r.markNotReady(ctx, &key, "export failed: "+err.Error())
		}
		meta.SetStatusCondition(&key.Status.Conditions, condition(
			sshkeysv1alpha1.ConditionExportSynced, metav1.ConditionTrue, "Synced", "exported key matches the secret", key.Generation))
	} else {
		meta.RemoveStatusCondition(&key.Status.Conditions, sshkeysv1alpha1.ConditionExportSynced)
	}

	key.Status.PublicKey = key.PublicKey
	key.Status.Ready = true
	key.Status.Message = "key ready"
	key.Status.Expires = expires
	key.Status.KeyHash = hash
	if err := r.Status().Update(ctx, &key); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: requeueAfter(&key)}, nil
}

// resolvePassphrase returns the passphrase bytes from the referenced secret, or nil when none is set.
func (r *SSHKeyReconciler) resolvePassphrase(ctx context.Context, key *sshkeysv1alpha1.SSHKey) ([]byte, error) {
	ref := key.Spec.PassPhaseSecret
	if !hasRef(ref.Name) && ref.Namespace == "" {
		return nil, nil
	}
	ns, err := r.SecretPolicy.ResolveForObject(key, ref.Namespace, ref.Name, secretpolicy.Use)
	if err != nil {
		return nil, err
	}
	var secret corev1.Secret
	if err := r.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: ns}, &secret); err != nil {
		return nil, err
	}
	pass, ok := secret.Data[sshkeysv1alpha1.PassphraseSecretKey]
	if !ok || len(pass) == 0 {
		return nil, fmt.Errorf("secret %s/%s has no non-empty %q key", ns, ref.Name, sshkeysv1alpha1.PassphraseSecretKey)
	}
	return pass, nil
}

// exportToSecret creates or updates the ssh-auth secret holding the key pair.
func (r *SSHKeyReconciler) exportToSecret(ctx context.Context, key *sshkeysv1alpha1.SSHKey) error {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: key.Spec.ExportToSecret.Name, Namespace: key.Namespace},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, secret, func() error {
		secret.Type = corev1.SecretTypeSSHAuth
		secret.Data = map[string][]byte{
			corev1.SSHAuthPrivateKey: []byte(key.PrivateKey),
			"ssh-publickey":          []byte(key.PublicKey),
		}
		return controllerutil.SetControllerReference(key, secret, r.Scheme)
	})
	return err
}

// markNotReady records the failure on the status and stops the current reconcile.
func (r *SSHKeyReconciler) markNotReady(ctx context.Context, key *sshkeysv1alpha1.SSHKey, msg string) (ctrl.Result, error) {
	key.Status.Ready = false
	key.Status.Message = msg
	if err := r.Status().Update(ctx, key); err != nil {
		return ctrl.Result{}, err
	}
	// A related Secret can arrive after the primary object through Sync Agent.
	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

// generateKeyPair creates a new key pair, sets the name@namespace comment, and stores it on the object.
func generateKeyPair(key *sshkeysv1alpha1.SSHKey, passphrase []byte) error {
	comment := key.Name + "@" + key.Namespace

	var priv crypto.PrivateKey
	var pub crypto.PublicKey
	switch effectiveKeyType(key) {
	case sshkeysv1alpha1.KeyTypeRSA:
		size := key.Spec.KeySize
		if size == 0 {
			size = 4096
		}
		rsaKey, err := rsa.GenerateKey(rand.Reader, size)
		if err != nil {
			return err
		}
		priv, pub = rsaKey, &rsaKey.PublicKey
	case sshkeysv1alpha1.KeyTypeECDSA:
		ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return err
		}
		priv, pub = ecKey, &ecKey.PublicKey
	case sshkeysv1alpha1.KeyTypeED25519:
		pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return err
		}
		priv, pub = privKey, pubKey
	default:
		return fmt.Errorf("unsupported key type %q", key.Spec.KeyType)
	}

	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return err
	}

	var block *pem.Block
	if len(passphrase) > 0 {
		block, err = ssh.MarshalPrivateKeyWithPassphrase(priv, comment, passphrase)
	} else {
		block, err = ssh.MarshalPrivateKey(priv, comment)
	}
	if err != nil {
		return err
	}

	key.PrivateKey = string(pem.EncodeToMemory(block))
	key.PublicKey = formatPublicKey(sshPub, comment, key.Spec.KeyFormat)
	return nil
}

// formatPublicKey renders the public key as OpenSSH authorized-keys (default) or RFC 4716.
func formatPublicKey(pub ssh.PublicKey, comment string, format sshkeysv1alpha1.KeyFormat) string {
	if format == sshkeysv1alpha1.KeyFormatRFC4716 {
		b64 := base64.StdEncoding.EncodeToString(pub.Marshal())
		var sb strings.Builder
		sb.WriteString("---- BEGIN SSH2 PUBLIC KEY ----\n")
		fmt.Fprintf(&sb, "Comment: %q\n", comment)
		for i := 0; i < len(b64); i += 70 {
			end := min(i+70, len(b64))
			sb.WriteString(b64[i:end])
			sb.WriteString("\n")
		}
		sb.WriteString("---- END SSH2 PUBLIC KEY ----\n")
		return sb.String()
	}
	line := strings.TrimSuffix(string(ssh.MarshalAuthorizedKey(pub)), "\n")
	return line + " " + comment + "\n"
}

// specHash digests the key-defining spec fields. A change regenerates the key pair.
// Note: it hashes the passphrase secret reference, not its contents; rotating the
// passphrase secret in place does not retrigger generation.
func specHash(key *sshkeysv1alpha1.SSHKey) string {
	size := key.Spec.KeySize
	if size == 0 {
		size = 4096
	}
	format := key.Spec.KeyFormat
	if format == "" {
		format = sshkeysv1alpha1.KeyFormatPEM
	}
	ref := key.Spec.PassPhaseSecret
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%s|%s/%s",
		effectiveKeyType(key), size, format, ref.Namespace, ref.Name)))
	return fmt.Sprintf("%x", sum)
}

// formatSupportsPassphrase reports whether a passphrase-encrypted private key can be
// produced for the given format. RFC4716 is a public-key-only format with no passphrase.
func formatSupportsPassphrase(format sshkeysv1alpha1.KeyFormat) bool {
	return format != sshkeysv1alpha1.KeyFormatRFC4716
}

func effectiveKeyType(key *sshkeysv1alpha1.SSHKey) sshkeysv1alpha1.KeyType {
	if key.Spec.KeyType == "" {
		return sshkeysv1alpha1.KeyTypeED25519
	}
	return key.Spec.KeyType
}

// nextExpires returns the expiration time derived from spec.expire, or zero when no expiration.
func nextExpires(key *sshkeysv1alpha1.SSHKey) metav1.Time {
	if key.Spec.Expire.Duration == 0 {
		return metav1.Time{}
	}
	base := key.CreationTimestamp.Time
	if base.IsZero() {
		base = time.Now()
	}
	return metav1.NewTime(base.Add(key.Spec.Expire.Duration))
}

// rotationDue reports whether an existing key must be regenerated because it expired,
// or is within the rotation window when auto-rotation is enabled.
func rotationDue(key *sshkeysv1alpha1.SSHKey) bool {
	till := nextExpires(key)
	if till.IsZero() {
		return false
	}
	if time.Now().After(till.Time) {
		return true
	}
	return key.Spec.AutoRotate && time.Now().After(till.Add(-rotateBefore))
}

// requeueAfter schedules the next reconcile at the rotation/expiration boundary.
func requeueAfter(key *sshkeysv1alpha1.SSHKey) time.Duration {
	till := nextExpires(key)
	if till.IsZero() {
		return 0
	}
	next := till.Time
	if key.Spec.AutoRotate {
		next = next.Add(-rotateBefore)
	}
	d := time.Until(next)
	if d < time.Minute {
		return time.Minute
	}
	return d
}

func hasRef(name string) bool { return name != "" }

func condition(condType string, status metav1.ConditionStatus, reason, msg string, gen int64) metav1.Condition {
	return metav1.Condition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            msg,
		ObservedGeneration: gen,
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *SSHKeyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&sshkeysv1alpha1.SSHKey{}).
		Owns(&corev1.Secret{}).
		Named("sshkey").
		Complete(r)
}
