package workloads

import (
	"fmt"
	"math/rand"
	"testing"
	"time"
)

func TestSequentialWorkload(t *testing.T) {
	generator := rand.New(rand.NewSource(int64(time.Now().Nanosecond())))
	testCases := []struct {
		partitionOffset    int64
		partitionCount     int64
		clusteringRowCount int64
	}{
		{10, 20, 30},
		{0, 1, 1},
		{generator.Int63n(100), generator.Int63n(100) + 100, generator.Int63n(99) + 1},
		{generator.Int63n(100), generator.Int63n(100), generator.Int63n(100)},
		{generator.Int63n(100), generator.Int63n(100) + 100, 1},
		{0, generator.Int63n(100) + 100, generator.Int63n(99) + 1},
		{0, generator.Int63n(100) + 100, generator.Int63n(99) + 1},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("rand%d", i), func(t *testing.T) {
			wrkld := NewSequentialVisitAll(tc.partitionOffset, tc.partitionCount, tc.clusteringRowCount)

			expectedPk := tc.partitionOffset
			expectedCk := int64(0)

			for {
				if wrkld.IsDone() && expectedPk < tc.partitionOffset+tc.partitionCount {
					t.Errorf("got end of stream; expected %d", expectedPk)
				}
				if !wrkld.IsDone() && expectedPk == tc.partitionOffset+tc.partitionCount {
					t.Errorf("expected end of stream at %d", expectedPk)
				}

				if wrkld.IsDone() {
					t.Log("got end of stream")
					break
				}

				pk := wrkld.NextPartitionKey()
				if pk != expectedPk {
					t.Errorf("wrong PK; got %d; expected %d", pk, expectedPk)
				} else {
					t.Logf("got PK %d", pk)
				}

				ck := wrkld.NextClusteringKey()
				if ck != expectedCk {
					t.Errorf("wrong CK; got %d; expected %d", pk, expectedCk)
				} else {
					t.Logf("got CK %d", ck)
				}

				expectedCk++
				if expectedCk == tc.clusteringRowCount {
					if !wrkld.IsPartitionDone() {
						t.Errorf("expected end of partition at %d", expectedCk)
					} else {
						t.Log("got end of partition")
					}
					expectedCk = 0
					expectedPk++
				} else if wrkld.IsPartitionDone() {
					t.Errorf("got end of partition; expected %d", expectedCk)
				}
			}
		})
	}
}

func TestUniformWorkload(t *testing.T) {
	generator := rand.New(rand.NewSource(int64(time.Now().Nanosecond())))
	testCases := []struct {
		partitionCount     int64
		partitionOffset    int64
		clusteringRowCount int64
	}{
		{20, 0, 30},
		{20, 20, 30},
		{20, 40, 30},
		{1, 0, 1},
		{generator.Int63n(100) + 100, 0, generator.Int63n(99) + 1},
		{generator.Int63n(100), 0, generator.Int63n(100)},
		{generator.Int63n(100) + 100, 0, 1},
		{generator.Int63n(100) + 100, 0, generator.Int63n(99) + 1},
		{generator.Int63n(100) + 100, 0, generator.Int63n(99) + 1},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("rand%d", i), func(t *testing.T) {
			wrkld := NewRandomUniform(i, tc.partitionCount, tc.partitionOffset, tc.clusteringRowCount)

			pkMin := tc.partitionOffset
			pkMax := tc.partitionCount + tc.partitionOffset
			for i := 0; i < 1000; i++ {
				if wrkld.IsDone() {
					t.Error("got end of stream")
				}

				pk := wrkld.NextPartitionKey()
				if pk < pkMin || pk >= pkMax {
					t.Errorf("PK %d out of range: [0-%d)", pk, tc.partitionCount)
				}

				ck := wrkld.NextClusteringKey()
				if ck < 0 || ck >= tc.clusteringRowCount {
					t.Errorf("CK %d out of range: [0-%d)", pk, tc.clusteringRowCount)
				}

				if wrkld.IsPartitionDone() {
					t.Error("got end of partition")
				}
			}
		})
	}
}
