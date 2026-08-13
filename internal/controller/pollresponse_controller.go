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

package controller

import (
	"context"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	pollsv1 "github.com/Jefwillems/poll-operator/api/v1"
)

// Condition types on PollResponse.
const (
	// pollResponseValidatedCondition indicates whether the response passed
	// schema validation against the referenced Poll.
	pollResponseValidatedCondition = "Validated"
	// pollResponseCountedCondition indicates whether the response has been
	// included in the referenced Poll's aggregated results.
	pollResponseCountedCondition = "Counted"
)

// Validation reason strings. Kept as constants so tests can assert on them.
const (
	reasonAccepted        = "Accepted"
	reasonPollNotFound    = "PollNotFound"
	reasonPollClosed      = "PollClosed"
	reasonUnknownQuestion = "UnknownQuestion"
	reasonWrongAnswerType = "WrongAnswerType"
	reasonUnknownOption   = "UnknownOption"
	reasonEmptyAnswer     = "EmptyAnswer"
	reasonTooManyChoices  = "TooManyChoices"
	reasonTextTooLong     = "TextTooLong"
	reasonInvalidQuestion = "InvalidQuestion"
)

// PollResponseReconciler reconciles a PollResponse object
type PollResponseReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=polls.jef.app,resources=pollresponses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=polls.jef.app,resources=pollresponses/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=polls.jef.app,resources=pollresponses/finalizers,verbs=update
// +kubebuilder:rbac:groups=polls.jef.app,resources=polls,verbs=get;list;watch

// Reconcile validates a PollResponse against its referenced Poll and reflects
// the outcome in status. It also sets an owner reference on the Poll so the
// response is garbage-collected when the Poll is deleted.
func (r *PollResponseReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var resp pollsv1.PollResponse
	if err := r.Get(ctx, req.NamespacedName, &resp); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get PollResponse: %w", err)
	}

	if !resp.DeletionTimestamp.IsZero() {
		// Nothing to do; there is no finalizer on this resource.
		return ctrl.Result{}, nil
	}

	// Fetch the referenced Poll.
	var poll pollsv1.Poll
	pollKey := client.ObjectKey{Namespace: resp.Namespace, Name: resp.Spec.PollRef.Name}
	err := r.Get(ctx, pollKey, &poll)
	switch {
	case apierrors.IsNotFound(err):
		return r.reject(ctx, &resp, 0, reasonPollNotFound,
			fmt.Sprintf("Poll %q not found in namespace %q", pollKey.Name, pollKey.Namespace))
	case err != nil:
		return ctrl.Result{}, fmt.Errorf("get Poll: %w", err)
	}

	// Make sure the response is owned by the Poll for GC.
	if err := r.ensureOwnerReference(ctx, &resp, &poll); err != nil {
		return ctrl.Result{}, err
	}

	// Validate.
	if verr := validateResponse(&poll, &resp, time.Now()); verr != nil {
		return r.reject(ctx, &resp, poll.Generation, verr.reason, verr.message)
	}

	return r.accept(ctx, &resp, poll.Generation)
}

// ensureOwnerReference sets the Poll as the controller-owner of the response
// if it isn't already, and patches the object.
func (r *PollResponseReconciler) ensureOwnerReference(ctx context.Context, resp *pollsv1.PollResponse, poll *pollsv1.Poll) error {
	for _, ref := range resp.OwnerReferences {
		if ref.UID == poll.UID {
			return nil
		}
	}
	patch := client.MergeFrom(resp.DeepCopy())
	if err := controllerutil.SetControllerReference(poll, resp, r.Scheme); err != nil {
		return fmt.Errorf("set controller reference: %w", err)
	}
	if err := r.Patch(ctx, resp, patch); err != nil {
		return fmt.Errorf("patch owner reference: %w", err)
	}
	return nil
}

// validationError describes why a response was rejected.
type validationError struct {
	reason  string
	message string
}

func (v *validationError) Error() string { return v.reason + ": " + v.message }

