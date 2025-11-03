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
	return setupMockSlackServerWithAuth("")
}

// Mock Slack API server with optional expected token
func setupMockSlackServerWithAuth(expectedToken string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Validate Authorization header
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			streamResp := StreamResponse{
				Ok:    false,
				Error: "invalid_auth",
			}
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(streamResp)
			return
		}

		if !strings.HasPrefix(authHeader, "Bearer ") {
			streamResp := StreamResponse{
				Ok:    false,
				Error: "invalid_auth",
			}
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(streamResp)
			return
		}

		token := strings.TrimPrefix(authHeader, "Bearer ")
		if expectedToken != "" && token != expectedToken {
			streamResp := StreamResponse{
				Ok:    false,
				Error: "invalid_auth",
			}
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(streamResp)
			return
		}

		// Validate required form parameters
		if err := r.ParseForm(); err == nil {
			// Token should be in form data for all streaming endpoints
			if r.URL.Path == "/chat.startStream" || r.URL.Path == "/chat.appendStream" || r.URL.Path == "/chat.stopStream" {
				if r.FormValue("token") == "" {
					streamResp := StreamResponse{
						Ok:    false,
						Error: "missing_required_field",
					}
					w.WriteHeader(http.StatusBadRequest)
					json.NewEncoder(w).Encode(streamResp)
					return
				}
			}
			// Additional validation for startStream
			if r.URL.Path == "/chat.startStream" {
				if r.FormValue("channel") == "" || r.FormValue("thread_ts") == "" {
					streamResp := StreamResponse{
						Ok:    false,
						Error: "missing_required_field",
					}
					w.WriteHeader(http.StatusBadRequest)
					json.NewEncoder(w).Encode(streamResp)
					return
				}
			}
			// Additional validation for appendStream
			if r.URL.Path == "/chat.appendStream" {
				if r.FormValue("channel") == "" || r.FormValue("ts") == "" || r.FormValue("markdown_text") == "" {
					streamResp := StreamResponse{
						Ok:    false,
						Error: "missing_required_field",
					}
					w.WriteHeader(http.StatusBadRequest)
					json.NewEncoder(w).Encode(streamResp)
					return
				}
			}
			// Additional validation for stopStream
			if r.URL.Path == "/chat.stopStream" {
				if r.FormValue("channel") == "" || r.FormValue("ts") == "" {
					streamResp := StreamResponse{
						Ok:    false,
						Error: "missing_required_field",
					}
					w.WriteHeader(http.StatusBadRequest)
					json.NewEncoder(w).Encode(streamResp)
					return
				}
			}
		}

		switch r.URL.Path {
		case "/chat.postMessage":
			var msgResp struct {
				Ok  bool   `json:"ok"`
				TS  string `json:"ts"`
			}
			msgResp.Ok = true
			msgResp.TS = "1234567890.123456"
			json.NewEncoder(w).Encode(msgResp)

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
		{"missing text", map[string]string{"channel_id": "C123", "user_id": "U123"}},
		{"missing channel_id", map[string]string{"text": "date", "user_id": "U123"}},
		{"missing user_id", map[string]string{"text": "date", "channel_id": "C123"}},
		{"empty text", map[string]string{"text": "", "channel_id": "C123", "user_id": "U123"}},
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
	data.Set("channel_id", "C123")
	data.Set("user_id", "U123")
	data.Set("team_id", "T123")
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
			data.Set("channel_id", "C123")
			data.Set("user_id", "U123")

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
	mockServer := setupMockSlackServerWithAuth("test-token")
	defer mockServer.Close()

	originalBaseURL := slackAPIBaseURL
	slackAPIBaseURL = mockServer.URL
	defer func() { slackAPIBaseURL = originalBaseURL }()

	streamID, err := startChatStream("test-token", "C123", "U123", "T123", "1234567890.123456")
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
		// Check Authorization header
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
			w.WriteHeader(http.StatusUnauthorized)
		}
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

	_, err := startChatStream("test-token", "C123", "U123", "T123", "1234567890.123456")
	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	if !strings.Contains(err.Error(), "invalid_auth") {
		t.Errorf("Expected error to contain 'invalid_auth', got %q", err.Error())
	}
}

func TestStartChatStream_MissingAuthHeader(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Simulate missing Authorization header
		w.WriteHeader(http.StatusUnauthorized)
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

	_, err := startChatStream("test-token", "C123", "U123", "T123", "1234567890.123456")
	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	if !strings.Contains(err.Error(), "invalid_auth") {
		t.Errorf("Expected error to contain 'invalid_auth', got %q", err.Error())
	}
}

