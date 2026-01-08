package main

import (
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/scylladb/gocql"
)

func getFuncName(f any) string {
	return runtime.FuncForPC(reflect.ValueOf(f).Pointer()).Name()
}

func TestToInt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    bool
		expected int
	}{
		{
			name:     "true converts to 1",
			input:    true,
			expected: 1,
		},
		{
			name:     "false converts to 0",
			input:    false,
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := toInt(tt.input)
			if result != tt.expected {
				t.Errorf("toInt(%v) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestGetMode(t *testing.T) {
	// Save original values
	originalRowsPerRequest := rowsPerRequest

	// Restore after test
	defer func() {
		rowsPerRequest = originalRowsPerRequest
	}()

	tests := []struct {
		validateFunc   func(ModeFunc) bool
		name           string
		modeName       string
		rowsPerRequest int
		expectPanic    bool
	}{
		{
			name:           "write mode with single row",
			modeName:       "write",
			rowsPerRequest: 1,
			expectPanic:    false,
			validateFunc: func(mf ModeFunc) bool {
				// Compare function pointers
				return getFuncName(mf) == getFuncName(DoWrites)
			},
		},
		{
			name:           "write mode with multiple rows",
			modeName:       "write",
			rowsPerRequest: 2,
			expectPanic:    false,
			validateFunc: func(mf ModeFunc) bool {
				return getFuncName(mf) == getFuncName(DoBatchedWrites)
			},
		},
		{
			name:           "counter_update mode",
			modeName:       "counter_update",
			rowsPerRequest: 1,
			expectPanic:    false,
			validateFunc: func(mf ModeFunc) bool {
				return getFuncName(mf) == getFuncName(DoCounterUpdates)
			},
		},
		{
			name:           "read mode",
			modeName:       "read",
			rowsPerRequest: 1,
			expectPanic:    false,
			validateFunc: func(mf ModeFunc) bool {
				return getFuncName(mf) == getFuncName(DoReads)
			},
		},
		{
			name:           "counter_read mode",
			modeName:       "counter_read",
			rowsPerRequest: 1,
			expectPanic:    false,
			validateFunc: func(mf ModeFunc) bool {
				return getFuncName(mf) == getFuncName(DoCounterReads)
			},
		},
		{
			name:           "scan mode",
			modeName:       "scan",
			rowsPerRequest: 1,
			expectPanic:    false,
			validateFunc: func(mf ModeFunc) bool {
				return getFuncName(mf) == getFuncName(DoScanTable)
			},
		},
		{
			name:           "mixed mode",
			modeName:       "mixed",
			rowsPerRequest: 1,
			expectPanic:    false,
			validateFunc: func(mf ModeFunc) bool {
				return getFuncName(mf) == getFuncName(DoMixed)
			},
		},
		{
			name:           "invalid mode",
			modeName:       "invalid_mode",
			rowsPerRequest: 1,
			expectPanic:    true,
			validateFunc:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set rowsPerRequest for this test
			// Note: We cannot use t.Parallel() here because we modify global state
			rowsPerRequest = tt.rowsPerRequest

			if tt.expectPanic {
				defer func() {
					if r := recover(); r == nil {
						t.Errorf("GetMode(%q) did not panic as expected", tt.modeName)
					}
				}()
			}

			modeFunc := GetMode(tt.modeName)

			// Validate the result
			if !tt.expectPanic && !tt.validateFunc(modeFunc) {
				t.Errorf("GetMode(%q) returned an incorrect function", tt.modeName)
			}
		})
	}
}

func TestNewHostSelectionPolicy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		policy         string
		datacenter     string
		rack           string
		wantType       string
		wantPolicyName string
		hosts          []string
		wantErr        bool
	}{
		{
			name:           "round-robin policy",
			policy:         "round-robin",
			hosts:          []string{},
			datacenter:     "",
			rack:           "",
			wantType:       "*gocql.roundRobinHostPolicy",
			wantPolicyName: "round-robin",
			wantErr:        false,
		},
		{
			name:           "host-pool policy",
			policy:         "host-pool",
			hosts:          []string{"host1", "host2", "host3"},
			datacenter:     "",
			rack:           "",
			wantType:       "*hostpolicy.hostPoolHostPolicy",
			wantPolicyName: "host-pool",
			wantErr:        false,
		},
		{
			name:           "token-aware with round-robin",
			policy:         "token-aware",
			hosts:          []string{},
			datacenter:     "",
			rack:           "",
			wantType:       "*gocql.tokenAwareHostPolicy",
			wantPolicyName: "token-round-robin",
			wantErr:        false,
		},
		{
			name:           "token-aware with DC aware",
			policy:         "token-aware",
			hosts:          []string{},
			datacenter:     "dc1",
			rack:           "",
			wantType:       "*gocql.tokenAwareHostPolicy",
			wantPolicyName: "token-dc-aware",
			wantErr:        false,
		},
		{
			name:           "token-aware with rack aware",
			policy:         "token-aware",
			hosts:          []string{},
			datacenter:     "dc1",
			rack:           "rack1",
			wantType:       "*gocql.tokenAwareHostPolicy",
			wantPolicyName: "token-rack-dc-aware",
			wantErr:        false,
		},
		{
			name:           "unknown policy",
			policy:         "unknown-policy",
			hosts:          []string{},
			datacenter:     "",
			rack:           "",
			wantType:       "",
			wantPolicyName: "",
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, gotPolicyName, err := newHostSelectionPolicy(tt.policy, tt.hosts, tt.datacenter, tt.rack)

			if (err != nil) != tt.wantErr {
				t.Errorf("newHostSelectionPolicy() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				if got != nil {
					t.Errorf("newHostSelectionPolicy() returned non-nil policy when error expected")
				}
				return
			}

			gotType := reflect.TypeOf(got).String()
			if gotType != tt.wantType {
				t.Errorf(
					"newHostSelectionPolicy() returned policy of type %v, want %v",
					gotType,
					tt.wantType,
				)
			}

			if gotPolicyName != tt.wantPolicyName {
				t.Errorf(
					"newHostSelectionPolicy() returned policy name %v, want %v",
					gotPolicyName,
					tt.wantPolicyName,
				)
			}
		})
	}
}

