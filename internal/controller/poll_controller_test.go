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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	pollsv1 "github.com/Jefwillems/poll-operator/api/v1"
)

var _ = Describe("Poll Controller", func() {
	Context("When reconciling a resource", func() {
		const (
			resourceName      = "test-resource"
			resourceNamespace = "default"
		)

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: resourceNamespace,
		}
		poll := &pollsv1.Poll{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind Poll")
			err := k8sClient.Get(ctx, typeNamespacedName, poll)
			if err != nil && errors.IsNotFound(err) {
				resource := &pollsv1.Poll{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: resourceNamespace,
					},
					Spec: pollsv1.PollSpec{
						Title: "Favourite pet",
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
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &pollsv1.Poll{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance Poll")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})

		It("should open the Poll and initialize results per question", func() {
			By("Reconciling the created resource")
			controllerReconciler := &PollReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			updated := &pollsv1.Poll{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updated)).To(Succeed())

			By("Transitioning to Open phase")
			Expect(updated.Status.Phase).To(Equal(pollsv1.PollPhaseOpen))
			Expect(updated.Status.OpenedAt).NotTo(BeNil())
			Expect(updated.Status.ClosedAt).To(BeNil())

			By("Seeding zero-vote results for each question")
			Expect(updated.Status.Results).To(HaveLen(2))
			var petResult *pollsv1.QuestionResult
			for i := range updated.Status.Results {
				if updated.Status.Results[i].QuestionID == testQuestionPetID {
					petResult = &updated.Status.Results[i]
				}
			}
			Expect(petResult).NotTo(BeNil())
			Expect(petResult.Options).To(HaveLen(2))
			for _, o := range petResult.Options {
				Expect(o.Votes).To(BeNumerically("==", 0))
			}

			By("Reporting Ready=True with reason Open")
			var ready *metav1.Condition
			for i := range updated.Status.Conditions {
				if updated.Status.Conditions[i].Type == pollReadyCondition {
					ready = &updated.Status.Conditions[i]
				}
			}
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionTrue))
			Expect(ready.Reason).To(Equal("Open"))
		})

		It("should close the Poll when closesAt has passed", func() {
			By("Setting closesAt in the past")
			toUpdate := &pollsv1.Poll{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, toUpdate)).To(Succeed())
			past := metav1.NewTime(time.Now().Add(-1 * time.Minute))
			toUpdate.Spec.ClosesAt = &past
			Expect(k8sClient.Update(ctx, toUpdate)).To(Succeed())

			controllerReconciler := &PollReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			updated := &pollsv1.Poll{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updated)).To(Succeed())
			Expect(updated.Status.Phase).To(Equal(pollsv1.PollPhaseClosed))
			Expect(updated.Status.ClosedAt).NotTo(BeNil())
		})
	})
})
