module github.com/scylladb/scylla-bench

go 1.12

require (
	github.com/HdrHistogram/hdrhistogram-go v1.1.2
	github.com/gocql/gocql v0.0.0-20190301043612-f6df8288f9b4
	github.com/hailocab/go-hostpool v0.0.0-20160125115350-e80d13ce29ed
	github.com/pkg/errors v0.8.1
)

replace github.com/gocql/gocql => github.com/scylladb/gocql v1.4.0