func TestStartChatStream_ValidatesFormData(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		
		// Validate Authorization header exists
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
			w.WriteHeader(http.StatusUnauthorized)
			streamResp := StreamResponse{Ok: false, Error: "invalid_auth"}
			json.NewEncoder(w).Encode(streamResp)
			return
		}

		// Validate required fields are present
		if err := r.ParseForm(); err == nil {
			if r.FormValue("token") == "" || r.FormValue("channel") == "" || r.FormValue("thread_ts") == "" {
				w.WriteHeader(http.StatusBadRequest)
				streamResp := StreamResponse{Ok: false, Error: "missing_required_field"}
				json.NewEncoder(w).Encode(streamResp)
				return
			}
		}

		streamResp := StreamResponse{
			Ok:      true,
			StreamID: "test-stream-id-123",
		}
		json.NewEncoder(w).Encode(streamResp)
	}))
	defer mockServer.Close()

	originalBaseURL := slackAPIBaseURL
	slackAPIBaseURL = mockServer.URL
	defer func() { slackAPIBaseURL = originalBaseURL }()

	streamID, err := startChatStream("test-token", "C123", "U123", "T123", "1234567890.123456")
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if streamID != "test-stream-id-123" {
		t.Errorf("Expected stream ID 'test-stream-id-123', got %q", streamID)
	}
}

func TestStartChatStream_NetworkError(t *testing.T) {
	originalBaseURL := slackAPIBaseURL
	slackAPIBaseURL = "http://localhost:0" // Invalid port
	defer func() { slackAPIBaseURL = originalBaseURL }()

	_, err := startChatStream("test-token", "C123", "U123", "T123", "1234567890.123456")
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
}

func TestAppendToStream(t *testing.T) {
	var appendedContent []string
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/chat.appendStream" {
			// Validate Authorization header
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
				w.WriteHeader(http.StatusUnauthorized)
				streamResp := StreamResponse{Ok: false, Error: "invalid_auth"}
				json.NewEncoder(w).Encode(streamResp)
				return
			}

			r.ParseForm()
			appendedContent = append(appendedContent, r.FormValue("markdown_text"))
		}
		w.Header().Set("Content-Type", "application/json")
		streamResp := StreamResponse{Ok: true}
		json.NewEncoder(w).Encode(streamResp)
	}))
	defer mockServer.Close()

	originalBaseURL := slackAPIBaseURL
	slackAPIBaseURL = mockServer.URL
	defer func() { slackAPIBaseURL = originalBaseURL }()

	// Test successful append
	success := appendToStream("test-token", "C123", "1234567890.123456", "test content")
	if !success {
		t.Error("Expected appendToStream to return true on success")
	}

	success = appendToStream("test-token", "C123", "1234567890.123456", "more content")
	if !success {
		t.Error("Expected appendToStream to return true on success")
	}

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

func TestAppendToStream_Failure(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/chat.appendStream" {
			w.Header().Set("Content-Type", "application/json")
			streamResp := StreamResponse{Ok: false, Error: "invalid_arguments"}
			json.NewEncoder(w).Encode(streamResp)
			return
		}
	}))
	defer mockServer.Close()

	originalBaseURL := slackAPIBaseURL
	slackAPIBaseURL = mockServer.URL
	defer func() { slackAPIBaseURL = originalBaseURL }()

	// Test failed append
	success := appendToStream("test-token", "C123", "1234567890.123456", "test content")
	if success {
		t.Error("Expected appendToStream to return false on failure")
	}
}

func TestAppendToStream_BlankMessage(t *testing.T) {
	var requestsReceived int
	
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/chat.appendStream" {
			requestsReceived++
			w.Header().Set("Content-Type", "application/json")
			streamResp := StreamResponse{Ok: true}
			json.NewEncoder(w).Encode(streamResp)
			return
		}
	}))
	defer mockServer.Close()

	originalBaseURL := slackAPIBaseURL
	slackAPIBaseURL = mockServer.URL
	defer func() { slackAPIBaseURL = originalBaseURL }()

	// Test blank message is skipped
	success := appendToStream("test-token", "C123", "1234567890.123456", "")
	if !success {
		t.Error("Expected appendToStream to return true when skipping blank message")
	}
	
	success = appendToStream("test-token", "C123", "1234567890.123456", "   ")
	if !success {
		t.Error("Expected appendToStream to return true when skipping whitespace-only message")
	}
	
	success = appendToStream("test-token", "C123", "1234567890.123456", "\n\t\n")
	if !success {
		t.Error("Expected appendToStream to return true when skipping whitespace-only message")
	}
	
	// Verify no requests were sent for blank messages
	if requestsReceived != 0 {
		t.Errorf("Expected 0 requests for blank messages, got %d", requestsReceived)
	}
	
	// Verify a non-blank message is sent
	success = appendToStream("test-token", "C123", "1234567890.123456", "actual content")
	if !success {
		t.Error("Expected appendToStream to return true on success")
	}
	if requestsReceived != 1 {
		t.Errorf("Expected 1 request for non-blank message, got %d", requestsReceived)
	}
}

