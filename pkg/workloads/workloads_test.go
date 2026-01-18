package workloads

import (
	"fmt"
	"math/rand"
	"testing"
	"time"
)

func TestSequentialWorkload(t *testing.T) {
	generator := rand.New(rand.NewSource(int64(time.Now().UTC().Nanosecond())))
	testCases := []struct {
		rowOffset          int64
		rowCount           int64
		clusteringRowCount int64
	}{
		{0, 51, 81},
		{51, 51, 81},
		{102, 51, 81},
		{153, 51, 81},
		{204, 51, 81},
		{255, 50, 81},
		{305, 50, 81},
		{355, 50, 81},
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
			wrkld := NewSequentialVisitAll(tc.rowOffset, tc.rowCount, tc.clusteringRowCount)
			currentPk := tc.rowOffset / tc.clusteringRowCount
			currentCk := tc.rowOffset % tc.clusteringRowCount
			lastPk := (tc.rowOffset + tc.rowCount) / tc.clusteringRowCount
			lastCk := (tc.rowOffset + tc.rowCount) % tc.clusteringRowCount
			rowCounter := int64(0)
			for {
				if wrkld.IsDone() {
					if currentPk != lastPk {
						t.Errorf("wrong last PK; got %d; expected %d", currentPk, lastPk)
					}
					if currentCk != lastCk {
						t.Errorf("wrong last CK; got %d; expected %d", currentCk, lastCk)
					}
					if rowCounter != tc.rowCount {
						t.Errorf(
							"Expected '%d' rows to be processed, but got '%d'",
							tc.rowCount,
							rowCounter,
						)
					}
					break
				}

				pk := wrkld.NextPartitionKey()
				if pk != currentPk {
					t.Errorf("wrong PK; got %d; expected %d", pk, currentPk)
				}

				ck := wrkld.NextClusteringKey()
				if ck != currentCk {
					t.Errorf("wrong CK; got %d; expected %d", pk, currentCk)
				}

				currentCk++
				rowCounter++
				if currentCk == tc.clusteringRowCount {
					if !wrkld.IsPartitionDone() {
						t.Errorf("expected end of partition at %d", currentCk)
					}
					currentCk = 0
					currentPk++
				} else if wrkld.IsPartitionDone() {
					t.Errorf("got end of partition; expected %d", currentCk)
				}
			}
		})
	}
}

func TestUniformWorkload(t *testing.T) {
	generator := rand.New(rand.NewSource(int64(time.Now().UTC().Nanosecond())))
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
			wrkld := NewRandomUniform(
				i,
				tc.partitionCount,
				tc.partitionOffset,
				tc.clusteringRowCount,
			)

			pkMin := tc.partitionOffset
			pkMax := tc.partitionCount + tc.partitionOffset
			for j := 0; j < 1000; j++ {
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
