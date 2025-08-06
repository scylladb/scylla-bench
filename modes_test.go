package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"strings"
	"testing"
)

func Must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func GenerateTestData(pk, ck, size int64) []byte {
	return Must(GenerateData(pk, ck, size, true))
}

func TestBuildReadQuery(t *testing.T) {
	t.Parallel()

	keyspaceName = "test"
	tableName = "test_table"
	// Define test cases
	testCases := []struct {
		name           string
		table          string
		orderBy        string
		setupVars      func()
		expectedResult string
	}{
		{
			name:    "normal table with default settings",
			table:   "test_table",
			orderBy: "",
			setupVars: func() {
				inRestriction = false
				provideUpperBound = false
				noLowerBound = false
				bypassCache = false
			},
			expectedResult: "SELECT pk, ck, v FROM",
		},
		{
			name:    "counter table with ORDER BY",
			table:   "counter_table",
			orderBy: "ORDER BY ck ASC",
			setupVars: func() {
				inRestriction = false
				provideUpperBound = false
				noLowerBound = false
				bypassCache = false
			},
			expectedResult: "SELECT pk, ck, c1, c2, c3, c4, c5 FROM",
		},
		{
			name:           "with inRestriction",
			table:          "test_table",
			orderBy:        "",
			setupVars:      func() { inRestriction = true; provideUpperBound = false; noLowerBound = false; bypassCache = false },
			expectedResult: "SELECT pk, ck, v FROM",
		},
		{
			name:    "with inRestriction and rows per request",
			table:   "test_table",
			orderBy: "",
			setupVars: func() {
				inRestriction = true
				provideUpperBound = false
				noLowerBound = false
				bypassCache = false

				rowsPerRequest = 3
			},
			expectedResult: "SELECT pk, ck, v FROM",
		},
		{
			name:           "with provideUpperBound",
			table:          "test_table",
			orderBy:        "",
			setupVars:      func() { inRestriction = false; provideUpperBound = true; noLowerBound = false; bypassCache = false },
			expectedResult: "SELECT pk, ck, v FROM",
		},
		{
			name:           "with noLowerBound",
			table:          "test_table",
			orderBy:        "",
			setupVars:      func() { inRestriction = false; provideUpperBound = false; noLowerBound = true; bypassCache = false },
			expectedResult: "SELECT pk, ck, v FROM",
		},
		{
			name:           "with bypassCache",
			table:          "test_table",
			orderBy:        "",
			setupVars:      func() { inRestriction = false; provideUpperBound = false; noLowerBound = false; bypassCache = true },
			expectedResult: "SELECT pk, ck, v FROM",
		},
	}

	// Run test cases
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Setup variables
			tc.setupVars()

			// Call the function
			query := BuildReadQueryString(tc.table, tc.orderBy)

			// Check result
			if !strings.HasPrefix(query, tc.expectedResult) {
				t.Errorf("Expected query to contain %q, got %q", tc.expectedResult, query)
			}

			// More specific assertions based on the query type
			if tc.setupVars == nil {
				return
			}

			switch {
			case inRestriction:
				if !strings.Contains(query, "IN (") {
					t.Errorf("Expected IN clause in query for inRestriction=true, got: %s", query)
				}
			case provideUpperBound:
				if !strings.Contains(query, "ck < ?") {
					t.Errorf(
						"Expected upper bound in query for provideUpperBound=true, got: %s",
						query,
					)
				}
			case noLowerBound:
				if strings.Contains(query, "ck >=") {
					t.Errorf(
						"Expected no lower bound in query for noLowerBound=true, got: %s",
						query,
					)
				}
			}

			// Check for BYPASS CACHE
			if bypassCache && !strings.Contains(query, "BYPASS CACHE") {
				t.Errorf("Expected BYPASS CACHE in query for bypassCache=true, got: %s", query)
			}

			// Check for ORDER BY
			if tc.orderBy != "" && !strings.Contains(query, tc.orderBy) {
				t.Errorf("Expected ORDER BY clause %q in query, got: %s", tc.orderBy, query)
			}
		})
	}
}

