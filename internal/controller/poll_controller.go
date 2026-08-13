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
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	pollsv1 "github.com/Jefwillems/poll-operator/api/v1"
)

// pollReadyCondition is the condition type used to summarize a Poll's state.
const pollReadyCondition = "Ready"

// PollReconciler reconciles a Poll object
type PollReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=polls.jef.app,resources=polls,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=polls.jef.app,resources=polls/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=polls.jef.app,resources=polls/finalizers,verbs=update
// +kubebuilder:rbac:groups=polls.jef.app,resources=pollresponses,verbs=get;list;watch

// Reconcile drives a Poll toward its desired state: it manages the Poll's
// lifecycle phase (Pending -> Open -> Closed) and aggregates accepted
// PollResponses into status.results.
func (r *PollReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var poll pollsv1.Poll
	if err := r.Get(ctx, req.NamespacedName, &poll); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get Poll: %w", err)
	}

	now := time.Now()

	// Determine phase and lifecycle timestamps.
	desiredStatus := poll.Status.DeepCopy()
	phase := computePhase(&poll, desiredStatus, now)
	desiredStatus.Phase = phase

	// Aggregate results from accepted PollResponses.
	var responses pollsv1.PollResponseList
	if err := r.List(ctx, &responses, client.InNamespace(poll.Namespace)); err != nil {
		return ctrl.Result{}, fmt.Errorf("list PollResponses: %w", err)
	}

	results, total := aggregateResults(&poll, responses.Items)
	desiredStatus.Results = results
	desiredStatus.TotalResponses = total

	// Summarize state on the Ready condition.
	setReadyCondition(desiredStatus, phase, poll.Generation)

	// Persist status only when it changed.
	if !statusEqual(&poll.Status, desiredStatus) {
		patch := client.MergeFrom(poll.DeepCopy())
		poll.Status = *desiredStatus
		if err := r.Status().Patch(ctx, &poll, patch); err != nil {
			if apierrors.IsConflict(err) {
				// Someone else updated the Poll; re-reconcile with fresh state.
				return ctrl.Result{Requeue: true}, nil
			}
			return ctrl.Result{}, fmt.Errorf("patch Poll status: %w", err)
		}
		log.Info("Updated Poll status",
			"phase", phase,
			"responses", total,
		)
	}

	// Requeue at closesAt so we transition to Closed on time.
	if phase == pollsv1.PollPhaseOpen && poll.Spec.ClosesAt != nil {
		if wait := time.Until(poll.Spec.ClosesAt.Time); wait > 0 {
			return ctrl.Result{RequeueAfter: wait}, nil
		}
	}

	return ctrl.Result{}, nil
}

// computePhase returns the Poll's current phase and mutates the desired status
// with lifecycle timestamps (openedAt, closedAt) as transitions happen.
func computePhase(poll *pollsv1.Poll, status *pollsv1.PollStatus, now time.Time) pollsv1.PollPhase {
	// Closed if closesAt has passed.
	if poll.Spec.ClosesAt != nil && !now.Before(poll.Spec.ClosesAt.Time) {
		if status.ClosedAt == nil {
			t := metav1.NewTime(now)
			status.ClosedAt = &t
		}
		if status.OpenedAt == nil {
			// The Poll closed before we ever observed it open (e.g. closesAt
			// was in the past at creation time). Record openedAt too so the
			// timeline is coherent.
			status.OpenedAt = status.ClosedAt
		}
		return pollsv1.PollPhaseClosed
	}

	// Otherwise Open. We treat creation as the moment voting opens.
	if status.OpenedAt == nil {
		t := metav1.NewTime(now)
		status.OpenedAt = &t
	}
	status.ClosedAt = nil
	return pollsv1.PollPhaseOpen
}

// aggregateResults builds per-question results by tallying accepted responses.
// Choice questions get one OptionResult per configured option (zero-initialized
// so the shape is stable). FreeText questions only get a response count.
func aggregateResults(poll *pollsv1.Poll, responses []pollsv1.PollResponse) ([]pollsv1.QuestionResult, int32) {
	results := make([]pollsv1.QuestionResult, 0, len(poll.Spec.Questions))
	// Index for O(1) lookup while tallying.
	byID := make(map[string]*pollsv1.QuestionResult, len(poll.Spec.Questions))
	optIdx := make(map[string]map[string]*pollsv1.OptionResult, len(poll.Spec.Questions))

	for _, q := range poll.Spec.Questions {
		qr := pollsv1.QuestionResult{QuestionID: q.ID}
		if q.Type == pollsv1.QuestionTypeChoice && q.Choice != nil {
			qr.Options = make([]pollsv1.OptionResult, 0, len(q.Choice.Options))
			for _, opt := range q.Choice.Options {
				qr.Options = append(qr.Options, pollsv1.OptionResult{Option: opt, Votes: 0})
			}
		}
		results = append(results, qr)
	}
	// Build lookup indices after the slice is stable (avoid pointer invalidation).
	for i := range results {
		byID[results[i].QuestionID] = &results[i]
		if len(results[i].Options) > 0 {
			m := make(map[string]*pollsv1.OptionResult, len(results[i].Options))
			for j := range results[i].Options {
				m[results[i].Options[j].Option] = &results[i].Options[j]
			}
			optIdx[results[i].QuestionID] = m
		}
	}

	var totalResponses int32
	for i := range responses {
		resp := &responses[i]
		if resp.Status.Phase != pollsv1.PollResponsePhaseAccepted {
			continue
		}
		if resp.Spec.PollRef.Name != poll.Name {
			// Field indexer should already filter this; defensive check.
			continue
		}
		totalResponses++

		for _, ans := range resp.Spec.Answers {
			qr, ok := byID[ans.QuestionID]
			if !ok {
				// Answer references an unknown question; the response should
				// have been rejected. Skip it here.
				continue
			}
			qr.Responses++

			if opts, isChoice := optIdx[ans.QuestionID]; isChoice {
				for _, sel := range ans.SelectedOptions {
					if or, known := opts[sel]; known {
						or.Votes++
					}
				}
			}
		}
	}

	return results, totalResponses
}

