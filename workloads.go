package main

import (
	"math/rand"
	"time"
)

type WorkloadGenerator interface {
	NextPartitionKey() int
	NextClusteringKey() int
	IsPartitionDone() bool
	IsDone() bool
}

type SequentialVisitAll struct {
	PartitionCount     int
	ClusteringRowCount int
	NextPartition      int
	NextClusteringRow  int
}

func NewSequentialVisitAll(partitionOffset int, partitionCount int, clusteringRowCount int) *SequentialVisitAll {
	return &SequentialVisitAll{partitionOffset + partitionCount, clusteringRowCount, partitionOffset, 0}
}

func (sva *SequentialVisitAll) NextPartitionKey() int {
	if sva.NextClusteringRow < sva.ClusteringRowCount {
		return sva.NextPartition
	}
	sva.NextClusteringRow = 0
	sva.NextPartition++
	pk := sva.NextPartition
	return pk
}

func (sva *SequentialVisitAll) NextClusteringKey() int {
	ck := sva.NextClusteringRow
	sva.NextClusteringRow++
	return ck
}

func (sva *SequentialVisitAll) IsDone() bool {
	return sva.NextPartition >= sva.PartitionCount || (sva.NextPartition+1 == sva.PartitionCount && sva.NextClusteringRow >= sva.ClusteringRowCount)
}

func (sva *SequentialVisitAll) IsPartitionDone() bool {
	return sva.NextClusteringRow == sva.ClusteringRowCount
}

type RandomUniform struct {
	Generator          *rand.Rand
	PartitionCount     int
	ClusteringRowCount int
}

func NewRandomUniform(i int, partitionCount int, clusteringRowCount int) *RandomUniform {
	generator := rand.New(rand.NewSource(int64(time.Now().Nanosecond() * (i + 1))))
	return &RandomUniform{generator, partitionCount, clusteringRowCount}
}

func (ru *RandomUniform) NextPartitionKey() int {
	return ru.Generator.Intn(ru.PartitionCount)
}

func (ru *RandomUniform) NextClusteringKey() int {
	return ru.Generator.Intn(ru.ClusteringRowCount)
}

func (ru *RandomUniform) IsDone() bool {
	return false
}

func (ru *RandomUniform) IsPartitionDone() bool {
	return false
}