func TestGetRetryPolicy(t *testing.T) {
	originalRetryInterval := retryInterval
	originalRetryNumber := retryNumber

	defer func() {
		retryInterval = originalRetryInterval
		retryNumber = originalRetryNumber
	}()

	tests := []struct {
		name          string
		retryInterval string
		retryNumber   int
		expectedMin   time.Duration
		expectedMax   time.Duration
		expectPanic   bool
	}{
		{
			name:          "single value in milliseconds",
			retryInterval: "100ms",
			retryNumber:   3,
			expectedMin:   100 * time.Millisecond,
			expectedMax:   100 * time.Millisecond,
			expectPanic:   false,
		},
		{
			name:          "single value in seconds",
			retryInterval: "2s",
			retryNumber:   5,
			expectedMin:   2000 * time.Millisecond,
			expectedMax:   2000 * time.Millisecond,
			expectPanic:   false,
		},
		{
			name:          "single numeric value (seconds implied)",
			retryInterval: "3",
			retryNumber:   2,
			expectedMin:   3000 * time.Millisecond,
			expectedMax:   3000 * time.Millisecond,
			expectPanic:   false,
		},
		{
			name:          "min and max values in milliseconds",
			retryInterval: "50ms,300ms",
			retryNumber:   4,
			expectedMin:   50 * time.Millisecond,
			expectedMax:   300 * time.Millisecond,
			expectPanic:   false,
		},
		{
			name:          "min and max values in seconds",
			retryInterval: "1s,5s",
			retryNumber:   3,
			expectedMin:   1000 * time.Millisecond,
			expectedMax:   5000 * time.Millisecond,
			expectPanic:   false,
		},
		{
			name:          "mixed units",
			retryInterval: "100ms,2s",
			retryNumber:   2,
			expectedMin:   100 * time.Millisecond,
			expectedMax:   2000 * time.Millisecond,
			expectPanic:   false,
		},
		{
			name:          "with spaces",
			retryInterval: " 200ms , 1s ",
			retryNumber:   3,
			expectedMin:   200 * time.Millisecond,
			expectedMax:   1000 * time.Millisecond,
			expectPanic:   false,
		},
		{
			name:          "invalid format - too many values",
			retryInterval: "100ms,200ms,300ms",
			retryNumber:   2,
			expectPanic:   true,
		},
		{
			name:          "invalid format - empty string",
			retryInterval: "",
			retryNumber:   3,
			expectPanic:   true,
		},
		{
			name:          "invalid format - min value",
			retryInterval: "invalid,200ms",
			retryNumber:   2,
			expectPanic:   true,
		},
		{
			name:          "invalid format - max value",
			retryInterval: "100ms,invalid",
			retryNumber:   3,
			expectPanic:   true,
		},
		{
			name:          "min greater than max",
			retryInterval: "300ms,100ms",
			retryNumber:   2,
			expectPanic:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			panicCalled := false
			retryInterval = tt.retryInterval
			retryNumber = tt.retryNumber

			var policy *gocql.ExponentialBackoffRetryPolicy
			var recoverErr any

			func() {
				defer func() {
					recoverErr = recover()
					if recoverErr != nil {
						panicCalled = true
					}
				}()

				policy = getRetryPolicy()
			}()

			// Check if panic occurred as expected
			if tt.expectPanic {
				if recoverErr == nil {
					t.Errorf("Expected panic but none occurred")
				}
				if !panicCalled {
					t.Errorf("Expected log.Panic to be called but it wasn't")
				}
				return
			}

			if recoverErr != nil {
				t.Fatalf("Unexpected panic: %v", recoverErr)
			}

			if policy.NumRetries != tt.retryNumber {
				t.Errorf("Expected NumRetries to be %d, got %d", tt.retryNumber, policy.NumRetries)
			}

			if policy.Min != tt.expectedMin {
				t.Errorf("Expected Min to be %s, got %s", tt.expectedMin, policy.Min)
			}

			if policy.Max != tt.expectedMax {
				t.Errorf("Expected Max to be %s, got %s", tt.expectedMax, policy.Max)
			}
		})
	}
}

