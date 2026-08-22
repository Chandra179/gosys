// Command dashboard serves a small web UI around pipeline.Analyze: fetch a
// heap pprof profile live from a target's /debug/pprof/heap, run it against
// a repo checkout given in the form, and view the findings rendered as
// HTML.
package main

import (
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	_ "net/http/pprof" // registers /debug/pprof/* on the default mux, incl. /debug/pprof/heap
	"os"
	"strings"
	"time"

	"gosys/internal/pipeline"
)

// defaultRepoDir is the pre-filled "Repo dir" value: it must be the
// checkout the live target was built from, since findings only make sense
// against source that matches the live target's build. "." only resolves
// correctly when the dashboard is run from that checkout; otherwise the
// user types the actual repo path into the form.
const defaultRepoDir = "."

//go:embed templates/*.html
var templatesFS embed.FS

var tmpl = template.Must(template.ParseFS(templatesFS, "templates/*.html"))

type pageData struct {
	RepoDir     string
	Top         int
	Results     []pipeline.Result
	Err         string
	TreemapJSON template.JS
}

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

func main() {
	addr := flag.String("addr", ":8080", "address to listen on")
	flag.Parse()

	http.HandleFunc("/", handleIndex)
	http.HandleFunc("/analyze", handleAnalyze)

	log.Printf("gosys dashboard listening on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, nil))
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	renderPage(w, pageData{RepoDir: defaultRepoDir, Top: 10, TreemapJSON: treemapJSON(nil)})
}

func handleAnalyze(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	data := pageData{
		RepoDir: defaultRepoDir,
		Top:     10,
	}
	if v := r.FormValue("top"); v != "" {
		fmt.Sscanf(v, "%d", &data.Top)
	}
	if v := strings.TrimSpace(r.FormValue("repo")); v != "" {
		data.RepoDir = v
	}

	target := strings.TrimSpace(r.FormValue("target"))
	if target == "" {
		data.Err = "live target is required"
		renderResults(w, data)
		return
	}

	profilePath, cleanup, err := fetchLiveProfile(target)
	if err != nil {
		data.Err = err.Error()
		renderResults(w, data)
		return
	}
	defer cleanup()

	results, err := pipeline.Analyze(pipeline.Config{
		ProfilePath: profilePath,
		RepoDir:     data.RepoDir,
		Top:         data.Top,
	})
	if err != nil {
		data.Err = err.Error()
		renderResults(w, data)
		return
	}

	data.Results = results
	data.TreemapJSON = treemapJSON(results)
	renderResults(w, data)
}

// fetchLiveProfile pulls a heap profile directly from a running Go
// process's net/http/pprof endpoint, saving the round trip of manually
// capturing and uploading a file when the target is reachable from the
// dashboard. target is a bare host:port (never a full URL) so the scheme
// and path are always ours to control, not attacker-suppliable.
func fetchLiveProfile(target string) (path string, cleanup func(), err error) {
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get("http://" + target + "/debug/pprof/heap")
	if err != nil {
		return "", nil, fmt.Errorf("fetch heap profile from %s: %w", target, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("fetch heap profile from %s: unexpected status %s", target, resp.Status)
	}

	tmp, err := os.CreateTemp("", "gosys-live-*.pprof")
	if err != nil {
		return "", nil, fmt.Errorf("create temp file: %w", err)
	}
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", nil, fmt.Errorf("write temp file: %w", err)
	}
	tmp.Close()

	return tmp.Name(), func() { os.Remove(tmp.Name()) }, nil
}

func renderPage(w http.ResponseWriter, data pageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "index.html", data); err != nil {
		log.Println("template execute:", err)
	}
}

// renderResults renders just the results fragment, which is what
// hx-target="#results" swaps into the page on each /analyze submission.
func renderResults(w http.ResponseWriter, data pageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "results", data); err != nil {
		log.Println("template execute:", err)
	}
}