// setReadyCondition writes a summary Ready condition based on phase.
func setReadyCondition(status *pollsv1.PollStatus, phase pollsv1.PollPhase, generation int64) {
	cond := metav1.Condition{
		Type:               pollReadyCondition,
		ObservedGeneration: generation,
		LastTransitionTime: metav1.Now(),
	}
	switch phase {
	case pollsv1.PollPhaseOpen:
		cond.Status = metav1.ConditionTrue
		cond.Reason = "Open"
		cond.Message = "Poll is accepting responses"
	case pollsv1.PollPhaseClosed:
		cond.Status = metav1.ConditionFalse
		cond.Reason = "Closed"
		cond.Message = "Poll is no longer accepting responses"
	default:
		cond.Status = metav1.ConditionUnknown
		cond.Reason = "Pending"
		cond.Message = "Poll has not yet been opened"
	}
	meta.SetStatusCondition(&status.Conditions, cond)
}

// statusEqual reports whether two PollStatus values are equivalent for the
// purposes of skipping a status patch. LastTransitionTime on the Ready
// condition is ignored so we don't churn on every reconcile.
func statusEqual(a, b *pollsv1.PollStatus) bool {
	if a.Phase != b.Phase ||
		a.TotalResponses != b.TotalResponses ||
		!timePtrEqual(a.OpenedAt, b.OpenedAt) ||
		!timePtrEqual(a.ClosedAt, b.ClosedAt) {
		return false
	}
	if len(a.Results) != len(b.Results) {
		return false
	}
	// Results are a listType=map keyed by questionID; compare by key.
	aByID := make(map[string]pollsv1.QuestionResult, len(a.Results))
	for _, r := range a.Results {
		aByID[r.QuestionID] = r
	}
	for _, br := range b.Results {
		ar, ok := aByID[br.QuestionID]
		if !ok || ar.Responses != br.Responses || len(ar.Options) != len(br.Options) {
			return false
		}
		aOpts := make(map[string]int32, len(ar.Options))
		for _, o := range ar.Options {
			aOpts[o.Option] = o.Votes
		}
		for _, bo := range br.Options {
			if aOpts[bo.Option] != bo.Votes {
				return false
			}
		}
	}
	return conditionsEqualIgnoringTransition(a.Conditions, b.Conditions)
}

func timePtrEqual(a, b *metav1.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(b)
}

func conditionsEqualIgnoringTransition(a, b []metav1.Condition) bool {
	if len(a) != len(b) {
		return false
	}
	byType := make(map[string]metav1.Condition, len(a))
	for _, c := range a {
		byType[c.Type] = c
	}
	for _, bc := range b {
		ac, ok := byType[bc.Type]
		if !ok ||
			ac.Status != bc.Status ||
			ac.Reason != bc.Reason ||
			ac.Message != bc.Message ||
			ac.ObservedGeneration != bc.ObservedGeneration {
			return false
		}
	}
	return true
}

// SetupWithManager sets up the controller with the Manager.
func (r *PollReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&pollsv1.Poll{}).
		Watches(
			&pollsv1.PollResponse{},
			handler.EnqueueRequestsFromMapFunc(mapResponseToPoll),
			builder.WithPredicates(),
		).
		Named("poll").
		Complete(r)
}

// mapResponseToPoll enqueues the Poll that a PollResponse refers to whenever
// the response changes.
func mapResponseToPoll(_ context.Context, obj client.Object) []reconcile.Request {
	resp, ok := obj.(*pollsv1.PollResponse)
	if !ok || resp.Spec.PollRef.Name == "" {
		return nil
	}
	return []reconcile.Request{{
		NamespacedName: types.NamespacedName{
			Namespace: resp.Namespace,
			Name:      resp.Spec.PollRef.Name,
		},
	}}
}