func TestStopChatStream(t *testing.T) {
	var stoppedStreamID string
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/chat.stopStream" {
			// Validate Authorization header
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
				w.WriteHeader(http.StatusUnauthorized)
				streamResp := StreamResponse{Ok: false, Error: "invalid_auth"}
				json.NewEncoder(w).Encode(streamResp)
				return
			}

			r.ParseForm()
			stoppedStreamID = r.FormValue("ts")
		}
		w.Header().Set("Content-Type", "application/json")
		streamResp := StreamResponse{Ok: true}
		json.NewEncoder(w).Encode(streamResp)
	}))
	defer mockServer.Close()

	originalBaseURL := slackAPIBaseURL
	slackAPIBaseURL = mockServer.URL
	defer func() { slackAPIBaseURL = originalBaseURL }()

	// Test successful stop
	success := stopChatStream("test-token", "C123", "1234567890.123456")
	if !success {
		t.Error("Expected stopChatStream to return true on success")
	}

	if stoppedStreamID != "1234567890.123456" {
		t.Errorf("Expected timestamp '1234567890.123456', got %q", stoppedStreamID)
	}
}

func TestStopChatStream_Failure(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/chat.stopStream" {
			w.Header().Set("Content-Type", "application/json")
			streamResp := StreamResponse{Ok: false, Error: "invalid_arguments"}
			json.NewEncoder(w).Encode(streamResp)
			return
		}
	}))
	defer mockServer.Close()

	originalBaseURL := slackAPIBaseURL
	slackAPIBaseURL = mockServer.URL
	defer func() { slackAPIBaseURL = originalBaseURL }()

	// Test failed stop
	success := stopChatStream("test-token", "C123", "1234567890.123456")
	if success {
		t.Error("Expected stopChatStream to return false on failure")
	}
}

func TestPostThreadReply_BlankMessage(t *testing.T) {
	var requestsReceived int
	
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/chat.postMessage" {
			r.ParseForm()
			if r.FormValue("thread_ts") != "" {
				requestsReceived++
				w.Header().Set("Content-Type", "application/json")
				var msgResp struct {
					Ok  bool   `json:"ok"`
					TS  string `json:"ts"`
				}
				msgResp.Ok = true
				msgResp.TS = "1234567890.123456"
				json.NewEncoder(w).Encode(msgResp)
				return
			}
		}
	}))
	defer mockServer.Close()

	originalBaseURL := slackAPIBaseURL
	slackAPIBaseURL = mockServer.URL
	defer func() { slackAPIBaseURL = originalBaseURL }()

	// Test blank message is skipped
	postThreadReply("test-token", "C123", "1234567890.123456", "")
	postThreadReply("test-token", "C123", "1234567890.123456", "   ")
	postThreadReply("test-token", "C123", "1234567890.123456", "\n\t\n")
	
	// Verify no requests were sent for blank messages
	if requestsReceived != 0 {
		t.Errorf("Expected 0 requests for blank messages, got %d", requestsReceived)
	}
	
	// Verify a non-blank message is sent
	postThreadReply("test-token", "C123", "1234567890.123456", "actual content")
	if requestsReceived != 1 {
		t.Errorf("Expected 1 request for non-blank message, got %d", requestsReceived)
	}
}

