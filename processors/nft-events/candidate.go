package nft_events

type CandidateKind string

const (
	CandidateKindEvent      CandidateKind = "event"
	CandidateKindInvocation CandidateKind = "invocation"
	CandidateKindState      CandidateKind = "state"
)

type Candidate struct {
	Kind          CandidateKind
	ContractID    string
	ActionHint    string
	MethodName    string
	RawTopics     []string
	RawData       string
	KeyParts      []string
	ValueParts    []string
	TokenID       string
	From          string
	To            string
	Owner         string
	Approved      string
	Operator      string
	MetadataURI   string
	MetadataStore string
	StateKey      string
	StateValue    string
	Durability    string
}
