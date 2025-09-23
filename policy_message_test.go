package main

import (
	"testing"
)

// TestPolicyMessages verifies the logging messages match expected formats
func TestPolicyMessages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		policy         string
		datacenter     string
		rack           string
		wantPolicyName string
		hosts          []string
	}{
		{
			name:           "rack aware policy",
			policy:         "token-aware",
			hosts:          []string{},
			datacenter:     "dc1",
			rack:           "rack1",
			wantPolicyName: "token-rack-dc-aware",
		},
		{
			name:           "dc aware policy",
			policy:         "token-aware",
			hosts:          []string{},
			datacenter:     "dc1",
			rack:           "",
			wantPolicyName: "token-dc-aware",
		},
		{
			name:           "token aware with round robin fallback",
			policy:         "token-aware",
			hosts:          []string{},
			datacenter:     "",
			rack:           "",
			wantPolicyName: "token-round-robin",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, gotPolicyName, err := newHostSelectionPolicy(tt.policy, tt.hosts, tt.datacenter, tt.rack)
			if err != nil {
				t.Errorf("newHostSelectionPolicy() error = %v", err)
				return
			}

			if gotPolicyName != tt.wantPolicyName {
				t.Errorf(
					"newHostSelectionPolicy() returned policy name %v, want %v",
					gotPolicyName,
					tt.wantPolicyName,
				)
			}
		})
	}
}
