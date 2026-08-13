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
	"embed"
	"html/template"
	"io"
)

//go:embed templates
var templatesFS embed.FS

// templates holds the parsed HTML templates. Parsing at init keeps the request
// path allocation-free and surfaces template errors on startup rather than on
// the first request.
var templates = template.Must(
	template.New("").Funcs(template.FuncMap{
		"maxVotes": func(opts []optionView) int32 {
			var m int32
			for _, o := range opts {
				if o.Votes > m {
					m = o.Votes
				}
			}
			return m
		},
		"barWidth": func(votes, max int32) int32 {
			if max <= 0 {
				return 0
			}
			return (votes * 100) / max
		},
	}).ParseFS(templatesFS, "templates/*.html"),
)

// render writes the named template out with the supplied data.
func render(w io.Writer, name string, data any) error {
	return templates.ExecuteTemplate(w, name, data)
}
