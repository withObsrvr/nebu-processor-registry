package nft_events

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/stellar/go-stellar-sdk/ingest"
	"github.com/stellar/go-stellar-sdk/strkey"
	"github.com/stellar/go-stellar-sdk/xdr"
	"github.com/withObsrvr/nebu/pkg/processor"
)

var nftEventNames = map[string]struct{}{
	"transfer":        {},
	"mint":            {},
	"burn":            {},
	"approve":         {},
	"approve_for_all": {},
}

var nftMethodNames = map[string]struct{}{
	"transfer":            {},
	"transfer_from":       {},
	"approve":             {},
	"approve_for_all":     {},
	"owner_of":            {},
	"balance":             {},
	"token_uri":           {},
	"token_uri_of":        {},
	"name":                {},
	"symbol":              {},
	"burn":                {},
	"burn_from":           {},
	"mint":                {},
	"get_approved":        {},
	"is_approved_for_all": {},
	"update_uri":          {},
	"get_total_minted":    {},
	"get_max_supply":      {},
	"next_token_id":       {},
	"set_trait":           {},
	"trait_value":         {},
	"trait_values":        {},
	"trait_metadata_uri":  {},
	"clawback":            {},
	"governance":          {},
	"total_supply":        {},
	"allowance":           {},
	"decimals":            {},
}

var sep50MethodNames = map[string]struct{}{
	"transfer":            {},
	"transfer_from":       {},
	"approve":             {},
	"approve_for_all":     {},
	"owner_of":            {},
	"balance":             {},
	"token_uri":           {},
	"token_uri_of":        {},
	"name":                {},
	"symbol":              {},
	"get_approved":        {},
	"is_approved_for_all": {},
}

// Origin extracts NFT-like events and calls from Soroban ledger activity.
type Origin struct {
	passphrase string
	emitter    *processor.Emitter[*NftEvent]
	engine     *heuristicEngine
	profiles   map[string]*contractProfile
}

type contractProfile struct {
	Standard       string
	Implementation string
	Confidence     float64
	Confirmed      bool
	RuleIDs        []string
}

func NewOrigin(passphrase string) *Origin {
	return &Origin{
		passphrase: passphrase,
		emitter:    processor.NewEmitter[*NftEvent](256),
		engine:     newHeuristicEngine(),
		profiles:   map[string]*contractProfile{},
	}
}

func (o *Origin) Name() string         { return "nft-events" }
func (o *Origin) Type() processor.Type { return processor.TypeOrigin }
func (o *Origin) Out() <-chan *NftEvent {
	return o.emitter.Out()
}
func (o *Origin) Close() { o.emitter.Close() }

func (o *Origin) ProcessLedger(ctx context.Context, ledger xdr.LedgerCloseMeta) {
	ledgerSeq := ledger.LedgerSequence()
	closeTime := int64(ledger.LedgerHeaderHistoryEntry().Header.ScpValue.CloseTime)

	txReader, err := ingest.NewLedgerTransactionReaderFromLedgerCloseMeta(o.passphrase, ledger)
	if err != nil {
		processor.ReportWarning(ctx, o.Name(), fmt.Errorf("ledger %d: create tx reader: %w", ledgerSeq, err))
		return
	}
	defer txReader.Close()

	for {
		tx, err := txReader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			processor.ReportWarning(ctx, o.Name(), fmt.Errorf("ledger %d: read tx: %w", ledgerSeq, err))
			return
		}

		txHash := tx.Result.TransactionHash.HexString()
		successful := tx.Result.Successful()
		txIndex := uint32(tx.Index)

		if txEvents, err := tx.GetTransactionEvents(); err == nil {
			for opIndex, opEvents := range txEvents.OperationEvents {
				for eventIndex, event := range opEvents {
					ev := o.buildFromContractEvent(event, ledgerSeq, closeTime, txHash, txIndex, int32(opIndex), int32(eventIndex), successful)
					if ev != nil {
						select {
						case <-ctx.Done():
							return
						default:
							o.emitter.Emit(ev)
						}
					}
				}
			}
			for eventIndex, txEvent := range txEvents.TransactionEvents {
				ev := o.buildFromContractEvent(txEvent.Event, ledgerSeq, closeTime, txHash, txIndex, -1, int32(eventIndex), successful)
				if ev != nil {
					select {
					case <-ctx.Done():
						return
					default:
						o.emitter.Emit(ev)
					}
				}
			}
		}

		for opIndex, op := range tx.Envelope.Operations() {
			if op.Body.Type != xdr.OperationTypeInvokeHostFunction {
				continue
			}
			ev := o.buildFromInvocation(op, ledgerSeq, closeTime, txHash, txIndex, int32(opIndex), successful)
			if ev != nil {
				select {
				case <-ctx.Done():
					return
				default:
					o.emitter.Emit(ev)
				}
			}
		}

		if changes, err := tx.GetChanges(); err == nil {
			for _, change := range changes {
				ev := o.buildFromContractDataChange(change, ledgerSeq, closeTime, txHash, txIndex, successful)
				if ev != nil {
					select {
					case <-ctx.Done():
						return
					default:
						o.emitter.Emit(ev)
					}
				}
			}
		}
	}
}

