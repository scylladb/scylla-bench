package usermode

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/gocql/gocql"
)

// Driver is responsible for executing profile-driven benchmark in user mode.
//
// TODO(rjeczalik): Add support for more column types (currently only "int" and
// "text" are supported).
type Driver struct {
	Profile *Profile
	Session *gocql.Session

	table *gocql.TableMetadata
	stmt  string   // insert statement
	pkeys []string // partition keys
	ckeys []string // clustering keys
	keys  []string // the rest
}

// CreateKeyspace creates a workspace using user-provided CQL.
// If the keyspace already exists, it is used as-is.
func (r *Driver) CreateKeyspace() error {
	switch err := r.Session.Query(r.Profile.KeyspaceDefinition).Exec().(type) {
	case *gocql.RequestErrAlreadyExists:
		return nil // ignore
	default:
		return err
	}
}

// CreateTable creates a table using user-provided CQL.
// If the table already exists, it is truncated.
func (r *Driver) CreateTable() error {
	stmt := fixTableStmt(r.Profile.TableDefinition, r.Profile.Keyspace, r.Profile.Table)
	switch err := r.Session.Query(stmt).Exec().(type) {
	case *gocql.RequestErrAlreadyExists:
		return nonil(r.truncateTable(), r.readMeta())
	case nil:
		return r.readMeta()
	default:
		return err
	}
}

// Summary returns a human-readable string representing parsed profile.
func (d *Driver) Summary() string {
	var buf bytes.Buffer
	fmt.Fprintln(&buf, "--------------")
	fmt.Fprintln(&buf, "Keyspace Name:", d.Profile.Keyspace)
	fmt.Fprintln(&buf, "Table Name:", d.Profile.Table)
	fmt.Fprintln(&buf, "Generator Configs:")
	for _, name := range flattenStrings(d.pkeys, d.ckeys, d.keys) {
		col := d.colSpec(name)
		fmt.Fprintf(&buf, "  %s:\n", col.Name)
		fmt.Fprintln(&buf, "    Size:", col.Size)
		fmt.Fprintln(&buf, "    Population:", col.Population)
		if d.table.Columns[col.Name].Kind == gocql.ColumnClusteringKey {
			fmt.Fprintln(&buf, "    Clustering:", col.Cluster)
		}
	}
	fmt.Fprintln(&buf, "Insert Settings:")
	fmt.Fprintln(&buf, "  Partitions:", d.Profile.Insert.Partitions)
	fmt.Fprintln(&buf, "  Batch Type:", d.Profile.Insert.BatchType)
	fmt.Fprintln(&buf, "  Select:", d.Profile.Insert.Select)
	return buf.String()
}

// BatchInsert creates a single batch of insert statements, which are generated
// basing on insert settings and column specifications. More resources on the
// generation are available under https://git.io/fAVOa and https://goo.gl/obxGxK.
//
// TODO(rjeczalik): Currently scylla-bench inserts more rows than cassandra-stress
// does due to unhandled insert.visits distributions.
func (r *Driver) BatchInsert(ctx context.Context) error {
	tr := ContextTrace(ctx)
	batch := r.Session.NewBatch(r.batchType())
	var cols []*ColumnSpec
	for _, name := range r.ckeys {
		cols = append(cols, r.colSpec(name))
	}
	partitions := r.Profile.Insert.Partitions.Generate()
	var info ExecutedBatchInfo
	for i := int64(0); i < partitions; i++ {
		pk := r.generateMany(r.pkeys...)
		// Generate values for all cluster keys in the amount given
		// by the column specification.
		var ckeys [][]interface{}
		for _, col := range cols {
			n := int(Product(col.Cluster, r.Profile.Insert.Select)) // apply ratio
			ckeys = append(ckeys, r.generate(col.Name, n))
		}
		// Having n sets of all available cluster keys for this iteration,
		// combine their permutations into a set of ck tuples, one for
		// each query (row).
		info.Size += forEachPermutation(ckeys, func(ck []interface{}) {
			row := flattenValues(pk, ck, r.generateMany(r.keys...))
			batch.Query(r.stmt, row...)
		})
	}
	start := time.Now()
	err := r.Session.ExecuteBatch(batch)
	info.Latency = time.Since(start)
	info.Err = err
	tr.ExecutedBatch(info)
	return err
}

func (d *Driver) generateMany(names ...string) []interface{} {
	var v []interface{}
	for _, name := range names {
		v = append(v, d.generate(name, 1)[0])
	}
	return v
}

