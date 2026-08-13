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

package ui

import (
	"bytes"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	pollsv1 "github.com/Jefwillems/poll-operator/api/v1"
)

const (
	testPromptBest = "Best?"
	testPromptWhy  = "Why?"
	testOptionCat  = "cat"
)

// TestTemplatesRender ensures every page template parses and renders end-to-end
// against representative view models. It guards against typos in template
// action expressions that would otherwise only surface at runtime.
func TestTemplatesRender(t *testing.T) {
	t.Parallel()

	t.Run("index", func(t *testing.T) {
		var buf bytes.Buffer
		err := render(&buf, "index.html", map[string]any{
			dataKeyTitle: pageTitlePolls,
			dataKeyPolls: []pollListItem{
				{Namespace: "default", Name: "p1", Title: "One", Phase: "Open", TotalResponses: 3},
			},
		})
		if err != nil {
			t.Fatalf("render index: %v", err)
		}
		if !strings.Contains(buf.String(), pageTitlePolls) {
			t.Fatalf("expected page to include title, got %q", buf.String())
		}
	})

	t.Run("poll detail", func(t *testing.T) {
		poll := pollsv1.Poll{
			ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default"},
			Spec: pollsv1.PollSpec{
				Title: "Favourite",
				Questions: []pollsv1.PollQuestion{
					{ID: "q1", Prompt: testPromptBest, Type: pollsv1.QuestionTypeChoice},
					{ID: "q2", Prompt: testPromptWhy, Type: pollsv1.QuestionTypeFreeText},
				},
			},
			Status: pollsv1.PollStatus{
				Phase:          pollsv1.PollPhaseOpen,
				TotalResponses: 5,
			},
		}

		questions := []questionView{
			{
				ID:      "q1",
				Prompt:  testPromptBest,
				Type:    pollsv1.QuestionTypeChoice,
				Options: []optionView{{Option: testOptionCat, Votes: 3}, {Option: "dog", Votes: 2}},
			},
			{
				ID:            "q2",
				Prompt:        testPromptWhy,
				Type:          pollsv1.QuestionTypeFreeText,
				TextResponses: 4,
				MaxLength:     100,
			},
		}

		var buf bytes.Buffer
		err := render(&buf, "poll.html", map[string]any{
			dataKeyTitle: poll.Spec.Title,
			"Poll":       poll,
			"Questions":  questions,
			"Flash":      "hi",
			"IsOpen":     true,
		})
		if err != nil {
			t.Fatalf("render poll: %v", err)
		}
		out := buf.String()
		for _, want := range []string{"Favourite", testPromptBest, testPromptWhy, "Cast a vote", testOptionCat} {
			if !strings.Contains(out, want) {
				t.Fatalf("rendered page missing %q, got:\n%s", want, out)
			}
		}
	})
}

// TestBuildAnswers exercises the form-to-Answer conversion for the mixed
// choice/free-text case.
func TestBuildAnswers(t *testing.T) {
	t.Parallel()

	poll := &pollsv1.Poll{
		Spec: pollsv1.PollSpec{
			Questions: []pollsv1.PollQuestion{
				{ID: "pet", Type: pollsv1.QuestionTypeChoice, Choice: &pollsv1.ChoiceQuestion{
					Options: []string{testOptionCat, "dog"},
				}},
				{ID: "why", Type: pollsv1.QuestionTypeFreeText, FreeText: &pollsv1.FreeTextQuestion{
					MaxResponseLength: 100,
				}},
				{ID: "skipped", Type: pollsv1.QuestionTypeFreeText, FreeText: &pollsv1.FreeTextQuestion{
					MaxResponseLength: 100,
				}},
			},
		},
	}

	form := map[string][]string{
		"q.pet":     {testOptionCat},
		"q.why":     {"  it purrs "},
		"q.skipped": {"   "},
	}

	answers, err := buildAnswers(poll, form)
	if err != nil {
		t.Fatalf("buildAnswers: %v", err)
	}
	if len(answers) != 2 {
		t.Fatalf("expected 2 answers, got %d", len(answers))
	}
	if answers[0].QuestionID != "pet" || len(answers[0].SelectedOptions) != 1 || answers[0].SelectedOptions[0] != testOptionCat {
		t.Fatalf("bad choice answer: %+v", answers[0])
	}
	if answers[1].QuestionID != "why" || answers[1].Text != "it purrs" {
		t.Fatalf("bad text answer: %+v", answers[1])
	}
}
