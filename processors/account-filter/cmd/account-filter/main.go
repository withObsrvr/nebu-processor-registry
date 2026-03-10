// Package main provides a standalone CLI for the account-filter transform processor.
//
// This processor filters events by Stellar account address. It checks common
// address fields (from, to, account, source) and passes through events that
// match any of the specified accounts.
//
// Usage:
//
//	# Filter for a specific account
//	token-transfer --start-ledger 60200000 | account-filter --account GABC...XYZ
//
//	# Filter for multiple accounts
//	token-transfer --start-ledger 60200000 | account-filter --account GABC...XYZ --account GDEF...123
//
//	# Filter using a watchlist file
//	token-transfer --start-ledger 60200000 | account-filter --account-file watchlist.txt
package main

import (
	"bufio"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/withObsrvr/nebu/pkg/processor/cli"
)

var version = "0.1.0"

var (
	accounts       []string
	accountFile    string
	role           string
	accountSet     map[string]bool
	accountsLoaded bool
)

func main() {
	config := cli.TransformConfig{
		Name:        "account-filter",
		Description: "Filter events by Stellar account address",
		Version:     version,
	}

	cli.RunTransformCLI(config, filterByAccount, addFlags)
}

func addFlags(cmd *cobra.Command) {
	cmd.Flags().StringArrayVar(&accounts, "account", nil, "Account address to filter for (repeatable)")
	cmd.Flags().StringVar(&accountFile, "account-file", "", "File containing account addresses (one per line)")
	cmd.Flags().StringVar(&role, "role", "any", "Filter role: 'any', 'source', or 'destination'")
}

func loadAccounts() {
	if accountsLoaded {
		return
	}
	accountsLoaded = true
	accountSet = make(map[string]bool)

	for _, addr := range accounts {
		accountSet[strings.TrimSpace(addr)] = true
	}

	if accountFile != "" {
		f, err := os.Open(accountFile)
		if err != nil {
			return
		}
		defer f.Close()

		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line != "" && !strings.HasPrefix(line, "#") {
				accountSet[line] = true
			}
		}
	}
}

// filterByAccount filters events to only include those involving specified accounts.
func filterByAccount(event map[string]interface{}) map[string]interface{} {
	loadAccounts()

	if len(accountSet) == 0 {
		return event
	}

	if matchesAccount(event) {
		return event
	}

	return nil
}

func matchesAccount(event map[string]interface{}) bool {
	checkSource := role == "any" || role == "source"
	checkDest := role == "any" || role == "destination"

	// Source fields
	sourceFields := []fieldPath{
		{[]string{"transfer", "from"}},
		{[]string{"burn", "from"}},
		{[]string{"clawback", "from"}},
		{[]string{"fee", "from"}},
		{[]string{"fee", "account"}},
		{[]string{"invokingAccount"}},
		{[]string{"invoking_account"}},
		{[]string{"account"}},
	}

	// Destination fields
	destFields := []fieldPath{
		{[]string{"transfer", "to"}},
		{[]string{"mint", "to"}},
	}

	if checkSource {
		for _, fp := range sourceFields {
			if val := resolveFieldPath(event, fp.parts); val != "" && accountSet[val] {
				return true
			}
		}
	}

	if checkDest {
		for _, fp := range destFields {
			if val := resolveFieldPath(event, fp.parts); val != "" && accountSet[val] {
				return true
			}
		}
	}

	// Meta fields (always checked, role-agnostic)
	if meta, ok := event["meta"].(map[string]interface{}); ok {
		if addr, ok := meta["contractAddress"].(string); ok && accountSet[addr] {
			return true
		}
	}

	return false
}

type fieldPath struct {
	parts []string
}

func resolveFieldPath(event map[string]interface{}, parts []string) string {
	current := event
	for i, part := range parts {
		val, ok := current[part]
		if !ok {
			return ""
		}
		if i == len(parts)-1 {
			if str, ok := val.(string); ok {
				return str
			}
			return ""
		}
		current, ok = val.(map[string]interface{})
		if !ok {
			return ""
		}
	}
	return ""
}
