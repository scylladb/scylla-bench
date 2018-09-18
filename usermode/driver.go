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
	profile *Profile
	session *gocql.Session
	table   *gocql.TableMetadata
	gen     *Generator
	stmt    string   // insert statement
	pkeys   []string // partition keys
	ckeys   []string // clustering keys
	keys    []string // the rest
}

// NewDriver gives new driver value.
func NewDriver(p *Profile, s *gocql.Session) *Driver {
	return &Driver{
		profile: p,
		session: s,
		gen:     NewGenerator(),
	}
}

// CreateKeyspace creates a workspace using user-provided CQL.
// If the keyspace already exists, it is used as-is.
func (d *Driver) CreateKeyspace() error {
	switch err := d.session.Query(d.profile.KeyspaceDefinition).Exec().(type) {
	case *gocql.RequestErrAlreadyExists:
		return nil // ignore
	default:
		return err
	}
}

// CreateTable creates a table using user-provided CQL.
// If the table already exists, it is truncated.
func (d *Driver) CreateTable() error {
	stmt := fixTableStmt(d.profile.TableDefinition, d.profile.Keyspace, d.profile.Table)
	switch err := d.session.Query(stmt).Exec().(type) {
	case *gocql.RequestErrAlreadyExists:
		return nonil(d.truncateTable(), d.readMeta())
	case nil:
		return d.readMeta()
	default:
		return err
	}
}

// Summary returns a human-readable string representing parsed profile.
func (d *Driver) Summary() string {
	var buf bytes.Buffer
	fmt.Fprintln(&buf, "--------------")
	fmt.Fprintln(&buf, "Keyspace Name:", d.profile.Keyspace)
	fmt.Fprintln(&buf, "Table Name:", d.profile.Table)
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
	fmt.Fprintln(&buf, "  Partitions:", d.profile.Insert.Partitions)
	fmt.Fprintln(&buf, "  Batch Type:", d.profile.Insert.BatchType)
	fmt.Fprintln(&buf, "  Select:", d.profile.Insert.Select)
	return buf.String()
}

// BatchInsert creates a single batch of insert statements, which are generated
// basing on insert settings and column specifications. More resources on the
// generation are available under https://git.io/fAVOa and https://goo.gl/obxGxK.
//
// TODO(rjeczalik): Currently scylla-bench inserts more rows than cassandra-stress
// does due to unhandled insert.visits distributions.
func (d *Driver) BatchInsert(ctx context.Context) error {
	tr := ContextTrace(ctx)
	batch := d.session.NewBatch(d.batchType())
	var cols []*ColumnSpec
	for _, name := range d.ckeys {
		cols = append(cols, d.colSpec(name))
	}
	partitions := d.profile.Insert.Partitions.Generate()
	var info ExecutedBatchInfo
	for i := int64(0); i < partitions; i++ {
		pk := d.generateMany(true, d.pkeys...)
		if len(pk) != len(d.pkeys) {
			continue // at least one of the keys overlap, skip
		}
		// Generate values for all cluster keys in the amount given
		// by the column specification.
		var ckeys [][]interface{}
		for _, col := range cols {
			n := int(Product(col.Cluster, d.profile.Insert.Select)) // apply ratio
			ckeys = append(ckeys, d.generate(col.Name, n, true))
		}
		// Having n sets of all available cluster keys for this iteration,
		// combine their permutations into a set of ck tuples, one for
		// each query (row).
		info.Size += forEachPermutation(ckeys, func(ck []interface{}) {
			row := flattenValues(pk, ck, d.generateMany(false, d.keys...))
			batch.Query(d.stmt, row...)
		})
	}
	start := time.Now()
	err := d.session.ExecuteBatch(batch)
	info.Latency = time.Since(start)
	info.Err = err
	tr.ExecutedBatch(info)
	return err
}

func (d *Driver) generateMany(uniq bool, names ...string) []interface{} {
	many := make([]interface{}, 0, len(names))
	for _, name := range names {
		if v := d.generate(name, 1, uniq); v != nil {
			many = append(many, v[0])
		}
	}
	return many
}

func (d *Driver) generate(name string, n int, uniq bool) []interface{} {
	col, ok := d.table.Columns[name]
	if !ok {
		return nil
	}
	spec := d.colSpec(col.Name)
	var vals []interface{}
	for i := 0; i < n; i++ {
		v := col.Type.New()
		if !d.gen.Generate(spec, v, uniq) {
			continue
		}
		vals = append(vals, reflect.ValueOf(v).Elem().Interface())
	}
	return vals
}

func (d *Driver) colSpec(name string) *ColumnSpec {
	for _, col := range d.profile.ColumnSpec {
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

func (d *Driver) batchType() gocql.BatchType {
	switch strings.ToLower(d.profile.Insert.BatchType) {
	case "logged":
		return gocql.LoggedBatch
	case "counter":
		return gocql.CounterBatch
	default:
		return gocql.UnloggedBatch
	}
}

func (d *Driver) readMeta() error {
	keyspace, err := d.session.KeyspaceMetadata(d.profile.Keyspace)
	if err != nil {
		return err
	}
	var ok bool
	if d.table, ok = keyspace.Tables[d.profile.Table]; !ok {
		return fmt.Errorf("no metadata found for table %q", d.profile.Table)
	}
	uniq := make(map[string]struct{})
	for _, col := range d.table.PartitionKey {
		d.pkeys = append(d.pkeys, col.Name)
		uniq[col.Name] = struct{}{}
	}
	for _, col := range d.table.ClusteringColumns {
		d.ckeys = append(d.ckeys, col.Name)
		uniq[col.Name] = struct{}{}
	}
	for _, col := range d.table.Columns {
		switch typ := col.Type.Type(); typ {
		case gocql.TypeInt, gocql.TypeText:
			if _, ok := uniq[col.Name]; !ok {
				d.keys = append(d.keys, col.Name)
			}
		default:
			return fmt.Errorf("unsupported %q type for %q column", typ, col.Name)
		}
	}
	sort.Strings(d.keys) // deterministic batches

	d.stmt = makeInsertStmt(d.fullname(), flattenStrings(d.pkeys, d.ckeys, d.keys)...)

	return nil
}

func (d *Driver) truncateTable() error {
	return d.session.Query("TRUNCATE TABLE " + d.fullname()).Exec()
}

func (d *Driver) fullname() string {
	return d.profile.Keyspace + "." + d.profile.Table
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
	for _, set := range sets {
		if len(set) == 0 {
			return 0
		}
	}
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
