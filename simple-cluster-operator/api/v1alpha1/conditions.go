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

	// ConditionResourcePrefix is prepended to a ClusterResource.Name to form
	// the per-resource apply condition type (e.g. "Resource-deployment-x").
	ConditionResourcePrefix = "Resource-"

	FinalizerName = "simplecluster.apeirora.eu/finalizer"
)

// +kubebuilder:validation:Enum=Pending;Provisioning;Ready;Failed;Deleting
type Phase string

const (
	PhasePending      Phase = "Pending"
	PhaseProvisioning Phase = "Provisioning"
	PhaseReady        Phase = "Ready"
	PhaseFailed       Phase = "Failed"
	PhaseDeleting     Phase = "Deleting"
)
