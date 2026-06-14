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
	// RSAKey is a single inline public SSH key copied into the server at order
	// time. Note: the API's public_key field is NOT this — it takes IDs of
	// pre-registered SSH keys, not inline key material.
	RSAKey  string `json:"rsa_key,omitempty"`
	PowerOn bool   `json:"power_on"`
	// FirewallPolicyID assigns an existing firewall policy at order time.
	// When empty the API auto-creates a restrictive default (SSH only),
	// which blocks intra-cluster k3s traffic (6443/10250/8472) — so the
	// operator always sets this to a policy that opens the k3s ports.
	FirewallPolicyID string `json:"firewall_policy_id,omitempty"`
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

// StateEnabled is the BaremetalModel.State value for an orderable model.
const StateEnabled = "ENABLED"

// BaremetalModel is one dedicated-server model from GET /servers/baremetal_models.
type BaremetalModel struct {
	ID           string                  `json:"id"`
	Name         string                  `json:"name"`
	Hardware     BaremetalHardware       `json:"hardware"`
	State        string                  `json:"state"`
	Availability []BaremetalAvailability `json:"availability"`
}

// BaremetalHardware is a model's fixed compute. TotalCores is core (sockets)
// times cores_per_processor; ram is in Unit (always "GB" in practice).
type BaremetalHardware struct {
	Core              int32  `json:"core"`
	CoresPerProcessor int32  `json:"cores_per_processor"`
	RAM               int32  `json:"ram"`
	Unit              string `json:"unit"`
}

// BaremetalAvailability is whether a model can be ordered in a datacenter.
type BaremetalAvailability struct {
	DatacenterID string `json:"datacenter_id"`
	Available    bool   `json:"available"`
}

// TotalCores is the model's usable core count (sockets * cores per processor).
func (h BaremetalHardware) TotalCores() int32 { return h.Core * h.CoresPerProcessor }

// RAMGB returns the RAM normalized to GB, honouring the Unit field.
func (h BaremetalHardware) RAMGB() int32 {
	switch h.Unit {
	case "MB":
		return h.RAM / 1024
	case "TB":
		return h.RAM * 1024
	default: // "GB" or unspecified
		return h.RAM
	}
}

// AvailableIn reports whether the model is orderable in the given datacenter.
func (m *BaremetalModel) AvailableIn(datacenterID string) bool {
	for _, a := range m.Availability {
		if a.DatacenterID == datacenterID && a.Available {
			return true
		}
	}
	return false
}

// ListBaremetalModels lists the dedicated-server models (GET
// /servers/baremetal_models). Undocumented but live; the operator uses it to
// auto-pick a model when the CR omits baremetalModelID.
func (c *Client) ListBaremetalModels(ctx context.Context) ([]BaremetalModel, error) {
	var out []BaremetalModel
	if err := c.do(ctx, http.MethodGet, "/servers/baremetal_models", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// SmallestModel returns the cheapest-fitting model: the ENABLED model available
// in datacenterID with at least minCores cores and minRAMGB GB of RAM, choosing
// the fewest cores and then the least RAM. Returns nil if none qualifies.
func SmallestModel(models []BaremetalModel, datacenterID string, minCores, minRAMGB int32) *BaremetalModel {
	var best *BaremetalModel
	for i := range models {
		m := &models[i]
		if m.State != StateEnabled || !m.AvailableIn(datacenterID) {
			continue
		}
		if m.Hardware.TotalCores() < minCores || m.Hardware.RAMGB() < minRAMGB {
			continue
		}
		if best == nil || smaller(m.Hardware, best.Hardware) {
			best = m
		}
	}
	return best
}

// smaller orders by fewest total cores, then least RAM.
func smaller(a, b BaremetalHardware) bool {
	if ac, bc := a.TotalCores(), b.TotalCores(); ac != bc {
		return ac < bc
	}
	return a.RAMGB() < b.RAMGB()
}

// HDD is one disk in the order.
type HDD struct {
	Size   int32 `json:"size"`
	IsMain bool  `json:"is_main"`
}

// ListServers returns all servers in the account (GET /servers).
func (c *Client) ListServers(ctx context.Context) ([]Server, error) {
	var out []Server
	if err := c.do(ctx, http.MethodGet, "/servers", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
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

// FirewallPolicy is the subset of a firewall policy object the operator uses.
type FirewallPolicy struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// FirewallRule is one inbound rule. Source is a CIDR/IP; empty means any.
type FirewallRule struct {
	Protocol string `json:"protocol"`
	PortFrom int32  `json:"port_from,omitempty"`
	PortTo   int32  `json:"port_to,omitempty"`
	Source   string `json:"source,omitempty"`
}

// FirewallPolicyCreate is the POST /firewall_policies body.
type FirewallPolicyCreate struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Rules       []FirewallRule `json:"rules"`
}

// ListFirewallPolicies returns all firewall policies in the account
// (GET /firewall_policies).
func (c *Client) ListFirewallPolicies(ctx context.Context) ([]FirewallPolicy, error) {
	var out []FirewallPolicy
	if err := c.do(ctx, http.MethodGet, "/firewall_policies", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CreateFirewallPolicy creates a firewall policy with the given rules
// (POST /firewall_policies).
func (c *Client) CreateFirewallPolicy(ctx context.Context, req FirewallPolicyCreate) (*FirewallPolicy, error) {
	var out FirewallPolicy
	if err := c.do(ctx, http.MethodPost, "/firewall_policies", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
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
