package contract_created

import "strings"

type Candidate struct {
	ContractID             string
	Deployer               string
	PreimageAddress        string
	WasmHash               string
	ExecutableType         string
	CreateHostFunctionType string
	ConstructorName        string
	ConstructorArgs        []string
	InitializedState       []*StateEntry
	TTLToLedger            uint32
	KnownFamilyHints       []string
	KnownTags              []string
}

func (c Candidate) Keys() []string {
	out := make([]string, 0, len(c.InitializedState))
	for _, entry := range c.InitializedState {
		if entry == nil || entry.Key == "" {
			continue
		}
		out = append(out, entry.Key)
	}
	return out
}

func (c Candidate) Values() []string {
	out := make([]string, 0, len(c.InitializedState)+len(c.ConstructorArgs))
	out = append(out, c.ConstructorArgs...)
	for _, entry := range c.InitializedState {
		if entry == nil || entry.Value == "" {
			continue
		}
		out = append(out, entry.Value)
	}
	return out
}

func (c Candidate) JoinedLower() string {
	parts := []string{c.ContractID, c.Deployer, c.PreimageAddress, c.WasmHash, c.ExecutableType, c.CreateHostFunctionType, c.ConstructorName}
	parts = append(parts, c.KnownFamilyHints...)
	parts = append(parts, c.KnownTags...)
	parts = append(parts, c.ConstructorArgs...)
	for _, entry := range c.InitializedState {
		if entry == nil {
			continue
		}
		parts = append(parts, entry.Key, entry.Value)
	}
	return strings.ToLower(strings.Join(parts, " "))
}

func (c Candidate) HasKey(key string) bool {
	for _, k := range c.Keys() {
		if strings.EqualFold(k, key) {
			return true
		}
	}
	return false
}

func (c Candidate) HasKeyPrefix(prefix string) bool {
	prefix = strings.ToLower(prefix)
	for _, k := range c.Keys() {
		if strings.HasPrefix(strings.ToLower(k), prefix) {
			return true
		}
	}
	return false
}

func (c Candidate) Contains(parts ...string) bool {
	joined := c.JoinedLower()
	for _, part := range parts {
		if strings.Contains(joined, strings.ToLower(part)) {
			return true
		}
	}
	return false
}

func (c Candidate) CountMatchingKeyPrefixes(prefixes ...string) int {
	count := 0
	for _, prefix := range prefixes {
		if c.HasKeyPrefix(prefix) {
			count++
		}
	}
	return count
}
