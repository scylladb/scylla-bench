package results

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

// TestOutputAlignment verifies that time values with and without fractional seconds
// are properly aligned in the output.
func TestOutputAlignment(t *testing.T) {
	// Don't use t.Parallel() because we're modifying os.Stdout

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Print sample rows with different time formats
	// 12m41.2s has fractional seconds (8 chars)
	// 12m42s has no fractional seconds (6 chars)
	// Both should align properly with %8v format
	fmt.Printf(withLatencyLineFmt, Round(12*time.Minute+41*time.Second+200*time.Millisecond), 407603, 407603, 0, "60ms", "28ms", "12ms", "2.7ms", "1.8ms", "852µs", "1.3ms", "")
	fmt.Printf(withLatencyLineFmt, Round(12*time.Minute+42*time.Second), 401019, 401019, 0, "48ms", "29ms", "9.1ms", "2.3ms", "1.7ms", "852µs", "1.2ms", "")
	fmt.Printf(withLatencyLineFmt, Round(12*time.Minute+42*time.Second+200*time.Millisecond), 398582, 398582, 0, "49ms", "27ms", "11ms", "2.8ms", "2ms", "950µs", "1.3ms", "")

	// Restore stdout and capture output
	w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("Failed to capture output: %v", err)
	}
	output := buf.String()

	lines := strings.Split(output, "\n")
	// Filter out empty lines
	var nonEmptyLines []string
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			nonEmptyLines = append(nonEmptyLines, line)
		}
	}

	if len(nonEmptyLines) < 3 {
		t.Fatalf("Expected at least 3 non-empty lines, got %d", len(nonEmptyLines))
	}

	// Check that the "ops/s" column values align across all lines
	// by verifying they start at the same position
	opsPositions := make([]int, len(nonEmptyLines))
	for i, line := range nonEmptyLines {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			t.Fatalf("Line %d has fewer than 2 fields: %q", i, line)
		}
		// Find where the second field (ops/s) starts in the original line
		opsPositions[i] = strings.Index(line, fields[1])
	}

	// All ops/s values should start at the same column position
	firstPos := opsPositions[0]
	for i, pos := range opsPositions[1:] {
		if pos != firstPos {
			t.Errorf("Line %d: ops/s column at position %d, expected %d\nOutput:\n%s", i+1, pos, firstPos, output)
		}
	}
}

// TestOutputAlignmentWithoutLatency verifies alignment for output without latency columns
func TestOutputAlignmentWithoutLatency(t *testing.T) {
	// Don't use t.Parallel() because we're modifying os.Stdout

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Print sample rows with different time formats
	fmt.Printf(withoutLatencyLineFmt, Round(12*time.Minute+41*time.Second+200*time.Millisecond), 407603, 407603, 0)
	fmt.Printf(withoutLatencyLineFmt, Round(12*time.Minute+42*time.Second), 401019, 401019, 0)
	fmt.Printf(withoutLatencyLineFmt, Round(12*time.Minute+42*time.Second+600*time.Millisecond), 402153, 402153, 0)

	// Restore stdout and capture output
	w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("Failed to capture output: %v", err)
	}
	output := buf.String()

	lines := strings.Split(output, "\n")
	// Filter out empty lines
	var nonEmptyLines []string
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			nonEmptyLines = append(nonEmptyLines, line)
		}
	}

	if len(nonEmptyLines) < 3 {
		t.Fatalf("Expected at least 3 non-empty lines, got %d", len(nonEmptyLines))
	}

	// Check that the "ops/s" column values align across all lines
	opsPositions := make([]int, len(nonEmptyLines))
	for i, line := range nonEmptyLines {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			t.Fatalf("Line %d has fewer than 2 fields: %q", i, line)
		}
		opsPositions[i] = strings.Index(line, fields[1])
	}

	// All ops/s values should start at the same column position
	firstPos := opsPositions[0]
	for i, pos := range opsPositions[1:] {
		if pos != firstPos {
			t.Errorf("Line %d: ops/s column at position %d, expected %d\nOutput:\n%s", i+1, pos, firstPos, output)
		}
	}
}