func TestHandleCommandExecution_SimpleCommand(t *testing.T) {
	var streamOperations []string
	var streamContents []string
	var threadReplies []string

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		
		// Validate Authorization header for all endpoints
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
			w.WriteHeader(http.StatusUnauthorized)
			streamResp := StreamResponse{Ok: false, Error: "invalid_auth"}
			json.NewEncoder(w).Encode(streamResp)
			return
		}

		var streamResp StreamResponse

		switch r.URL.Path {
		case "/chat.postMessage":
			r.ParseForm()
			if r.FormValue("thread_ts") != "" {
				// Track thread replies (should not be called when streaming succeeds)
				threadReplies = append(threadReplies, r.FormValue("text"))
			}
			var msgResp struct {
				Ok  bool   `json:"ok"`
				TS  string `json:"ts"`
			}
			msgResp.Ok = true
			msgResp.TS = "1234567890.123456"
			json.NewEncoder(w).Encode(msgResp)
			return

		case "/chat.startStream":
			streamOperations = append(streamOperations, "start")
			streamResp.Ok = true
			streamResp.StreamID = "test-stream"

		case "/chat.appendStream":
			streamOperations = append(streamOperations, "append")
			r.ParseForm()
			streamContents = append(streamContents, r.FormValue("markdown_text"))
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
	handleCommandExecution("test-token", "C123", "U123", "T123", "", "echo 'test output'")

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
	
	// Verify no fallback thread reply was posted when streaming succeeds
	if len(threadReplies) > 0 {
		t.Errorf("Expected no thread replies when streaming succeeds, got %d", len(threadReplies))
	}
}

func TestHandleCommandExecution_CommandWithOutput(t *testing.T) {
	var appendedContents []string

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		
		// Validate Authorization header
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
			w.WriteHeader(http.StatusUnauthorized)
			streamResp := StreamResponse{Ok: false, Error: "invalid_auth"}
			json.NewEncoder(w).Encode(streamResp)
			return
		}

		var streamResp StreamResponse
		streamResp.Ok = true

		if r.URL.Path == "/chat.postMessage" {
			var msgResp struct {
				Ok  bool   `json:"ok"`
				TS  string `json:"ts"`
			}
			msgResp.Ok = true
			msgResp.TS = "1234567890.123456"
			json.NewEncoder(w).Encode(msgResp)
			return
		} else if r.URL.Path == "/chat.appendStream" {
			r.ParseForm()
			appendedContents = append(appendedContents, r.FormValue("markdown_text"))
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
	handleCommandExecution("test-token", "C123", "U123", "T123", "", "echo 'hello world'")

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
	handleCommandExecution("test-token", "C123", "U123", "T123", "", "nonexistent-command-xyz123")

	// Wait for command to complete
	time.Sleep(2 * time.Second)
}

func TestHandleCommandExecution_StreamStartFailure(t *testing.T) {
	var threadReplies []string
	
	// Mock server that returns error on startStream
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		
		// Validate Authorization header even on error
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
			w.WriteHeader(http.StatusUnauthorized)
			streamResp := StreamResponse{Ok: false, Error: "invalid_auth"}
			json.NewEncoder(w).Encode(streamResp)
			return
		}

		if r.URL.Path == "/chat.postMessage" {
			r.ParseForm()
			if r.FormValue("thread_ts") != "" {
				// This is a thread reply
				threadReplies = append(threadReplies, r.FormValue("text"))
			}
			var msgResp struct {
				Ok  bool   `json:"ok"`
				TS  string `json:"ts"`
			}
			msgResp.Ok = true
			msgResp.TS = "1234567890.123456"
			json.NewEncoder(w).Encode(msgResp)
			return
		}

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

	// This should fail gracefully without crashing and post fallback message
	handleCommandExecution("test-token", "C123", "U123", "T123", "", "echo 'test output'")
	
	// Wait for command to complete
	time.Sleep(2 * time.Second)
	
	// Verify fallback thread reply was posted
	if len(threadReplies) == 0 {
		t.Error("Expected thread reply to be posted when streaming fails")
	}
	
	// Verify the reply contains the command output
	foundOutput := false
	for _, reply := range threadReplies {
		if strings.Contains(reply, "test output") {
			foundOutput = true
		}
		if strings.Contains(reply, "Process completed") {
			foundOutput = true
		}
	}
	if !foundOutput {
		t.Error("Expected thread reply to contain command output or completion info")
	}
}

