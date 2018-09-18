package usermode_test

import "github.com/scylladb/scylla-bench/usermode"

var ExampleYaml = []byte(`columnspec:
- name: host
  population: uniform(1..600)
  size: fixed(32)
- name: bucket_time
  population: uniform(1..288)
  size: fixed(18)
- name: service
  population: uniform(1000..2000)
  size: uniform(10..100)
- cluster: fixed(15)
  name: time
- name: state
  size: fixed(4)
insert:
  batchtype: UNLOGGED
  partitions: fixed(1)
  select: fixed(10)/10
keyspace: example
keyspace_definition: cql
table: events
table_definition: cql
`)

var Example = &usermode.Profile{
	ColumnSpec: []usermode.ColumnSpec{{
		Name: "host",
		Population: &usermode.Uniform{
			Min: 1,
			Max: 600,
		},
		Size: &usermode.Fixed{
			Value: 32,
		},
	}, {
		Name: "bucket_time",
		Population: &usermode.Uniform{
			Min: 1,
			Max: 288,
		},
		Size: &usermode.Fixed{
			Value: 18,
		},
	}, {
		Name: "service",
		Population: &usermode.Uniform{
			Min: 1000,
			Max: 2000,
		},
		Size: &usermode.Uniform{
			Min: 10,
			Max: 100,
		},
	}, {
		Name: "time",
		Cluster: &usermode.Fixed{
			Value: 15,
		},
		Population: &usermode.Uniform{
			Min: 1,
			Max: 1000000000,
		},
		Size: &usermode.Uniform{
			Min: 4,
			Max: 8,
		},
	}, {
		Name: "state",
		Size: &usermode.Fixed{
			Value: 4,
		},
		Population: &usermode.Uniform{
			Min: 1,
			Max: 1000000000,
		},
	}},
	Insert: &usermode.Insert{
		BatchType: "UNLOGGED",
		Partitions: &usermode.Fixed{
			Value: 1,
		},
		Visits: &usermode.Fixed{
			Value: 1,
		},
		Select: &usermode.Ratio{
			Distribution: &usermode.Fixed{
				Value: 10,
			},
			Value: 10,
		},
	},
	Keyspace:           "example",
	KeyspaceDefinition: "cql",
	Table:              "events",
	TableDefinition:    "cql",
}
