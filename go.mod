module github.com/scylladb/scylla-bench

go 1.17

require (
	github.com/HdrHistogram/hdrhistogram-go v1.1.2
	github.com/gocql/gocql v1.2.1
	github.com/hailocab/go-hostpool v0.0.0-20160125115350-e80d13ce29ed
	github.com/pkg/errors v0.8.1
)

require (
	github.com/golang/snappy v0.0.3 // indirect
	golang.org/x/net v0.17.0 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	sigs.k8s.io/yaml v1.3.0 // indirect
)

replace github.com/gocql/gocql => github.com/scylladb/gocql v1.12.1-0.20240116102025-f614a51d47c7
