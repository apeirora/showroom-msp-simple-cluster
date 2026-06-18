/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	showroomv1alpha1 "github.com/apeirora/showroom-msp-simple-cluster/simple-cluster-operator/api/v1alpha1"
)

func node(name string, labels map[string]string, ready bool, addrs ...corev1.NodeAddress) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels},
		Status: corev1.NodeStatus{
			Addresses: addrs,
			Conditions: []corev1.NodeCondition{{
				Type:   corev1.NodeReady,
				Status: map[bool]corev1.ConditionStatus{true: corev1.ConditionTrue, false: corev1.ConditionFalse}[ready],
			}},
		},
	}
}

func TestNodeStatusOf(t *testing.T) {
	n := node("cp-0",
		map[string]string{"node-role.kubernetes.io/control-plane": ""},
		true,
		corev1.NodeAddress{Type: corev1.NodeInternalIP, Address: "10.0.0.5"},
		corev1.NodeAddress{Type: corev1.NodeExternalIP, Address: "1.2.3.4"},
		corev1.NodeAddress{Type: corev1.NodeHostName, Address: "cp-0"},
	)
	got := nodeStatusOf(n)
	want := showroomv1alpha1.NodeStatus{
		Name: "cp-0", Role: "control-plane",
		ExternalIP: "1.2.3.4", InternalIP: "10.0.0.5", Ready: true,
	}
	if got != want {
		t.Fatalf("nodeStatusOf = %+v, want %+v", got, want)
	}
}

func TestNodeRole(t *testing.T) {
	cases := []struct {
		labels map[string]string
		want   string
	}{
		{map[string]string{"node-role.kubernetes.io/control-plane": ""}, "control-plane"},
		{map[string]string{"node-role.kubernetes.io/master": ""}, "control-plane"},
		{map[string]string{"kubernetes.io/hostname": "w0"}, "worker"},
		{nil, "worker"},
	}
	for _, c := range cases {
		if got := nodeRole(c.labels); got != c.want {
			t.Errorf("nodeRole(%v) = %q, want %q", c.labels, got, c.want)
		}
	}
}

func TestNodeReadyFalse(t *testing.T) {
	if got := nodeStatusOf(node("w0", nil, false)); got.Ready {
		t.Errorf("expected Ready=false, got %+v", got)
	}
}

func TestSortNodes(t *testing.T) {
	nodes := []showroomv1alpha1.NodeStatus{
		{Name: "w1", Role: "worker"},
		{Name: "cp1", Role: "control-plane"},
		{Name: "w0", Role: "worker"},
		{Name: "cp0", Role: "control-plane"},
	}
	sortNodes(nodes)
	want := []string{"cp0", "cp1", "w0", "w1"}
	for i, n := range nodes {
		if n.Name != want[i] {
			t.Fatalf("sortNodes order = %v, want %v", names(nodes), want)
		}
	}
}

func names(nodes []showroomv1alpha1.NodeStatus) []string {
	out := make([]string, len(nodes))
	for i, n := range nodes {
		out[i] = n.Name
	}
	return out
}
