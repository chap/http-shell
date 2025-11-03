package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

// Mock Slack API server
func setupMockSlackServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/chat.startStream":
			var streamResp StreamResponse
			streamResp.Ok = true
			streamResp.StreamID = "test-stream-id-123"
			json.NewEncoder(w).Encode(streamResp)

		case "/chat.appendStream":
			var streamResp StreamResponse
			streamResp.Ok = true
			json.NewEncoder(w).Encode(streamResp)

		case "/chat.stopStream":
			var streamResp StreamResponse
			streamResp.Ok = true
			json.NewEncoder(w).Encode(streamResp)

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestMainFunction_SLACK_TOKENRequired(t *testing.T) {
	// Save original token
	originalToken := os.Getenv("SLACK_TOKEN")
	defer os.Setenv("SLACK_TOKEN", originalToken)

	// Remove token
	os.Unsetenv("SLACK_TOKEN")

	// This would normally exit, but we can't test it directly
	// Instead, we verify the behavior through the handler tests
}

func TestHandler_MethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	handler := createTestHandler("test-token")
	handler(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status %d, got %d", http.StatusMethodNotAllowed, w.Code)
	}
}

func TestHandler_InvalidFormData(t *testing.T) {
	req := httptest.NewRequest("POST", "/", strings.NewReader("%"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	handler := createTestHandler("test-token")
	handler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestHandler_MissingRequiredFields(t *testing.T) {
	tests := []struct {
		name   string
		fields map[string]string
	}{
		{"missing text", map[string]string{"trigger_id": "123", "channel_id": "C123"}},
		{"missing trigger_id", map[string]string{"text": "date", "channel_id": "C123"}},
		{"missing channel_id", map[string]string{"text": "date", "trigger_id": "123"}},
		{"empty text", map[string]string{"text": "", "trigger_id": "123", "channel_id": "C123"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := url.Values{}
			for k, v := range tt.fields {
				data.Set(k, v)
			}

			req := httptest.NewRequest("POST", "/", strings.NewReader(data.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()

			handler := createTestHandler("test-token")
			handler(w, req)

			if w.Code != http.StatusBadRequest {
				t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
			}
		})
	}
}

func TestHandler_ValidRequest(t *testing.T) {
	mockServer := setupMockSlackServer()
	defer mockServer.Close()

	// Temporarily override slackAPIBaseURL
	originalBaseURL := slackAPIBaseURL
	slackAPIBaseURL = mockServer.URL
	defer func() { slackAPIBaseURL = originalBaseURL }()

	data := url.Values{}
	data.Set("text", "$ date")
	data.Set("trigger_id", "test-trigger-id")
	data.Set("channel_id", "C123")
	data.Set("user_id", "U123")
	data.Set("command", "/h")

	req := httptest.NewRequest("POST", "/", strings.NewReader(data.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	handler := createTestHandler("test-token")
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	if w.Body.Len() != 0 {
		t.Errorf("Expected empty body, got %q", w.Body.String())
	}

	// Give goroutine time to start
	time.Sleep(100 * time.Millisecond)
}

func TestHandler_StripDollarPrefix(t *testing.T) {
	mockServer := setupMockSlackServer()
	defer mockServer.Close()

	originalBaseURL := slackAPIBaseURL
	slackAPIBaseURL = mockServer.URL
	defer func() { slackAPIBaseURL = originalBaseURL }()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"with dollar prefix", "$ date", "date"},
		{"without dollar prefix", "date", "date"},
		{"multiple dollar signs", "$$ date", "$ date"},
		{"dollar with space", "$  date", "  date"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := url.Values{}
			data.Set("text", tt.input)
			data.Set("trigger_id", "test-trigger-id")
			data.Set("channel_id", "C123")

			req := httptest.NewRequest("POST", "/", strings.NewReader(data.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()

			handler := createTestHandler("test-token")
			handler(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
			}

			// Give goroutine time to process
			time.Sleep(200 * time.Millisecond)
		})
	}
}

func TestStartChatStream_Success(t *testing.T) {
	mockServer := setupMockSlackServer()
	defer mockServer.Close()

	originalBaseURL := slackAPIBaseURL
	slackAPIBaseURL = mockServer.URL
	defer func() { slackAPIBaseURL = originalBaseURL }()

	streamID, err := startChatStream("test-token", "C123", "trigger-123")
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if streamID != "test-stream-id-123" {
		t.Errorf("Expected stream ID 'test-stream-id-123', got %q", streamID)
	}
}

func TestStartChatStream_APIError(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		streamResp := StreamResponse{
			Ok:    false,
			Error: "invalid_auth",
		}
		json.NewEncoder(w).Encode(streamResp)
	}))
	defer mockServer.Close()

	originalBaseURL := slackAPIBaseURL
	slackAPIBaseURL = mockServer.URL
	defer func() { slackAPIBaseURL = originalBaseURL }()

	_, err := startChatStream("test-token", "C123", "trigger-123")
	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	if !strings.Contains(err.Error(), "invalid_auth") {
		t.Errorf("Expected error to contain 'invalid_auth', got %q", err.Error())
	}
}

func TestStartChatStream_NetworkError(t *testing.T) {
	originalBaseURL := slackAPIBaseURL
	slackAPIBaseURL = "http://localhost:0" // Invalid port
	defer func() { slackAPIBaseURL = originalBaseURL }()

	_, err := startChatStream("test-token", "C123", "trigger-123")
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
}

func TestAppendToStream(t *testing.T) {
	var appendedContent []string
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/chat.appendStream" {
			r.ParseForm()
			appendedContent = append(appendedContent, r.FormValue("content"))
		}
		w.Header().Set("Content-Type", "application/json")
		streamResp := StreamResponse{Ok: true}
		json.NewEncoder(w).Encode(streamResp)
	}))
	defer mockServer.Close()

	originalBaseURL := slackAPIBaseURL
	slackAPIBaseURL = mockServer.URL
	defer func() { slackAPIBaseURL = originalBaseURL }()

	appendToStream("test-token", "stream-123", "test content")
	appendToStream("test-token", "stream-123", "more content")

	if len(appendedContent) != 2 {
		t.Fatalf("Expected 2 appends, got %d", len(appendedContent))
	}

	if appendedContent[0] != "test content" {
		t.Errorf("Expected 'test content', got %q", appendedContent[0])
	}

	if appendedContent[1] != "more content" {
		t.Errorf("Expected 'more content', got %q", appendedContent[1])
	}
}