// validateResponse checks the response against the Poll's schema. It returns
// a validationError describing the first problem it encounters, or nil if the
// response is acceptable.
func validateResponse(poll *pollsv1.Poll, resp *pollsv1.PollResponse, now time.Time) *validationError {
	// Reject responses submitted after the Poll closed.
	if poll.Spec.ClosesAt != nil && !now.Before(poll.Spec.ClosesAt.Time) {
		return &validationError{reasonPollClosed,
			fmt.Sprintf("Poll closed at %s", poll.Spec.ClosesAt.Format(time.RFC3339))}
	}

	// Index questions by id for O(1) lookup.
	questionsByID := make(map[string]*pollsv1.PollQuestion, len(poll.Spec.Questions))
	for i := range poll.Spec.Questions {
		q := &poll.Spec.Questions[i]
		questionsByID[q.ID] = q
	}

	for _, ans := range resp.Spec.Answers {
		q, ok := questionsByID[ans.QuestionID]
		if !ok {
			return &validationError{reasonUnknownQuestion,
				fmt.Sprintf("answer references unknown questionID %q", ans.QuestionID)}
		}
		if verr := validateAnswer(q, &ans); verr != nil {
			return verr
		}
	}

	return nil
}

// validateAnswer checks one answer against the question's declared shape.
func validateAnswer(q *pollsv1.PollQuestion, ans *pollsv1.PollAnswer) *validationError {
	switch q.Type {
	case pollsv1.QuestionTypeChoice:
		if q.Choice == nil {
			return &validationError{reasonInvalidQuestion,
				fmt.Sprintf("question %q has type Choice but no choice configuration", q.ID)}
		}
		if ans.Text != "" {
			return &validationError{reasonWrongAnswerType,
				fmt.Sprintf("question %q is Choice; text must be empty", q.ID)}
		}
		if len(ans.SelectedOptions) == 0 {
			return &validationError{reasonEmptyAnswer,
				fmt.Sprintf("question %q requires at least one selected option", q.ID)}
		}
		if !q.Choice.AllowMultipleChoices && len(ans.SelectedOptions) > 1 {
			return &validationError{reasonTooManyChoices,
				fmt.Sprintf("question %q does not allow multiple choices", q.ID)}
		}
		allowed := make(map[string]struct{}, len(q.Choice.Options))
		for _, o := range q.Choice.Options {
			allowed[o] = struct{}{}
		}
		seen := make(map[string]struct{}, len(ans.SelectedOptions))
		for _, sel := range ans.SelectedOptions {
			if _, ok := allowed[sel]; !ok {
				return &validationError{reasonUnknownOption,
					fmt.Sprintf("question %q: option %q is not one of the configured choices", q.ID, sel)}
			}
			if _, dup := seen[sel]; dup {
				return &validationError{reasonTooManyChoices,
					fmt.Sprintf("question %q: option %q selected more than once", q.ID, sel)}
			}
			seen[sel] = struct{}{}
		}

	case pollsv1.QuestionTypeFreeText:
		if len(ans.SelectedOptions) > 0 {
			return &validationError{reasonWrongAnswerType,
				fmt.Sprintf("question %q is FreeText; selectedOptions must be empty", q.ID)}
		}
		if ans.Text == "" {
			return &validationError{reasonEmptyAnswer,
				fmt.Sprintf("question %q requires a text response", q.ID)}
		}
		if q.FreeText != nil && q.FreeText.MaxResponseLength > 0 &&
			int32(len(ans.Text)) > q.FreeText.MaxResponseLength {
			return &validationError{reasonTextTooLong,
				fmt.Sprintf("question %q: text exceeds maxResponseLength (%d)", q.ID, q.FreeText.MaxResponseLength)}
		}

	default:
		return &validationError{reasonInvalidQuestion,
			fmt.Sprintf("question %q has unsupported type %q", q.ID, q.Type)}
	}
	return nil
}

