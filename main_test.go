package main

import (
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

		result := executeCommand(command)

		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(result))
	})(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	body := w.Body.String()
	if body == "" {
		t.Error("Expected non-empty response body")
	}

	if !strings.Contains(body, "hello") {
		t.Errorf("Expected response to contain 'hello', got %q", body)
	}

	if !strings.Contains(body, "Process completed") {
		t.Errorf("Expected response to contain 'Process completed', got %q", body)
	}

	if !strings.Contains(body, "Exit code") {
		t.Errorf("Expected response to contain 'Exit code', got %q", body)
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

				w.Header().Set("Content-Type", "text/plain")
				w.WriteHeader(http.StatusOK)
			})(w, req)

			if executedCommand != tt.expected {
				t.Errorf("Expected command %q, got %q", tt.expected, executedCommand)
			}
		})
	}
}

func TestExecuteCommand_SimpleCommand(t *testing.T) {
	result := executeCommand("echo 'test output'")

	if !strings.Contains(result, "test output") {
		t.Errorf("Expected result to contain 'test output', got %q", result)
	}

	if !strings.Contains(result, "```") {
		t.Errorf("Expected result to contain code block markers, got %q", result)
	}

	if !strings.Contains(result, "Process completed") {
		t.Errorf("Expected result to contain 'Process completed', got %q", result)
	}

	if !strings.Contains(result, "Exit code: 0") {
		t.Errorf("Expected result to contain 'Exit code: 0', got %q", result)
	}
}

func TestExecuteCommand_CommandWithStderr(t *testing.T) {
	result := executeCommand("echo 'stdout' && echo 'stderr' >&2")

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
	result := executeCommand("false")

	if !strings.Contains(result, "Exit code: 1") {
		t.Errorf("Expected result to contain 'Exit code: 1', got %q", result)
	}

	if !strings.Contains(result, "Process completed") {
		t.Errorf("Expected result to contain 'Process completed', got %q", result)
	}
}

func TestExecuteCommand_NonexistentCommand(t *testing.T) {
	result := executeCommand("nonexistent-command-xyz123")

	if !strings.Contains(result, "Process completed") {
		t.Errorf("Expected result to contain 'Process completed', got %q", result)
	}

	// Should have a non-zero exit code
	if strings.Contains(result, "Exit code: 0") {
		t.Errorf("Expected non-zero exit code for nonexistent command, got %q", result)
	}
}

func TestExecuteCommand_ExecutionTime(t *testing.T) {
	result := executeCommand("sleep 0.1")

	if !strings.Contains(result, "Execution time") {
		t.Errorf("Expected result to contain 'Execution time', got %q", result)
	}
}
