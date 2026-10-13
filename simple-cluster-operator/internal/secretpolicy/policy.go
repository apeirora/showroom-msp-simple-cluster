// Package secretpolicy validates operator Secret references.
package secretpolicy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"k8s.io/apimachinery/pkg/util/validation"
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
	token, err := decoder.Token()
	if err != nil || token != json.Delim('[') {
		return Policy{}, errors.New("Secret reference allowlist must be a JSON array")
	}
	for decoder.More() {
		token, err = decoder.Token()
		if err != nil || token != json.Delim('{') {
			return Policy{}, errors.New("each Secret reference exception must be an object")
		}
		entry := Entry{}
		seen := map[string]bool{}
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return Policy{}, err
			}
			field, ok := key.(string)
			if !ok || seen[field] {
				return Policy{}, errors.New("duplicate Secret reference exception field")
			}
			seen[field] = true
			var value *string
			switch field {
			case "namespace":
				value = &entry.Namespace
			case "name":
				value = &entry.Name
			case "purpose":
				value = &entry.Purpose
			default:
				return Policy{}, fmt.Errorf("unknown Secret reference exception field %q", field)
			}
			if err := decoder.Decode(value); err != nil {
				return Policy{}, err
			}
		}
		if _, err := decoder.Token(); err != nil {
			return Policy{}, err
		}
		if err := validate(entry.Namespace, entry.Name, entry.Purpose); err != nil {
			return Policy{}, err
		}
		p.Allowlist = append(p.Allowlist, entry)
	}
	if _, err := decoder.Token(); err != nil {
		return Policy{}, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return Policy{}, errors.New("Secret reference allowlist must contain one JSON value")
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
	if len(validation.IsDNS1123Label(namespace)) != 0 {
		return fmt.Errorf("%w: invalid reference namespace", ErrDenied)
	}
	if len(validation.IsDNS1123Subdomain(name)) != 0 {
		return fmt.Errorf("%w: invalid reference name", ErrDenied)
	}
	switch purpose {
	case Use, Copy, Write, GardenerCredentialBinding, GardenerSecretBinding, GardenerDNS:
	default:
		return fmt.Errorf("%w: invalid reference purpose", ErrDenied)
	}
	return nil
}
