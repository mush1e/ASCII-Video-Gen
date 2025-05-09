package main

import (
	"html/template"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/mush1e/ASCII-Video-Gen/internal/converter"
)

var (
	tplUpload = template.Must(template.ParseFiles("web/templates/upload.html"))
	tplResult = template.Must(template.ParseFiles("web/templates/result.html"))
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/", uploadPage)
	mux.HandleFunc("/upload", uploadHandler)
	mux.HandleFunc("/stream/", streamHandler)

	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("web/static"))))

	srv := &http.Server{
		Addr:         ":8080",
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	log.Println("Server started on :8080")
	log.Fatal(srv.ListenAndServe())
}

func uploadPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	tplUpload.Execute(w, nil)
}

func uploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	jobID, err := converter.StartJob(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tplResult.Execute(w, jobID)
}

func streamHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	jobID := strings.TrimPrefix(r.URL.Path, "/stream/")
	if jobID == "" {
		http.Error(w, "Missing job ID", http.StatusBadRequest)
		return
	}
	converter.StreamJob(w, r.Context(), jobID)
}
