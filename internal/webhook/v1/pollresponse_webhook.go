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
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	pollsv1 "github.com/Jefwillems/poll-operator/api/v1"
)

// pollresponselog is used for structured logging within the webhook.
var pollresponselog = logf.Log.WithName("pollresponse-resource")

// pollResponseGK is the GroupKind used when building status errors so the API
// server reports a well-typed rejection.
var pollResponseGK = schema.GroupKind{Group: "polls.jef.app", Kind: "PollResponse"}

// SetupPollResponseWebhookWithManager registers the webhook for PollResponse in the manager.
func SetupPollResponseWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &pollsv1.PollResponse{}).
		WithValidator(&PollResponseCustomValidator{}).
		Complete()
}

// NOTE: If you want to customise the 'path', use the flags '--defaulting-path' or '--validation-path'.
// +kubebuilder:webhook:path=/validate-polls-jef-app-v1-pollresponse,mutating=false,failurePolicy=fail,sideEffects=None,groups=polls.jef.app,resources=pollresponses,verbs=create;update,versions=v1,name=vpollresponse-v1.kb.io,admissionReviewVersions=v1

// PollResponseCustomValidator enforces structural constraints on PollResponse
// that the CRD OpenAPI schema cannot express, notably the exactly-one-of
// answer discriminator and immutability of pollRef.
//
// Semantic checks (does the referenced Poll exist, is it still open, is the
// selected option one of the Poll's configured choices) live in the
// PollResponse controller so admission remains fast and doesn't race with
// concurrent Poll edits.
type PollResponseCustomValidator struct{}

// ValidateCreate validates a new PollResponse.
func (v *PollResponseCustomValidator) ValidateCreate(_ context.Context, obj *pollsv1.PollResponse) (admission.Warnings, error) {
	pollresponselog.Info("Validating PollResponse on create", "name", obj.GetName())
	if errs := validatePollResponseSpec(obj); len(errs) > 0 {
		return nil, apierrors.NewInvalid(pollResponseGK, obj.Name, errs)
	}
	return nil, nil
}

// ValidateUpdate validates a change to an existing PollResponse.
func (v *PollResponseCustomValidator) ValidateUpdate(_ context.Context, oldObj, newObj *pollsv1.PollResponse) (admission.Warnings, error) {
	pollresponselog.Info("Validating PollResponse on update", "name", newObj.GetName())

	errs := validatePollResponseSpec(newObj)

	// pollRef.name is immutable: once submitted, a response is bound to its Poll.
	if oldObj.Spec.PollRef.Name != newObj.Spec.PollRef.Name {
		errs = append(errs, field.Forbidden(
			field.NewPath("spec", "pollRef", "name"),
			fmt.Sprintf("field is immutable (was %q)", oldObj.Spec.PollRef.Name),
		))
	}

	if len(errs) > 0 {
		return nil, apierrors.NewInvalid(pollResponseGK, newObj.Name, errs)
	}
	return nil, nil
}

// ValidateDelete is a no-op; deletes are always allowed.
func (v *PollResponseCustomValidator) ValidateDelete(_ context.Context, _ *pollsv1.PollResponse) (admission.Warnings, error) {
	return nil, nil
}

// validatePollResponseSpec enforces the structural rules that the CRD schema
// cannot: primarily that each answer sets exactly one of selectedOptions or
// text.
func validatePollResponseSpec(obj *pollsv1.PollResponse) field.ErrorList {
	var errs field.ErrorList
	answersPath := field.NewPath("spec", "answers")

	for i, ans := range obj.Spec.Answers {
		p := answersPath.Index(i)
		hasSelections := len(ans.SelectedOptions) > 0
		hasText := ans.Text != ""

		switch {
		case hasSelections && hasText:
			errs = append(errs, field.Invalid(p, ans,
				"exactly one of selectedOptions or text must be set, not both"))
		case !hasSelections && !hasText:
			errs = append(errs, field.Required(p,
				"exactly one of selectedOptions or text must be set"))
		}

		if hasSelections {
			seen := make(map[string]struct{}, len(ans.SelectedOptions))
			for j, sel := range ans.SelectedOptions {
				if sel == "" {
					errs = append(errs, field.Invalid(
						p.Child("selectedOptions").Index(j), sel,
						"selected option must not be empty"))
					continue
				}
				if _, dup := seen[sel]; dup {
					errs = append(errs, field.Duplicate(
						p.Child("selectedOptions").Index(j), sel))
				}
				seen[sel] = struct{}{}
			}
		}
	}

	return errs
}
