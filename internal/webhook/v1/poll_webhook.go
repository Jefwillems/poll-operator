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

// polllog is used for structured logging within the webhook.
var polllog = logf.Log.WithName("poll-resource")

// pollGK is the GroupKind used when building status errors.
var pollGK = schema.GroupKind{Group: "polls.jef.app", Kind: "Poll"}

// SetupPollWebhookWithManager registers the webhook for Poll in the manager.
func SetupPollWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &pollsv1.Poll{}).
		WithValidator(&PollCustomValidator{}).
		Complete()
}

// NOTE: If you want to customise the 'path', use the flags '--defaulting-path' or '--validation-path'.
// +kubebuilder:webhook:path=/validate-polls-jef-app-v1-poll,mutating=false,failurePolicy=fail,sideEffects=None,groups=polls.jef.app,resources=polls,verbs=create;update,versions=v1,name=vpoll-v1.kb.io,admissionReviewVersions=v1

// PollCustomValidator enforces the structural and integrity rules on Poll that
// the CRD OpenAPI schema cannot express:
//   - the type/choice/freeText discriminator on each question
//   - option uniqueness within a Choice question
//   - immutability of question ids and types on update (both key into
//     status.results and PollResponse.spec.answers)
type PollCustomValidator struct{}

// ValidateCreate validates a new Poll.
func (v *PollCustomValidator) ValidateCreate(_ context.Context, obj *pollsv1.Poll) (admission.Warnings, error) {
	polllog.Info("Validating Poll on create", "name", obj.GetName())
	if errs := validatePollSpec(obj); len(errs) > 0 {
		return nil, apierrors.NewInvalid(pollGK, obj.Name, errs)
	}
	return nil, nil
}

// ValidateUpdate validates a change to an existing Poll.
func (v *PollCustomValidator) ValidateUpdate(_ context.Context, oldObj, newObj *pollsv1.Poll) (admission.Warnings, error) {
	polllog.Info("Validating Poll on update", "name", newObj.GetName())

	errs := validatePollSpec(newObj)
	errs = append(errs, validatePollUpdate(oldObj, newObj)...)

	if len(errs) > 0 {
		return nil, apierrors.NewInvalid(pollGK, newObj.Name, errs)
	}
	return nil, nil
}

// ValidateDelete is a no-op; deletes are always allowed.
func (v *PollCustomValidator) ValidateDelete(_ context.Context, _ *pollsv1.Poll) (admission.Warnings, error) {
	return nil, nil
}

// validatePollSpec enforces per-question structural rules.
func validatePollSpec(obj *pollsv1.Poll) field.ErrorList {
	errs := make(field.ErrorList, 0, len(obj.Spec.Questions))
	questionsPath := field.NewPath("spec", "questions")

	for i, q := range obj.Spec.Questions {
		p := questionsPath.Index(i)
		errs = append(errs, validateQuestion(&q, p)...)
	}
	return errs
}

// validateQuestion checks a single question's shape against its declared type.
func validateQuestion(q *pollsv1.PollQuestion, p *field.Path) field.ErrorList {
	var errs field.ErrorList

	switch q.Type {
	case pollsv1.QuestionTypeChoice:
		if q.Choice == nil {
			errs = append(errs, field.Required(p.Child("choice"),
				fmt.Sprintf("questions of type %q must set choice", q.Type)))
		} else {
			errs = append(errs, validateChoiceOptions(q.Choice, p.Child("choice"))...)
		}
		if q.FreeText != nil {
			errs = append(errs, field.Forbidden(p.Child("freeText"),
				fmt.Sprintf("must not be set for questions of type %q", q.Type)))
		}

	case pollsv1.QuestionTypeFreeText:
		if q.FreeText == nil {
			errs = append(errs, field.Required(p.Child("freeText"),
				fmt.Sprintf("questions of type %q must set freeText", q.Type)))
		}
		if q.Choice != nil {
			errs = append(errs, field.Forbidden(p.Child("choice"),
				fmt.Sprintf("must not be set for questions of type %q", q.Type)))
		}

	default:
		// The CRD schema's enum already rejects other values, but be defensive.
		errs = append(errs, field.NotSupported(
			p.Child("type"),
			q.Type,
			[]string{string(pollsv1.QuestionTypeChoice), string(pollsv1.QuestionTypeFreeText)},
		))
	}

	return errs
}

// validateChoiceOptions enforces that options within a single choice question
// are unique and non-empty.
func validateChoiceOptions(c *pollsv1.ChoiceQuestion, p *field.Path) field.ErrorList {
	var errs field.ErrorList
	optsPath := p.Child("options")
	seen := make(map[string]struct{}, len(c.Options))
	for i, opt := range c.Options {
		if opt == "" {
			errs = append(errs, field.Invalid(optsPath.Index(i), opt,
				"option must not be empty"))
			continue
		}
		if _, dup := seen[opt]; dup {
			errs = append(errs, field.Duplicate(optsPath.Index(i), opt))
		}
		seen[opt] = struct{}{}
	}
	return errs
}

// validatePollUpdate enforces integrity rules across an update: existing
// question ids may not have their type changed, and the set of ids can only
// grow or shrink, not be renamed in place. (Renaming an id would silently
// invalidate every PollResponse referencing it and lose the aggregated
// status.results row for that question.)
func validatePollUpdate(oldObj, newObj *pollsv1.Poll) field.ErrorList {
	var errs field.ErrorList
	questionsPath := field.NewPath("spec", "questions")

	oldByID := make(map[string]pollsv1.PollQuestion, len(oldObj.Spec.Questions))
	for _, q := range oldObj.Spec.Questions {
		oldByID[q.ID] = q
	}

	for i, q := range newObj.Spec.Questions {
		prev, ok := oldByID[q.ID]
		if !ok {
			// A new question id is always fine.
			continue
		}
		if prev.Type != q.Type {
			errs = append(errs, field.Forbidden(
				questionsPath.Index(i).Child("type"),
				fmt.Sprintf("field is immutable for existing question %q (was %q)", q.ID, prev.Type),
			))
		}
	}

	return errs
}