func (o *Origin) buildFromContractEvent(
	event xdr.ContractEvent,
	ledgerSeq uint32,
	closeTime int64,
	txHash string,
	txIndex uint32,
	opIndex int32,
	eventIndex int32,
	successful bool,
) *NftEvent {
	if event.Type != xdr.ContractEventTypeContract && event.Type != xdr.ContractEventTypeSystem {
		return nil
	}
	if event.ContractId == nil || event.Body.V0 == nil {
		return nil
	}

	contractID, err := strkey.Encode(strkey.VersionByteContract, event.ContractId[:])
	if err != nil {
		return nil
	}

	rawTopics := make([]string, 0, len(event.Body.V0.Topics))
	for _, topic := range event.Body.V0.Topics {
		rawTopics = append(rawTopics, scValToString(topic))
	}
	action := detectActionFromTopics(event.Body.V0.Topics)
	if action == "" {
		return nil
	}

	fromAddr, toAddr := firstTwoAddresses(rawTopics)
	rawData := scValToString(event.Body.V0.Data)
	tokenID := extractTokenID(rawTopics, rawData)
	uri, storage := extractMetadataURI(append(rawTopics, rawData)...)
	candidate := Candidate{Kind: CandidateKindEvent, ContractID: contractID, ActionHint: action, RawTopics: rawTopics, RawData: rawData, TokenID: tokenID, From: fromAddr, To: toAddr, MetadataURI: uri, MetadataStore: storage}
	classification := o.engine.Classify(candidate)
	classification = o.applyContractProfile(candidate, classification)
	if classification.Confidence < minimumConfidence(candidate.Kind) {
		return nil
	}
	defer o.rememberContractProfile(candidate, classification)

	ev := &NftEvent{
		Meta: &EventMeta{
			LedgerSequence:   ledgerSeq,
			ClosedAtUnix:     closeTime,
			TxHash:           txHash,
			TransactionIndex: txIndex,
			OperationIndex:   opIndex,
			EventIndex:       eventIndex,
			InSuccessfulTx:   successful,
		},
		ContractId:           contractID,
		TokenId:              tokenID,
		Action:               action,
		Standard:             classification.Standard,
		Implementation:       classification.Implementation,
		ClassificationSource: classification.Source,
		Confidence:           classification.Confidence,
		From:                 fromAddr,
		To:                   toAddr,
		EventTypesDetected:   []string{action},
		RawTopics:            rawTopics,
		RawData:              rawData,
		SourceKind:           "contract_event",
		HeuristicRuleIds:     classification.RuleIDs,
		HeuristicReasons:     classification.Reasons,
		HeuristicTags:        classification.Tags,
	}

	switch action {
	case "mint":
		ev.To = toAddr
	case "burn":
		ev.From = fromAddr
	case "approve":
		ev.Owner = fromAddr
		ev.Approved = toAddr
	case "approve_for_all":
		ev.Owner = fromAddr
		ev.Operator = toAddr
	}

	ev.TokenMetadataUri = uri
	ev.MetadataStorage = storage

	return ev
}

