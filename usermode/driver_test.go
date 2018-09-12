package usermode_test

import (
	"reflect"
	"testing"

	"github.com/scylladb/scylla-bench/usermode"
)

func TestMakeInsertStmt(t *testing.T) {
	cases := map[string]struct {
		table string
		cols  []string
		want  string
	}{
		"single col": {
			"table", []string{"foo"},
			"INSERT INTO table (foo) VALUES (?)",
		},
		"multiple cols": {
			"keyspace.table", []string{"foo", "bar", "baz"},
			"INSERT INTO keyspace.table (foo,bar,baz) VALUES (?,?,?)",
		},
	}

	for name, cas := range cases {
		t.Run(name, func(t *testing.T) {
			got := usermode.MakeInsertStmt(cas.table, cas.cols...)
			if got != cas.want {
				t.Errorf("got %q, want %q", got, cas.want)
			}
		})
	}
}

func TestFixTableStmt(t *testing.T) {
	cases := map[string]struct {
		stmt     string
		keyspace string
		table    string
		want     string
	}{
		"syntax #1": {
			"CREATE TABLE table (id int, PRIMARY KEY(id))",
			"keyspace", "table",
			"CREATE TABLE keyspace.table (id int, PRIMARY KEY(id))",
		},
		"syntax #2": {
			"CREATE TABLE IF NOT EXISTS table (id int, PRIMARY KEY(id))",
			"keyspace", "table",
			"CREATE TABLE IF NOT EXISTS keyspace.table (id int, PRIMARY KEY(id))",
		},
		"syntax #3": {
			"CREATE TABLE table (id int, PRIMARY KEY(id))",
			"keyspace", "table",
			"CREATE TABLE keyspace.table (id int, PRIMARY KEY(id))",
		},
		"syntax #4": {
			"create table if not exists table (id int, PRIMARY KEY(id))",
			"keyspace", "table",
			"create table if not exists keyspace.table (id int, PRIMARY KEY(id))",
		},
		"no fix": {
			"CREATE TABLE othertable (id int, PRIMARY KEY(id))",
			"keyspace", "table",
			"CREATE TABLE othertable (id int, PRIMARY KEY(id))",
		},
	}

	for name, cas := range cases {
		t.Run(name, func(t *testing.T) {
			got := usermode.FixTableStmt(cas.stmt, cas.keyspace, cas.table)
			if got != cas.want {
				t.Errorf("got %q, want %q", got, cas.want)
			}
		})
	}
}

func TestForEachPermutation(t *testing.T) {
	cases := map[string]struct {
		sets [][]interface{}
		want [][]interface{}
	}{
		"2-2": {
			[][]interface{}{{1, 2}, {3, 4}},
			[][]interface{}{{1, 3}, {1, 4}, {2, 3}, {2, 4}},
		},
		"3-3-3": {
			[][]interface{}{{1, 2, 3}, {4, 5, 6}, {7, 8, 9}},
			[][]interface{}{{1, 4, 7}, {1, 4, 8}, {1, 4, 9}, {1, 5, 7}, {1, 5, 8},
				{1, 5, 9}, {1, 6, 7}, {1, 6, 8}, {1, 6, 9}, {2, 4, 7}, {2, 4, 8},
				{2, 4, 9}, {2, 5, 7}, {2, 5, 8}, {2, 5, 9}, {2, 6, 7}, {2, 6, 8},
				{2, 6, 9}, {3, 4, 7}, {3, 4, 8}, {3, 4, 9}, {3, 5, 7}, {3, 5, 8},
				{3, 5, 9}, {3, 6, 7}, {3, 6, 8}, {3, 6, 9}},
		},
		"1-2-3": {
			[][]interface{}{{1}, {2, 3}, {4, 5, 6}},
			[][]interface{}{{1, 2, 4}, {1, 2, 5}, {1, 2, 6}, {1, 3, 4}, {1, 3, 5}, {1, 3, 6}},
		},
		"empty set": {
			[][]interface{}{{1, 2}, {}, {4, 5}},
			nil,
		},
	}
	for name, cas := range cases {
		t.Run(name, func(t *testing.T) {
			var got [][]interface{}

			usermode.ForEachPermutation(cas.sets, func(v []interface{}) {
				got = append(got, v)
			})

			if !reflect.DeepEqual(got, cas.want) {
				t.Errorf("got %# v, want %# v", got, cas.want)
			}
		})
	}
}
