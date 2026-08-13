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
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	pollsv1 "github.com/Jefwillems/poll-operator/api/v1"
)

const (
	pageTitlePolls = "Polls"

	dataKeyTitle = "Title"
	dataKeyPolls = "Polls"
)

// routes wires the HTTP endpoints exposed by the UI.
func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.handleRoot)
	mux.HandleFunc("GET /polls", s.handleListPolls)
	mux.HandleFunc("GET /polls/{namespace}/{name}", s.handlePollDetail)
	mux.HandleFunc("POST /polls/{namespace}/{name}/vote", s.handleVote)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	return mux
}

// handleRoot redirects the root path to the poll listing.
func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, "/polls", http.StatusFound)
}

// pollListItem is the view model for a single row on the index page.
type pollListItem struct {
	Namespace      string
	Name           string
	Title          string
	Phase          string
	TotalResponses int32
}

// handleListPolls renders every Poll the manager can see, ordered by
// (namespace, name).
func (s *Server) handleListPolls(w http.ResponseWriter, r *http.Request) {
	var polls pollsv1.PollList
	if err := s.client.List(r.Context(), &polls); err != nil {
		serverError(w, r, "list polls", err)
		return
	}

	items := make([]pollListItem, 0, len(polls.Items))
	for _, p := range polls.Items {
		items = append(items, pollListItem{
			Namespace:      p.Namespace,
			Name:           p.Name,
			Title:          p.Spec.Title,
			Phase:          string(p.Status.Phase),
			TotalResponses: p.Status.TotalResponses,
		})
	}
	slices.SortFunc(items, func(a, b pollListItem) int {
		if a.Namespace != b.Namespace {
			return strings.Compare(a.Namespace, b.Namespace)
		}
		return strings.Compare(a.Name, b.Name)
	})

	renderPage(w, r, "index.html", map[string]any{
		dataKeyTitle: pageTitlePolls,
		dataKeyPolls: items,
	})
}

// questionView is the view model for one question on the detail page. It
// pre-computes everything the template needs so the template stays trivial.
type questionView struct {
	ID            string
	Prompt        string
	Type          pollsv1.QuestionType
	AllowMultiple bool
	Options       []optionView
	TextResponses int32 // populated for FreeText questions
	MaxLength     int32 // populated for FreeText questions
}

type optionView struct {
	Option string
	Votes  int32
}

// handlePollDetail renders a single Poll with aggregated results and, if the
// Poll is open, a vote form.
func (s *Server) handlePollDetail(w http.ResponseWriter, r *http.Request) {
	ns := r.PathValue("namespace")
	name := r.PathValue("name")

	var poll pollsv1.Poll
	if err := s.client.Get(r.Context(), client.ObjectKey{Namespace: ns, Name: name}, &poll); err != nil {
		if apierrors.IsNotFound(err) {
			http.NotFound(w, r)
			return
		}
		serverError(w, r, "get poll", err)
		return
	}

	resultByID := make(map[string]pollsv1.QuestionResult, len(poll.Status.Results))
	for _, q := range poll.Status.Results {
		resultByID[q.QuestionID] = q
	}

	questions := make([]questionView, 0, len(poll.Spec.Questions))
	for _, q := range poll.Spec.Questions {
		qv := questionView{ID: q.ID, Prompt: q.Prompt, Type: q.Type}
		if q.Choice != nil {
			qv.AllowMultiple = q.Choice.AllowMultipleChoices
			votes := make(map[string]int32, len(q.Choice.Options))
			for _, or := range resultByID[q.ID].Options {
				votes[or.Option] = or.Votes
			}
			qv.Options = make([]optionView, 0, len(q.Choice.Options))
			for _, opt := range q.Choice.Options {
				qv.Options = append(qv.Options, optionView{Option: opt, Votes: votes[opt]})
			}
		}
		if q.FreeText != nil {
			qv.MaxLength = q.FreeText.MaxResponseLength
			qv.TextResponses = resultByID[q.ID].Responses
		}
		questions = append(questions, qv)
	}

	flash := readFlash(r)

	renderPage(w, r, "poll.html", map[string]any{
		dataKeyTitle: poll.Spec.Title,
		"Poll":       poll,
		"Questions":  questions,
		"Flash":      flash,
		"IsOpen":     poll.Status.Phase == pollsv1.PollPhaseOpen,
	})
}

