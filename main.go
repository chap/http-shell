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
	Ok       bool   `json:"ok"`
	Error    string `json:"error,omitempty"`
	StreamID string `json:"stream_id,omitempty"`
}

type PostMessageResponse struct {
	Ok      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
	TS      string `json:"ts,omitempty"`
	Message struct {
		TS string `json:"ts,omitempty"`
	} `json:"message,omitempty"`
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
		channelID := r.FormValue("channel_id")
		userID := r.FormValue("user_id")
		teamID := r.FormValue("team_id")
		responseURL := r.FormValue("response_url")

		if text == "" || channelID == "" || userID == "" {
			http.Error(w, "Missing required fields", http.StatusBadRequest)
			return
		}

		// Strip leading '$' from text
		command := strings.TrimPrefix(text, "$")
		command = strings.TrimSpace(command)

		// Return empty response immediately
		w.WriteHeader(http.StatusOK)

		// Spawn goroutine to handle command execution and streaming
		go handleCommandExecution(slackToken, channelID, userID, teamID, responseURL, command)
	})

	fmt.Printf("Starting server on port %s\n", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		fmt.Fprintf(os.Stderr, "Error starting server: %v\n", err)
		os.Exit(1)
	}
}

func handleCommandExecution(token, channelID, userID, teamID, responseURL, command string) {
	// First, post an initial message to get a valid thread_ts
	threadTS, err := postInitialMessage(token, channelID, userID, teamID, command)
	if err != nil {
		fmt.Printf("Error posting initial message: %v\n", err)
		return
	}

	// Track if streaming is working
	streamingEnabled := false
	_, err = startChatStream(token, channelID, userID, teamID, threadTS)
	if err != nil {
		fmt.Printf("Error starting chat stream: %v\n", err)
		// Continue without streaming - will post all output at the end
	} else {
		streamingEnabled = true
	}

	// Execute command
	cmd := exec.Command("sh", "-c", command)

	// Capture stdout and stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Printf("Error creating stdout pipe: %v\n", err)
		if streamingEnabled {
			stopChatStream(token, channelID, threadTS)
		}
		return
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		fmt.Printf("Error creating stderr pipe: %v\n", err)
		if streamingEnabled {
			stopChatStream(token, channelID, threadTS)
		}
		return
	}

	if err := cmd.Start(); err != nil {
		errorMsg := fmt.Sprintf("Error starting command: %v\n", err)
		if streamingEnabled {
			appendToStream(token, channelID, threadTS, errorMsg)
			stopChatStream(token, channelID, threadTS)
		} else {
			// Post error as reply if streaming not available
			postThreadReply(token, channelID, threadTS, errorMsg)
		}
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
	streamingFailed := false

	for {
		select {
		case data := <-outputCh:
			// Collect output into buffer
			outputBuf.Write(data)

		case <-ticker.C:
			// Append new output since last append
			if outputBuf.Len() > lastSentLen && streamingEnabled && !streamingFailed {
				newOutput := outputBuf.Bytes()[lastSentLen:]
				if len(newOutput) > 0 {
					if !appendToStream(token, channelID, threadTS, string(newOutput)) {
						// If append fails, mark streaming as failed
						streamingFailed = true
					} else {
						lastSentLen = outputBuf.Len()
					}
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

			// Get exit code
			exitCode := 0
			if err != nil {
				if exitError, ok := err.(*exec.ExitError); ok {
					exitCode = exitError.ExitCode()
				}
			}

			// Calculate execution time
			duration := time.Since(startTime)

			// Prepare final output
			var finalOutput bytes.Buffer
			finalOutput.Write(outputBuf.Bytes())
			debugInfo := fmt.Sprintf("```\n\n**Process completed**\n- Exit code: %d\n- Execution time: %v\n", exitCode, duration)
			finalOutput.WriteString(debugInfo)

			// If streaming failed or was never enabled, post all output as a reply
			if !streamingEnabled || streamingFailed {
				postThreadReply(token, channelID, threadTS, finalOutput.String())
			} else {
				// Try to append remaining output and debug info
				if outputBuf.Len() > lastSentLen {
					remainingOutput := outputBuf.Bytes()[lastSentLen:]
					if len(remainingOutput) > 0 {
						if !appendToStream(token, channelID, threadTS, string(remainingOutput)) {
							streamingFailed = true
						}
					}
				}
				if !streamingFailed {
					if !appendToStream(token, channelID, threadTS, debugInfo) {
						streamingFailed = true
					} else {
						// Try to stop stream - if it fails, mark as failed
						if !stopChatStream(token, channelID, threadTS) {
							streamingFailed = true
						}
					}
				}
				// If streaming failed at any point (append or stop), post everything as reply
				if streamingFailed {
					postThreadReply(token, channelID, threadTS, finalOutput.String())
				}
			}
			return
		}
	}
}

func postInitialMessage(token, channelID, userID, teamID, command string) (string, error) {
	data := url.Values{}
	data.Set("token", token)
	data.Set("channel", channelID)
	// Tag user and show command
	messageText := fmt.Sprintf("<@%s> starting sandbox `%s`...\n", userID, command)
	data.Set("text", messageText)

	req, err := http.NewRequest("POST", slackAPIBaseURL+"/chat.postMessage", strings.NewReader(data.Encode()))
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

	var msgResp PostMessageResponse
	if err := json.NewDecoder(resp.Body).Decode(&msgResp); err != nil {
		return "", err
	}

	if !msgResp.Ok {
		return "", fmt.Errorf("slack API error: %s", msgResp.Error)
	}

	// Get timestamp from response
	ts := msgResp.TS
	if ts == "" && msgResp.Message.TS != "" {
		ts = msgResp.Message.TS
	}

	if ts == "" {
		return "", fmt.Errorf("no timestamp in postMessage response")
	}

	return ts, nil
}

func startChatStream(token, channelID, userID, teamID, threadTS string) (string, error) {
	data := url.Values{}
	data.Set("token", token)
	data.Set("channel", channelID)
	data.Set("thread_ts", threadTS)

	// recipient_user_id and recipient_team_id are required when streaming to channels
	// Channels start with 'C', DMs start with 'D', groups start with 'G'
	if strings.HasPrefix(channelID, "C") || strings.HasPrefix(channelID, "G") {
		if userID != "" {
			data.Set("recipient_user_id", userID)
		}
		if teamID != "" {
			data.Set("recipient_team_id", teamID)
		}
	}

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

func appendToStream(token, channelID, ts, markdownText string) bool {
	data := url.Values{}
	data.Set("token", token)
	data.Set("channel", channelID)
	data.Set("ts", ts)
	data.Set("markdown_text", markdownText)

	req, err := http.NewRequest("POST", slackAPIBaseURL+"/chat.appendStream", strings.NewReader(data.Encode()))
	if err != nil {
		fmt.Printf("Error creating append request: %v\n", err)
		return false
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("Error appending to stream: %v\n", err)
		return false
	}
	defer resp.Body.Close()

	var streamResp StreamResponse
	if err := json.NewDecoder(resp.Body).Decode(&streamResp); err != nil {
		fmt.Printf("Error decoding append response: %v\n", err)
		return false
	}

	if !streamResp.Ok {
		fmt.Printf("Slack API error appending: %s\n", streamResp.Error)
		return false
	}

	return true
}

func postThreadReply(token, channelID, threadTS, text string) {
	data := url.Values{}
	data.Set("token", token)
	data.Set("channel", channelID)
	data.Set("thread_ts", threadTS)
	data.Set("text", text)

	req, err := http.NewRequest("POST", slackAPIBaseURL+"/chat.postMessage", strings.NewReader(data.Encode()))
	if err != nil {
		fmt.Printf("Error creating thread reply request: %v\n", err)
		return
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("Error posting thread reply: %v\n", err)
		return
	}
	defer resp.Body.Close()

	var msgResp PostMessageResponse
	if err := json.NewDecoder(resp.Body).Decode(&msgResp); err != nil {
		fmt.Printf("Error decoding thread reply response: %v\n", err)
		return
	}

	if !msgResp.Ok {
		fmt.Printf("Slack API error posting thread reply: %s\n", msgResp.Error)
	}
}

func stopChatStream(token, channelID, ts string) bool {
	data := url.Values{}
	data.Set("token", token)
	data.Set("channel", channelID)
	data.Set("ts", ts)

	req, err := http.NewRequest("POST", slackAPIBaseURL+"/chat.stopStream", strings.NewReader(data.Encode()))
	if err != nil {
		fmt.Printf("Error creating stop request: %v\n", err)
		return false
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("Error stopping stream: %v\n", err)
		return false
	}
	defer resp.Body.Close()

	var streamResp StreamResponse
	if err := json.NewDecoder(resp.Body).Decode(&streamResp); err != nil {
		fmt.Printf("Error decoding stop response: %v\n", err)
		return false
	}

	if !streamResp.Ok {
		fmt.Printf("Slack API error stopping stream: %s\n", streamResp.Error)
		return false
	}

	return true
}
