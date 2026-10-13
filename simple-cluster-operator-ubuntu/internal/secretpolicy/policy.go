// Package secretpolicy validates operator Secret references.
package secretpolicy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
)

const (
	Use                       = "use"
	Copy                      = "copy"
	Write                     = "write"
	GardenerCredentialBinding = "gardener-credential-binding"
	GardenerSecretBinding     = "gardener-secret-binding"
	GardenerDNS               = "gardener-dns"
)

var ErrDenied = errors.New("Secret reference denied")
var namespacePattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
var namePattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9.]*[a-z0-9])?$`)

// Entry permits one reference for one purpose.
type Entry struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Purpose   string `json:"purpose"`
}

// Policy contains only operator-configured exceptions.
type Policy struct{ Allowlist []Entry }

// Parse reads the operator's JSON allowlist and rejects ambiguous configuration.
func Parse(raw string) (Policy, error) {
	var p Policy
	if raw == "" {
		return p, nil
	}
	decoder := json.NewDecoder(bytes.NewBufferString(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&p.Allowlist); err != nil {
		return p, fmt.Errorf("invalid Secret reference allowlist: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return Policy{}, errors.New("Secret reference allowlist must contain one JSON value")
	}
	for _, entry := range p.Allowlist {
		if err := validate(entry.Namespace, entry.Name, entry.Purpose); err != nil {
			return Policy{}, err
		}
	}
	return p, nil
}

// Resolve defaults the namespace to the request namespace before authorization.
func (p Policy) Resolve(ownerNamespace, namespace, name, purpose string) (string, error) {
	if namespace == "" {
		namespace = ownerNamespace
	}
	if err := validate(namespace, name, purpose); err != nil {
		return "", err
	}
	if ownerNamespace != "" && namespace == ownerNamespace && (purpose == Use || purpose == Copy || purpose == Write) {
		return namespace, nil
	}
	if err := p.Require(namespace, name, purpose); err != nil {
		return "", err
	}
	return namespace, nil
}

// Require validates references that always need an explicit operator exception.
func (p Policy) Require(namespace, name, purpose string) error {
	if err := validate(namespace, name, purpose); err != nil {
		return err
	}
	for _, entry := range p.Allowlist {
		if entry.Namespace == namespace && entry.Name == name && entry.Purpose == purpose {
			return nil
		}
	}
	return fmt.Errorf("%w: %s/%s is not allowed for %s", ErrDenied, namespace, name, purpose)
}

func validate(namespace, name, purpose string) error {
	if len(namespace) == 0 || len(namespace) > 63 || !namespacePattern.MatchString(namespace) {
		return fmt.Errorf("%w: invalid reference namespace", ErrDenied)
	}
	if len(name) == 0 || len(name) > 253 || !namePattern.MatchString(name) {
		return fmt.Errorf("%w: invalid reference name", ErrDenied)
	}
	switch purpose {
	case Use, Copy, Write, GardenerCredentialBinding, GardenerSecretBinding, GardenerDNS:
	default:
		return fmt.Errorf("%w: invalid reference purpose", ErrDenied)
	}
	return nil
}
