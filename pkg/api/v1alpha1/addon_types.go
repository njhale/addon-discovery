/*

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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AddonSpec defines the desired state of Addon
type AddonSpec struct{}

// AddonStatus describes the observed state of an addon and its components.
type AddonStatus struct {
	// Components describes resources that compose the addon.
	// +optional
	Components *Components `json:"components,omitempty"`
}

// Components tracks the resources that compose an addon.
type Components struct {
	// LabelSelector is a label query over a set of resources used to select the addon's components
	LabelSelector *metav1.LabelSelector `json:"labelSelector"`

	// Refs are a set of references to the addon's component resources, selected with LabelSelector.
	// +optional
	Refs []Ref `json:"refs,omitempty"`
}

// Ref is a resource reference.
type Ref struct {
	*corev1.ObjectReference `json:",inline"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:storageversion

// Addon represents a cluster addon.
type Addon struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AddonSpec   `json:"spec,omitempty"`
	Status AddonStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AddonList contains a list of Addons.
type AddonList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Addon `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Addon{}, &AddonList{})
}
