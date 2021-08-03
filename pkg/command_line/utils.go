package command_line

import (
	"fmt"
	"github.com/gocql/gocql"
	"github.com/hailocab/go-hostpool"
)

func toInt(value bool) int {
	if value {
		return 1
	} else {
		return 0
	}
}

func buildHostSelectionPolicy(policy string, hosts []string) (gocql.HostSelectionPolicy, error) {
	switch policy {
	case "round-robin":
		return gocql.RoundRobinHostPolicy(), nil
	case "host-pool":
		return gocql.HostPoolHostPolicy(hostpool.New(hosts)), nil
	case "token-aware":
		return gocql.TokenAwareHostPolicy(gocql.RoundRobinHostPolicy()), nil
	default:
		return nil, fmt.Errorf("unknown host selection policy, %s", policy)
	}
}

