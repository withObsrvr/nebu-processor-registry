package contract_created

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/stellar/go-stellar-sdk/ingest"
	"github.com/stellar/go-stellar-sdk/strkey"
	"github.com/stellar/go-stellar-sdk/xdr"
	"github.com/withObsrvr/nebu/pkg/processor"
)

// Origin extracts contract creation events from Stellar ledgers.
type Origin struct {
	passphrase string
	emitter    *processor.Emitter[*ContractCreatedEvent]
	engine     *heuristicEngine
	profiles   map[string]*familyProfile
}

type familyProfile struct {
	Family string
	Tags   []string
}

func NewOrigin(passphrase string) *Origin {
	return &Origin{
		passphrase: passphrase,
		emitter:    processor.NewEmitter[*ContractCreatedEvent](256),
		engine:     newHeuristicEngine(),
		profiles:   map[string]*familyProfile{},
	}
}

func (o *Origin) Name() string         { return "contract-created" }
func (o *Origin) Type() processor.Type { return processor.TypeOrigin }
func (o *Origin) Out() <-chan *ContractCreatedEvent {
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

		if !tx.Result.Successful() {
			continue
		}

		txHash := tx.Result.TransactionHash.HexString()
		txIndex := uint32(tx.Index)
		changes, _ := tx.GetChanges()

		for opIndex, op := range tx.Envelope.Operations() {
			if op.Body.Type != xdr.OperationTypeInvokeHostFunction {
				continue
			}

			ev := o.buildContractCreatedEvent(tx, changes, op, ledgerSeq, closeTime, txHash, txIndex, int32(opIndex))
			if ev == nil {
				continue
			}

			select {
			case <-ctx.Done():
				return
			default:
				o.emitter.Emit(ev)
			}
		}
	}
}

func (o *Origin) buildContractCreatedEvent(
	tx ingest.LedgerTransaction,
	changes []ingest.Change,
	op xdr.Operation,
	ledgerSeq uint32,
	closeTime int64,
	txHash string,
	txIndex uint32,
	opIndex int32,
) *ContractCreatedEvent {
	invoke := op.Body.MustInvokeHostFunctionOp()

	createDetails, ok := extractCreateDetails(o.passphrase, tx, invoke)
	if !ok {
		return nil
	}

	initializedState, contractInstanceCreated, ttlExtendedTo := collectContractCreationState(changes, createDetails.ContractID, createDetails.WasmHash)
	if createDetails.ContractID == "" {
		return nil
	}
	if len(initializedState) == 0 && !contractInstanceCreated {
		return nil
	}

	deployer := tx.Envelope.SourceAccount().ToAccountId().Address()
	if op.SourceAccount != nil {
		deployer = op.SourceAccount.ToAccountId().Address()
	}

	candidate := Candidate{
		ContractID:             createDetails.ContractID,
		Deployer:               deployer,
		PreimageAddress:        createDetails.PreimageAddress,
		WasmHash:               createDetails.WasmHash,
		ExecutableType:         createDetails.ExecutableType,
		CreateHostFunctionType: createDetails.HostFunctionType,
		ConstructorName:        createDetails.ConstructorName,
		ConstructorArgs:        createDetails.ConstructorArgs,
		InitializedState:       initializedState,
		TTLToLedger:            ttlExtendedTo,
	}
	if profile, ok := o.profiles[createDetails.WasmHash]; ok {
		candidate.KnownFamilyHints = append(candidate.KnownFamilyHints, profile.Family)
		candidate.KnownTags = append(candidate.KnownTags, profile.Tags...)
	}
	classification := o.engine.Classify(candidate)
	if createDetails.WasmHash != "" && classification.Family != "generic_contract" {
		o.profiles[createDetails.WasmHash] = &familyProfile{Family: classification.Family, Tags: classification.Tags}
	}
	familyCandidates := make([]*FamilyCandidate, 0, len(classification.Candidates))
	for _, c := range classification.Candidates {
		familyCandidates = append(familyCandidates, &FamilyCandidate{
			Family:     c.Family,
			Confidence: c.Confidence,
			Reasons:    c.Reasons,
			Tags:       c.Tags,
			RuleIds:    c.RuleIDs,
		})
	}

	return &ContractCreatedEvent{
		Meta: &EventMeta{
			LedgerSequence:   ledgerSeq,
			ClosedAtUnix:     closeTime,
			TxHash:           txHash,
			TransactionIndex: txIndex,
			OperationIndex:   opIndex,
			InSuccessfulTx:   true,
		},
		ContractId:              createDetails.ContractID,
		Deployer:                deployer,
		PreimageAddress:         createDetails.PreimageAddress,
		SaltHex:                 createDetails.SaltHex,
		ExecutableType:          createDetails.ExecutableType,
		WasmHash:                createDetails.WasmHash,
		CreateHostFunctionType:  createDetails.HostFunctionType,
		ConstructorInvoked:      len(createDetails.ConstructorArgs) > 0,
		ConstructorName:         createDetails.ConstructorName,
		ConstructorArgs:         createDetails.ConstructorArgs,
		ContractInstanceCreated: contractInstanceCreated,
		InitializedState:        initializedState,
		TtlExtendedToLedger:     ttlExtendedTo,
		ClassificationHint:      classification.Family,
		ClassificationReasons:   classification.Reasons,
		FamilyHint:              classification.Family,
		FamilyConfidence:        classification.Confidence,
		FamilyReasons:           classification.Reasons,
		FamilyTags:              classification.Tags,
		FamilyCandidates:        familyCandidates,
	}
}