// accept transitions the response to Accepted and marks it counted.
func (r *PollResponseReconciler) accept(ctx context.Context, resp *pollsv1.PollResponse, pollGen int64) (ctrl.Result, error) {
	desired := resp.Status.DeepCopy()
	desired.Phase = pollsv1.PollResponsePhaseAccepted
	desired.ObservedPollGeneration = pollGen
	if desired.AcceptedAt == nil {
		t := metav1.Now()
		desired.AcceptedAt = &t
	}
	setResponseCondition(desired, pollResponseValidatedCondition, metav1.ConditionTrue, reasonAccepted,
		"Response passed validation", resp.Generation)
	setResponseCondition(desired, pollResponseCountedCondition, metav1.ConditionTrue, reasonAccepted,
		"Response is included in Poll results", resp.Generation)

	if responseStatusEqual(&resp.Status, desired) {
		return ctrl.Result{}, nil
	}

	patch := client.MergeFrom(resp.DeepCopy())
	resp.Status = *desired
	if err := r.Status().Patch(ctx, resp, patch); err != nil {
		if apierrors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, fmt.Errorf("patch PollResponse status: %w", err)
	}
	logf.FromContext(ctx).Info("Accepted PollResponse", "poll", resp.Spec.PollRef.Name)
	return ctrl.Result{}, nil
}

// reject transitions the response to Rejected with the given reason and message.
func (r *PollResponseReconciler) reject(ctx context.Context, resp *pollsv1.PollResponse, pollGen int64, reason, message string) (ctrl.Result, error) {
	desired := resp.Status.DeepCopy()
	desired.Phase = pollsv1.PollResponsePhaseRejected
	desired.ObservedPollGeneration = pollGen
	desired.AcceptedAt = nil
	setResponseCondition(desired, pollResponseValidatedCondition, metav1.ConditionFalse, reason,
		message, resp.Generation)
	setResponseCondition(desired, pollResponseCountedCondition, metav1.ConditionFalse, reason,
		"Rejected responses are not counted", resp.Generation)

	if responseStatusEqual(&resp.Status, desired) {
		return ctrl.Result{}, nil
	}

	patch := client.MergeFrom(resp.DeepCopy())
	resp.Status = *desired
	if err := r.Status().Patch(ctx, resp, patch); err != nil {
		if apierrors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, fmt.Errorf("patch PollResponse status: %w", err)
	}
	logf.FromContext(ctx).Info("Rejected PollResponse",
		"poll", resp.Spec.PollRef.Name, "reason", reason)
	return ctrl.Result{}, nil
}

func setResponseCondition(status *pollsv1.PollResponseStatus, condType string, s metav1.ConditionStatus, reason, message string, generation int64) {
	meta.SetStatusCondition(&status.Conditions, metav1.Condition{
		Type:               condType,
		Status:             s,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: generation,
	})
}

// responseStatusEqual compares two PollResponseStatus values for the purposes
// of skipping a no-op status patch.
func responseStatusEqual(a, b *pollsv1.PollResponseStatus) bool {
	if a.Phase != b.Phase ||
		a.ObservedPollGeneration != b.ObservedPollGeneration ||
		!timePtrEqual(a.AcceptedAt, b.AcceptedAt) {
		return false
	}
	return conditionsEqualIgnoringTransition(a.Conditions, b.Conditions)
}

// SetupWithManager sets up the controller with the Manager.
func (r *PollResponseReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&pollsv1.PollResponse{}).
		Watches(
			&pollsv1.Poll{},
			handler.EnqueueRequestsFromMapFunc(r.mapPollToResponses),
		).
		Named("pollresponse").
		Complete(r)
}

// mapPollToResponses enqueues every PollResponse referencing the given Poll so
// they get re-validated when the Poll's spec changes (or the Poll closes).
func (r *PollResponseReconciler) mapPollToResponses(ctx context.Context, obj client.Object) []reconcile.Request {
	poll, ok := obj.(*pollsv1.Poll)
	if !ok {
		return nil
	}
	var responses pollsv1.PollResponseList
	if err := r.List(ctx, &responses, client.InNamespace(poll.Namespace)); err != nil {
		logf.FromContext(ctx).Error(err, "Failed to list PollResponses for Poll change",
			"poll", poll.Name)
		return nil
	}
	requests := make([]reconcile.Request, 0, len(responses.Items))
	for _, resp := range responses.Items {
		if resp.Spec.PollRef.Name != poll.Name {
			continue
		}
		requests = append(requests, reconcile.Request{
			NamespacedName: client.ObjectKey{Namespace: resp.Namespace, Name: resp.Name},
		})
	}
	return requests
}