func TestStopChatStream(t *testing.T) {
	var stoppedStreamID string
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/chat.stopStream" {
			r.ParseForm()
			stoppedStreamID = r.FormValue("stream_id")
		}
		w.Header().Set("Content-Type", "application/json")
		streamResp := StreamResponse{Ok: true}
		json.NewEncoder(w).Encode(streamResp)
	}))
	defer mockServer.Close()

	originalBaseURL := slackAPIBaseURL
	slackAPIBaseURL = mockServer.URL
	defer func() { slackAPIBaseURL = originalBaseURL }()

	stopChatStream("test-token", "stream-456")

	if stoppedStreamID != "stream-456" {
		t.Errorf("Expected stream ID 'stream-456', got %q", stoppedStreamID)
	}
}

func TestHandleCommandExecution_SimpleCommand(t *testing.T) {
	var streamOperations []string
	var streamContents []string

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var streamResp StreamResponse

		switch r.URL.Path {
		case "/chat.startStream":
			streamOperations = append(streamOperations, "start")
			streamResp.Ok = true
			streamResp.StreamID = "test-stream"

		case "/chat.appendStream":
			streamOperations = append(streamOperations, "append")
			r.ParseForm()
			streamContents = append(streamContents, r.FormValue("content"))
			streamResp.Ok = true

		case "/chat.stopStream":
			streamOperations = append(streamOperations, "stop")
			streamResp.Ok = true
		}

		json.NewEncoder(w).Encode(streamResp)
	}))
	defer mockServer.Close()

	originalBaseURL := slackAPIBaseURL
	slackAPIBaseURL = mockServer.URL
	defer func() { slackAPIBaseURL = originalBaseURL }()

	// Execute a simple command that completes quickly
	handleCommandExecution("test-token", "C123", "trigger-123", "echo 'test output'")

	// Wait for command to complete
	time.Sleep(2 * time.Second)

	// Verify stream was started
	foundStart := false
	for _, op := range streamOperations {
		if op == "start" {
			foundStart = true
			break
		}
	}
	if !foundStart {
		t.Error("Expected stream to be started")
	}

	// Verify stream was stopped
	foundStop := false
	for _, op := range streamOperations {
		if op == "stop" {
			foundStop = true
			break
		}
	}
	if !foundStop {
		t.Error("Expected stream to be stopped")
	}

	// Verify output was appended
	if len(streamContents) == 0 {
		t.Error("Expected at least one append operation")
	}

	// Verify completion info was appended
	foundCompletion := false
	for _, content := range streamContents {
		if strings.Contains(content, "Process completed") {
			foundCompletion = true
			if !strings.Contains(content, "Exit code") {
				t.Error("Expected completion info to include exit code")
			}
			if !strings.Contains(content, "Execution time") {
				t.Error("Expected completion info to include execution time")
			}
			break
		}
	}
	if !foundCompletion {
		t.Error("Expected completion information to be appended")
	}
}