type createContractDetails struct {
	ContractID       string
	PreimageAddress  string
	SaltHex          string
	ExecutableType   string
	WasmHash         string
	HostFunctionType string
	ConstructorName  string
	ConstructorArgs  []string
}

func extractCreateDetails(passphrase string, tx ingest.LedgerTransaction, invoke xdr.InvokeHostFunctionOp) (createContractDetails, bool) {
	hf := invoke.HostFunction
	details := createContractDetails{ConstructorName: "__constructor"}

	switch hf.Type {
	case xdr.HostFunctionTypeHostFunctionTypeCreateContract:
		details.HostFunctionType = "create_contract"
		args := hf.MustCreateContract()
		details.ContractID = deriveContractID(passphrase, args.ContractIdPreimage)
		details.PreimageAddress, details.SaltHex = preimageAddressAndSalt(args.ContractIdPreimage)
		details.ExecutableType, details.WasmHash = executableDetails(args.Executable)
	case xdr.HostFunctionTypeHostFunctionTypeCreateContractV2:
		details.HostFunctionType = "create_contract_v2"
		args := hf.MustCreateContractV2()
		details.ContractID = deriveContractID(passphrase, args.ContractIdPreimage)
		details.PreimageAddress, details.SaltHex = preimageAddressAndSalt(args.ContractIdPreimage)
		details.ExecutableType, details.WasmHash = executableDetails(args.Executable)
		for _, arg := range args.ConstructorArgs {
			details.ConstructorArgs = append(details.ConstructorArgs, scValToString(arg))
		}
	default:
		return createContractDetails{}, false
	}

	if details.ContractID == "" {
		if contractID, ok := tx.ContractIdFromTxEnvelope(); ok {
			details.ContractID = contractID
		}
	}
	if details.ConstructorName == "__constructor" && len(details.ConstructorArgs) == 0 {
		if name, args := extractConstructorFromAuth(invoke.Auth, details.ContractID); len(args) > 0 {
			details.ConstructorName = name
			details.ConstructorArgs = args
		}
	}
	if len(details.ConstructorArgs) == 0 {
		details.ConstructorName = ""
	}
	return details, true
}

func collectContractCreationState(changes []ingest.Change, contractID string, initialWasmHash string) ([]*StateEntry, bool, uint32) {
	if contractID == "" {
		return nil, false, 0
	}

	state := make([]*StateEntry, 0)
	keyHashes := map[string]struct{}{}
	contractInstanceCreated := false
	maxTTL := uint32(0)
	wasmHash := initialWasmHash

	for _, change := range changes {
		if change.Type != xdr.LedgerEntryTypeContractData {
			continue
		}

		entry, operation := contractDataEntryFromChange(change)
		if entry == nil || entry.Contract.ContractId == nil {
			continue
		}

		entryContractID, err := strkey.Encode(strkey.VersionByteContract, entry.Contract.ContractId[:])
		if err != nil || entryContractID != contractID {
			continue
		}

		ledgerKeyHash := hashContractDataLedgerKey(*entry)
		if ledgerKeyHash != "" {
			keyHashes[ledgerKeyHash] = struct{}{}
		}

		stateKey := stateKeyString(entry.Key)
		stateValue := scValToString(entry.Val)
		durability := durabilityString(entry.Durability)
		state = append(state, &StateEntry{Key: stateKey, Value: stateValue, Operation: operation, Durability: durability})

		if entry.Key.Type == xdr.ScValTypeScvLedgerKeyContractInstance {
			contractInstanceCreated = operation == "create" || operation == "update"
			if entry.Val.Type == xdr.ScValTypeScvContractInstance {
				instance := entry.Val.MustInstance()
				if wasmHash == "" {
					_, wasmHash = executableDetails(instance.Executable)
				}
				state = append(state, expandInstanceStorageState(instance, operation, durability)...)
			}
		}
	}

	for _, change := range changes {
		if change.Type != xdr.LedgerEntryTypeTtl {
			continue
		}
		entry := ttlEntryFromChange(change)
		if entry == nil {
			continue
		}
		keyHash := fmt.Sprintf("%x", entry.KeyHash[:])
		if _, ok := keyHashes[keyHash]; ok {
			if live := uint32(entry.LiveUntilLedgerSeq); live > maxTTL {
				maxTTL = live
			}
		}
	}

	if wasmHash != "" {
		for _, s := range state {
			if s.Key == "LedgerKeyContractInstance" && s.Value == "" {
				s.Value = wasmHash
			}
		}
	}

	return state, contractInstanceCreated, maxTTL
}