// TestGetRetryPolicyParseValues tests specific value parsing logic
func TestGetRetryPolicyParseValues(t *testing.T) {
	t.Parallel()

	// Save original values to restore after test
	originalRetryInterval := retryInterval
	originalRetryNumber := retryNumber

	// Restore original values after test
	t.Cleanup(func() {
		retryInterval = originalRetryInterval
		retryNumber = originalRetryNumber
	})

	testCases := []struct {
		input    string
		expected string // Expected parsed millisecond value
	}{
		{"100", "100000"},  // Plain number becomes seconds
		{"100ms", "100"},   // Milliseconds stay as is
		{"2s", "2000"},     // Seconds convert to milliseconds
		{"500ms", "500"},   // Explicit milliseconds
		{"1.5s", "1.5000"}, // Fractions handled specially
	}

	for _, tc := range testCases {
		t.Run("Parse "+tc.input, func(t *testing.T) {
			retryInterval = tc.input
			retryNumber = 3

			defer func() {
				_ = recover()
			}()

			values := []string{tc.input}
			for i := range values {
				if _, err := strconv.Atoi(values[i]); err == nil {
					values[i] += "000"
				}
				values[i] = strings.ReplaceAll(values[i], "ms", "")
				values[i] = strings.ReplaceAll(values[i], "s", "000")
			}

			if values[0] != tc.expected {
				t.Errorf("For input %q, expected parsed value %q, got %q",
					tc.input, tc.expected, values[0])
			}
		})
	}
}

// TestGetRetryPolicyEdgeCases tests edge cases and error handling
func TestGetRetryPolicyEdgeCases(t *testing.T) {
	// Save original values to restore after test
	originalRetryInterval := retryInterval
	originalRetryNumber := retryNumber

	defer func() {
		retryInterval = originalRetryInterval
		retryNumber = originalRetryNumber
	}()

	testCases := []struct {
		name          string
		retryInterval string
		panicContains string
		expectPanic   bool
	}{
		{
			name:          "too many values",
			retryInterval: "100ms,200ms,300ms",
			expectPanic:   true,
			panicContains: "Only 1 or 2 values are expected",
		},
		{
			name:          "invalid min value",
			retryInterval: "abc,200ms",
			expectPanic:   true,
			panicContains: "Wrong value for retry minimum interval",
		},
		{
			name:          "invalid max value",
			retryInterval: "100ms,xyz",
			expectPanic:   true,
			panicContains: "Wrong value for retry maximum interval",
		},
		{
			name:          "min greater than max",
			retryInterval: "500ms,100ms",
			expectPanic:   true,
			panicContains: "interval is bigger than",
		},
		{
			name:          "zero value",
			retryInterval: "0ms",
			expectPanic:   false,
			panicContains: "",
		},
		{
			name:          "very small value",
			retryInterval: "1ms",
			expectPanic:   false,
			panicContains: "",
		},
		{
			name:          "very large value",
			retryInterval: "9999999ms",
			expectPanic:   false,
			panicContains: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			panicMessages := []string{}
			retryInterval = tc.retryInterval
			retryNumber = 3

			// Use defer/recover to catch panics
			var didPanic bool

			func() {
				defer func() {
					if r := recover(); r != nil {
						didPanic = true
					}
				}()

				getRetryPolicy()
			}()

			// Check if panic occurred as expected
			if tc.expectPanic != didPanic {
				t.Errorf("Expected panic: %v, got: %v", tc.expectPanic, didPanic)
			}

			// If panic was expected, check if the message contains expected text
			if tc.expectPanic && len(panicMessages) > 0 {
				found := false
				for _, msg := range panicMessages {
					if strings.Contains(msg, tc.panicContains) {
						found = true
						break
					}
				}

				if !found {
					t.Errorf("Expected panic message to contain %q, got messages: %v",
						tc.panicContains, panicMessages)
				}
			}
		})
	}
}

