// Command dashboard serves a small web UI around pipeline.Analyze: upload a
// heap pprof file, point it at a repo on disk, and view the same findings
// `cmd/analyze` prints, rendered as HTML instead of stdout.
package main

import (
	"embed"
	"flag"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"

	"gosys/internal/pipeline"
)

//go:embed templates/*.html
var templatesFS embed.FS

var tmpl = template.Must(template.ParseFS(templatesFS, "templates/*.html"))

type pageData struct {
	RepoDir string
	Top     int
	Results []pipeline.Result
	Err     string
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
	renderPage(w, pageData{RepoDir: ".", Top: 10})
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