func contractDataEntryFromChange(change ingest.Change) (*xdr.ContractDataEntry, string) {
	switch change.ChangeType {
	case xdr.LedgerEntryChangeTypeLedgerEntryCreated:
		if change.Post != nil && change.Post.Data.Type == xdr.LedgerEntryTypeContractData {
			entry := change.Post.Data.MustContractData()
			return &entry, "create"
		}
	case xdr.LedgerEntryChangeTypeLedgerEntryUpdated:
		if change.Post != nil && change.Post.Data.Type == xdr.LedgerEntryTypeContractData {
			entry := change.Post.Data.MustContractData()
			return &entry, "update"
		}
	case xdr.LedgerEntryChangeTypeLedgerEntryRemoved:
		if change.Pre != nil && change.Pre.Data.Type == xdr.LedgerEntryTypeContractData {
			entry := change.Pre.Data.MustContractData()
			return &entry, "delete"
		}
	}
	return nil, ""
}

func ttlEntryFromChange(change ingest.Change) *xdr.TtlEntry {
	switch change.ChangeType {
	case xdr.LedgerEntryChangeTypeLedgerEntryCreated, xdr.LedgerEntryChangeTypeLedgerEntryUpdated:
		if change.Post != nil && change.Post.Data.Type == xdr.LedgerEntryTypeTtl {
			entry := change.Post.Data.MustTtl()
			return &entry
		}
	case xdr.LedgerEntryChangeTypeLedgerEntryRemoved:
		if change.Pre != nil && change.Pre.Data.Type == xdr.LedgerEntryTypeTtl {
			entry := change.Pre.Data.MustTtl()
			return &entry
		}
	}
	return nil
}

func hashContractDataLedgerKey(entry xdr.ContractDataEntry) string {
	ledgerKey, err := xdr.NewLedgerKey(xdr.LedgerEntryTypeContractData, xdr.LedgerKeyContractData{
		Contract:   entry.Contract,
		Key:        entry.Key,
		Durability: entry.Durability,
	})
	if err != nil {
		return ""
	}
	b, err := ledgerKey.MarshalBinary()
	if err != nil {
		return ""
	}
	h := sha256.Sum256(b)
	return fmt.Sprintf("%x", h[:])
}

func deriveContractID(passphrase string, preimage xdr.ContractIdPreimage) string {
	networkID := xdr.Hash(sha256.Sum256([]byte(passphrase)))
	hashPreimage := xdr.HashIdPreimage{
		Type: xdr.EnvelopeTypeEnvelopeTypeContractId,
		ContractId: &xdr.HashIdPreimageContractId{
			NetworkId:          networkID,
			ContractIdPreimage: preimage,
		},
	}
	b, err := hashPreimage.MarshalBinary()
	if err != nil {
		return ""
	}
	h := sha256.Sum256(b)
	contractID, err := strkey.Encode(strkey.VersionByteContract, h[:])
	if err != nil {
		return ""
	}
	return contractID
}

func preimageAddressAndSalt(preimage xdr.ContractIdPreimage) (string, string) {
	if preimage.Type != xdr.ContractIdPreimageTypeContractIdPreimageFromAddress {
		return "", ""
	}
	from := preimage.MustFromAddress()
	return scAddressToString(from.Address), fmt.Sprintf("%x", from.Salt[:])
}

func executableDetails(exec xdr.ContractExecutable) (string, string) {
	switch exec.Type {
	case xdr.ContractExecutableTypeContractExecutableWasm:
		wasmHash := exec.MustWasmHash()
		return "wasm", fmt.Sprintf("%x", wasmHash[:])
	case xdr.ContractExecutableTypeContractExecutableStellarAsset:
		return "stellar_asset", ""
	default:
		return strings.ToLower(exec.Type.String()), ""
	}
}

