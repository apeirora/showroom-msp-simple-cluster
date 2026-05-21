/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package kubeconfig

import (
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"strings"
	"time"

	"k8s.io/client-go/tools/clientcmd"
)

// TokenTimes inspects the first kubeconfig AuthInfo with a recognisable
// credential and returns its (issuedAt, expiresAt). Both may be nil when
// the credential carries no time information (static token, exec plugin).
// The function returns an error only when the kubeconfig itself fails to
// parse — unrecognised credentials yield (nil, nil, nil).
func TokenTimes(kubeconfigBytes []byte) (issuedAt, expiresAt *time.Time, err error) {
	cfg, err := clientcmd.Load(kubeconfigBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("parse kubeconfig: %w", err)
	}
	for _, ai := range cfg.AuthInfos {
		if ai == nil {
			continue
		}
		if len(ai.ClientCertificateData) > 0 {
			iss, exp, err := certTimes(ai.ClientCertificateData)
			if err != nil {
				return nil, nil, err
			}
			return iss, exp, nil
		}
		if ai.Token != "" {
			iss, exp, err := jwtTimes(ai.Token)
			if err != nil {
				return nil, nil, err
			}
			if iss != nil || exp != nil {
				return iss, exp, nil
			}
		}
	}
	return nil, nil, nil
}

func certTimes(pemBytes []byte) (*time.Time, *time.Time, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, nil, fmt.Errorf("client-certificate-data: not PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("parse client certificate: %w", err)
	}
	iss := cert.NotBefore.UTC()
	exp := cert.NotAfter.UTC()
	return &iss, &exp, nil
}

func jwtTimes(token string) (*time.Time, *time.Time, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, nil, nil
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		body, err = base64.URLEncoding.DecodeString(parts[1])
		if err != nil {
			return nil, nil, fmt.Errorf("decode JWT payload: %w", err)
		}
	}
	var claims struct {
		Iat int64 `json:"iat"`
		Exp int64 `json:"exp"`
		Nbf int64 `json:"nbf"`
	}
	if err := json.Unmarshal(body, &claims); err != nil {
		return nil, nil, fmt.Errorf("parse JWT claims: %w", err)
	}
	var iss, exp *time.Time
	switch {
	case claims.Iat > 0:
		t := time.Unix(claims.Iat, 0).UTC()
		iss = &t
	case claims.Nbf > 0:
		t := time.Unix(claims.Nbf, 0).UTC()
		iss = &t
	}
	if claims.Exp > 0 {
		t := time.Unix(claims.Exp, 0).UTC()
		exp = &t
	}
	return iss, exp, nil
}
