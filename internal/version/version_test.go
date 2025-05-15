package version

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestGetVersionInfo(t *testing.T) {
	info := GetVersionInfo()
	if info.ScyllaBench.Version == "" {
		t.Error("ScyllaBench version should not be empty")
	}
	if info.Driver.Version == "" {
		t.Error("Driver version should not be empty")
	}
}

func TestVersionInfoFormatHuman(t *testing.T) {
	info := VersionInfo{
		ScyllaBench: ComponentInfo{
			Version:    "v1.0.0",
			CommitSHA:  "abcdef123456",
			CommitDate: "2023-01-01T00:00:00Z",
		},
		Driver: ComponentInfo{
			Version:    "v2.0.0",
			CommitSHA:  "987654abcdef",
			CommitDate: "2023-02-02T00:00:00Z",
		},
	}

	humanOutput := info.FormatHuman()
	expectedSubstrings := []string{
		"scylla-bench:",
		"version: v1.0.0",
		"commit sha: abcdef123456",
		"commit date: 2023-01-01T00:00:00Z",
		"scylla-gocql-driver:",
		"version: v2.0.0",
		"commit sha: 987654abcdef",
		"commit date: 2023-02-02T00:00:00Z",
	}
	for _, substr := range expectedSubstrings {
		if !strings.Contains(humanOutput, substr) {
			t.Errorf("Expected human output to contain '%s'. Actual output: %s", substr, humanOutput)
		}
	}
}

func TestVersionInfoFormatJSON(t *testing.T) {
	info := VersionInfo{
		ScyllaBench: ComponentInfo{
			Version:    "v1.0.0",
			CommitSHA:  "abcdef123456",
			CommitDate: "2023-01-01T00:00:00Z",
		},
		Driver: ComponentInfo{
			Version:    "v2.0.0",
			CommitSHA:  "987654abcdef",
			CommitDate: "2023-02-02T00:00:00Z",
		},
	}

	jsonOutput, err := info.FormatJSON()
	if err != nil {
		t.Fatalf("FormatJSON() returned error: %v", err)
	}

	var parsed VersionInfo
	if err = json.Unmarshal([]byte(jsonOutput), &parsed); err != nil {
		t.Fatalf("Failed to parse JSON output: %v", err)
	}

	if parsed.ScyllaBench.Version != "v1.0.0" {
		t.Errorf("Expected ScyllaBench version '%s', got '%s'", info.ScyllaBench.Version, parsed.ScyllaBench.Version)
	}
	if parsed.Driver.CommitSHA != "987654abcdef" {
		t.Errorf("Expected Driver commit SHA '%s', got '%s'", info.Driver.CommitSHA, parsed.Driver.CommitSHA)
	}
}

func TestExtractCommitSHA(t *testing.T) {
	testCases := []struct {
		input    string
		expected string
	}{
		{"1234567", "1234567"},
		{"v0.0.0-20230101120000-abcdef123456", "abcdef123456"},
		{"v1.2.3", ""},
		{"", ""},
	}

	for _, tc := range testCases {
		result := extractCommitSHA(tc.input)
		if result != tc.expected {
			t.Errorf("extractCommitSHA(%q) = %q, expected %q", tc.input, result, tc.expected)
		}
	}
}

func TestGithubClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != userAgent {
			t.Errorf("Expected User-Agent header %q, got %q", userAgent, r.Header.Get("User-Agent"))
		}

		// Mock responses based on the path
		switch r.URL.Path {
		case "/repos/testowner/testrepo/releases":
			_, _ = w.Write([]byte(`[
				{
					"tag_name": "v1.0.0",
					"created_at": "2023-01-01T00:00:00Z"
				}
			]`))
		case "/repos/testowner/testrepo/git/refs/tags/v1.0.0":
			_, _ = w.Write([]byte(`{
				"object": {
					"sha": "abcdef123456"
				}
			}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := &githubClient{
		client:    &http.Client{},
		baseURL:   server.URL,
		userAgent: userAgent,
	}

	var (
		sha string
		err error
	)
	date, sha, err = client.getReleaseInfo("testowner", "testrepo", "1.0.0")
	if err != nil {
		t.Fatalf("getReleaseInfo() returned error: %v", err)
	}
	if date != "2023-01-01T00:00:00Z" {
		t.Errorf("Expected date '2023-01-01T00:00:00Z', got '%s'", date)
	}
	if sha != "abcdef123456" {
		t.Errorf("Expected SHA 'abcdef123456', got '%s'", sha)
	}
}

func TestExtractRepoOwner(t *testing.T) {
	testCases := []struct {
		repoPath      string
		defaultOwner  string
		expectedOwner string
	}{
		{"github.com/scylladb/gocql", "default", "scylladb"},
		{"example.com/owner/repo", "default", "owner"},
		{"invalid", "default", "default"},
		{"", "default", "default"},
	}

	for _, tc := range testCases {
		result := extractRepoOwner(tc.repoPath, tc.defaultOwner)
		if result != tc.expectedOwner {
			t.Errorf("extractRepoOwner(%q, %q) = %q, expected %q",
				tc.repoPath, tc.defaultOwner, result, tc.expectedOwner)
		}
	}
}

func TestGetDriverVersionWithEnvVar(t *testing.T) {
	originalRepo := os.Getenv("GOCQL_REPO")
	originalVersion := os.Getenv("GOCQL_VERSION")
	defer func() {
		t.Setenv("GOCQL_REPO", originalRepo)
		t.Setenv("GOCQL_VERSION", originalVersion)
	}()

	t.Setenv("GOCQL_REPO", "github.com/testowner/gocql")
	t.Setenv("GOCQL_VERSION", "1234567890abcdef")

	info := getDriverVersionInfo()
	if info.Version == "" {
		t.Error("Driver version should not be empty")
	}
}
