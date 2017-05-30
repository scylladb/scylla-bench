package main

import (
	"math/rand"
	"time"
)

type WorkloadGenerator interface {
	NextPartitionKey() int64
	NextClusteringKey() int64
	IsPartitionDone() bool
	IsDone() bool
}

type SequentialVisitAll struct {
	PartitionCount     int64
	ClusteringRowCount int64
	NextPartition      int64
	NextClusteringRow  int64
}

func NewSequentialVisitAll(partitionOffset int64, partitionCount int64, clusteringRowCount int64) *SequentialVisitAll {
	return &SequentialVisitAll{partitionOffset + partitionCount, clusteringRowCount, partitionOffset, 0}
}

func (sva *SequentialVisitAll) NextPartitionKey() int64 {
	if sva.NextClusteringRow < sva.ClusteringRowCount {
		return sva.NextPartition
	}
	sva.NextClusteringRow = 0
	sva.NextPartition++
	pk := sva.NextPartition
	return pk
}

func (sva *SequentialVisitAll) NextClusteringKey() int64 {
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
	PartitionCount     int64
	ClusteringRowCount int64
}

func NewRandomUniform(i int, partitionCount int64, clusteringRowCount int64) *RandomUniform {
	generator := rand.New(rand.NewSource(int64(time.Now().Nanosecond() * (i + 1))))
	return &RandomUniform{generator, int64(partitionCount), int64(clusteringRowCount)}
}

func (ru *RandomUniform) NextPartitionKey() int64 {
	return ru.Generator.Int63n(ru.PartitionCount)
}

func (ru *RandomUniform) NextClusteringKey() int64 {
	return ru.Generator.Int63n(ru.ClusteringRowCount)
}

func (ru *RandomUniform) IsDone() bool {
	return false
}

func (ru *RandomUniform) IsPartitionDone() bool {
	return false
}
