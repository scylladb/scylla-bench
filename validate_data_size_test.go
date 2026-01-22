package main

import (
	"testing"
)

// TestValidateDataWithSmallSizes tests the reported issue where data validation
// fails for clustering-row-size values less than 57.
// This test specifically targets the bug reported in the issue where:
// - Writing with -clustering-row-size 16 succeeds
// - Reading with -clustering-row-size 16 -validate-data fails with corruption errors
func TestValidateDataWithSmallSizes(t *testing.T) {
	t.Parallel()

	// Test cases covering different size ranges based on the constants:
	// - generatedDataHeaderSize = 24
	// - generatedDataMinSize = 24 + 33 = 57
	testCases := []struct {
		name string
		size int64
		pk   int64
		ck   int64
	}{
		// Sizes less than header size (< 24) - uses compact format (int8 size + pk^ck)
		{name: "size 1", size: 1, pk: 1, ck: 1},
		{name: "size 9", size: 9, pk: 100, ck: 200}, // 9 = 1 byte size + 8 bytes pk^ck
		{name: "size 10", size: 10, pk: 1882, ck: 10},
		{name: "size 16 (from issue)", size: 16, pk: 1882, ck: 10}, // Exact case from the issue
		{name: "size 20", size: 20, pk: 500, ck: 700},
		{name: "size 23", size: 23, pk: 999, ck: 123},

		// Sizes >= header but < min size (24 <= size < 57) - uses full header but no checksum
		{name: "size 24 (boundary)", size: 24, pk: 1000, ck: 2000},
		{name: "size 30", size: 30, pk: 1500, ck: 2500},
		{name: "size 40", size: 40, pk: 2000, ck: 3000},
		{name: "size 50", size: 50, pk: 2500, ck: 3500},
		{name: "size 56", size: 56, pk: 3000, ck: 4000}, // Just below min size

		// Sizes >= min size (>= 57) - uses full header with payload and checksum
		{name: "size 57 (boundary)", size: 57, pk: 3500, ck: 4500},
		{name: "size 60", size: 60, pk: 4000, ck: 5000},
		{name: "size 100", size: 100, pk: 5000, ck: 6000},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Generate data with validation enabled
			data, err := GenerateData(tc.pk, tc.ck, tc.size, true)
			if err != nil {
				t.Fatalf("GenerateData failed: %v", err)
			}

			// Verify the generated data has the correct size
			if int64(len(data)) != tc.size {
				t.Fatalf("Generated data size %d doesn't match expected size %d", len(data), tc.size)
			}

			// Validate the data - this should NOT fail
			err = ValidateData(tc.pk, tc.ck, data, true)
			if err != nil {
				t.Errorf("ValidateData failed for size %d: %v\nThis reproduces the issue where sizes < 57 fail validation",
					tc.size, err)
			}
		})
	}
}

// TestValidateDataRoundTripAllSizes performs a comprehensive round-trip test
// for all sizes from 1 to 100, ensuring generate/validate cycle works correctly
func TestValidateDataRoundTripAllSizes(t *testing.T) {
	t.Parallel()

	// Test a range of partition and clustering keys
	testKeys := []struct {
		pk int64
		ck int64
	}{
		{pk: 0, ck: 0},
		{pk: 1, ck: 1},
		{pk: 100, ck: 200},
		{pk: 1882, ck: 10}, // From the original issue
		{pk: 10000, ck: 50000},
		{pk: -1, ck: -1}, // Negative values
	}

	for _, keys := range testKeys {
		keys := keys // Capture loop variable
		t.Run("", func(t *testing.T) {
			t.Parallel()

			// Test sizes from 1 to 100
			for size := int64(1); size <= 100; size++ {
				// Generate data
				data, err := GenerateData(keys.pk, keys.ck, size, true)
				if err != nil {
					t.Fatalf("GenerateData failed for pk=%d, ck=%d, size=%d: %v",
						keys.pk, keys.ck, size, err)
				}

				// Validate data
				err = ValidateData(keys.pk, keys.ck, data, true)
				if err != nil {
					t.Errorf("Round-trip validation failed for pk=%d, ck=%d, size=%d: %v",
						keys.pk, keys.ck, size, err)
				}
			}
		})
	}
}

// TestValidateDataDetectsCorruption ensures that ValidateData properly detects
// corrupted data for all size ranges
func TestValidateDataDetectsCorruption(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		corruptFunc func([]byte) []byte
		name        string
		expectError string
		size        int64
		pk          int64
		ck          int64
	}{
		{
			name: "corrupt small data - flip bit in xor value",
			size: 16,
			pk:   100,
			ck:   200,
			corruptFunc: func(data []byte) []byte {
				// Flip a bit in the xor value (byte position 1-8)
				if len(data) > 5 {
					data[5] ^= 0xFF
				}
				return data
			},
			expectError: "actual value doesn't match expected value",
		},
		{
			name: "corrupt medium data - flip bit in zeros",
			size: 40,
			pk:   500,
			ck:   600,
			corruptFunc: func(data []byte) []byte {
				// Flip a bit in the zero-filled area
				if len(data) > 30 {
					data[30] = 0xFF
				}
				return data
			},
			expectError: "actual value doesn't match expected value",
		},
		{
			name: "corrupt large data - wrong pk",
			size: 100,
			pk:   1000,
			ck:   2000,
			corruptFunc: func(data []byte) []byte {
				// This will be caught by pk validation
				// We can't easily corrupt just pk without regenerating,
				// so we'll use a different approach in this test
				return data
			},
			expectError: "", // This test case will use different data
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Generate valid data
			data, err := GenerateData(tc.pk, tc.ck, tc.size, true)
			if err != nil {
				t.Fatalf("GenerateData failed: %v", err)
			}

			// Corrupt the data if corruption function is provided
			if tc.corruptFunc != nil {
				data = tc.corruptFunc(data)
			}

			// For the large data case, test with wrong pk/ck
			if tc.size >= generatedDataMinSize && tc.expectError == "" {
				// Generate data with different pk
				wrongPkData, _ := GenerateData(tc.pk+1, tc.ck, tc.size, true)
				err = ValidateData(tc.pk, tc.ck, wrongPkData, true)
				if err == nil {
					t.Error("ValidateData should detect wrong pk")
				}
				return
			}

			// Validate corrupted data - should fail
			err = ValidateData(tc.pk, tc.ck, data, true)
			if err == nil && tc.expectError != "" {
				t.Error("ValidateData should detect corruption but didn't")
			}
		})
	}
}