func TestGenerateData(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		pk        int64
		ck        int64
		size      int64
		validate  bool
		wantError bool
	}{
		{
			name:      "Small size without validation",
			pk:        1,
			ck:        2,
			size:      10,
			validate:  false,
			wantError: false,
		},
		{
			name:      "Small size with validation",
			pk:        1,
			ck:        2,
			size:      10,
			validate:  true,
			wantError: false,
		},
		{
			name:      "Large size with validation",
			pk:        1000,
			ck:        2000,
			size:      100,
			validate:  true,
			wantError: false,
		},
		{
			name:      "Large size without validation",
			pk:        1000,
			ck:        2000,
			size:      100,
			validate:  false,
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := GenerateData(tt.pk, tt.ck, tt.size, tt.validate)

			if (err != nil) != tt.wantError {
				t.Errorf("GenerateData() error = %v, wantError %v", err, tt.wantError)
				return
			}

			if len(got) != int(tt.size) {
				t.Errorf("GenerateData() returned wrong size = %v, want %v", len(got), tt.size)
			}

			if !tt.validate {
				// When validation is disabled, just check if the size matches
				return
			}

			// Validate the generated data structure
			buf := bytes.NewBuffer(got)

			if tt.size < generatedDataHeaderSize {
				// Check small size format
				var storedSize int8
				if err = binary.Read(buf, binary.LittleEndian, &storedSize); err != nil {
					t.Errorf("Failed to read size: %v", err)
				}
				if int64(storedSize) != tt.size {
					t.Errorf("Stored size %v doesn't match expected size %v", storedSize, tt.size)
				}

				var storedXor int64
				if err = binary.Read(buf, binary.LittleEndian, &storedXor); err != nil {
					t.Errorf("Failed to read xor value: %v", err)
				}
				if storedXor != (tt.pk ^ tt.ck) {
					t.Errorf("Stored xor %v doesn't match expected %v", storedXor, tt.pk^tt.ck)
				}
			} else {
				// Check normal size format
				var storedSize int64
				if err = binary.Read(buf, binary.LittleEndian, &storedSize); err != nil {
					t.Errorf("Failed to read size: %v", err)
				}
				if storedSize != tt.size {
					t.Errorf("Stored size %v doesn't match expected size %v", storedSize, tt.size)
				}

				var storedPk, storedCk int64
				if err = binary.Read(buf, binary.LittleEndian, &storedPk); err != nil {
					t.Errorf("Failed to read pk: %v", err)
				}
				if err = binary.Read(buf, binary.LittleEndian, &storedCk); err != nil {
					t.Errorf("Failed to read ck: %v", err)
				}

				if storedPk != tt.pk || storedCk != tt.ck {
					t.Errorf("Stored pk/ck (%v/%v) don't match expected (%v/%v)",
						storedPk, storedCk, tt.pk, tt.ck)
				}

				if tt.size >= generatedDataMinSize {
					// For large sizes, verify payload and checksum
					payloadSize := tt.size - generatedDataHeaderSize - sha256.Size
					payload := make([]byte, payloadSize)
					if err = binary.Read(buf, binary.LittleEndian, payload); err != nil {
						t.Errorf("Failed to read payload: %v", err)
					}

					var storedChecksum [sha256.Size]byte
					if err = binary.Read(buf, binary.LittleEndian, &storedChecksum); err != nil {
						t.Errorf("Failed to read checksum: %v", err)
					}

					calculatedChecksum := sha256.Sum256(payload)
					if !bytes.Equal(calculatedChecksum[:], storedChecksum[:]) {
						t.Error("Checksum verification failed")
					}
				}
			}
		})
	}
}

func TestValidateData(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		errContains string
		data        []byte
		pk          int64
		ck          int64
		validate    bool
		wantErr     bool
	}{
		{
			name:     "validation disabled",
			pk:       1,
			ck:       2,
			data:     []byte{1, 2, 3},
			validate: false,
			wantErr:  false,
		},
		{
			name:        "small size with corrupted data",
			pk:          1,
			ck:          2,
			data:        []byte{5, 0, 0, 0, 0, 0, 0, 0, 0}, // incorrect size
			validate:    true,
			wantErr:     true,
			errContains: "actual size of value",
		},
		{
			name:     "valid large data",
			pk:       100,
			ck:       100,
			data:     GenerateTestData(100, 100, 200),
			validate: true,
			wantErr:  false,
		},
		{
			name:     "valid small data",
			pk:       1,
			ck:       2,
			data:     GenerateTestData(1, 2, 10),
			validate: true,
			wantErr:  false,
		},
		{
			name:        "small data with mismatched bytes",
			pk:          10,
			ck:          20,
			data:        createSmallTestDataWithMismatch(10, 10, 20),
			validate:    true,
			wantErr:     true,
			errContains: "actual value doesn't match expected value",
		},
		{
			name:        "corrupted checksum",
			pk:          1,
			ck:          2,
			data:        createCorruptedChecksumData(100, 1, 2),
			validate:    true,
			wantErr:     true,
			errContains: "corrupt checksum",
		},

		{
			name:        "invalid stored pk",
			pk:          1,
			ck:          2,
			data:        GenerateTestData(2, 2, 100), // wrong pk
			validate:    true,
			wantErr:     true,
			errContains: "actual pk",
		},
		{
			name:        "invalid stored ck",
			pk:          1,
			ck:          2,
			data:        GenerateTestData(1, 3, 100), // wrong ck
			validate:    true,
			wantErr:     true,
			errContains: "actual ck",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateData(tt.pk, tt.ck, tt.data, tt.validate)

			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateData() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && tt.errContains != "" && err != nil {
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("ValidateData() error = %v, should contain %v", err, tt.errContains)
				}
			}
		})
	}
}

// Helper function to create data with corrupted checksum
func createCorruptedChecksumData(size, pk, ck int64) []byte {
	buf := bytes.Buffer{}
	buf.Grow(int(size))

	_ = binary.Write(&buf, binary.LittleEndian, size)
	_ = binary.Write(&buf, binary.LittleEndian, pk)
	_ = binary.Write(&buf, binary.LittleEndian, ck)

	payload := make([]byte, size-generatedDataHeaderSize-sha256.Size)
	for i := range payload {
		payload[i] = byte(i)
	}

	// Write payload
	_ = binary.Write(&buf, binary.LittleEndian, payload)

	// Write incorrect checksum
	incorrectChecksum := sha256.Sum256([]byte("wrong data"))
	_ = binary.Write(&buf, binary.LittleEndian, incorrectChecksum)

	return buf.Bytes()
}

// Helper function to create test data for small sizes with intentional mismatch
func createSmallTestDataWithMismatch(size, pk, ck int64) []byte {
	buf := bytes.Buffer{}
	buf.Grow(int(size))

	// Write correct size
	_ = binary.Write(&buf, binary.LittleEndian, int8(size))

	// Write incorrect XOR value (adding 1 to make it different)
	_ = binary.Write(&buf, binary.LittleEndian, (pk^ck)+1)

	// Add some extra bytes to match the size
	for buf.Len() < int(size) {
		buf.WriteByte(0xFF) // Using 0xFF to make it obviously different
	}

	return buf.Bytes()
}
