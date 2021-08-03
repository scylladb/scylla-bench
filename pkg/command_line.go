package command_line

import (
	"github.com/scylladb/scylla-bench/random"
	"time"
)

type CommandLineArguments struct {
	HostVerification      bool
	CaCertFile            string
	ClientCertFile        string
	ClientCompression     bool
	ClientKeyFile         string
	ClusteringRowCount    int64
	ClusteringRowSizeDist random.Distribution
	Concurrency           int
	ConnectionCount       int
	ConsistencyLevel      string
	Distribution          string
	// Any error response that comes with delay greater than errorToTimeoutCutoffTime
	// to be considered as timeout error and recorded to histogram as such
	ErrorToTimeoutCutoffTime time.Duration
	HostSelectionPolicy      string
	InRestriction            bool
	Iterations               uint
	KeyspaceName             string
	LatencyType              string
	MaxErrorsAtRow           int
	MaximumRate              int
	MeasureLatency           bool
	Mode                     string
	Nodes                    string
	NoLowerBound             bool
	PageSize                 int
	PartitionCount           int64
	Password                 string
	PartitionOffset          int64
	ProvideUpperBound        bool
	RangeCount               int
	ReplicationFactor        int
	RowsPerRequest           int
	ServerName               string
	TableName                string
	TestDuration             time.Duration
	Timeout                  time.Duration
	TLSEncryption            bool
	Username                 string
	ValidateData             bool
	Workload                 string
	WriteRate                int64
}