func (o *Origin) buildFromInvocation(
	op xdr.Operation,
	ledgerSeq uint32,
	closeTime int64,
	txHash string,
	txIndex uint32,
	opIndex int32,
	successful bool,
) *NftEvent {
	invoke := op.Body.MustInvokeHostFunctionOp()
	if invoke.HostFunction.Type != xdr.HostFunctionTypeHostFunctionTypeInvokeContract {
		return nil
	}
	contractFn := invoke.HostFunction.MustInvokeContract()
	method := string(contractFn.FunctionName)
	if _, ok := nftMethodNames[method]; !ok {
		return nil
	}

	contractID, err := strkey.Encode(strkey.VersionByteContract, contractFn.ContractAddress.ContractId[:])
	if err != nil {
		return nil
	}

	rawArgs := make([]string, 0, len(contractFn.Args))
	for _, arg := range contractFn.Args {
		rawArgs = append(rawArgs, scValToString(arg))
	}
	fromAddr, toAddr := firstTwoAddresses(rawArgs)
	tokenID := extractTokenID(rawArgs, "")
	uri, storage := extractMetadataURI(rawArgs...)
	action := actionForMethod(method)
	candidate := Candidate{Kind: CandidateKindInvocation, ContractID: contractID, ActionHint: action, MethodName: method, RawTopics: rawArgs, TokenID: tokenID, From: fromAddr, To: toAddr, MetadataURI: uri, MetadataStore: storage}
	if action == "approve" {
		candidate.Owner = fromAddr
		candidate.Approved = toAddr
	}
	if action == "approve_for_all" {
		candidate.Owner = fromAddr
		candidate.Operator = toAddr
	}
	classification := o.engine.Classify(candidate)
	classification = o.applyContractProfile(candidate, classification)
	if classification.Confidence < minimumConfidence(candidate.Kind) {
		return nil
	}
	defer o.rememberContractProfile(candidate, classification)

	return &NftEvent{
		Meta: &EventMeta{
			LedgerSequence:   ledgerSeq,
			ClosedAtUnix:     closeTime,
			TxHash:           txHash,
			TransactionIndex: txIndex,
			OperationIndex:   opIndex,
			EventIndex:       -1,
			InSuccessfulTx:   successful,
		},
		ContractId:           contractID,
		TokenId:              tokenID,
		Action:               action,
		Standard:             classification.Standard,
		Implementation:       classification.Implementation,
		ClassificationSource: classification.Source,
		Confidence:           classification.Confidence,
		From:                 fromAddr,
		To:                   toAddr,
		Owner:                candidate.Owner,
		Approved:             candidate.Approved,
		Operator:             candidate.Operator,
		FunctionName:         method,
		MethodsDetected:      []string{method},
		RawTopics:            rawArgs,
		SourceKind:           "contract_invocation",
		TokenMetadataUri:     uri,
		MetadataStorage:      storage,
		HeuristicRuleIds:     classification.RuleIDs,
		HeuristicReasons:     classification.Reasons,
		HeuristicTags:        classification.Tags,
	}
}

