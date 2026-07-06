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

package v1alpha1

const (
	ConditionReady           = "Ready"
	ConditionProgressing     = "Progressing"
	ConditionError           = "Error"
	ConditionKubeconfigValid = "KubeconfigValid"

	// ConditionAutoscalingSupported is set only when the SC requests
	// workers.autoscaling: True when the chosen SCPC declares autoscaling
	// support, False when the pool stays fixed at workers.min.
	ConditionAutoscalingSupported = "AutoscalingSupported"

	// ConditionResourcePrefix is prepended to a ClusterResource.Name to form
	// the per-resource apply condition type (e.g. "Resource-deployment-x").
	ConditionResourcePrefix = "Resource-"

	FinalizerName = "simplecluster.apeirora.eu/finalizer"
)

// +kubebuilder:validation:Enum=Pending;Provisioning;Ready;Failed;Deleting;NoHardware
type Phase string

const (
	PhasePending      Phase = "Pending"
	PhaseProvisioning Phase = "Provisioning"
	PhaseReady        Phase = "Ready"
	PhaseFailed       Phase = "Failed"
	PhaseDeleting     Phase = "Deleting"
	// PhaseNoHardware — a from=profile input has no SC override and no
	// SCPC profile is large enough for the SC's requested resources. No
	// resource R is created until a fitting profile appears or the request
	// shrinks.
	PhaseNoHardware Phase = "NoHardware"
)