func stateKeyString(val xdr.ScVal) string {
	if val.Type == xdr.ScValTypeScvLedgerKeyContractInstance {
		return "LedgerKeyContractInstance"
	}
	return scValToString(val)
}

func expandInstanceStorageState(instance xdr.ScContractInstance, operation, durability string) []*StateEntry {
	if instance.Storage == nil {
		return nil
	}
	entries := make([]*StateEntry, 0, len(*instance.Storage))
	for _, entry := range *instance.Storage {
		entries = append(entries, &StateEntry{
			Key:        stateKeyString(entry.Key),
			Value:      scValToString(entry.Val),
			Operation:  operation,
			Durability: durability,
		})
	}
	return entries
}

func extractConstructorFromAuth(auth []xdr.SorobanAuthorizationEntry, contractID string) (string, []string) {
	for _, entry := range auth {
		if name, args, ok := findConstructorInvocation(entry.RootInvocation, contractID); ok {
			return name, args
		}
	}
	return "", nil
}

func findConstructorInvocation(inv xdr.SorobanAuthorizedInvocation, contractID string) (string, []string, bool) {
	if inv.Function.Type == xdr.SorobanAuthorizedFunctionTypeSorobanAuthorizedFunctionTypeContractFn {
		fn := inv.Function.MustContractFn()
		invokedContractID, err := strkey.Encode(strkey.VersionByteContract, fn.ContractAddress.ContractId[:])
		if err == nil && (contractID == "" || invokedContractID == contractID) {
			name := string(fn.FunctionName)
			if name == "__constructor" {
				args := make([]string, 0, len(fn.Args))
				for _, arg := range fn.Args {
					args = append(args, scValToString(arg))
				}
				return name, args, true
			}
		}
	}
	for _, sub := range inv.SubInvocations {
		if name, args, ok := findConstructorInvocation(sub, contractID); ok {
			return name, args, true
		}
	}
	return "", nil, false
}

func looksLikeNFTConstructor(args []string) bool {
	if len(args) < 3 {
		return false
	}
	if !looksLikeAddress(args[0]) {
		return false
	}
	if looksLikeAddress(args[1]) || looksLikeAddress(args[2]) {
		return false
	}
	return looksLikeDisplayString(args[1]) && looksLikeSymbolString(args[2])
}

func uniqueStrings(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
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

func scAddressToString(addr xdr.ScAddress) string {
	switch addr.Type {
	case xdr.ScAddressTypeScAddressTypeAccount:
		return addr.MustAccountId().Address()
	case xdr.ScAddressTypeScAddressTypeContract:
		cid := addr.MustContractId()
		encoded, err := strkey.Encode(strkey.VersionByteContract, cid[:])
		if err == nil {
			return encoded
		}
	}
	return ""
}

func looksLikeAddress(v string) bool {
	if len(v) < 2 {
		return false
	}
	return strings.HasPrefix(v, "G") || strings.HasPrefix(v, "C") || strings.HasPrefix(v, "M")
}

func looksLikeDisplayString(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" || looksLikeAddress(v) || len(v) > 80 {
		return false
	}
	if isLikelyHexBlob(v) || isNumericString(v) {
		return false
	}
	return true
}

func looksLikeSymbolString(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" || len(v) > 16 || looksLikeAddress(v) || isLikelyHexBlob(v) || isNumericString(v) {
		return false
	}
	startsWithLetter := false
	for i, r := range v {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			if i == 0 {
				startsWithLetter = true
			}
			continue
		}
		if (r >= '0' && r <= '9') || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return startsWithLetter
}

func isNumericString(v string) bool {
	if v == "" {
		return false
	}
	for _, r := range v {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func isLikelyHexBlob(v string) bool {
	trimmed := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(v)), "0x")
	if len(trimmed) < 16 {
		return false
	}
	for _, r := range trimmed {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') {
			continue
		}
		return false
	}
	return true
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
		return scAddressToString(val.MustAddress())
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
	case xdr.ScValTypeScvLedgerKeyContractInstance:
		return "LedgerKeyContractInstance"
	case xdr.ScValTypeScvLedgerKeyNonce:
		return "LedgerKeyNonce"
	case xdr.ScValTypeScvContractInstance:
		instance := val.MustInstance()
		kind, wasmHash := executableDetails(instance.Executable)
		if wasmHash != "" {
			return wasmHash
		}
		return kind
	}
	return val.Type.String()
}
