// Command dashboard serves a small web UI around pipeline.Analyze: upload a
// heap pprof file, point it at a repo on disk, and view the same findings
// `cmd/analyze` prints, rendered as HTML instead of stdout.
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
	"os"
	"strings"

	"gosys/internal/pipeline"
)

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

// treemapItem is one allocation site flattened for the client-side treemap:
// one entry per Result, with any matched findings collapsed onto it since
// the treemap groups by site, not by individual finding.
type treemapItem struct {
	File    string `json:"file"`
	Line    int64  `json:"line"`
	Fn      string `json:"fn"`
	Bytes   int64  `json:"bytes"`
	Pattern string `json:"pattern"` // "" if no rule matched
	Message string `json:"message"`
	Source  string `json:"source"`
}

// treemapJSON flattens results into the shape the dashboard's treemap script
// expects and marshals it for embedding in an `application/json` data
// island. json.Marshal HTML-escapes <, >, and & by default, which is what
// makes it safe to inline as template.JS without a script-breakout risk.
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
			messages := make([]string, len(r.Findings))
			for i, f := range r.Findings {
				patterns[i] = f.Pattern
				messages[i] = f.Message
			}
			item.Pattern = strings.Join(patterns, ", ")
			item.Message = strings.Join(messages, " ")
			item.Source = r.Findings[0].Source
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
	renderPage(w, pageData{RepoDir: ".", Top: 10, TreemapJSON: treemapJSON(nil)})
}

func handleAnalyze(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	data := pageData{
		RepoDir: r.FormValue("repo"),
		Top:     10,
	}
	if v := r.FormValue("top"); v != "" {
		fmt.Sscanf(v, "%d", &data.Top)
	}

	profilePath, cleanup, err := saveUpload(r)
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

// saveUpload copies the "pprof" multipart field to a temp file, since
// pprofstats.Top needs a filesystem path, not an io.Reader.
func saveUpload(r *http.Request) (path string, cleanup func(), err error) {
	file, _, err := r.FormFile("pprof")
	if err != nil {
		return "", nil, fmt.Errorf("read uploaded pprof file: %w", err)
	}
	defer file.Close()

	tmp, err := os.CreateTemp("", "gosys-upload-*.pprof")
	if err != nil {
		return "", nil, fmt.Errorf("create temp file: %w", err)
	}
	if _, err := io.Copy(tmp, file); err != nil {
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
