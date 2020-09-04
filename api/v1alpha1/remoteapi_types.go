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

// RemoteAPISpec defines a remote api to add to this cluster
type RemoteAPISpec struct {
	RemoteCluster ObjectReference `json:"remoteCluster"`
	CRD           *CRDConfig      `json:"crd"`
}

// CRDConfig describes how a remote CRD is made available in this cluster
type CRDConfig struct {
	// Name of the CRD object in the remote cluster
	Name string `json:"name"`
}

// RemoteAPIStatus defines the observed state of RemoteAPI
type RemoteAPIStatus struct {
	// ObservedGeneration is the most recent generation observed by the controller
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Current conditions that apply to this instance
	Conditions []RemoteAPICondition `json:"conditions,omitempty"`
	// DEPRECATED.
	// Phase represents the current lifecycle state of this object.
	// Consider this field DEPRECATED, it will be removed as soon as there
	// is a mechanism to map conditions to strings when printing the property.
	// This is only for display purpose, for everything else use conditions.
	Phase RemoteAPIPhase `json:"phase,omitempty"`
}

func (s *RemoteAPIStatus) updatePhase() {
	// Unready
	if s.GetCondition(RemoteAPIReady).Status != ConditionTrue {
		s.Phase = RemoteAPIPhaseUnready
		return
	}

	// Available
	if s.GetCondition(RemoteAPIAvailable).Status == ConditionTrue {
		s.Phase = RemoteAPIPhaseAvailable
		return
	}

	// Fallback
	s.Phase = RemoteAPIPhasePending
}

func (s *RemoteAPIStatus) SetCondition(condition RemoteAPICondition) {
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
func (s *RemoteAPIStatus) GetCondition(t RemoteAPIConditionType) RemoteAPICondition {
	for _, cond := range s.Conditions {
		if cond.Type == t {
			return cond
		}
	}
	return RemoteAPICondition{Type: t}
}

// RemoteAPIPhase is a simple representation of the curent state of an instance for kubectl.
type RemoteAPIPhase string

const (
	// used for RemoteAPIs that are not yet available.
	RemoteAPIPhasePending RemoteAPIPhase = "Pending"
	// used for RemoteAPIs that are available.
	RemoteAPIPhaseAvailable RemoteAPIPhase = "Available"
	// used for RemoteAPIs that are not ready.
	RemoteAPIPhaseUnready RemoteAPIPhase = "Unready"
)

type RemoteAPICondition struct {
	// Type is the type of the RemoteAPI condition.
	Type RemoteAPIConditionType `json:"type"`
	// Status is the status of the condition, one of ('True', 'False', 'Unknown').
	Status ConditionStatus `json:"status"`
	// LastTransitionTime is the last time the condition transits from one status to another.
	LastTransitionTime metav1.Time `json:"lastTransitionTime"`
	// Reason is the (brief) reason for the condition's last transition.
	Reason string `json:"reason"`
	// Message is the human readable message indicating details about last transition.
	Message string `json:"message"`
}

type RemoteAPIConditionType string

const (
	// RemoteAPIReady tracks if the RemoteAPI is alive and well.
	// RemoteAPIs may become unready, after beeing available as part of their normal lifecycle.
	RemoteAPIReady RemoteAPIConditionType = "Ready"
	// RemoteAPIAvailable tracks if the RemoteAPI was setup
	RemoteAPIAvailable RemoteAPIConditionType = "Available"
)

// RemoteAPI configures the use of an API from a remote kubernetes cluster.
// +kubebuilder:object:root=true
// +kubebuilder:printcolumn:name="Remote Cluster",type="string",JSONPath=".spec.remoteCluster.name"
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:shortName=rapi,categories=catapult
type RemoteAPI struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RemoteAPISpec   `json:"spec,omitempty"`
	Status RemoteAPIStatus `json:"status,omitempty"`
}

// RemoteAPIList contains a list of RemoteAPIs.
// +kubebuilder:object:root=true
type RemoteAPIList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RemoteAPI `json:"items"`
}

func init() {
	SchemeBuilder.Register(&RemoteAPI{}, &RemoteAPIList{})
}
