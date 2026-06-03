/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

// Package arsys is a thin client for the Arsys CloudBuilder API, scoped to
// the dedicated-server lifecycle the operator needs: order, poll, delete.
// See https://api.cloudbuilder.es/documentation/v1/en/api/documentation.html
package arsys

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// DefaultBaseURL is the v1 API root.
const DefaultBaseURL = "https://api.cloudbuilder.es/v1"

// Server states returned in status.state.
const (
	StatePoweredOn  = "POWERED_ON"
	StatePoweredOff = "POWERED_OFF"
	StateDeploying  = "DEPLOYING"
	StateRemoving   = "REMOVING"
)

// Client talks to the Arsys CloudBuilder API with an X-TOKEN credential.
type Client struct {
	BaseURL string
	token   string
	http    *http.Client
}

// New returns a Client for the given API token.
func New(token string) *Client {
	return &Client{
		BaseURL: DefaultBaseURL,
		token:   token,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

// Server is the subset of the Arsys server object the operator consumes.
type Server struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	FirstPassword string `json:"first_password"`
	Status        Status `json:"status"`
	IPs           []IP   `json:"ips"`
}

// Status is the server's lifecycle state and operation progress.
type Status struct {
	State   string `json:"state"`
	Percent int    `json:"percent"`
}

// IP is one address assigned to the server.
type IP struct {
	IP string `json:"ip"`
}

// PrimaryIP returns the first assigned address, or "" if none yet.
func (s *Server) PrimaryIP() string {
	if len(s.IPs) == 0 {
		return ""
	}
	return s.IPs[0].IP
}

// CreateRequest is the POST /servers order body. ServerType "baremetal"
// (with Hardware.BaremetalModelID) orders a dedicated server; empty orders a
// cloud server (with Hardware.VCore/RAM/HDDs).
type CreateRequest struct {
	Name         string   `json:"name"`
	ServerType   string   `json:"server_type,omitempty"`
	Hardware     Hardware `json:"hardware"`
	ApplianceID  string   `json:"appliance_id"`
	DatacenterID string   `json:"datacenter_id"`
	SSHPassword  bool     `json:"ssh_password"`
	PublicKey    []string `json:"public_key,omitempty"`
	PowerOn      bool     `json:"power_on"`
}

// Hardware is the order's compute/storage shape. For a dedicated order set
// only BaremetalModelID; for cloud set VCore/CoresPerProcessor/RAM/HDDs.
type Hardware struct {
	BaremetalModelID  string `json:"baremetal_model_id,omitempty"`
	VCore             int32  `json:"vcore,omitempty"`
	CoresPerProcessor int32  `json:"cores_per_processor,omitempty"`
	RAM               int32  `json:"ram,omitempty"`
	HDDs              []HDD  `json:"hdds,omitempty"`
}

// ServerTypeBaremetal is the server_type value for a dedicated order.
const ServerTypeBaremetal = "baremetal"

// HDD is one disk in the order.
type HDD struct {
	Size   int32 `json:"size"`
	IsMain bool  `json:"is_main"`
}

// CreateServer orders a dedicated server (POST /servers). The returned
// Server carries the id and first_password; provisioning is asynchronous.
func (c *Client) CreateServer(ctx context.Context, req CreateRequest) (*Server, error) {
	var out Server
	if err := c.do(ctx, http.MethodPost, "/servers", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetServer fetches current server state (GET /servers/{id}).
func (c *Client) GetServer(ctx context.Context, id string) (*Server, error) {
	var out Server
	if err := c.do(ctx, http.MethodGet, "/servers/"+id, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteServer cancels a server (DELETE /servers/{id}). A 404 is treated as
// already gone.
func (c *Client) DeleteServer(ctx context.Context, id string) error {
	err := c.do(ctx, http.MethodDelete, "/servers/"+id, nil, nil)
	if apiErr, ok := err.(*APIError); ok && apiErr.StatusCode == http.StatusNotFound {
		return nil
	}
	return err
}

// APIError is a non-2xx response from the API.
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("arsys api: status %d: %s", e.StatusCode, e.Body)
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var rdr io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		rdr = bytes.NewReader(buf)
	}
	httpReq, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, rdr)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("X-TOKEN", c.token)
	httpReq.Header.Set("Accept", "application/json")
	// Always set Content-Type — Arsys rejects bodyless requests (e.g. DELETE)
	// without it (406 WRONG_CONTENT_TYPE).
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &APIError{StatusCode: resp.StatusCode, Body: string(data)}
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}