func (o *Origin) buildFromContractDataChange(
	change ingest.Change,
	ledgerSeq uint32,
	closeTime int64,
	txHash string,
	txIndex uint32,
	successful bool,
) *NftEvent {
	if change.Type != xdr.LedgerEntryTypeContractData {
		return nil
	}

	var entry *xdr.ContractDataEntry
	actionHint := "update"
	stateValue := ""
	durability := ""
	tokenExists := true
	burned := false

	switch change.ChangeType {
	case xdr.LedgerEntryChangeTypeLedgerEntryCreated:
		if change.Post == nil || change.Post.Data.Type != xdr.LedgerEntryTypeContractData {
			return nil
		}
		cd := change.Post.Data.MustContractData()
		entry = &cd
		stateValue = scValToString(cd.Val)
		actionHint = "create"
		durability = durabilityString(cd.Durability)
	case xdr.LedgerEntryChangeTypeLedgerEntryUpdated:
		if change.Post == nil || change.Post.Data.Type != xdr.LedgerEntryTypeContractData {
			return nil
		}
		cd := change.Post.Data.MustContractData()
		entry = &cd
		stateValue = scValToString(cd.Val)
		actionHint = "update"
		durability = durabilityString(cd.Durability)
	case xdr.LedgerEntryChangeTypeLedgerEntryRemoved:
		if change.Pre == nil || change.Pre.Data.Type != xdr.LedgerEntryTypeContractData {
			return nil
		}
		cd := change.Pre.Data.MustContractData()
		entry = &cd
		stateValue = scValToString(cd.Val)
		actionHint = "delete"
		durability = durabilityString(cd.Durability)
	default:
		return nil
	}

	if entry == nil || entry.Contract.ContractId == nil {
		return nil
	}

	contractID, err := strkey.Encode(strkey.VersionByteContract, entry.Contract.ContractId[:])
	if err != nil {
		return nil
	}

	keyParts := scValToStrings(entry.Key)
	stateKey := scValToString(entry.Key)
	valueParts := scValToStrings(entry.Val)
	allParts := append(append([]string{}, keyParts...), valueParts...)
	joinedLower := strings.ToLower(strings.Join(allParts, " "))

	action := detectStateAction(joinedLower, actionHint)
	if action == "" {
		return nil
	}

	owner, approved := firstTwoAddresses(valueParts)
	fromAddr, toAddr := firstTwoAddresses(allParts)
	tokenID := extractStateTokenID(keyParts, valueParts, stateValue)
	uri, storage := extractMetadataURI(append(keyParts, valueParts...)...)
	candidate := Candidate{Kind: CandidateKindState, ContractID: contractID, ActionHint: action, KeyParts: keyParts, ValueParts: valueParts, TokenID: tokenID, From: fromAddr, To: toAddr, Owner: owner, Approved: approved, MetadataURI: uri, MetadataStore: storage, StateKey: stateKey, StateValue: stateValue, Durability: durability}
	classification := o.engine.Classify(candidate)
	classification = o.applyContractProfile(candidate, classification)
	if classification.Confidence < minimumConfidence(candidate.Kind) {
		return nil
	}
	defer o.rememberContractProfile(candidate, classification)

	ev := &NftEvent{
		Meta: &EventMeta{
			LedgerSequence:   ledgerSeq,
			ClosedAtUnix:     closeTime,
			TxHash:           txHash,
			TransactionIndex: txIndex,
			OperationIndex:   -1,
			EventIndex:       -1,
			InSuccessfulTx:   successful,
		},
		ContractId:           contractID,
		TokenId:              tokenID,
		Action:               action,
		Standard:             classification.Standard,
		Implementation:       classification.Implementation,
		ClassificationSource: classification.Source,
		Confidence:           classification.Confidence,
		From:                 fromAddr,
		To:                   toAddr,
		Owner:                owner,
		Approved:             approved,
		TokenMetadataUri:     uri,
		MetadataStorage:      storage,
		RawTopics:            keyParts,
		RawData:              stateValue,
		SourceKind:           "contract_state",
		StateKey:             stateKey,
		StateValue:           stateValue,
		Durability:           durability,
		TokenExists:          tokenExists,
		Burned:               burned,
		HeuristicRuleIds:     classification.RuleIDs,
		HeuristicReasons:     classification.Reasons,
		HeuristicTags:        classification.Tags,
	}

	if strings.Contains(joinedLower, "name") && !looksLikeAddress(stateValue) && storage == "unknown" {
		ev.CollectionName = stateValue
	}
	if strings.Contains(joinedLower, "symbol") && !looksLikeAddress(stateValue) {
		ev.Symbol = stateValue
	}
	if action == "approve_for_all" {
		ev.Operator = approved
		ev.Approved = ""
	}
	if action == "owner_update" {
		ev.Owner = owner
	}
	if action == "burn" {
		ev.TokenExists = false
		ev.Burned = true
	}

	return ev
}

func (o *Origin) applyContractProfile(candidate Candidate, classification Classification) Classification {
	profile, ok := o.profiles[candidate.ContractID]
	if !ok || !profile.Confirmed {
		return classification
	}

	if classification.Standard == "unknown" {
		classification.Standard = profile.Standard
	}
	if classification.Implementation == "unknown" {
		classification.Implementation = profile.Implementation
	}

	isWeakAction := candidate.ActionHint == "transfer" || candidate.ActionHint == "approve" || candidate.MethodName == "transfer" || candidate.MethodName == "approve" || candidate.MethodName == "balance"
	if isWeakAction && classification.Confidence < minimumConfidence(candidate.Kind) {
		classification.Confidence += 0.30
		classification.RuleIDs = append(classification.RuleIDs, "profile.confirmed_contract")
		classification.Reasons = append(classification.Reasons, "contract previously showed strong NFT evidence")
		classification.Tags = append(classification.Tags, "contract_profile")
	}
	if classification.Confidence > 0.99 {
		classification.Confidence = 0.99
	}
	classification.RuleIDs = uniqueStrings(classification.RuleIDs)
	classification.Reasons = uniqueStrings(classification.Reasons)
	classification.Tags = uniqueStrings(classification.Tags)
	return classification
}