// handleVote parses a submitted vote form and creates a PollResponse.
func (s *Server) handleVote(w http.ResponseWriter, r *http.Request) {
	ns := r.PathValue("namespace")
	name := r.PathValue("name")

	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	// Load the Poll to know each question's type and shape the answers.
	var poll pollsv1.Poll
	if err := s.client.Get(r.Context(), client.ObjectKey{Namespace: ns, Name: name}, &poll); err != nil {
		if apierrors.IsNotFound(err) {
			http.NotFound(w, r)
			return
		}
		serverError(w, r, "get poll", err)
		return
	}

	answers, err := buildAnswers(&poll, r.PostForm)
	if err != nil {
		flashAndRedirect(w, r, ns, name, "Could not build response: "+err.Error())
		return
	}

	resp := &pollsv1.PollResponse{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: name + "-",
			Namespace:    ns,
		},
		Spec: pollsv1.PollResponseSpec{
			PollRef:    pollsv1.PollReference{Name: name},
			Respondent: strings.TrimSpace(r.PostFormValue("respondent")),
			Answers:    answers,
		},
	}

	if err := s.client.Create(r.Context(), resp); err != nil {
		// Surface admission errors verbatim so the user sees the actual reason
		// (e.g. duplicate option, closed poll if the reconciler already marked
		// it closed and a webhook chose to reject).
		flashAndRedirect(w, r, ns, name, "Server refused the vote: "+err.Error())
		return
	}

	flashAndRedirect(w, r, ns, name, "Thanks — your response was recorded as "+resp.Name+".")
}

// buildAnswers converts the submitted form into the answer slice required by
// PollResponseSpec. Empty answers are skipped so voters may leave individual
// questions blank without polluting status counts.
func buildAnswers(poll *pollsv1.Poll, form map[string][]string) ([]pollsv1.PollAnswer, error) {
	answers := make([]pollsv1.PollAnswer, 0, len(poll.Spec.Questions))
	for _, q := range poll.Spec.Questions {
		key := "q." + q.ID
		values := form[key]

		switch q.Type {
		case pollsv1.QuestionTypeChoice:
			selected := make([]string, 0, len(values))
			for _, v := range values {
				v = strings.TrimSpace(v)
				if v != "" {
					selected = append(selected, v)
				}
			}
			if len(selected) == 0 {
				continue
			}
			answers = append(answers, pollsv1.PollAnswer{
				QuestionID:      q.ID,
				SelectedOptions: selected,
			})

		case pollsv1.QuestionTypeFreeText:
			text := ""
			if len(values) > 0 {
				text = strings.TrimSpace(values[0])
			}
			if text == "" {
				continue
			}
			answers = append(answers, pollsv1.PollAnswer{
				QuestionID: q.ID,
				Text:       text,
			})
		}
	}

	if len(answers) == 0 {
		return nil, errors.New("no answers were provided")
	}
	return answers, nil
}

// renderPage executes the named page template.
func renderPage(w http.ResponseWriter, r *http.Request, pageTemplate string, data map[string]any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := render(w, pageTemplate, data); err != nil {
		logf.FromContext(r.Context()).Error(err, "Failed to render page",
			"template", pageTemplate)
	}
}

// serverError logs the failure and returns a 500. We deliberately do not
// forward the raw error message to the browser.
func serverError(w http.ResponseWriter, r *http.Request, action string, err error) {
	logf.FromContext(r.Context()).Error(err, "UI request failed", "action", action)
	http.Error(w, fmt.Sprintf("Failed to %s", action), http.StatusInternalServerError)
}

// Flash message plumbing: we set a short-lived cookie so a POST can redirect
// (PRG pattern) and still communicate feedback on the next GET. Values are URL
// path segments so they survive the cookie transport unmodified.

const flashCookieName = "poll_ui_flash"

func flashAndRedirect(w http.ResponseWriter, r *http.Request, ns, name, msg string) {
	http.SetCookie(w, &http.Cookie{
		Name:     flashCookieName,
		Value:    msg,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   30,
	})
	http.Redirect(w, r, fmt.Sprintf("/polls/%s/%s", ns, name), http.StatusSeeOther)
}

func readFlash(r *http.Request) string {
	c, err := r.Cookie(flashCookieName)
	if err != nil {
		return ""
	}
	return c.Value
}
