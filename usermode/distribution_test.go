package usermode_test

import (
	"reflect"
	"testing"

	"github.com/scylladb/scylla-bench/usermode"
)

func TestParseDistribution(t *testing.T) {
	cases := map[string]struct {
		input string
		want  usermode.Distribution
		ok    bool
	}{
		"fixed": {
			`fixed(1)`,
			&usermode.Fixed{Value: 1},
			true,
		},
		"uniform": {
			`uniform(1..10)`,
			&usermode.Uniform{Min: 1, Max: 10},
			true,
		},
		"invalid fixed #1": {
			`fixed(asdfs)`,
			nil,
			false,
		},
		"invalid fixed #2": {
			`fixed)1(`,
			nil,
			false,
		},
		"invalid fixed #3": {
			`fixed()`,
			nil,
			false,
		},
		"invalid uniform #1": {
			`uniform(1..)`,
			nil,
			false,
		},
		"invalid uniform #2": {
			`uniform(1..)`,
			nil,
			false,
		},
		"invalid uniform #3": {
			`uniform(..)`,
			nil,
			false,
		},
		"invalid uniform #4": {
			`uniform(abc..def)`,
			nil,
			false,
		},
		"invalid uniform #5": {
			`uniform(300..100)`,
			nil,
			false,
		},
		"unsupported #1": {
			`unsupported(1..100, 10)`,
			nil,
			false,
		},
		"unsupported #2": {
			`~fixed(1..100, 10)`,
			nil,
			false,
		},
	}

	for name, cas := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := usermode.ParseDistribution(cas.input)
			if err != nil {
				if !cas.ok {
					return
				}
				t.Fatalf("ParseDistribution(%q)=%s", cas.input, err)
			}
			if !cas.ok {
				t.Errorf("expected ParseDistribution(%q) to fail", cas.input)
			}
			if !reflect.DeepEqual(got, cas.want) {
				t.Errorf("got %+v, want %+v", got, cas.want)
			}
		})
	}
}

func TestParseRatio(t *testing.T) {
	cases := map[string]struct {
		input string
		want  int64
		ok    bool
	}{
		"fixed":       {`fixed(1)/1`, 1, true},
		"uniform":     {`uniform(1..10)/10`, 10, true},
		"unsupported": {`unsupported(1..10, 10)/10`, 0, false},
		"invalid #1":  {`fixed(1)/`, 0, false},
		"invalid #2":  {`fixed(1)/abc`, 0, false},
		"invalid #3":  {`fixed(1)/0`, 0, false},
	}

	for name, cas := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := usermode.ParseRatio(cas.input)
			if err != nil {
				if !cas.ok {
					return
				}
				t.Fatalf("ParseRatio(%q)=%s", cas.input, err)
			}
			if !cas.ok {
				t.Errorf("expected ParseRatio(%q) to fail", cas.input)
			}
			if got.Value != cas.want {
				t.Errorf("got %d, want %d", got.Value, cas.want)
			}
		})
	}
}