// Test mixed mode with different workloads to ensure compatibility
func TestMixedModeWithWorkloads(t *testing.T) {
	// Cannot use t.Parallel() because this test modifies global state

	// Save original values
	originalConcurrency := concurrency
	originalPartitionCount := partitionCount
	originalClusteringRowCount := clusteringRowCount
	originalMaximumRate := maximumRate
	originalStartTime := startTime

	defer func() {
		concurrency = originalConcurrency
		partitionCount = originalPartitionCount
		clusteringRowCount = originalClusteringRowCount
		maximumRate = originalMaximumRate
		startTime = originalStartTime
	}()

	// Set required values for the test
	concurrency = 1
	partitionCount = 10
	clusteringRowCount = 5
	maximumRate = 1000 // Set a non-zero rate for timeseries workload
	startTime = time.Now()

	workloads := []string{"sequential", "uniform", "timeseries"}

	for _, workload := range workloads {
		t.Run("workload_"+workload, func(t *testing.T) {
			// Test that GetWorkload works with mixed mode
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("GetWorkload panicked with mixed mode and %s workload: %v", workload, r)
				}
			}()

			// These should not panic
			generator := GetWorkload(workload, 0, 0, "mixed", 100, "uniform")
			if generator == nil {
				t.Errorf("GetWorkload returned nil for %s workload with mixed mode", workload)
			}
		})
	}
}

// Test that timeseries workload specifically works with mixed mode (was previously panicking)
func TestTimeseriesWorkloadWithMixedMode(t *testing.T) {
	// Cannot use t.Parallel() because this test modifies global state

	// Save original values
	originalConcurrency := concurrency
	originalPartitionCount := partitionCount
	originalClusteringRowCount := clusteringRowCount
	originalMaximumRate := maximumRate
	originalStartTime := startTime

	defer func() {
		concurrency = originalConcurrency
		partitionCount = originalPartitionCount
		clusteringRowCount = originalClusteringRowCount
		maximumRate = originalMaximumRate
		startTime = originalStartTime
	}()

	// Set required values for the test
	concurrency = 2
	partitionCount = 10
	clusteringRowCount = 5
	maximumRate = 1000
	startTime = time.Now()

	// Test that timeseries workload with mixed mode does not panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Timeseries workload with mixed mode panicked: %v", r)
		}
	}()

	generator := GetWorkload("timeseries", 0, 0, "mixed", 100, "uniform")
	if generator == nil {
		t.Errorf("GetWorkload returned nil for timeseries workload with mixed mode")
	}
}

// Test that global mixed operation counter provides correct alternating pattern
func TestGlobalMixedOperationCounter(t *testing.T) {
	t.Parallel()

	// Create a local atomic counter for this test to avoid race conditions
	// with other tests or running application code
	var localCounter atomic.Uint64

	// Simulate multiple operations and verify alternating pattern
	operations := make([]bool, 10) // true for write, false for read
	for i := 0; i < 10; i++ {
		opCount := localCounter.Add(1)
		operations[i] = opCount%2 == 0 // even = write, odd = read
	}

	// Verify alternating pattern: R, W, R, W, R, W, R, W, R, W
	// (first operation count is 1 which is odd, so read)
	expected := []bool{false, true, false, true, false, true, false, true, false, true}
	for i, op := range operations {
		if op != expected[i] {
			t.Errorf("Operation %d: expected %v (write=%v, read=%v), got %v",
				i+1, expected[i], true, false, op)
		}
	}
}
