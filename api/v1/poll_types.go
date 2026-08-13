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

// PollPhase represents the lifecycle phase of a Poll.
// +kubebuilder:validation:Enum=Pending;Open;Closed
type PollPhase string

const (
	// PollPhasePending indicates the Poll has been created but is not yet open for voting.
	PollPhasePending PollPhase = "Pending"
	// PollPhaseOpen indicates the Poll is currently accepting votes.
	PollPhaseOpen PollPhase = "Open"
	// PollPhaseClosed indicates the Poll is no longer accepting votes.
	PollPhaseClosed PollPhase = "Closed"
)

// QuestionType discriminates between the supported question formats.
// +kubebuilder:validation:Enum=Choice;FreeText
type QuestionType string

const (
	// QuestionTypeChoice indicates the voter selects from a fixed set of options.
	QuestionTypeChoice QuestionType = "Choice"
	// QuestionTypeFreeText indicates the voter submits arbitrary text.
	QuestionTypeFreeText QuestionType = "FreeText"
)

// PollQuestion defines a single question within a Poll.
type PollQuestion struct {
	// id is a stable identifier for this question, unique within the Poll.
	// It is used to correlate spec entries with status results and must not
	// change after the Poll is created.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern="^[a-z0-9]([-a-z0-9]*[a-z0-9])?$"
	ID string `json:"id"`

	// prompt is the question text shown to voters.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=500
	Prompt string `json:"prompt"`

	// type selects the question format. Choice questions require options;
	// FreeText questions accept an arbitrary text response.
	// +kubebuilder:validation:Required
	Type QuestionType `json:"type"`

	// choice holds configuration for Choice-type questions.
	// Must be set when type is Choice and must be unset otherwise.
	// +optional
	Choice *ChoiceQuestion `json:"choice,omitempty"`

	// freeText holds configuration for FreeText-type questions.
	// Must be set when type is FreeText and must be unset otherwise.
	// +optional
	FreeText *FreeTextQuestion `json:"freeText,omitempty"`
}

// ChoiceQuestion configures a question where voters pick from a fixed list.
type ChoiceQuestion struct {
	// options is the set of choices voters may select from.
	// At least two options are required.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=2
	// +kubebuilder:validation:MaxItems=20
	// +listType=atomic
	Options []string `json:"options"`

	// allowMultipleChoices controls whether a voter may select more than one option.
	// +kubebuilder:default=false
	// +optional
	AllowMultipleChoices bool `json:"allowMultipleChoices,omitempty"`
}

// FreeTextQuestion configures a question where voters submit arbitrary text.
type FreeTextQuestion struct {
	// maxResponseLength caps the length of an accepted response, in characters.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=10000
	// +kubebuilder:default=1000
	// +optional
	MaxResponseLength int32 `json:"maxResponseLength,omitempty"`
}

// PollSpec defines the desired state of Poll
type PollSpec struct {
	// title is a short human-readable name for the Poll.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=200
	Title string `json:"title"`

	// questions is the ordered list of questions the Poll asks.
	// Each question must have a unique id.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=50
	// +listType=map
	// +listMapKey=id
	Questions []PollQuestion `json:"questions"`

	// closesAt is the time at which the Poll stops accepting votes.
	// If unset, the Poll remains open until explicitly closed.
	// +optional
	ClosesAt *metav1.Time `json:"closesAt,omitempty"`
}

// OptionResult reports the vote count for a single Choice option.
type OptionResult struct {
	// option is the choice text, matching an entry in the question's options.
	// +kubebuilder:validation:Required
	Option string `json:"option"`

	// votes is the number of votes recorded for this option.
	// +kubebuilder:validation:Minimum=0
	Votes int32 `json:"votes"`
}

// QuestionResult reports aggregated responses for a single question.
type QuestionResult struct {
	// questionID matches the id of a question in spec.questions.
	// +kubebuilder:validation:Required
	QuestionID string `json:"questionID"`

	// options reports per-option vote tallies for Choice questions.
	// Empty for FreeText questions.
	// +listType=map
	// +listMapKey=option
	// +optional
	Options []OptionResult `json:"options,omitempty"`

	// responses is the number of responses recorded for this question.
	// For Choice questions this counts voters; for FreeText questions this
	// counts submitted texts.
	// +kubebuilder:validation:Minimum=0
	// +optional
	Responses int32 `json:"responses,omitempty"`
}

// PollStatus defines the observed state of Poll.
type PollStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// For Kubernetes API conventions, see:
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties

	// conditions represent the current state of the Poll resource.
	// Each condition has a unique type and reflects the status of a specific aspect of the resource.
	//
	// Standard condition types include:
	// - "Available": the resource is fully functional
	// - "Progressing": the resource is being created or updated
	// - "Degraded": the resource failed to reach or maintain its desired state
	//
	// The status of each condition is one of True, False, or Unknown.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// phase reports the current lifecycle phase of the Poll.
	// +optional
	Phase PollPhase `json:"phase,omitempty"`

	// openedAt records when the Poll transitioned to the Open phase.
	// +optional
	OpenedAt *metav1.Time `json:"openedAt,omitempty"`

	// closedAt records when the Poll transitioned to the Closed phase.
	// +optional
	ClosedAt *metav1.Time `json:"closedAt,omitempty"`

	// results reports per-question aggregated responses.
	// +listType=map
	// +listMapKey=questionID
	// +optional
	Results []QuestionResult `json:"results,omitempty"`

	// totalResponses is the number of voters who have submitted a response
	// to at least one question.
	// +kubebuilder:validation:Minimum=0
	// +optional
	TotalResponses int32 `json:"totalResponses,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Title",type=string,JSONPath=".spec.title"
// +kubebuilder:printcolumn:name="Responses",type=integer,JSONPath=".status.totalResponses"
// +kubebuilder:printcolumn:name="Closes-At",type=string,JSONPath=".spec.closesAt"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"

// Poll is the Schema for the polls API
type Poll struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of Poll
	// +required
	Spec PollSpec `json:"spec"`

	// status defines the observed state of Poll
	// +optional
	Status PollStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// PollList contains a list of Poll
type PollList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []Poll `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &Poll{}, &PollList{})
		return nil
	})
}
