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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	pollsv1 "github.com/Jefwillems/poll-operator/api/v1"
)

var _ = Describe("PollResponse Controller", func() {
	const (
		pollName          = "response-test-poll"
		resourceNamespace = "default"
	)

	ctx := context.Background()

	// findCondition returns a copy of the condition with the given type, or nil.
	findCondition := func(conds []metav1.Condition, t string) *metav1.Condition {
		for i := range conds {
			if conds[i].Type == t {
				return &conds[i]
			}
		}
		return nil
	}

	// createPoll installs a two-question Poll (Choice + FreeText) used by the
	// tests below. It is safe to call multiple times.
	createPoll := func() {
		poll := &pollsv1.Poll{}
		key := types.NamespacedName{Name: pollName, Namespace: resourceNamespace}
		err := k8sClient.Get(ctx, key, poll)
		if err == nil {
			return
		}
		poll = &pollsv1.Poll{
			ObjectMeta: metav1.ObjectMeta{Name: pollName, Namespace: resourceNamespace},
			Spec: pollsv1.PollSpec{
				Title: "Response test",
				Questions: []pollsv1.PollQuestion{
					{
						ID:     testQuestionPetID,
						Prompt: "Which is best?",
						Type:   pollsv1.QuestionTypeChoice,
						Choice: &pollsv1.ChoiceQuestion{
							Options: []string{testOptionCat, testOptionDog},
						},
					},
					{
						ID:       testQuestionWhyID,
						Prompt:   "Why?",
						Type:     pollsv1.QuestionTypeFreeText,
						FreeText: &pollsv1.FreeTextQuestion{MaxResponseLength: 100},
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, poll)).To(Succeed())
	}

	AfterEach(func() {
		// Clean up any PollResponses in the namespace so tests don't leak.
		var list pollsv1.PollResponseList
		Expect(k8sClient.List(ctx, &list, client.InNamespace(resourceNamespace))).To(Succeed())
		for i := range list.Items {
			Expect(k8sClient.Delete(ctx, &list.Items[i])).To(Succeed())
		}
		// Delete the shared Poll if present.
		poll := &pollsv1.Poll{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: pollName, Namespace: resourceNamespace}, poll); err == nil {
			Expect(k8sClient.Delete(ctx, poll)).To(Succeed())
		}
	})

	It("accepts a valid response and sets the owner reference", func() {
		createPoll()
		const respName = "valid-response"
		resp := &pollsv1.PollResponse{
			ObjectMeta: metav1.ObjectMeta{Name: respName, Namespace: resourceNamespace},
			Spec: pollsv1.PollResponseSpec{
				PollRef: pollsv1.PollReference{Name: pollName},
				Answers: []pollsv1.PollAnswer{
					{QuestionID: testQuestionPetID, SelectedOptions: []string{testOptionCat}},
					{QuestionID: testQuestionWhyID, Text: "purrs"},
				},
			},
		}
		Expect(k8sClient.Create(ctx, resp)).To(Succeed())

		reconciler := &PollResponseReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		_, err := reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: respName, Namespace: resourceNamespace},
		})
		Expect(err).NotTo(HaveOccurred())

		updated := &pollsv1.PollResponse{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: respName, Namespace: resourceNamespace}, updated)).To(Succeed())

		Expect(updated.Status.Phase).To(Equal(pollsv1.PollResponsePhaseAccepted))
		Expect(updated.Status.AcceptedAt).NotTo(BeNil())
		validated := findCondition(updated.Status.Conditions, pollResponseValidatedCondition)
		Expect(validated).NotTo(BeNil())
		Expect(validated.Status).To(Equal(metav1.ConditionTrue))
		Expect(validated.Reason).To(Equal(reasonAccepted))

		By("setting the Poll as controller-owner")
		Expect(updated.OwnerReferences).To(HaveLen(1))
		Expect(updated.OwnerReferences[0].Name).To(Equal(pollName))
		Expect(updated.OwnerReferences[0].Kind).To(Equal("Poll"))
	})

	It("rejects a response that selects an unknown option", func() {
		createPoll()
		const respName = "bad-option"
		resp := &pollsv1.PollResponse{
			ObjectMeta: metav1.ObjectMeta{Name: respName, Namespace: resourceNamespace},
			Spec: pollsv1.PollResponseSpec{
				PollRef: pollsv1.PollReference{Name: pollName},
				Answers: []pollsv1.PollAnswer{
					{QuestionID: testQuestionPetID, SelectedOptions: []string{testOptionParrot}},
					{QuestionID: testQuestionWhyID, Text: "colourful"},
				},
			},
		}
		Expect(k8sClient.Create(ctx, resp)).To(Succeed())

		reconciler := &PollResponseReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		_, err := reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: respName, Namespace: resourceNamespace},
		})
		Expect(err).NotTo(HaveOccurred())

		updated := &pollsv1.PollResponse{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: respName, Namespace: resourceNamespace}, updated)).To(Succeed())

		Expect(updated.Status.Phase).To(Equal(pollsv1.PollResponsePhaseRejected))
		validated := findCondition(updated.Status.Conditions, pollResponseValidatedCondition)
		Expect(validated).NotTo(BeNil())
		Expect(validated.Status).To(Equal(metav1.ConditionFalse))
		Expect(validated.Reason).To(Equal(reasonUnknownOption))
	})

	It("rejects a response whose Poll does not exist", func() {
		const respName = "orphan-response"
		resp := &pollsv1.PollResponse{
			ObjectMeta: metav1.ObjectMeta{Name: respName, Namespace: resourceNamespace},
			Spec: pollsv1.PollResponseSpec{
				PollRef: pollsv1.PollReference{Name: "does-not-exist"},
				Answers: []pollsv1.PollAnswer{
					{QuestionID: testQuestionPetID, SelectedOptions: []string{testOptionCat}},
				},
			},
		}
		Expect(k8sClient.Create(ctx, resp)).To(Succeed())

		reconciler := &PollResponseReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		_, err := reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: respName, Namespace: resourceNamespace},
		})
		Expect(err).NotTo(HaveOccurred())

		updated := &pollsv1.PollResponse{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: respName, Namespace: resourceNamespace}, updated)).To(Succeed())

		Expect(updated.Status.Phase).To(Equal(pollsv1.PollResponsePhaseRejected))
		validated := findCondition(updated.Status.Conditions, pollResponseValidatedCondition)
		Expect(validated).NotTo(BeNil())
		Expect(validated.Reason).To(Equal(reasonPollNotFound))
	})
})
