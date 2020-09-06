/*
Copyright 2020 The Catapult authors.

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

// RemoteNamespaceClaimSpec defines the desired state of RemoteNamespaceClaim.
type RemoteNamespaceClaimSpec struct {
	// RemoteCluster this claim is targeting.
	RemoteCluster ObjectReference `json:"remoteCluster"`
	// RemoteNamespace this Claim is bound to.
	// Must be set when the Claim is Bound.
	RemoteNamespace *ObjectReference `json:"remoteNamespace,omitempty"`
}

// RemoteNamespaceClaimStatus defines the observed state of RemoteNamespaceClaim.
type RemoteNamespaceClaimStatus struct {
	// ObservedGeneration is the most recent generation observed by the controller.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Current conditions that apply to this instance.
	Conditions []RemoteNamespaceClaimCondition `json:"conditions,omitempty"`
	// DEPRECATED.
	// Phase represents the current lifecycle state of this object.
	// Consider this field DEPRECATED, it will be removed as soon as there
	// is a mechanism to map conditions to strings when printing the property.
	// This is only for display purpose, for everything else use conditions.
	Phase RemoteNamespaceClaimPhase `json:"phase,omitempty"`
}

func (s *RemoteNamespaceClaimStatus) updatePhase() {
	// Lost
	if s.GetCondition(RemoteNamespaceClaimLost).Status == ConditionTrue {
		s.Phase = RemoteNamespaceClaimPhaseLost
		return
	}

	// Bound
	if s.GetCondition(RemoteNamespaceClaimBound).Status == ConditionTrue {
		s.Phase = RemoteNamespaceClaimPhaseBound
		return
	}

	// Fallback
	s.Phase = RemoteNamespaceClaimPhasePending
}

func (s *RemoteNamespaceClaimStatus) SetCondition(condition RemoteNamespaceClaimCondition) {
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
func (s *RemoteNamespaceClaimStatus) GetCondition(t RemoteNamespaceClaimConditionType) RemoteNamespaceClaimCondition {
	for _, cond := range s.Conditions {
		if cond.Type == t {
			return cond
		}
	}
	return RemoteNamespaceClaimCondition{Type: t}
}

// RemoteNamespaceClaimPhase is a simple representation of the curent state of an instance for kubectl.
type RemoteNamespaceClaimPhase string

const (
	// Pending is the initial state of the RemoteNamespaceClaim until Bound to an RemoteNamespace.
	RemoteNamespaceClaimPhasePending RemoteNamespaceClaimPhase = "Pending"
	// The Claim is Bound after a matching RemoteNamespace has been found.
	RemoteNamespaceClaimPhaseBound RemoteNamespaceClaimPhase = "Bound"
	// The Bound RemoteNamespace can no longer be found.
	RemoteNamespaceClaimPhaseLost RemoteNamespaceClaimPhase = "Lost"
)

type RemoteNamespaceClaimCondition struct {
	// Type is the type of the RemoteNamespaceClaim condition.
	Type RemoteNamespaceClaimConditionType `json:"type"`
	// Status is the status of the condition, one of ('True', 'False', 'Unknown').
	Status ConditionStatus `json:"status"`
	// LastTransitionTime is the last time the condition transits from one status to another.
	LastTransitionTime metav1.Time `json:"lastTransitionTime"`
	// Reason is the (brief) reason for the condition's last transition.
	Reason string `json:"reason"`
	// Message is the human readable message indicating details about last transition.
	Message string `json:"message"`
}

type RemoteNamespaceClaimConditionType string

const (
	// RemoteNamespaceClaimBound signifies whether the RemoteNamespaceClaim is bound to an InterfaceInstance.
	RemoteNamespaceClaimBound RemoteNamespaceClaimConditionType = "Bound"
	// RemoteNamespaceClaimLost is set to true when the Bound InterfaceInstance was deleted.
	RemoteNamespaceClaimLost RemoteNamespaceClaimConditionType = "Lost"
)

// RemoteNamespaceClaim is a user's request to bind to an RemoteNamespace.
// +kubebuilder:object:root=true
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:shortName=rnc,categories=catapult
type RemoteNamespaceClaim struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec RemoteNamespaceClaimSpec `json:"spec,omitempty"`
}

// RemoteNamespaceClaimList contains a list of RemoteNamespaceClaim
// +kubebuilder:object:root=true
type RemoteNamespaceClaimList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RemoteNamespaceClaim `json:"items"`
}

func init() {
	SchemeBuilder.Register(&RemoteNamespaceClaim{}, &RemoteNamespaceClaimList{})
}
