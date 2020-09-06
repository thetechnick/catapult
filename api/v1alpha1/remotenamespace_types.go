/*
Copyright 2020 The Vedette authors.

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

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RemoteNamespaceSpec defines the desired state of RemoteNamespace.
type RemoteNamespaceSpec struct {
	// Name of the provisioner this RemoteNamespace was assigned to.
	RemoteNamespaceClass string `json:"remoteNamespaceClass,omitempty"`
	// Back-Reference to the Claim binding to this RemoteNamespace.
	// Must be set if Bound.
	Claim *ObjectReference `json:"claim,omitempty"`
	// Name of the Namespace in the RemoteCluster.
	RemoteNamespaceName *string `json:"remoteNamespaceName,omitempty"`
	// RemoteCluster this namespace belongs to.
	RemoteCluster ObjectReference `json:"remoteCluster"`
	// What happens with this RemoteNamespace after it is released from a Claim.
	// +kubebuilder:default=Delete
	ReclaimPolicy RemoteNamespaceReclaimPolicy `json:"reclaimPolicy,omitempty"`
}

type RemoteNamespaceReclaimPolicy string

const (
	RemoteNamespaceReclaimPolicyDelete RemoteNamespaceReclaimPolicy = "Delete"
	RemoteNamespaceReclaimPolicyRetain RemoteNamespaceReclaimPolicy = "Retain"
)

// RemoteNamespaceStatus defines the observed state of RemoteNamespace.
type RemoteNamespaceStatus struct {
	// ObservedGeneration is the most recent generation observed by the controller.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Current conditions that apply to this instance.
	Conditions []RemoteNamespaceCondition `json:"conditions,omitempty"`
	// DEPRECATED.
	// Phase represents the current lifecycle state of this object.
	// Consider this field DEPRECATED, it will be removed as soon as there
	// is a mechanism to map conditions to strings when printing the property.
	// This is only for display purpose, for everything else use conditions.
	Phase RemoteNamespacePhase `json:"phase,omitempty"`
}

func (s *RemoteNamespaceStatus) updatePhase() {
	// Unready
	if s.GetCondition(RemoteNamespaceReady).Status != ConditionTrue {
		s.Phase = RemoteNamespacePhaseUnready
		return
	}

	// Bound
	if bound := s.GetCondition(RemoteNamespaceBound); bound.Status == ConditionTrue {
		s.Phase = RemoteNamespacePhaseBound
		return
	} else if bound.Status == ConditionFalse &&
		// Released
		bound.Reason == "Released" {
		s.Phase = RemoteNamespacePhaseReleased
		return
	}

	// Available
	if s.GetCondition(RemoteNamespaceAvailable).Status == ConditionTrue {
		s.Phase = RemoteNamespacePhaseAvailable
		return
	}

	// Fallback
	s.Phase = RemoteNamespacePhasePending
}

func (s *RemoteNamespaceStatus) SetCondition(condition RemoteNamespaceCondition) {
	// always update the phase when conditions change
	defer s.updatePhase()

	// update existing condition
	for i := range s.Conditions {
		if s.Conditions[i].Type == condition.Type {
			if s.Conditions[i].Status != condition.Status {
				s.Conditions[i].LastTransitionTime = metav1.Now()
			}
			s.Conditions[i].Status = condition.Status
			s.Conditions[i].Reason = condition.Reason
			s.Conditions[i].Message = condition.Message
			return
		}
	}

	if condition.LastTransitionTime.IsZero() {
		condition.LastTransitionTime = metav1.Now()
	}

	// add to conditions
	s.Conditions = append(s.Conditions, condition)
}

// GetCondition returns the Condition of the given type.
func (s *RemoteNamespaceStatus) GetCondition(t RemoteNamespaceConditionType) RemoteNamespaceCondition {
	for _, cond := range s.Conditions {
		if cond.Type == t {
			return cond
		}
	}
	return RemoteNamespaceCondition{Type: t}
}

// RemoteNamespacePhase is a simple representation of the curent state of an instance for kubectl.
type RemoteNamespacePhase string

const (
	// used for RemoteNamespaces that are not yet available.
	RemoteNamespacePhasePending RemoteNamespacePhase = "Pending"
	// used for RemoteNamespaces that are not yet bound.
	RemoteNamespacePhaseAvailable RemoteNamespacePhase = "Available"
	// Bound RemoteNamespaces are bound to an InterfaceClaim.
	RemoteNamespacePhaseBound RemoteNamespacePhase = "Bound"
	// used for RemoteNamespaces that are not ready. May appear on Bound or Available instances.
	RemoteNamespacePhaseUnready RemoteNamespacePhase = "Unready"
	// used for RemoteNamespaces that where previously bound to a Claim.
	RemoteNamespacePhaseReleased RemoteNamespacePhase = "Released"
)

type RemoteNamespaceCondition struct {
	// Type is the type of the RemoteNamespace condition.
	Type RemoteNamespaceConditionType `json:"type"`
	// Status is the status of the condition, one of ('True', 'False', 'Unknown').
	Status ConditionStatus `json:"status"`
	// LastTransitionTime is the last time the condition transits from one status to another.
	LastTransitionTime metav1.Time `json:"lastTransitionTime"`
	// Reason is the (brief) reason for the condition's last transition.
	Reason string `json:"reason"`
	// Message is the human readable message indicating details about last transition.
	Message string `json:"message"`
}

type RemoteNamespaceConditionType string

const (
	// RemoteNamespaceBound tracks if the RemoteNamespace is bound to an InterfaceClaim.
	RemoteNamespaceBound RemoteNamespaceConditionType = "Bound"
	// RemoteNamespaceReady tracks if the RemoteNamespace is alive and well.
	// RemoteNamespaces may become unready, after beeing bound or when reconfigured as part of their normal lifecycle.
	RemoteNamespaceReady RemoteNamespaceConditionType = "Ready"
	// RemoteNamespaceAvailable tracks if the RemoteNamespace ready to be bound.
	// Instances are marked as Available as soon as their provisioning is completed.
	// Instances stay Available until deleted or bound.
	RemoteNamespaceAvailable RemoteNamespaceConditionType = "Available"
)

// RemoteNamespace is the Schema for the interfaceinstances API
// +kubebuilder:object:root=true
// +kubebuilder:scope=Cluster
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Claim",type="string",JSONPath=".spec.claim.name"
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:shortName=ii,categories=vedette
type RemoteNamespace struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RemoteNamespaceSpec   `json:"spec,omitempty"`
	Status RemoteNamespaceStatus `json:"status,omitempty"`
}

// RemoteNamespaceList contains a list of RemoteNamespace
// +kubebuilder:object:root=true
type RemoteNamespaceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RemoteNamespace `json:"items"`
}

func init() {
	SchemeBuilder.Register(&RemoteNamespace{}, &RemoteNamespaceList{})
}
