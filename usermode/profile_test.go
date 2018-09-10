package usermode_test

import (
	"reflect"
	"testing"

	"github.com/scylladb/scylla-bench/usermode"
)

func TestParse(t *testing.T) {
	got, err := usermode.ParseProfile(ExampleYaml)
	if err != nil {
		t.Fatalf("Parse()=%s", err)
	}
	if !reflect.DeepEqual(got, Example) {
		t.Fatalf("got %+v, want %+v", got, Example)
	}
}
