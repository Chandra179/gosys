package main

import (
	"encoding/json"
	"html/template"
	"log"
	"strings"

	"gosys/internal/pipeline"
)

// treemapItem is one allocation site flattened for the client-side treemap
// and findings panel: one entry per Result. The treemap groups by site (via
// Pattern/Bytes), while Findings carries each individual match's own text so
// the findings panel can render one entry per finding, not one per site.
type treemapItem struct {
	File     string        `json:"file"`
	Line     int64         `json:"line"`
	Fn       string        `json:"fn"`
	Bytes    int64         `json:"bytes"`
	Pattern  string        `json:"pattern"` // "" if no rule matched; joined if several did
	Findings []findingItem `json:"findings"`
}

// findingItem is one rules.Finding, flattened for the JSON data island.
type findingItem struct {
	Pattern    string `json:"pattern"`
	Message    string `json:"message"`
	Source     string `json:"source"`
	SourceLine int64  `json:"sourceLine"`
}

// treemapJSON flattens results into the shape the dashboard's client-side
// script expects and marshals it for embedding in an `application/json`
// data island. json.Marshal HTML-escapes <, >, and & by default, which is
// what makes it safe to inline as template.JS without a script-breakout
// risk.
func treemapJSON(results []pipeline.Result) template.JS {
	items := make([]treemapItem, 0, len(results))
	for _, r := range results {
		item := treemapItem{
			File:  r.Site.File,
			Line:  r.Site.Line,
			Fn:    r.Site.Function,
			Bytes: r.Site.Flat,
		}
		if len(r.Findings) > 0 {
			patterns := make([]string, len(r.Findings))
			findings := make([]findingItem, len(r.Findings))
			for i, f := range r.Findings {
				patterns[i] = f.Pattern
				findings[i] = findingItem{
					Pattern:    f.Pattern,
					Message:    f.Message,
					Source:     f.Source,
					SourceLine: f.SourceLine,
				}
			}
			item.Pattern = strings.Join(patterns, ", ")
			item.Findings = findings
		}
		items = append(items, item)
	}

	b, err := json.Marshal(items)
	if err != nil {
		log.Println("marshal treemap json:", err)
		return template.JS("[]")
	}
	return template.JS(b)
}
