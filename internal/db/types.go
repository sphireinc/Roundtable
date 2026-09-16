package db

type Run struct {
	ID           string
	Goal         string
	Status       string
	StartedAt    string
	EndedAt      string
	MetadataJSON string
}

type Workspace struct {
	ID                          string
	DisplayName                 string
	RootAlias                   string
	CanonicalRepositoryIdentity string
	Status                      string
	DefaultBranch               string
	RootPath                    string
	CreatedAt                   string
	LastOpenedAt                string
	UpdatedAt                   string
}

type Event struct {
	ID          int64
	RunID       string
	Type        string
	ActorID     string
	TaskID      string
	PayloadJSON string
	CreatedAt   string
}

type Agent struct {
	ID        string
	Role      string
	Name      string
	Adapter   string
	Command   string
	Status    string
	IsEnabled bool
	CreatedAt string
}

type AgentSession struct {
	ID                    string
	AgentID               string
	RunID                 string
	Adapter               string
	Provider              string
	Model                 string
	ExternalSessionID     string
	ExternalResumeCommand string
	WorkingDirectory      string
	MCPSocket             string
	Status                string
	StartedAt             string
	LastSeenAt            string
	EndedAt               string
	MetadataJSON          string
}

type AgentSessionEvent struct {
	ID          int64
	SessionID   string
	EventType   string
	PayloadJSON string
	CreatedAt   string
}

type AdapterCapability struct {
	Adapter                   string
	SupportsResume            bool
	SupportsMCP               bool
	SupportsReadOnlyWorkspace bool
	SupportsSessionCapture    bool
	MetadataJSON              string
}

type RunSnapshot struct {
	ID           string
	RunID        string
	SnapshotJSON string
	CreatedAt    string
}

type Task struct {
	ID              string
	Title           string
	BodyMD          string
	Status          string
	Priority        int
	Risk            string
	AssignedAgentID string
	CreatedAt       string
	UpdatedAt       string
}

type Resource struct {
	ID           string
	Type         string
	Path         string
	Symbol       string
	Language     string
	StartLine    int
	EndLine      int
	CurrentHash  string
	MetadataJSON string
}

type Claim struct {
	ID           string
	ResourceID   string
	AgentID      string
	TaskID       string
	ClaimType    string
	BaseHash     string
	Status       string
	ExpiresAt    string
	HeartbeatAt  string
	Renewable    bool
	ResumePolicy string
	RationaleMD  string
	CreatedAt    string
}

type Proposal struct {
	ID        string
	TaskID    string
	AgentID   string
	Title     string
	SummaryMD string
	PatchPath string
	Status    string
	Risk      string
	CreatedAt string
}

type ProposalResource struct {
	ProposalID string
	ResourceID string
}

type Vote struct {
	ID         string
	ProposalID string
	AgentID    string
	Vote       string
	Confidence float64
	ReasonMD   string
	CreatedAt  string
}

type Decision struct {
	ID          string
	ProposalID  string
	TaskID      string
	Decision    string
	RationaleMD string
	DecidedBy   string
	CreatedAt   string
}

type Transaction struct {
	ID                string
	ProposalID        string
	RunID             string
	BeforeGitHash     string
	AfterGitHash      string
	Status            string
	AppliedBy         string
	AppliedAt         string
	RollbackPatchPath string
	MetadataJSON      string
}

type HumanApproval struct {
	ID             string
	ProposalID     string
	TaskID         string
	Subject        string
	ReasonMD       string
	Status         string
	RequestedBy    string
	DecisionMD     string
	DecidedBy      string
	OverridePolicy bool
	CreatedAt      string
	UpdatedAt      string
}

type SecurityReview struct {
	ID           string
	ProposalID   string
	ResourceID   string
	TaskID       string
	ReviewerID   string
	Status       string
	SummaryMD    string
	FindingsJSON string
	CreatedAt    string
	UpdatedAt    string
}

type TestRun struct {
	ID         string
	ProposalID string
	TaskID     string
	Command    string
	Status     string
	SummaryMD  string
	LogPath    string
	CreatedAt  string
}

type MemoryEntry struct {
	ID            string
	Scope         string
	Kind          string
	Title         string
	BodyMD        string
	SourceEventID int64
	Importance    int
	Status        string
	CreatedAt     string
	UpdatedAt     string
}
