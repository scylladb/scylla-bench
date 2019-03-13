module github.com/scylladb/scylla-bench

go 1.12

require (
	github.com/bitly/go-hostpool v0.0.0-20171023180738-a3a6125de932 // indirect
	github.com/bmizerany/assert v0.0.0-20160611221934-b7ed37b82869 // indirect
	github.com/codahale/hdrhistogram v0.0.0-20161010025455-3a0bb77429bd
	github.com/gocql/gocql v0.0.0-20190301043612-f6df8288f9b4
	github.com/hailocab/go-hostpool v0.0.0-20160125115350-e80d13ce29ed
	github.com/kr/pretty v0.1.0 // indirect
	github.com/pkg/errors v0.8.1
	github.com/stretchr/testify v1.3.0 // indirect
)

replace github.com/gocql/gocql => github.com/scylladb/gocql v1.0.1
