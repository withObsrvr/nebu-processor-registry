package validatoridentityenricher

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Snapshot is the canonical deterministic input file format. A raw JSON array
// from Radar's bulk node endpoint is accepted as a convenience as well.
type Snapshot struct {
	Network     string         `json:"network,omitempty"`
	GeneratedAt string         `json:"generatedAt,omitempty"`
	Nodes       []NodeIdentity `json:"nodes"`
}

// SnapshotResolver performs offline identity joins from a file loaded once.
type SnapshotResolver struct {
	network     string
	generatedAt string
	nodes       map[string]NodeIdentity
}

// LoadSnapshot loads either a Snapshot object or a raw Radar node array.
func LoadSnapshot(path string) (*SnapshotResolver, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read identity snapshot: %w", err)
	}
	return ParseSnapshot(data)
}

// ParseSnapshot parses snapshot bytes and is exported for embedding/testing.
func ParseSnapshot(data []byte) (*SnapshotResolver, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("identity snapshot is empty")
	}

	var snapshot Snapshot
	if trimmed[0] == '[' {
		if err := json.Unmarshal(trimmed, &snapshot.Nodes); err != nil {
			return nil, fmt.Errorf("decode Radar node array: %w", err)
		}
	} else {
		if err := json.Unmarshal(trimmed, &snapshot); err != nil {
			return nil, fmt.Errorf("decode identity snapshot: %w", err)
		}
	}

	networkName := strings.ToLower(strings.TrimSpace(snapshot.Network))
	if networkName != "" && networkName != "mainnet" && networkName != "testnet" {
		return nil, fmt.Errorf("snapshot network must be mainnet or testnet")
	}
	nodes := make(map[string]NodeIdentity, len(snapshot.Nodes))
	for i, node := range snapshot.Nodes {
		node.PublicKey = strings.TrimSpace(node.PublicKey)
		if node.PublicKey == "" {
			return nil, fmt.Errorf("snapshot node %d has no publicKey", i)
		}
		if _, exists := nodes[node.PublicKey]; exists {
			return nil, fmt.Errorf("snapshot contains duplicate publicKey %s", node.PublicKey)
		}
		nodes[node.PublicKey] = node
	}
	return &SnapshotResolver{
		network:     networkName,
		generatedAt: strings.TrimSpace(snapshot.GeneratedAt),
		nodes:       nodes,
	}, nil
}

func (r *SnapshotResolver) Source() string        { return "snapshot" }
func (r *SnapshotResolver) TemporalBasis() string { return "snapshot" }

func (r *SnapshotResolver) Resolve(_ context.Context, networkName, publicKey string) LookupResult {
	if r.network != "" && r.network != networkName {
		return LookupResult{Status: StatusUnavailable, Reason: "snapshot_network_mismatch"}
	}
	node, ok := r.nodes[publicKey]
	if !ok {
		return LookupResult{Status: StatusNotFound, Reason: "snapshot_not_found"}
	}
	if node.DateUpdated == "" {
		node.DateUpdated = r.generatedAt
	}
	return LookupResult{Status: StatusResolved, Node: node}
}
