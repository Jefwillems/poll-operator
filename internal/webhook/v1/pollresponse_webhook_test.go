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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	pollsv1 "github.com/Jefwillems/poll-operator/api/v1"
)

const (
	testOptionCat = "cat"
	testOptionDog = "dog"
)

var _ = Describe("PollResponse Webhook", func() {
	var (
		obj       *pollsv1.PollResponse
		oldObj    *pollsv1.PollResponse
		validator PollResponseCustomValidator
	)

	// newResponse builds a valid PollResponse for tests to mutate.
	newResponse := func() *pollsv1.PollResponse {
		return &pollsv1.PollResponse{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			Spec: pollsv1.PollResponseSpec{
				PollRef: pollsv1.PollReference{Name: "my-poll"},
				Answers: []pollsv1.PollAnswer{
					{QuestionID: "pet", SelectedOptions: []string{testOptionCat}},
					{QuestionID: "why", Text: "purrs"},
				},
			},
		}
	}

	BeforeEach(func() {
		obj = newResponse()
		oldObj = newResponse()
		validator = PollResponseCustomValidator{}
	})

	Context("On create", func() {
		It("admits a well-formed response", func() {
			warns, err := validator.ValidateCreate(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
			Expect(warns).To(BeNil())
		})

		It("rejects an answer that sets both selectedOptions and text", func() {
			obj.Spec.Answers[0].Text = "I like cats"
			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("exactly one of selectedOptions or text"))
		})

		It("rejects an answer with neither selectedOptions nor text", func() {
			obj.Spec.Answers[0].SelectedOptions = nil
			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("exactly one of selectedOptions or text"))
		})

		It("rejects duplicate selected options", func() {
			obj.Spec.Answers[0].SelectedOptions = []string{testOptionCat, testOptionCat}
			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("Duplicate"))
		})
	})

	Context("On update", func() {
		It("admits a valid answer change", func() {
			obj.Spec.Answers[0].SelectedOptions = []string{testOptionDog}
			_, err := validator.ValidateUpdate(ctx, oldObj, obj)
			Expect(err).NotTo(HaveOccurred())
		})

		It("rejects changing spec.pollRef.name", func() {
			obj.Spec.PollRef.Name = "different-poll"
			_, err := validator.ValidateUpdate(ctx, oldObj, obj)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("immutable"))
		})
	})

	Context("On delete", func() {
		It("always allows deletion", func() {
			_, err := validator.ValidateDelete(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
