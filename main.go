package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Parse form data
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		text := r.FormValue("text")

		if text == "" {
			http.Error(w, "Missing required field: text", http.StatusBadRequest)
			return
		}

		// Strip leading '$' from text for execution
		command := strings.TrimPrefix(text, "$")
		command = strings.TrimSpace(command)

		// Execute command synchronously and return result (pass original text for display)
		result := executeCommand(command, text)

		// Create JSON response
		response := map[string]string{
			"response_type": "in_channel",
			"text":          result
		}

		// Return JSON response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	})

	fmt.Printf("Starting server on port %s\n", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		fmt.Fprintf(os.Stderr, "Error starting server: %v\n", err)
		os.Exit(1)
	}
}

func translateExitCode(code int) string {
	exitCodes := map[int]string{
		0:   "success",
		1:   "error",
		2:   "misuse",
		126: "cannot execute",
		127: "not found",
		128: "invalid exit",
		130: "terminated",
		143: "terminated",
	}

	if msg, ok := exitCodes[code]; ok {
		return msg
	}
	return fmt.Sprintf("error %d", code)
}

func executeCommand(command, originalText string) string {
	startTime := time.Now()

	// Execute command
	cmd := exec.Command("sh", "-c", command)

	// Capture stdout and stderr
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Run command and wait for completion
	err := cmd.Run()

	// Get exit code
	exitCode := 0
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			exitCode = exitError.ExitCode()
		}
	}

	// Calculate execution time
	duration := time.Since(startTime)

	// Combine stdout and stderr
	var combinedOutput bytes.Buffer
	combinedOutput.Write(stdout.Bytes())
	if stderr.Len() > 0 {
		combinedOutput.Write(stderr.Bytes())
	}

	// Clean up the output: remove "--- stderr ---" lines and trim blank lines
	outputLines := strings.Split(combinedOutput.String(), "\n")
	var cleanedLines []string
	for _, line := range outputLines {
		trimmed := strings.TrimSpace(line)
		// Skip "--- stderr ---" lines (case insensitive, with optional whitespace)
		if strings.EqualFold(trimmed, "--- stderr ---") {
			continue
		}
		cleanedLines = append(cleanedLines, line)
	}

	// Remove leading and trailing blank lines
	for len(cleanedLines) > 0 && strings.TrimSpace(cleanedLines[0]) == "" {
		cleanedLines = cleanedLines[1:]
	}
	for len(cleanedLines) > 0 && strings.TrimSpace(cleanedLines[len(cleanedLines)-1]) == "" {
		cleanedLines = cleanedLines[:len(cleanedLines)-1]
	}

	// Ensure we never create an empty code block
	// Check if we have any actual content (originalText should always have content, but be safe)
	hasContent := strings.TrimSpace(originalText) != "" || len(cleanedLines) > 0

	if !hasContent {
		// If no content, return just the status without code block
		return fmt.Sprintf("%s %.2fms", translateExitCode(exitCode), float64(duration.Nanoseconds())/1e6)
	}

	// Prepare output - all inside code block
	var result bytes.Buffer
	result.WriteString("```")
	result.WriteString(originalText)

	// Write cleaned output
	if len(cleanedLines) > 0 {
		result.WriteString("\n")
		result.WriteString(strings.Join(cleanedLines, "\n"))
	}

	// Add separator and status
	result.WriteString("\n---\n")
	result.WriteString(fmt.Sprintf("%s %.2fms", translateExitCode(exitCode), float64(duration.Nanoseconds())/1e6))
	result.WriteString("```\n")

	return result.String()
}
