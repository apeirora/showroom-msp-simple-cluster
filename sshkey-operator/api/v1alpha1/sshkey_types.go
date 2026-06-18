package v1alpha1

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type KeyType string

const (
	KeyTypeRSA     KeyType = "rsa"
	KeyTypeED25519 KeyType = "ed25519"
	KeyTypeECDSA   KeyType = "ecdsa"
)

type KeyFormat string

const (
	// KeyFormatPEM exports the public key as a single-line OpenSSH authorized-keys entry.
	KeyFormatPEM KeyFormat = "PEM"
	// KeyFormatRFC4716 exports the public key in the RFC 4716 (SSH2) multi-line format.
	KeyFormatRFC4716 KeyFormat = "RFC4716"
)

const (
	// ConditionPassPhaseSecret reports whether the referenced passphrase secret is valid.
	// Only present when passPhaseSecret is set.
	ConditionPassPhaseSecret = "PassPhaseSecret"
	// ConditionExportSynced reports whether the key in the exported secret matches the SSHKey.
	// Only present when exportToSecret is set.
	ConditionExportSynced = "ExportSynced"
)

// PassphraseSecretKey is the data key read from the passphrase secret.
const PassphraseSecretKey = "password"

// SSHKeySpec defines the desired state of SSHKey
type SSHKeySpec struct {

	// KeyType default: ed25519
	// +kubebuilder:validation:Enum=rsa;ed25519;ecdsa
	// +kubebuilder:default=ed25519
	// +optional
	KeyType KeyType `json:"keyType,omitempty"`

	// KeyFormat of the exported public key, default: PEM
	// +kubebuilder:validation:Enum=PEM;RFC4716
	// +kubebuilder:default=PEM
	// +optional
	KeyFormat KeyFormat `json:"keyFormat,omitempty"`

	// KeySize in bits for RSA type, default 4096
	// +kubebuilder:default=4096
	// +optional
	KeySize int `json:"keySize,omitempty"`

	// Expire When the key should expire, relative to its creation.
	// Not specified (zero) means no expiration date.
	// +optional
	Expire metav1.Duration `json:"expire,omitempty"`

	// PassPhaseSecret specify secret holding the passphrase under the "password" key,
	// used to encrypt the generated private key.
	// +optional
	PassPhaseSecret v1.SecretReference `json:"passPhaseSecret,omitempty"`

	// ExportToSecret specify secret (same namespace) where to export the key pair
	// as a kubernetes.io/ssh-auth secret (keys: ssh-privatekey, ssh-publickey).
	// +optional
	ExportToSecret v1.LocalObjectReference `json:"exportToSecret,omitempty"`

	// AutoRotate key if it's gonna to expire, 24h before expiration
	// +optional
	AutoRotate bool `json:"autoRotate,omitempty"`
}

// SSHKeyStatus defines the observed state of SSHKey.
type SSHKeyStatus struct {
	// Expires is the time the key expires, set when spec.expire is provided.
	// +optional
	Expires metav1.Time `json:"expires,omitempty"`

	// Ready is true once the key is generated and (if requested) exported.
	Ready bool `json:"ready"`

	// KeyHash is the hash of the key-defining spec fields the current key was generated from.
	// A mismatch with the spec triggers regeneration.
	// +optional
	KeyHash string `json:"keyHash,omitempty"`

	// Message holds a human-readable status detail.
	// +optional
	Message string `json:"message,omitempty"`

	// Conditions represent the current state of the SSHKey.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=`.spec.keyType`
// +kubebuilder:printcolumn:name="Ready",type=boolean,JSONPath=`.status.ready`
// +kubebuilder:printcolumn:name="Expires",type=date,JSONPath=`.status.expires`

// SSHKey is the Schema for generating sshkeys API
type SSHKey struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec defines the desired state of SSHKey
	// +required
	Spec SSHKeySpec `json:"spec"`

	// status defines the observed state of SSHKey
	// +optional
	Status SSHKeyStatus `json:"status,omitempty"`

	// PrivateKey is the generated private key (OpenSSH format, passphrase-encrypted when passPhaseSecret is set).
	// +optional
	PrivateKey string `json:"privateKey,omitempty"`

	// PublicKey is the generated public key.
	// +optional
	PublicKey string `json:"publicKey,omitempty"`
}

// +kubebuilder:object:root=true

// SSHKeyList contains a list of SSHKey
type SSHKeyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SSHKey `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SSHKey{}, &SSHKeyList{})
}
