package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"
)

var (
	slackAPIBaseURL = "https://slack.com/api"
)

type StreamResponse struct {
	Ok      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
	StreamID string `json:"stream_id,omitempty"`
}

func main() {
	slackToken := os.Getenv("SLACK_TOKEN")
	if slackToken == "" {
		fmt.Fprintf(os.Stderr, "Error: SLACK_TOKEN environment variable is not set\n")
		os.Exit(1)
	}

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
		triggerID := r.FormValue("trigger_id")
		channelID := r.FormValue("channel_id")

		if text == "" || triggerID == "" || channelID == "" {
			http.Error(w, "Missing required fields", http.StatusBadRequest)
			return
		}

		// Strip leading '$' from text
		command := strings.TrimPrefix(text, "$")
		command = strings.TrimSpace(command)

		// Return empty response immediately
		w.WriteHeader(http.StatusOK)

		// Spawn goroutine to handle command execution and streaming
		go handleCommandExecution(slackToken, channelID, triggerID, command)
	})

	fmt.Printf("Starting server on port %s\n", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		fmt.Fprintf(os.Stderr, "Error starting server: %v\n", err)
		os.Exit(1)
	}
}

func handleCommandExecution(token, channelID, triggerID, command string) {
	// Start chat stream
	streamID, err := startChatStream(token, channelID, triggerID)
	if err != nil {
		fmt.Printf("Error starting chat stream: %v\n", err)
		return
	}

	// Execute command
	cmd := exec.Command("sh", "-c", command)
	
	// Capture stdout and stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Printf("Error creating stdout pipe: %v\n", err)
		stopChatStream(token, streamID)
		return
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		fmt.Printf("Error creating stderr pipe: %v\n", err)
		stopChatStream(token, streamID)
		return
	}

	if err := cmd.Start(); err != nil {
		appendToStream(token, streamID, fmt.Sprintf("Error starting command: %v\n", err))
		stopChatStream(token, streamID)
		return
	}

	startTime := time.Now()

	// Use channels to collect output thread-safely
	outputCh := make(chan []byte, 100)
	var outputBuf bytes.Buffer
	outputBuf.WriteString("```\n")

	// Goroutine to read stdout
	stdoutDone := make(chan bool)
	go func() {
		defer close(stdoutDone)
		buf := make([]byte, 4096)
		for {
			n, err := stdout.Read(buf)
			if n > 0 {
				outputCh <- append([]byte(nil), buf[:n]...)
			}
			if err != nil {
				break
			}
		}
	}()

	// Goroutine to read stderr
	stderrDone := make(chan bool)
	go func() {
		defer close(stderrDone)
		buf := make([]byte, 4096)
		for {
			n, err := stderr.Read(buf)
			if n > 0 {
				outputCh <- append([]byte(nil), buf[:n]...)
			}
			if err != nil {
				break
			}
		}
	}()

	// Periodically append logs (every 1 second)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	// Monitor for command completion
	commandDone := make(chan error, 1)
	go func() {
		commandDone <- cmd.Wait()
	}()

	var lastSentLen int

	for {
		select {
		case data := <-outputCh:
			// Collect output into buffer
			outputBuf.Write(data)

		case <-ticker.C:
			// Append new output since last append
			if outputBuf.Len() > lastSentLen {
				newOutput := outputBuf.Bytes()[lastSentLen:]
				if len(newOutput) > 0 {
					appendToStream(token, streamID, string(newOutput))
					lastSentLen = outputBuf.Len()
				}
			}

		case err := <-commandDone:
			// Command finished, wait for all output to be read
			<-stdoutDone
			<-stderrDone

			// Drain any remaining output from channel
			for {
				select {
				case data := <-outputCh:
					outputBuf.Write(data)
				default:
					goto drained
				}
			}
		drained:

			// Append any remaining output
			if outputBuf.Len() > lastSentLen {
				remainingOutput := outputBuf.Bytes()[lastSentLen:]
				if len(remainingOutput) > 0 {
					appendToStream(token, streamID, string(remainingOutput))
				}
			}

			// Get exit code
			exitCode := 0
			if err != nil {
				if exitError, ok := err.(*exec.ExitError); ok {
					exitCode = exitError.ExitCode()
				}
			}

			// Calculate execution time
			duration := time.Since(startTime)

			// Append debugging information
			debugInfo := fmt.Sprintf("```\n\n**Process completed**\n- Exit code: %d\n- Execution time: %v\n", exitCode, duration)
			appendToStream(token, streamID, debugInfo)

			// Stop the stream
			stopChatStream(token, streamID)
			return
		}
	}
}

func startChatStream(token, channelID, triggerID string) (string, error) {
	data := url.Values{}
	data.Set("token", token)
	data.Set("channel", channelID)
	data.Set("trigger_id", triggerID)

	req, err := http.NewRequest("POST", slackAPIBaseURL+"/chat.startStream", strings.NewReader(data.Encode()))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var streamResp StreamResponse
	if err := json.NewDecoder(resp.Body).Decode(&streamResp); err != nil {
		return "", err
	}

	if !streamResp.Ok {
		return "", fmt.Errorf("slack API error: %s", streamResp.Error)
	}

	return streamResp.StreamID, nil
}

func appendToStream(token, streamID, content string) {
	data := url.Values{}
	data.Set("token", token)
	data.Set("stream_id", streamID)
	data.Set("content", content)

	req, err := http.NewRequest("POST", slackAPIBaseURL+"/chat.appendStream", strings.NewReader(data.Encode()))
	if err != nil {
		fmt.Printf("Error creating append request: %v\n", err)
		return
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("Error appending to stream: %v\n", err)
		return
	}
	defer resp.Body.Close()

	var streamResp StreamResponse
	if err := json.NewDecoder(resp.Body).Decode(&streamResp); err != nil {
		fmt.Printf("Error decoding append response: %v\n", err)
		return
	}

	if !streamResp.Ok {
		fmt.Printf("Slack API error appending: %s\n", streamResp.Error)
	}
}

func stopChatStream(token, streamID string) {
	data := url.Values{}
	data.Set("token", token)
	data.Set("stream_id", streamID)

	req, err := http.NewRequest("POST", slackAPIBaseURL+"/chat.stopStream", strings.NewReader(data.Encode()))
	if err != nil {
		fmt.Printf("Error creating stop request: %v\n", err)
		return
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("Error stopping stream: %v\n", err)
		return
	}
	defer resp.Body.Close()

	var streamResp StreamResponse
	if err := json.NewDecoder(resp.Body).Decode(&streamResp); err != nil {
		fmt.Printf("Error decoding stop response: %v\n", err)
		return
	}

	if !streamResp.Ok {
		fmt.Printf("Slack API error stopping stream: %s\n", streamResp.Error)
	}
}

