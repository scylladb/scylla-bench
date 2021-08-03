package command_line

import (
	"errors"
	"fmt"
	"github.com/scylladb/scylla-bench/random"
	"strconv"
)

type DistributionValue struct {
	Dist *random.Distribution
}

func MakeDistributionValue(dist *random.Distribution, defaultDist random.Distribution) *DistributionValue {
	*dist = defaultDist
	return &DistributionValue{dist}
}

func (v DistributionValue) String() string {
	if v.Dist == nil {
		return ""
	}
	return fmt.Sprintf("%s", *v.Dist)
}

func (v *DistributionValue) Set(s string) error {
	i, err := strconv.ParseInt(s, 10, 64)
	if err == nil {
		if i < 1 {
			return errors.New("value for fixed distribution is invalid: value has to be positive")
		}
		*v.Dist = random.Fixed{Value: i}
		return nil
	}

	dist, err := random.ParseDistribution(s)
	if err == nil {
		*v.Dist = dist
		return nil
	} else {
		return err
	}
}

