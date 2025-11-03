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
			"text":          result,
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

	// Prepare output
	var result bytes.Buffer
	result.WriteString(originalText)
	result.WriteString("\n\n")
	result.WriteString("```\n")
	result.Write(stdout.Bytes())
	if stderr.Len() > 0 {
		result.WriteString("\n--- stderr ---\n")
		result.Write(stderr.Bytes())
	}
	result.WriteString("```\n\n")
	result.WriteString(fmt.Sprintf("_%.2fms %dexit_\n", float64(duration.Nanoseconds())/1e6, exitCode))

	return result.String()
}