func (o *Origin) rememberContractProfile(candidate Candidate, classification Classification) {
	if candidate.ContractID == "" {
		return
	}
	profile, ok := o.profiles[candidate.ContractID]
	if !ok {
		profile = &contractProfile{Standard: classification.Standard, Implementation: classification.Implementation}
		o.profiles[candidate.ContractID] = profile
	}
	if classification.Standard != "unknown" {
		profile.Standard = classification.Standard
	}
	if classification.Implementation != "unknown" {
		profile.Implementation = classification.Implementation
	}
	if classification.Confidence > profile.Confidence {
		profile.Confidence = classification.Confidence
	}
	profile.RuleIDs = uniqueStrings(append(profile.RuleIDs, classification.RuleIDs...))
	for _, ruleID := range classification.RuleIDs {
		if ruleID == "method.sep50" || ruleID == "nft.approve_for_all" || ruleID == "nft.token_uri" || ruleID == "state.exact_owner" || ruleID == "state.exact_approval_for_all" || ruleID == "method.transfer_from" || ruleID == "method.mint_burn" {
			profile.Confirmed = true
			break
		}
	}
	if classification.Confidence >= 0.80 {
		profile.Confirmed = true
	}
}

func actionForMethod(method string) string {
	switch method {
	case "transfer", "transfer_from":
		return "transfer"
	case "approve":
		return "approve"
	case "approve_for_all":
		return "approve_for_all"
	case "mint":
		return "mint"
	case "burn", "burn_from":
		return "burn"
	default:
		return "detect"
	}
}

func extractStateTokenID(keyParts, valueParts []string, fallback string) string {
	if len(keyParts) >= 2 {
		switch keyParts[0] {
		case "Owner", "Approval", "BurnedToken", "GlobalTokens", "GlobalTokensIndex", "OwnerTokensIndex", "TokenRoyalty", "ApprovalForAll":
			if looksTokenIDLike(keyParts[1]) {
				return keyParts[1]
			}
		}
	}
	if len(keyParts) >= 3 {
		if keyParts[0] == "OwnerTokens" && looksTokenIDLike(keyParts[2]) {
			return keyParts[2]
		}
	}
	for _, v := range valueParts {
		if looksTokenIDLike(v) {
			return v
		}
	}
	return extractTokenID(append(keyParts, valueParts...), fallback)
}

func detectStateAction(joinedLower, actionHint string) string {
	switch {
	case strings.Contains(joinedLower, "approve_for_all") || strings.Contains(joinedLower, "operator"):
		return "approve_for_all"
	case strings.Contains(joinedLower, "approve"):
		return "approve"
	case strings.Contains(joinedLower, "token_uri"), strings.Contains(joinedLower, "metadata"), strings.Contains(joinedLower, "uri"):
		return "metadata_update"
	case strings.Contains(joinedLower, "owner"), strings.Contains(joinedLower, "holder"):
		if actionHint == "delete" {
			return "burn"
		}
		return "owner_update"
	case strings.Contains(joinedLower, "name"), strings.Contains(joinedLower, "symbol"):
		return "collection_metadata"
	}
	return ""
}

func detectActionFromTopics(topics []xdr.ScVal) string {
	for _, topic := range topics {
		if topic.Type != xdr.ScValTypeScvSymbol {
			continue
		}
		sym := strings.ToLower(string(topic.MustSym()))
		if _, ok := nftEventNames[sym]; ok {
			return sym
		}
	}
	return ""
}

func containsAny(s string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(s, n) {
			return true
		}
	}
	return false
}

func firstTwoAddresses(values []string) (string, string) {
	addresses := make([]string, 0, 2)
	for _, v := range values {
		if looksLikeAddress(v) {
			addresses = append(addresses, v)
			if len(addresses) == 2 {
				break
			}
		}
	}
	if len(addresses) == 0 {
		return "", ""
	}
	if len(addresses) == 1 {
		return addresses[0], ""
	}
	return addresses[0], addresses[1]
}

func extractTokenID(values []string, fallback string) string {
	for _, v := range values {
		lv := strings.ToLower(strings.TrimSpace(v))
		if lv == "" || lv == fallback {
			continue
		}
		if _, ok := nftEventNames[lv]; ok {
			continue
		}
		if looksLikeAddress(v) {
			continue
		}
		if strings.HasPrefix(lv, "ipfs://") || strings.HasPrefix(lv, "http://") || strings.HasPrefix(lv, "https://") || strings.HasPrefix(lv, "ar://") {
			continue
		}
		return v
	}
	if fallback != "" && !looksLikeAddress(fallback) {
		return fallback
	}
	return ""
}

