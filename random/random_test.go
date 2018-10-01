package random_test

import (
	"reflect"
	"testing"

	"github.com/scylladb/scylla-bench/random"
)

func TestParseDistribution(t *testing.T) {
	cases := map[string]struct {
		inputs []string
		want   random.Distribution
		ok     bool
	}{
		"fixed": {
			[]string{`fixed(1)`, `fixed:1`},
			&random.Fixed{Value: 1},
			true,
		},
		"uniform": {
			[]string{`uniform(1..10)`, `uniform:1..10`},
			&random.Uniform{Min: 1, Max: 10},
			true,
		},
		"invalid fixed #1": {
			[]string{`fixed(asdfs)`, `fixed:asdfs`},
			nil,
			false,
		},
		"invalid fixed #2": {
			[]string{`fixed)1(`},
			nil,
			false,
		},
		"invalid fixed #3": {
			[]string{`fixed()`, `fixed:`},
			nil,
			false,
		},
		"invalid fixed #4": {
			[]string{`fixed:1)`},
			nil,
			false,
		},
		"invalid uniform #1": {
			[]string{`uniform(1..)`, `uniform:1..`},
			nil,
			false,
		},
		"invalid uniform #2": {
			[]string{`uniform(..)`, `uniform:..`},
			nil,
			false,
		},
		"invalid uniform #3": {
			[]string{`uniform(10..abc)`, `uniform:10..abc`},
			nil,
			false,
		},
		"invalid uniform #4": {
			[]string{`uniform(abc..def)`, `uniform:abc..def`},
			nil,
			false,
		},
		"invalid uniform #5": {
			[]string{`uniform(300..100)`, `uniform:300..100`},
			nil,
			false,
		},
		"invalid uniform #6": {
			[]string{`uniform:10..100)`},
			nil,
			false,
		},
		"unsupported #1": {
			[]string{`unsupported(1..100, 10)`, `unsupported:1..100,10`},
			nil,
			false,
		},
		"unsupported #2": {
			[]string{`~fixed(1..100, 10)`, `~fixed:1..100,10`},
			nil,
			false,
		},
	}

	for name, cas := range cases {
		t.Run(name, func(t *testing.T) {
			for _, input := range cas.inputs {
				got, err := random.ParseDistribution(input)
				if err != nil {
					if !cas.ok {
						return
					}
					t.Fatalf("ParseDistribution(%q)=%s", input, err)
				}
				if !cas.ok {
					t.Errorf("expected ParseDistribution(%q) to fail", input)
				}
				if !reflect.DeepEqual(got, cas.want) {
					t.Errorf("got %+v, want %+v", got, cas.want)
				}
			}
		})
	}
}

func TestParseRatio(t *testing.T) {
	cases := map[string]struct {
		inputs []string
		want   int64
		ok     bool
	}{
		"fixed":       {[]string{`fixed(1)/1`, `fixed:1/1`}, 1, true},
		"uniform":     {[]string{`uniform(1..10)/10`, `uniform:1..10/10`}, 10, true},
		"unsupported": {[]string{`unsupported(1..10, 10)/10`, `unsupported:1..10,10/10`}, 0, false},
		"invalid #1":  {[]string{`fixed(1)/`, `fixed:1/`}, 0, false},
		"invalid #2":  {[]string{`fixed(1)/abc`, `fixed:1/abc`}, 0, false},
		"invalid #3":  {[]string{`fixed(1)/0`, `fixed:1/0`}, 0, false},
	}

	for name, cas := range cases {
		t.Run(name, func(t *testing.T) {
			for _, input := range cas.inputs {
				got, err := random.ParseRatio(input)
				if err != nil {
					if !cas.ok {
						return
					}
					t.Fatalf("ParseRatio(%q)=%s", input, err)
				}
				if !cas.ok {
					t.Errorf("expected ParseRatio(%q) to fail", input)
				}
				if got.Value != cas.want {
					t.Errorf("got %d, want %d", got.Value, cas.want)
				}
			}
		})
	}
}
