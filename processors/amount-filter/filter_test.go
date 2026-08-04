package amount_filter

import "testing"

func TestFilterEvent(t *testing.T) {
	tests := []struct {
		name   string
		filter *Filter
		event  map[string]interface{}
		want   bool
	}{
		{
			name:   "flat asset in range passes",
			filter: NewFilter(100, 200, "USDC"),
			event: map[string]interface{}{
				"transfer": map[string]interface{}{"amount": "150", "assetCode": "USDC"},
			},
			want: true,
		},
		{
			name:   "flat wrong asset is dropped",
			filter: NewFilter(0, 0, "USDC"),
			event: map[string]interface{}{
				"transfer": map[string]interface{}{"amount": "150", "assetCode": "XLM"},
			},
		},
		{
			name:   "legacy nested asset passes",
			filter: NewFilter(0, 0, "USDC"),
			event: map[string]interface{}{
				"transfer": map[string]interface{}{
					"amount": "150",
					"asset": map[string]interface{}{
						"issuedAsset": map[string]interface{}{"assetCode": "USDC"},
					},
				},
			},
			want: true,
		},
		{
			// token-transfer emits native as the flat assetCode "XLM", so
			// --asset native must still match it.
			name:   "flat native matches --asset native",
			filter: NewFilter(0, 0, "native"),
			event: map[string]interface{}{
				"transfer": map[string]interface{}{"amount": "150", "assetCode": "XLM"},
			},
			want: true,
		},
		{
			name:   "flat native matches --asset XLM",
			filter: NewFilter(0, 0, "XLM"),
			event: map[string]interface{}{
				"transfer": map[string]interface{}{"amount": "150", "assetCode": "XLM"},
			},
			want: true,
		},
		{
			name:   "native alias does not match an issued asset",
			filter: NewFilter(0, 0, "native"),
			event: map[string]interface{}{
				"transfer": map[string]interface{}{"amount": "150", "assetCode": "USDC"},
			},
		},
		{
			name:   "nested native matches --asset native",
			filter: NewFilter(0, 0, "native"),
			event: map[string]interface{}{
				"transfer": map[string]interface{}{
					"amount": "150",
					"asset":  map[string]interface{}{"native": true},
				},
			},
			want: true,
		},
		{
			name:   "nested native matches --asset XLM",
			filter: NewFilter(0, 0, "XLM"),
			event: map[string]interface{}{
				"transfer": map[string]interface{}{
					"amount": "150",
					"asset":  map[string]interface{}{"native": true},
				},
			},
			want: true,
		},
		{
			name:   "nested native is dropped for an issued-asset filter",
			filter: NewFilter(0, 0, "USDC"),
			event: map[string]interface{}{
				"transfer": map[string]interface{}{
					"amount": "150",
					"asset":  map[string]interface{}{"native": true},
				},
			},
		},
		{
			name:   "below minimum is dropped",
			filter: NewFilter(151, 0, ""),
			event: map[string]interface{}{
				"transfer": map[string]interface{}{"amount": "150"},
			},
		},
		{
			name:   "invalid amount is dropped",
			filter: NewFilter(0, 0, ""),
			event: map[string]interface{}{
				"transfer": map[string]interface{}{"amount": "not-a-number"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.filter.FilterEvent(tt.event)
			if (got != nil) != tt.want {
				t.Fatalf("FilterEvent() returned event = %t, want %t", got != nil, tt.want)
			}
		})
	}
}
