package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"

	aai "github.com/AssemblyAI/assemblyai-go-sdk"
	"github.com/joho/godotenv"
)

const (
	Info  = "INFO"
	Error = "ERROR"
)

var apiKey string

func logMessage(messageType, message string) {
	switch messageType {
	case Info:
		fmt.Printf("[INFO] %s\n", message)
	case Error:
		fmt.Printf("\033[31m[ERROR] %s\033[0m\n", message) // Red color for errors
	}
}

func uploadHandler(w http.ResponseWriter, r *http.Request) {
	logMessage(Info, "Received request")

	// Retrieve file from form data
	file, header, err := r.FormFile("uploadfile")
	if err != nil {
		logMessage(Error, fmt.Sprintf("Error getting file: %v", err))
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer file.Close()
	logMessage(Info, fmt.Sprintf("Got file: %s, Size: %d bytes", header.Filename, header.Size))

	// Create temporary file
	tempFile, err := os.CreateTemp("", "upload-*.tmp")
	if err != nil {
		logMessage(Error, fmt.Sprintf("Error creating temporary file: %v", err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer tempFile.Close()
	logMessage(Info, fmt.Sprintf("Created temporary file: %s", tempFile.Name()))

	// Copy contents to temporary file
	written, err := io.Copy(tempFile, file)
	if err != nil {
		logMessage(Error, fmt.Sprintf("Error copying file: %v", err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	logMessage(Info, fmt.Sprintf("Copied %d bytes to temporary file", written))

	// Send file to AssemblyAI
	transcript, err := sendToAssemblyAI(tempFile.Name())
	if err != nil {
		logMessage(Error, fmt.Sprintf("Error sending file to AssemblyAI: %v", err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	fmt.Printf("Transcript: %s\n", *transcript.Text)

	// Send success response
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, `{"status": "success", "transcript": "`+*transcript.Text+`"}`)
}

func sendToAssemblyAI(fileName string) (*aai.Transcript, error) {
	client := aai.NewClient(apiKey)
	ctx := context.Background()

	// Open the file
	file, err := os.Open(fileName)
	if err != nil {
		logMessage(Error, fmt.Sprintf("Error opening file: %v", err))
		return nil, err
	}
	defer file.Close()

	// transcript parameters
	params := &aai.TranscriptOptionalParams{}

	transcript, err := client.Transcripts.TranscribeFromReader(ctx, file, params)
	if err != nil {
		logMessage(Error, fmt.Sprintf("Error transcribing file: %v", err))
		return nil, err
	}

	return &transcript, nil
}

func main() {
	// Load .env file
	err := godotenv.Load()
	if err != nil {
		logMessage(Error, "Error loading .env file")
		os.Exit(1)
	}

	apiKey = os.Getenv("ASSEMBLY_API_KEY")
	if apiKey == "" {
		logMessage(Error, "ASSEMBLY_API_KEY not set in .env file")
		os.Exit(1)
	}

	// Start the server
	mux := http.NewServeMux()
	mux.Handle("POST /upload", http.HandlerFunc(uploadHandler))

	logMessage(Info, "Starting server on :8080")
	http.ListenAndServe("0.0.0.0:8080", mux)
}
