package main

import "testing"

func TestFilterUSDC(t *testing.T) {
	tests := []struct {
		name  string
		event map[string]interface{}
		want  bool
	}{
		{
			name: "flat USDC transfer passes",
			event: map[string]interface{}{
				"transfer": map[string]interface{}{"assetCode": "USDC"},
			},
			want: true,
		},
		{
			name: "other asset is dropped",
			event: map[string]interface{}{
				"transfer": map[string]interface{}{"assetCode": "XLM"},
			},
		},
		{
			name: "non-transfer event is dropped",
			event: map[string]interface{}{
				"fee": map[string]interface{}{"assetCode": "USDC"},
			},
		},
		{
			name: "missing asset code is dropped",
			event: map[string]interface{}{
				"transfer": map[string]interface{}{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterUSDC(tt.event)
			if (got != nil) != tt.want {
				t.Fatalf("filterUSDC() returned event = %t, want %t", got != nil, tt.want)
			}
		})
	}
}