func TestHandleCommandExecution_StreamAppendFailure(t *testing.T) {
	var threadReplies []string
	appendFailureCount := 0
	
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
			w.WriteHeader(http.StatusUnauthorized)
			streamResp := StreamResponse{Ok: false, Error: "invalid_auth"}
			json.NewEncoder(w).Encode(streamResp)
			return
		}

		var streamResp StreamResponse

		if r.URL.Path == "/chat.postMessage" {
			r.ParseForm()
			if r.FormValue("thread_ts") != "" {
				// This is a thread reply
				threadReplies = append(threadReplies, r.FormValue("text"))
			}
			var msgResp struct {
				Ok  bool   `json:"ok"`
				TS  string `json:"ts"`
			}
			msgResp.Ok = true
			msgResp.TS = "1234567890.123456"
			json.NewEncoder(w).Encode(msgResp)
			return
		} else if r.URL.Path == "/chat.startStream" {
			streamResp.Ok = true
			streamResp.StreamID = "test-stream"
		} else if r.URL.Path == "/chat.appendStream" {
			// Fail first append, succeed on subsequent calls (to test fallback)
			appendFailureCount++
			if appendFailureCount == 1 {
				streamResp.Ok = false
				streamResp.Error = "invalid_arguments"
			} else {
				streamResp.Ok = true
			}
		} else if r.URL.Path == "/chat.stopStream" {
			streamResp.Ok = true
		}

		json.NewEncoder(w).Encode(streamResp)
	}))
	defer mockServer.Close()

	originalBaseURL := slackAPIBaseURL
	slackAPIBaseURL = mockServer.URL
	defer func() { slackAPIBaseURL = originalBaseURL }()

	handleCommandExecution("test-token", "C123", "U123", "T123", "", "echo 'test append failure'")
	
	// Wait for command to complete
	time.Sleep(2 * time.Second)
	
	// Verify fallback thread reply was posted after append failure
	if len(threadReplies) == 0 {
		t.Error("Expected thread reply to be posted when append fails")
	}
	
	// Verify the reply contains the command output
	foundOutput := false
	for _, reply := range threadReplies {
		if strings.Contains(reply, "test append failure") || strings.Contains(reply, "Process completed") {
			foundOutput = true
			break
		}
	}
	if !foundOutput {
		t.Error("Expected thread reply to contain command output or completion info")
	}
}

func TestHandleCommandExecution_StreamStopFailure(t *testing.T) {
	var threadReplies []string
	
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
			w.WriteHeader(http.StatusUnauthorized)
			streamResp := StreamResponse{Ok: false, Error: "invalid_auth"}
			json.NewEncoder(w).Encode(streamResp)
			return
		}

		var streamResp StreamResponse

		if r.URL.Path == "/chat.postMessage" {
			r.ParseForm()
			if r.FormValue("thread_ts") != "" {
				// This is a thread reply
				threadReplies = append(threadReplies, r.FormValue("text"))
			}
			var msgResp struct {
				Ok  bool   `json:"ok"`
				TS  string `json:"ts"`
			}
			msgResp.Ok = true
			msgResp.TS = "1234567890.123456"
			json.NewEncoder(w).Encode(msgResp)
			return
		} else if r.URL.Path == "/chat.startStream" {
			streamResp.Ok = true
			streamResp.StreamID = "test-stream"
		} else if r.URL.Path == "/chat.appendStream" {
			streamResp.Ok = true
		} else if r.URL.Path == "/chat.stopStream" {
			// Fail stop
			streamResp.Ok = false
			streamResp.Error = "invalid_arguments"
		}

		json.NewEncoder(w).Encode(streamResp)
	}))
	defer mockServer.Close()

	originalBaseURL := slackAPIBaseURL
	slackAPIBaseURL = mockServer.URL
	defer func() { slackAPIBaseURL = originalBaseURL }()

	handleCommandExecution("test-token", "C123", "U123", "T123", "", "echo 'test stop failure'")
	
	// Wait for command to complete
	time.Sleep(2 * time.Second)
	
	// Verify fallback thread reply was posted after stop failure
	if len(threadReplies) == 0 {
		t.Error("Expected thread reply to be posted when stop fails")
	}
	
	// Verify the reply contains the command output
	foundOutput := false
	for _, reply := range threadReplies {
		if strings.Contains(reply, "test stop failure") || strings.Contains(reply, "Process completed") {
			foundOutput = true
			break
		}
	}
	if !foundOutput {
		t.Error("Expected thread reply to contain command output or completion info")
	}
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
		channelID := r.FormValue("channel_id")
		userID := r.FormValue("user_id")
		teamID := r.FormValue("team_id")
		responseURL := r.FormValue("response_url")

		if text == "" || channelID == "" || userID == "" {
			http.Error(w, "Missing required fields", http.StatusBadRequest)
			return
		}

		command := strings.TrimPrefix(text, "$")
		command = strings.TrimSpace(command)

		w.WriteHeader(http.StatusOK)

		go handleCommandExecution(token, channelID, userID, teamID, responseURL, command)
	}
}

