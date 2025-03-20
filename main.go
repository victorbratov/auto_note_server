package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"

	aai "github.com/AssemblyAI/assemblyai-go-sdk"
	"github.com/clerk/clerk-sdk-go/v2"
	clerkhttp "github.com/clerk/clerk-sdk-go/v2/http"
	"github.com/clerk/clerk-sdk-go/v2/user"
	"github.com/joho/godotenv"
)

const (
	Info  = "INFO"
	Error = "ERROR"
)

var (
	assemblyApiKey string
	groqApiKey     string
)

const (
	promptPrefix = `Task: Summarize the following transcript of a STEM lecture. Extract the main points, key concepts, and essential details.

If any critical information is missing or unclear, use your knowledge to fill in gaps while staying true to the topic.
Output format: The summary must be written in Markdown. You may use:
  Headings (#, ##, ###) to structure content.
  Bullet points (-, *) for key points.
  Tables when presenting structured data.
  LaTeX ($inline$ or $$block$$) for mathematical notation.
Strict formatting rule: Output only the Markdown-formatted summary—no extra text, explanations, or disclaimers. Any deviation from this instruction will result in a 0 grade.
Transcript:`
)

func logMessage(messageType, message string) {
	switch messageType {
	case Info:
		fmt.Printf("[INFO] %s\n", message)
	case Error:
		fmt.Printf("\033[31m[ERROR] %s\033[0m\n", message) // Red color for errors
	}
}

func sanitizeInput(input string) string {
	// Replace newline and tab characters with escape sequences
	input = strings.ReplaceAll(input, "\\", "\\\\")
	input = strings.ReplaceAll(input, "\n", "\\n")
	input = strings.ReplaceAll(input, "\t", "\\t")
	input = strings.ReplaceAll(input, "\r", "\\r")
	input = strings.ReplaceAll(input, "\f", "\\f")

	// Regex to match control characters (U+0000 - U+001F) except newline (\n) and tab (\t)
	re := regexp.MustCompile(`[\x00-\x08\x0B\x0C\x0E-\x1F]`)
	return re.ReplaceAllString(input, "")
}

func uploadHandler(w http.ResponseWriter, r *http.Request) {
	claims, ok := clerk.SessionClaimsFromContext(r.Context())

	if !ok {
		logMessage(Error, "No session claims found")
		logMessage(Info, fmt.Sprintf("headers: %v", r.Header))
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"access": "unauthorized"}`))
		return
	}

	usr, err := user.Get(r.Context(), claims.Subject)
	if err != nil {
		logMessage(Error, fmt.Sprintf("Error getting user: %v", err))
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"access": "internal server error"}`))
		return
		// handle the error
	}
	logMessage(Info, fmt.Sprintf("User: %s", usr.ID))

	if usr.Banned {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"access": "forbidden"}`))
		return
	}

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

func handleSummaryRequest(w http.ResponseWriter, r *http.Request) {
	claims, ok := clerk.SessionClaimsFromContext(r.Context())
	if !ok {
		logMessage(Error, "No session claims found")
		logMessage(Info, fmt.Sprintf("headers: %v", r.Header))
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"access": "unauthorized"}`))
		return
	}
	usr, err := user.Get(r.Context(), claims.Subject)
	if err != nil {
		logMessage(Error, fmt.Sprintf("Error getting user: %v", err))
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"access": "internal server error"}`))
		return
	}
	logMessage(Info, fmt.Sprintf("User: %s", usr.ID))
	if usr.Banned {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"access": "forbidden"}`))
		return
	}
	logMessage(Info, "Received request")

	// Decode JSON body
	var requestData struct {
		Text string `json:"text"`
	}
	err = json.NewDecoder(r.Body).Decode(&requestData)
	if err != nil {
		logMessage(Error, "Invalid JSON body")
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	transcript := requestData.Text
	if transcript == "" {
		logMessage(Error, "No transcript provided")
		http.Error(w, "No transcript provided", http.StatusBadRequest)
		return
	}

	// Get summary from AI
	summary, err := getAIResponse(promptPrefix + transcript)
	if err != nil {
		logMessage(Error, fmt.Sprintf("Error getting summary from AI: %v", err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	sanitizedSummary := sanitizeInput(summary)
	// Send success response
	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")
	logMessage(Info, fmt.Sprintf("Summary: %s", sanitizedSummary))
	fmt.Fprintf(w, `{"status": "success", "summary": "%s"}`, sanitizedSummary)
}

func sendToAssemblyAI(fileName string) (*aai.Transcript, error) {
	client := aai.NewClient(assemblyApiKey)
	ctx := context.Background()

	// Open the file
	file, err := os.Open(fileName)
	if err != nil {
		logMessage(Error, fmt.Sprintf("Error opening file: %v", err))
		return nil, err
	}
	defer file.Close()

	// transcript parameters
	params := &aai.TranscriptOptionalParams{
		Punctuate:  aai.Bool(true),
		FormatText: aai.Bool(true),
	}

	transcript, err := client.Transcripts.TranscribeFromReader(ctx, file, params)
	if err != nil {
		logMessage(Error, fmt.Sprintf("Error transcribing file: %v", err))
		return nil, err
	}

	return &transcript, nil
}

func getAIResponse(prompt string) (string, error) {
	url := "https://api.groq.com/openai/v1/chat/completions"
	payload := map[string]interface{}{
		"model": "llama-3.3-70b-versatile",
		"messages": []map[string]string{
			{
				"role":    "user",
				"content": prompt,
			},
		},
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(payloadBytes))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+groqApiKey)

	client := &http.Client{}
	resp, err := client.Do(req)
	logMessage(Info, fmt.Sprintf("request: %v", req))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code: %d, %s", resp.StatusCode, resp.Body)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	err = json.Unmarshal(body, &result)
	if err != nil {
		return "", err
	}

	logMessage(Info, fmt.Sprintf("response: %v", result))

	if len(result.Choices) == 0 {
		return "", fmt.Errorf("no response from AI")
	}

	return result.Choices[0].Message.Content, nil
}

func main() {
	// Load .env file
	err := godotenv.Load()
	if err != nil {
		logMessage(Error, "Error loading .env file")
		os.Exit(1)
	}

	assemblyApiKey = os.Getenv("ASSEMBLY_API_KEY")
	if assemblyApiKey == "" {
		logMessage(Error, "ASSEMBLY_API_KEY not set in .env file")
		os.Exit(1)
	}

	groqApiKey = os.Getenv("GROQ_API_KEY")
	if groqApiKey == "" {
		logMessage(Error, "GROQ_API_KEY not set in .env file")
		os.Exit(1)
	}

	clerkApiKey := os.Getenv("CLERK_API_KEY")
	if clerkApiKey == "" {
		logMessage(Error, "CLERK_API_KEY not set in .env file")
		os.Exit(1)
	}

	clerk.SetKey(clerkApiKey)

	// Start the server
	mux := http.NewServeMux()
	protectedHandler := http.HandlerFunc(uploadHandler)
	mux.Handle(
		"POST /transcribe",
		clerkhttp.WithHeaderAuthorization()(protectedHandler),
	)
	mux.Handle("POST /summarize", clerkhttp.WithHeaderAuthorization()(http.HandlerFunc(handleSummaryRequest)))

	logMessage(Info, "Starting server on :8080")
	http.ListenAndServe("0.0.0.0:8080", mux)
}
