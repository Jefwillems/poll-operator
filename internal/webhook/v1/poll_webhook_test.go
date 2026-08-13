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

var _ = Describe("Poll Webhook", func() {
	var validator PollCustomValidator

	// newPoll builds a valid Poll for tests to mutate.
	newPoll := func() *pollsv1.Poll {
		return &pollsv1.Poll{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			Spec: pollsv1.PollSpec{
				Title: "Favourite pet",
				Questions: []pollsv1.PollQuestion{
					{
						ID:     "pet",
						Prompt: "Which is best?",
						Type:   pollsv1.QuestionTypeChoice,
						Choice: &pollsv1.ChoiceQuestion{
							Options: []string{testOptionCat, testOptionDog},
						},
					},
					{
						ID:       "why",
						Prompt:   "Why?",
						Type:     pollsv1.QuestionTypeFreeText,
						FreeText: &pollsv1.FreeTextQuestion{MaxResponseLength: 100},
					},
				},
			},
		}
	}

	BeforeEach(func() {
		validator = PollCustomValidator{}
	})

	Context("On create", func() {
		It("admits a well-formed Poll", func() {
			warns, err := validator.ValidateCreate(ctx, newPoll())
			Expect(err).NotTo(HaveOccurred())
			Expect(warns).To(BeNil())
		})

		It("rejects a Choice question without choice configuration", func() {
			obj := newPoll()
			obj.Spec.Questions[0].Choice = nil
			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("must set choice"))
		})

		It("rejects a Choice question that also sets freeText", func() {
			obj := newPoll()
			obj.Spec.Questions[0].FreeText = &pollsv1.FreeTextQuestion{MaxResponseLength: 10}
			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("must not be set"))
		})

		It("rejects a FreeText question without freeText configuration", func() {
			obj := newPoll()
			obj.Spec.Questions[1].FreeText = nil
			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("must set freeText"))
		})

		It("rejects duplicate options within a Choice question", func() {
			obj := newPoll()
			obj.Spec.Questions[0].Choice.Options = []string{testOptionCat, testOptionCat}
			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("Duplicate"))
		})
	})

	Context("On update", func() {
		It("allows changing an existing question's prompt or options", func() {
			oldObj := newPoll()
			obj := newPoll()
			obj.Spec.Questions[0].Prompt = "Which is the greatest?"
			obj.Spec.Questions[0].Choice.Options = []string{testOptionCat, testOptionDog, "capybara"}
			_, err := validator.ValidateUpdate(ctx, oldObj, obj)
			Expect(err).NotTo(HaveOccurred())
		})

		It("allows adding a new question", func() {
			oldObj := newPoll()
			obj := newPoll()
			obj.Spec.Questions = append(obj.Spec.Questions, pollsv1.PollQuestion{
				ID:     "extra",
				Prompt: "Anything else?",
				Type:   pollsv1.QuestionTypeFreeText,
				FreeText: &pollsv1.FreeTextQuestion{
					MaxResponseLength: 200,
				},
			})
			_, err := validator.ValidateUpdate(ctx, oldObj, obj)
			Expect(err).NotTo(HaveOccurred())
		})

		It("rejects changing the type of an existing question", func() {
			oldObj := newPoll()
			obj := newPoll()
			obj.Spec.Questions[0].Type = pollsv1.QuestionTypeFreeText
			obj.Spec.Questions[0].Choice = nil
			obj.Spec.Questions[0].FreeText = &pollsv1.FreeTextQuestion{MaxResponseLength: 10}
			_, err := validator.ValidateUpdate(ctx, oldObj, obj)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("immutable"))
		})
	})

	Context("On delete", func() {
		It("always allows deletion", func() {
			_, err := validator.ValidateDelete(ctx, newPoll())
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