func extractMetadataURI(values ...string) (string, string) {
	for _, v := range values {
		lv := strings.ToLower(v)
		switch {
		case strings.HasPrefix(lv, "ipfs://"):
			return v, "ipfs"
		case strings.HasPrefix(lv, "https://"):
			return v, "https"
		case strings.HasPrefix(lv, "http://"):
			return v, "http"
		case strings.HasPrefix(lv, "ar://"):
			return v, "arweave"
		}
	}
	return "", "unknown"
}

func durabilityString(d xdr.ContractDataDurability) string {
	if d == xdr.ContractDataDurabilityPersistent {
		return "persistent"
	}
	if d == xdr.ContractDataDurabilityTemporary {
		return "temporary"
	}
	return "unknown"
}

func looksLikeAddress(v string) bool {
	if len(v) < 2 {
		return false
	}
	return strings.HasPrefix(v, "G") || strings.HasPrefix(v, "C") || strings.HasPrefix(v, "M")
}

func scValToStrings(val xdr.ScVal) []string {
	switch val.Type {
	case xdr.ScValTypeScvVec:
		vec := val.MustVec()
		out := make([]string, 0, len(*vec))
		for _, item := range *vec {
			out = append(out, scValToStrings(item)...)
		}
		return out
	case xdr.ScValTypeScvMap:
		scMap := val.MustMap()
		out := make([]string, 0, len(*scMap)*2)
		for _, entry := range *scMap {
			out = append(out, scValToStrings(entry.Key)...)
			out = append(out, scValToStrings(entry.Val)...)
		}
		return out
	default:
		v := scValToString(val)
		if v == "" {
			return nil
		}
		return []string{v}
	}
}

func scValToString(val xdr.ScVal) string {
	switch val.Type {
	case xdr.ScValTypeScvBool:
		if val.MustB() {
			return "true"
		}
		return "false"
	case xdr.ScValTypeScvVoid:
		return ""
	case xdr.ScValTypeScvU32:
		return fmt.Sprintf("%d", val.MustU32())
	case xdr.ScValTypeScvI32:
		return fmt.Sprintf("%d", val.MustI32())
	case xdr.ScValTypeScvU64:
		return fmt.Sprintf("%d", val.MustU64())
	case xdr.ScValTypeScvI64:
		return fmt.Sprintf("%d", val.MustI64())
	case xdr.ScValTypeScvU128:
		u := val.MustU128()
		return fmt.Sprintf("0x%016x%016x", u.Hi, u.Lo)
	case xdr.ScValTypeScvI128:
		i := val.MustI128()
		return fmt.Sprintf("0x%016x%016x", i.Hi, i.Lo)
	case xdr.ScValTypeScvString:
		return string(val.MustStr())
	case xdr.ScValTypeScvSymbol:
		return string(val.MustSym())
	case xdr.ScValTypeScvBytes:
		return fmt.Sprintf("%x", val.MustBytes())
	case xdr.ScValTypeScvAddress:
		addr := val.MustAddress()
		switch addr.Type {
		case xdr.ScAddressTypeScAddressTypeAccount:
			return addr.MustAccountId().Address()
		case xdr.ScAddressTypeScAddressTypeContract:
			contractID := addr.MustContractId()
			encoded, err := strkey.Encode(strkey.VersionByteContract, contractID[:])
			if err == nil {
				return encoded
			}
		}
	case xdr.ScValTypeScvTimepoint:
		return time.Unix(int64(val.MustTimepoint()), 0).UTC().Format(time.RFC3339)
	case xdr.ScValTypeScvDuration:
		return fmt.Sprintf("%d", val.MustDuration())
	case xdr.ScValTypeScvVec:
		vec := val.MustVec()
		parts := make([]string, 0, len(*vec))
		for _, item := range *vec {
			parts = append(parts, scValToString(item))
		}
		return "[" + strings.Join(parts, ",") + "]"
	case xdr.ScValTypeScvMap:
		scMap := val.MustMap()
		parts := make([]string, 0, len(*scMap))
		for _, entry := range *scMap {
			parts = append(parts, scValToString(entry.Key)+":"+scValToString(entry.Val))
		}
		return "{" + strings.Join(parts, ",") + "}"
	}
	return val.Type.String()
}