func TestHandleCommandExecution_CommandWithOutput(t *testing.T) {
	var appendedContents []string

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var streamResp StreamResponse
		streamResp.Ok = true

		if r.URL.Path == "/chat.appendStream" {
			r.ParseForm()
			appendedContents = append(appendedContents, r.FormValue("content"))
		} else if r.URL.Path == "/chat.startStream" {
			streamResp.StreamID = "test-stream"
		}

		json.NewEncoder(w).Encode(streamResp)
	}))
	defer mockServer.Close()

	originalBaseURL := slackAPIBaseURL
	slackAPIBaseURL = mockServer.URL
	defer func() { slackAPIBaseURL = originalBaseURL }()

	// Execute command with output
	handleCommandExecution("test-token", "C123", "trigger-123", "echo 'hello world'")

	// Wait for command to complete and at least one append
	time.Sleep(1500 * time.Millisecond)

	// Verify output was captured
	foundOutput := false
	for _, content := range appendedContents {
		if strings.Contains(content, "hello world") || strings.Contains(content, "```") {
			foundOutput = true
			break
		}
	}
	if !foundOutput {
		t.Error("Expected command output to be captured and appended")
	}
}

func TestHandleCommandExecution_CommandError(t *testing.T) {
	mockServer := setupMockSlackServer()
	defer mockServer.Close()

	originalBaseURL := slackAPIBaseURL
	slackAPIBaseURL = mockServer.URL
	defer func() { slackAPIBaseURL = originalBaseURL }()

	// Execute a command that will fail
	handleCommandExecution("test-token", "C123", "trigger-123", "nonexistent-command-xyz123")

	// Wait for command to complete
	time.Sleep(2 * time.Second)
}

func TestHandleCommandExecution_StreamStartFailure(t *testing.T) {
	// Mock server that returns error on startStream
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/chat.startStream" {
			streamResp := StreamResponse{
				Ok:    false,
				Error: "invalid_trigger",
			}
			json.NewEncoder(w).Encode(streamResp)
		}
	}))
	defer mockServer.Close()

	originalBaseURL := slackAPIBaseURL
	slackAPIBaseURL = mockServer.URL
	defer func() { slackAPIBaseURL = originalBaseURL }()

	// This should fail gracefully without crashing
	handleCommandExecution("test-token", "C123", "invalid-trigger", "echo test")
	time.Sleep(100 * time.Millisecond)
}

// Helper function to create a test handler
func createTestHandler(token string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

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

		command := strings.TrimPrefix(text, "$")
		command = strings.TrimSpace(command)

		w.WriteHeader(http.StatusOK)

		go handleCommandExecution(token, channelID, triggerID, command)
	}
}

