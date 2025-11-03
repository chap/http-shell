package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestHandler_MethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
	})(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status %d, got %d", http.StatusMethodNotAllowed, w.Code)
	}
}

func TestHandler_InvalidFormData(t *testing.T) {
	req := httptest.NewRequest("POST", "/", strings.NewReader("%"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if err := r.ParseForm(); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}
	})(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestHandler_MissingTextField(t *testing.T) {
	data := url.Values{}
	data.Set("text", "")

	req := httptest.NewRequest("POST", "/", strings.NewReader(data.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if err := r.ParseForm(); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		text := r.FormValue("text")

		if text == "" {
			http.Error(w, "Missing required field: text", http.StatusBadRequest)
			return
		}
	})(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestHandler_ValidRequest(t *testing.T) {
	data := url.Values{}
	data.Set("text", "$ echo hello")

	req := httptest.NewRequest("POST", "/", strings.NewReader(data.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if err := r.ParseForm(); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		text := r.FormValue("text")

		if text == "" {
			http.Error(w, "Missing required field: text", http.StatusBadRequest)
			return
		}

		command := strings.TrimPrefix(text, "$")
		command = strings.TrimSpace(command)

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
	})(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	// Parse JSON response
	var response map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse JSON response: %v", err)
	}

	if response["response_type"] != "in_channel" {
		t.Errorf("Expected response_type 'in_channel', got %q", response["response_type"])
	}

	text := response["text"]
	if text == "" {
		t.Error("Expected non-empty text field")
	}

	if !strings.Contains(text, "$ echo hello") {
		t.Errorf("Expected text to contain original command '$ echo hello', got %q", text)
	}

	if !strings.Contains(text, "hello") {
		t.Errorf("Expected text to contain 'hello', got %q", text)
	}

	if !strings.Contains(text, "success") {
		t.Errorf("Expected text to contain 'success', got %q", text)
	}
}

func TestHandler_StripDollarPrefix(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"with dollar prefix", "$ date", "date"},
		{"without dollar prefix", "date", "date"},
		{"multiple dollar signs", "$$ date", "$ date"},
		{"dollar with space", "$  date", "date"}, // TrimSpace removes leading/trailing spaces
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := url.Values{}
			data.Set("text", tt.input)

			req := httptest.NewRequest("POST", "/", strings.NewReader(data.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()

			var executedCommand string
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
					return
				}

				if err := r.ParseForm(); err != nil {
					http.Error(w, "Bad request", http.StatusBadRequest)
					return
				}

				text := r.FormValue("text")

				if text == "" {
					http.Error(w, "Missing required field: text", http.StatusBadRequest)
					return
				}

				command := strings.TrimPrefix(text, "$")
				command = strings.TrimSpace(command)
				executedCommand = command

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
			})(w, req)

			if executedCommand != tt.expected {
				t.Errorf("Expected command %q, got %q", tt.expected, executedCommand)
			}
		})
	}
}

func TestExecuteCommand_SimpleCommand(t *testing.T) {
	originalText := "$ echo 'test output'"
	result := executeCommand("echo 'test output'", originalText)

	if !strings.Contains(result, originalText) {
		t.Errorf("Expected result to contain original command %q, got %q", originalText, result)
	}

	if !strings.Contains(result, "test output") {
		t.Errorf("Expected result to contain 'test output', got %q", result)
	}

	if !strings.Contains(result, "```") {
		t.Errorf("Expected result to contain code block markers, got %q", result)
	}

	if !strings.Contains(result, "success") {
		t.Errorf("Expected result to contain 'success', got %q", result)
	}

	if !strings.Contains(result, "ms") {
		t.Errorf("Expected result to contain execution time with 'ms', got %q", result)
	}
}

func TestExecuteCommand_CommandWithStderr(t *testing.T) {
	originalText := "$ echo 'stdout' && echo 'stderr' >&2"
	result := executeCommand("echo 'stdout' && echo 'stderr' >&2", originalText)

	if !strings.Contains(result, "stdout") {
		t.Errorf("Expected result to contain 'stdout', got %q", result)
	}

	if !strings.Contains(result, "stderr") {
		t.Errorf("Expected result to contain 'stderr', got %q", result)
	}

	if !strings.Contains(result, "--- stderr ---") {
		t.Errorf("Expected result to contain '--- stderr ---', got %q", result)
	}
}

func TestExecuteCommand_CommandError(t *testing.T) {
	originalText := "$ false"
	result := executeCommand("false", originalText)

	if !strings.Contains(result, "error") {
		t.Errorf("Expected result to contain 'error', got %q", result)
	}

	if strings.Contains(result, "success") {
		t.Errorf("Expected result to not contain 'success' for failed command, got %q", result)
	}
}

func TestExecuteCommand_NonexistentCommand(t *testing.T) {
	originalText := "$ nonexistent-command-xyz123"
	result := executeCommand("nonexistent-command-xyz123", originalText)

	// Should have a non-zero exit code (127 = not found)
	if strings.Contains(result, "success") {
		t.Errorf("Expected non-zero exit code for nonexistent command, got %q", result)
	}

	if !strings.Contains(result, "not found") {
		t.Errorf("Expected result to contain 'not found' for nonexistent command, got %q", result)
	}
}

func TestExecuteCommand_ExecutionTime(t *testing.T) {
	originalText := "$ sleep 0.1"
	result := executeCommand("sleep 0.1", originalText)

	if !strings.Contains(result, "ms") {
		t.Errorf("Expected result to contain execution time with 'ms', got %q", result)
	}

	// Should contain the time in the format "_success X.XXms_" or "_error X.XXms_"
	if !strings.Contains(result, "_") {
		t.Errorf("Expected result to contain '_' for italic formatting, got %q", result)
	}
}
