package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

const (
	baseURL  = "http://localhost:8081"
	deviceID = "918002499033:78@s.whatsapp.net"
	testChatID = "917275860207@s.whatsapp.net"
)

type TestResult struct {
	Endpoint    string    `json:"endpoint"`
	Method      string    `json:"method"`
	StatusCode  int       `json:"status_code"`
	Success     bool      `json:"success"`
	Error       string    `json:"error,omitempty"`
	TimeStamp   time.Time `json:"timestamp"`
	ResponseTime float64   `json:"response_time_ms"`
}

type TestReport struct {
	TotalTests     int          `json:"total_tests"`
	PassedTests    int          `json:"passed_tests"`
	FailedTests    int          `json:"failed_tests"`
	TestResults    []TestResult `json:"test_results"`
	ExecutionTime  float64      `json:"total_execution_time_ms"`
	TimeStamp      time.Time    `json:"timestamp"`
}

func TestAPIEndpoints(t *testing.T) {
	report := TestReport{
		TimeStamp: time.Now(),
		TestResults: make([]TestResult, 0),
	}
	
	startTime := time.Now()

	// Test cases
	testCases := []struct {
		name     string
		method   string
		endpoint string
		body     interface{}
		expectedStatus int
	}{
		// Device endpoints
		{"Get Devices", "GET", "/devices", nil, 200},
		
		// Contacts endpoints
		{"Get Contacts", "GET", fmt.Sprintf("/contacts?deviceId=%s", deviceID), nil, 200},
		{"Check Contact", "POST", "/contacts", map[string]interface{}{
			"deviceId": deviceID,
			"phones":   []string{testChatID},
		}, 200},
		
		// Messages endpoints
		{"Get All Messages", "GET", fmt.Sprintf("/messages?deviceId=%s", deviceID), nil, 200},
		{"Get Chat Messages", "GET", fmt.Sprintf("/messages?deviceId=%s&chatId=%s", deviceID, testChatID), nil, 200},
		{"Send Text Message", "POST", "/messages/text", map[string]interface{}{
			"deviceId": deviceID,
			"to":      testChatID,
			"message": "Test message from automated test",
		}, 200},
		
		// Chats endpoints
		{"Get All Chats", "GET", fmt.Sprintf("/chats?deviceId=%s", deviceID), nil, 200},
		{"Get Specific Chat", "GET", fmt.Sprintf("/chats/%s?deviceId=%s", strings.TrimSuffix(testChatID, "@s.whatsapp.net"), deviceID), nil, 200},
		
		// Groups endpoints
		{"Get All Groups", "GET", fmt.Sprintf("/groups?deviceId=%s", deviceID), nil, 200},
	}

	// Run tests
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := TestResult{
				Endpoint:  tc.endpoint,
				Method:    tc.method,
				TimeStamp: time.Now(),
			}

			startRequest := time.Now()
			
			var resp *http.Response
			var err error

			switch tc.method {
			case "GET":
				resp, err = http.Get(baseURL + tc.endpoint)
			case "POST":
				jsonBody, _ := json.Marshal(tc.body)
				resp, err = http.Post(baseURL+tc.endpoint, "application/json", bytes.NewBuffer(jsonBody))
			case "PUT":
				jsonBody, _ := json.Marshal(tc.body)
				req, _ := http.NewRequest("PUT", baseURL+tc.endpoint, bytes.NewBuffer(jsonBody))
				req.Header.Set("Content-Type", "application/json")
				resp, err = http.DefaultClient.Do(req)
			case "DELETE":
				req, _ := http.NewRequest("DELETE", baseURL+tc.endpoint, nil)
				resp, err = http.DefaultClient.Do(req)
			}

			result.ResponseTime = float64(time.Since(startRequest).Milliseconds())

			if err != nil {
				result.Success = false
				result.Error = err.Error()
				t.Errorf("Error making request to %s: %v", tc.endpoint, err)
			} else {
				defer resp.Body.Close()
				result.StatusCode = resp.StatusCode
				result.Success = resp.StatusCode == tc.expectedStatus

				if !result.Success {
					body, _ := io.ReadAll(resp.Body)
					result.Error = fmt.Sprintf("Expected status %d, got %d. Response: %s", 
						tc.expectedStatus, resp.StatusCode, string(body))
					t.Errorf("Test %s failed: %s", tc.name, result.Error)
				} else {
					// Log successful response for debugging
					body, _ := io.ReadAll(resp.Body)
					t.Logf("Test %s succeeded. Response: %s", tc.name, string(body))
				}
			}

			report.TestResults = append(report.TestResults, result)
			if result.Success {
				report.PassedTests++
			} else {
				report.FailedTests++
			}
		})
	}

	report.TotalTests = len(testCases)
	report.ExecutionTime = float64(time.Since(startTime).Milliseconds())

	// Generate report file
	reportJSON, _ := json.MarshalIndent(report, "", "    ")
	reportFile := fmt.Sprintf("test_report_%s.json", time.Now().Format("2006-01-02_15-04-05"))
	err := os.WriteFile(reportFile, reportJSON, 0644)
	if err != nil {
		t.Errorf("Error writing report file: %v", err)
	}

	// Print summary
	fmt.Printf("\nAPI Test Summary:\n")
	fmt.Printf("Total Tests: %d\n", report.TotalTests)
	fmt.Printf("Passed: %d\n", report.PassedTests)
	fmt.Printf("Failed: %d\n", report.FailedTests)
	fmt.Printf("Total Time: %.2fms\n", report.ExecutionTime)
	fmt.Printf("Report saved to: %s\n", reportFile)

	if report.FailedTests > 0 {
		t.Errorf("%d tests failed. Check the report for details.", report.FailedTests)
	}
} 