func (d *Driver) generate(name string, n int) []interface{} {
	col, ok := d.table.Columns[name]
	if !ok {
		return nil
	}
	spec := d.colSpec(col.Name)
	vals := make([]interface{}, n)
	for i := range vals {
		v := col.Type.New()
		if !Generate(v, spec.Population, spec.Size) {
			return nil
		}
		vals[i] = reflect.ValueOf(v).Elem().Interface()
	}
	return vals
}

func (r *Driver) colSpec(name string) *ColumnSpec {
	for _, col := range r.Profile.ColumnSpec {
		if col.Name == name {
			return &col
		}
	}
	return &ColumnSpec{
		Name:       name,
		Cluster:    DefaultColumnSpec.Cluster,
		Population: DefaultColumnSpec.Population,
		Size:       DefaultColumnSpec.Size,
	}
}

func (r *Driver) batchType() gocql.BatchType {
	switch strings.ToLower(r.Profile.Insert.BatchType) {
	case "logged":
		return gocql.LoggedBatch
	case "counter":
		return gocql.CounterBatch
	default:
		return gocql.UnloggedBatch
	}
}

func (r *Driver) readMeta() error {
	keyspace, err := r.Session.KeyspaceMetadata(r.Profile.Keyspace)
	if err != nil {
		return err
	}
	var ok bool
	if r.table, ok = keyspace.Tables[r.Profile.Table]; !ok {
		return fmt.Errorf("no metadata found for table %q", r.Profile.Table)
	}
	uniq := make(map[string]struct{})
	for _, col := range r.table.PartitionKey {
		r.pkeys = append(r.pkeys, col.Name)
		uniq[col.Name] = struct{}{}
	}
	for _, col := range r.table.ClusteringColumns {
		r.ckeys = append(r.ckeys, col.Name)
		uniq[col.Name] = struct{}{}
	}
	for _, col := range r.table.Columns {
		switch typ := col.Type.Type(); typ {
		case gocql.TypeInt, gocql.TypeText:
			if _, ok := uniq[col.Name]; !ok {
				r.keys = append(r.keys, col.Name)
			}
		default:
			return fmt.Errorf("unsupported %q type for %q column", typ, col.Name)
		}
	}
	sort.Strings(r.keys) // deterministic batches

	r.stmt = makeInsertStmt(r.fullname(), flattenStrings(r.pkeys, r.ckeys, r.keys)...)

	return nil
}

func (r *Driver) truncateTable() error {
	return r.Session.Query("TRUNCATE TABLE " + r.fullname()).Exec()
}

func (r *Driver) fullname() string {
	return r.Profile.Keyspace + "." + r.Profile.Table
}

// NOTE(rjeczalik): Currently gocql.Query supports neither keyspace configuration
// nor USE statements, thus table_definition must specify full table name.
// For more details see: https://git.io/fABYd
func fixTableStmt(stmt, keyspace, table string) string {
	return strings.NewReplacer(
		"CREATE TABLE "+table, "CREATE TABLE "+keyspace+"."+table,
		"create table "+table, "create table "+keyspace+"."+table,
		"CREATE TABLE IF NOT EXISTS "+table, "CREATE TABLE IF NOT EXISTS "+keyspace+"."+table,
		"create table if not exists "+table, "create table if not exists "+keyspace+"."+table,
	).Replace(stmt)
}

func makeInsertStmt(table string, cols ...string) string {
	var buf bytes.Buffer
	buf.WriteString("INSERT INTO " + table + " (")
	buf.WriteString(cols[0])
	for _, col := range cols[1:] {
		buf.WriteString("," + col)
	}
	buf.WriteString(") VALUES (?")
	for range cols[1:] {
		buf.WriteString(",?")
	}
	buf.WriteString(")")
	return buf.String()
}

func forEachPermutation(sets [][]interface{}, fn func(perm []interface{})) int {
	indices, n := make([]int, len(sets)), 0
iterate:
	for {
		perm := make([]interface{}, len(indices))
		for i, index := range indices {
			perm[i] = sets[i][index]
		}
		fn(perm)
		n++
		for i := len(indices) - 1; i >= 0; i-- {
			indices[i] = (indices[i] + 1) % len(sets[i])
			if indices[i] != 0 {
				break
			}
			if i == 0 {
				break iterate
			}
		}
	}
	return n
}
