package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
)

func uploadHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Println("Received request")

	// Parse the incoming form
	//err := r.ParseMultipartForm(10 << 20) // 10MB limit
	//if err != nil {
	//	fmt.Println("Error parsing form:", err)
	//	http.Error(w, err.Error(), http.StatusInternalServerError)
	//	return
	//}
	//fmt.Println("Parsed multipart form")

	// Retrieve file from form data
	file, header, err := r.FormFile("uploadfile")
	if err != nil {
		fmt.Println("Error getting file:", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer file.Close()
	fmt.Printf("Got file: %s, Size: %d bytes\n", header.Filename, header.Size)

	// Create output file
	outputFile, err := os.Create("./upload/" + header.Filename)
	if err != nil {
		fmt.Println("Error opening file:", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer outputFile.Close()
	fmt.Println("Opened file on disk")

	// Copy contents to disk
	written, err := io.Copy(outputFile, file)
	if err != nil {
		fmt.Println("Error copying file:", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	fmt.Printf("Copied %d bytes to disk\n", written)

	// Send success response
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, `{"status": "success", "filename": "`+header.Filename+`"}`)
}

func main() {
	// Start the server
	mux := http.NewServeMux()
	mux.Handle("POST /upload", http.HandlerFunc(uploadHandler))

	fmt.Println("Starting server on :8080")
	http.ListenAndServe("0.0.0.0:8080", mux)
}
