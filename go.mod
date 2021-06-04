module github.com/scylladb/scylla-bench

go 1.12

require (
	github.com/codahale/hdrhistogram v0.0.0-20161010025455-3a0bb77429bd
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/gocql/gocql v0.0.0-20190301043612-f6df8288f9b4
	github.com/google/go-cmp v0.5.4 // indirect
	github.com/hailocab/go-hostpool v0.0.0-20160125115350-e80d13ce29ed
	github.com/kr/text v0.2.0 // indirect
	github.com/niemeyer/pretty v0.0.0-20200227124842-a10e7caefd8e // indirect
	github.com/pkg/errors v0.9.1
	github.com/stretchr/testify v1.7.0 // indirect
	golang.org/x/xerrors v0.0.0-20200804184101-5ec99f83aff1 // indirect
	gopkg.in/check.v1 v1.0.0-20200227125254-8fa46927fb4f // indirect
)

replace github.com/gocql/gocql => github.com/scylladb/gocql v1.4.0
