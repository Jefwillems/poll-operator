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

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// PollResponsePhase represents the validation lifecycle of a PollResponse.
// +kubebuilder:validation:Enum=Pending;Accepted;Rejected
type PollResponsePhase string

const (
	// PollResponsePhasePending indicates the response has not yet been validated.
	PollResponsePhasePending PollResponsePhase = "Pending"
	// PollResponsePhaseAccepted indicates the response passed validation and has
	// been counted toward the Poll's aggregated results.
	PollResponsePhaseAccepted PollResponsePhase = "Accepted"
	// PollResponsePhaseRejected indicates the response failed validation and has
	// not been counted. See status.conditions for the reason.
	PollResponsePhaseRejected PollResponsePhase = "Rejected"
)

// PollAnswer holds a voter's answer to a single question.
// Exactly one of selectedOptions or text must be set, matching the question type
// defined on the referenced Poll.
type PollAnswer struct {
	// questionID matches the id of a question in the referenced Poll's spec.questions.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	QuestionID string `json:"questionID"`

	// selectedOptions lists the options chosen for a Choice question. It must
	// contain exactly one entry unless the question allows multiple choices.
	// Each entry must match one of the question's spec.choice.options values.
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MaxItems=20
	SelectedOptions []string `json:"selectedOptions,omitempty"`

	// text is the response for a FreeText question. Its length must not exceed
	// the question's spec.freeText.maxResponseLength.
	// +optional
	// +kubebuilder:validation:MaxLength=10000
	Text string `json:"text,omitempty"`
}

// PollResponseSpec defines the desired state of PollResponse
type PollResponseSpec struct {
	// pollRef identifies the Poll being answered. The Poll must exist in the
	// same namespace as this PollResponse.
	// +kubebuilder:validation:Required
	PollRef PollReference `json:"pollRef"`

	// respondent is an opaque, caller-supplied identifier for the voter (for
	// example a username, email hash, or session id). It is stored as-is and
	// used only to deduplicate submissions when set.
	// +kubebuilder:validation:MaxLength=253
	// +optional
	Respondent string `json:"respondent,omitempty"`

	// answers holds one entry per question the voter is answering. Answers for
	// unknown questions cause the response to be rejected.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=50
	// +listType=map
	// +listMapKey=questionID
	Answers []PollAnswer `json:"answers"`

	// submittedAt is the time the voter submitted this response. Defaults to the
	// object's creationTimestamp when unset.
	// +optional
	SubmittedAt *metav1.Time `json:"submittedAt,omitempty"`
}

// PollReference points to a Poll in the same namespace.
type PollReference struct {
	// name is the name of the Poll resource.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name"`
}

// PollResponseStatus defines the observed state of PollResponse.
type PollResponseStatus struct {
	// For Kubernetes API conventions, see:
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties

	// conditions represent the current state of the PollResponse resource.
	// Each condition has a unique type and reflects the status of a specific aspect of the resource.
	//
	// Standard condition types include:
	// - "Validated": the response was checked against the referenced Poll's schema
	// - "Counted":   the response has been included in the Poll's aggregated results
	//
	// The status of each condition is one of True, False, or Unknown.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// phase reports the validation lifecycle of the response.
	// +optional
	Phase PollResponsePhase `json:"phase,omitempty"`

	// observedPollGeneration is the metadata.generation of the referenced Poll
	// at the time this response was last validated. When the Poll's generation
	// advances, the response should be re-validated.
	// +optional
	ObservedPollGeneration int64 `json:"observedPollGeneration,omitempty"`

	// acceptedAt records when the response was accepted and counted.
	// +optional
	AcceptedAt *metav1.Time `json:"acceptedAt,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Poll",type=string,JSONPath=".spec.pollRef.name"
// +kubebuilder:printcolumn:name="Respondent",type=string,JSONPath=".spec.respondent"
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"

// PollResponse is the Schema for the pollresponses API
type PollResponse struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of PollResponse
	// +required
	Spec PollResponseSpec `json:"spec"`

	// status defines the observed state of PollResponse
	// +optional
	Status PollResponseStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// PollResponseList contains a list of PollResponse
type PollResponseList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []PollResponse `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &PollResponse{}, &PollResponseList{})
		return nil
	})
}
