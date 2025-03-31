package random_test

import (
	"math"
	"reflect"
	"strconv"
	"testing"

	"github.com/scylladb/scylla-bench/random"
)

func TestParseDistribution(t *testing.T) {
	cases := map[string]struct {
		want   random.Distribution
		inputs []string
		ok     bool
	}{
		"fixed": {
			inputs: []string{`fixed(1)`, `fixed:1`},
			want:   &random.Fixed{Value: 1},
			ok:     true,
		},
		"uniform": {
			inputs: []string{`uniform(1..10)`, `uniform:1..10`},
			want:   &random.Uniform{Min: 1, Max: 10},
			ok:     true,
		},
		"invalid fixed #1": {
			inputs: []string{`fixed(asdfs)`, `fixed:asdfs`},
			want:   nil,
			ok:     false,
		},
		"invalid fixed #2": {
			inputs: []string{`fixed)1(`},
			want:   nil,
			ok:     false,
		},
		"invalid fixed #3": {
			inputs: []string{`fixed()`, `fixed:`},
			want:   nil,
			ok:     false,
		},
		"invalid fixed #4": {
			inputs: []string{`fixed:1)`},
			want:   nil,
			ok:     false,
		},
		"invalid uniform #1": {
			inputs: []string{`uniform(1..)`, `uniform:1..`},
			want:   nil,
			ok:     false,
		},
		"invalid uniform #2": {
			inputs: []string{`uniform(..)`, `uniform:..`},
			want:   nil,
			ok:     false,
		},
		"invalid uniform #3": {
			inputs: []string{`uniform(10..abc)`, `uniform:10..abc`},
			want:   nil,
			ok:     false,
		},
		"invalid uniform #4": {
			inputs: []string{`uniform(abc..def)`, `uniform:abc..def`},
			want:   nil,
			ok:     false,
		},
		"invalid uniform #5": {
			inputs: []string{`uniform(300..100)`, `uniform:300..100`},
			want:   nil,
			ok:     false,
		},
		"invalid uniform #6": {
			inputs: []string{`uniform:10..100)`},
			want:   nil,
			ok:     false,
		},
		"unsupported #1": {
			inputs: []string{`unsupported(1..100, 10)`, `unsupported:1..100,10`},
			want:   nil,
			ok:     false,
		},
		"unsupported #2": {
			inputs: []string{`~fixed(1..100, 10)`, `~fixed:1..100,10`},
			want:   nil,
			ok:     false,
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

func TestRandomString(t *testing.T) {
	t.Parallel()

	rnd := random.NewLockedRandomString(100)

	for _, size := range []int{128, 256, 512, 1024, 8192} {
		t.Run("generate_"+strconv.FormatInt(int64(size), 10), func(t *testing.T) {
			t.Parallel()

			generated := make(map[string]struct{}, 1000)
			same := 0
			for range 1000 {
				data := make([]byte, size)
				_, _ = rnd.Read(data)
				if _, ok := generated[string(data)]; ok {
					same++
				} else {
					generated[string(data)] = struct{}{}
				}
			}

			if same > 10 {
				t.Errorf("generated %d same strings of size %d", same, size)
			}
		})
	}
}

func BenchmarkNewLockedRandom(b *testing.B) {
	b.ReportAllocs()

	rnd := random.NewLockedRandom(100)
	b.ResetTimer()

	for b.Loop() {
		_ = rnd.Int64N(math.MaxInt64)
	}
}

func BenchmarkNewLockedRandomString(b *testing.B) {
	b.ReportAllocs()
	rnd := random.NewLockedRandomString(100)
	b.ResetTimer()

	sizes := []int{1, 10, 100, 1024, 8192}

	for _, size := range sizes {
		b.Run("generate_"+strconv.FormatInt(int64(size), 10), func(b *testing.B) {
			b.ResetTimer()
			b.ReportAllocs()
			data := make([]byte, size)
			for b.Loop() {
				_, _ = rnd.Read(data)
			}
		})
	}
}
