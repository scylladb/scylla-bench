// Package usermode provides functionality for parsing YAML profiles
// that describe schema used for benchmarking.
package usermode // import "github.com/scylladb/scylla-bench/usermode"

// TODO(rjeczalik): Add support for other distribution types and the inverted
// ones. Consider also adding "seq()" distribution, which is not documented
// in the link above. See relevant issue:
//
//   https://issues.apache.org/jira/browse/CASSANDRA-12490
//

// TODO(rjeczalik): Add support for units in ditribution strings. Right now
// the "uniform(1..1M)" string is not supported, it needs to be specified
// without a unit instead - "uniform(1..1000000)".

func nonil(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

func min(i, j int64) int64 {
	if i < j {
		return i
	}
	return j
}

func max(i, j int64) int64 {
	if i > j {
		return i
	}
	return j
}

func flattenValues(vals ...[]interface{}) []interface{} {
	n := 1
	for _, val := range vals {
		n += len(val)
	}
	all := make([]interface{}, 0, n)
	for _, val := range vals {
		all = append(all, val...)
	}
	return all
}

func flattenStrings(strings ...[]string) []string {
	n := 1
	for _, s := range strings {
		n += len(s)
	}
	all := make([]string, 0, n)
	for _, s := range strings {
		all = append(all, s...)
	}
	return all
}
