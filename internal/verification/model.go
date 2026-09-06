// Package verification owns M0.6 verification discovery, structured plans,
// deterministic execution, and normalized evidence.
package verification

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/project"
	"github.com/Eu-Pedro0ficial/praetor/internal/proposal"
	"github.com/Eu-Pedro0ficial/praetor/internal/source"
)

// VerificationAttemptId identifies one deterministic verification run.
type VerificationAttemptId string

// CandidateOrigin preserves the authority that proposed a verification step.
type CandidateOrigin string

const (
	OriginRepositoryDeclared        CandidateOrigin = "repository-declared"
	OriginDeterministicallyInferred CandidateOrigin = "deterministically-inferred"
	OriginAIAssisted                CandidateOrigin = "ai-assisted"
	OriginUserConfigured            CandidateOrigin = "user-configured"
)

// StepKind is the small Core V0 classification needed for readable evidence.
type StepKind string

const (
	KindBuild           StepKind = "build"
	KindTest            StepKind = "test"
	KindLint            StepKind = "lint"
	KindTypeCheck       StepKind = "typecheck"
	KindRepositoryCheck StepKind = "repository-check"
	KindPatchIntegrity  StepKind = "patch-integrity"
)

// RepositoryEvidence is bounded repository content considered by discovery
// and, only when needed, the read-only AI planner.
type RepositoryEvidence struct {
	path    source.RepositoryPath
	kind    string
	content string
}

func newRepositoryEvidence(pathValue string, kind string, content string) (RepositoryEvidence, error) {
	repositoryPath, err := source.NormalizeRepositoryPath(pathValue)
	if err != nil {
		return RepositoryEvidence{}, err
	}
	if strings.TrimSpace(kind) == "" || strings.TrimSpace(kind) != kind || len(kind) > 64 {
		return RepositoryEvidence{}, fmt.Errorf("repository evidence kind is invalid")
	}
	return RepositoryEvidence{path: repositoryPath, kind: kind, content: content}, nil
}

func (evidence RepositoryEvidence) Path() source.RepositoryPath { return evidence.path }
func (evidence RepositoryEvidence) Kind() string                { return evidence.kind }
func (evidence RepositoryEvidence) Content() string             { return evidence.content }

// VerificationCandidate is one provenance-bearing proposed deterministic
// command. It is runtime data, not a persistent plan representation.
type VerificationCandidate struct {
	id                 string
	kind               StepKind
	executable         string
	arguments          []string
	workingDirectory   string
	origin             CandidateOrigin
	supportingEvidence []source.RepositoryPath
}

// NewCandidate validates one repository/user/AI-proposed direct invocation.
func NewCandidate(
	kind StepKind,
	executable string,
	arguments []string,
	workingDirectory string,
	origin CandidateOrigin,
	supportingEvidence []string,
) (VerificationCandidate, error) {
	if !isKnownStepKind(kind) || kind == KindPatchIntegrity {
		return VerificationCandidate{}, fmt.Errorf("unknown executable verification kind %q", kind)
	}
	if err := validateExecutable(executable); err != nil {
		return VerificationCandidate{}, err
	}
	validatedArguments, err := validateArguments(arguments)
	if err != nil {
		return VerificationCandidate{}, err
	}
	if err := validateVerificationInvocation(executable, validatedArguments); err != nil {
		return VerificationCandidate{}, err
	}
	validatedDirectory, err := normalizeWorkingDirectory(workingDirectory)
	if err != nil {
		return VerificationCandidate{}, err
	}
	if !isKnownOrigin(origin) {
		return VerificationCandidate{}, fmt.Errorf("unknown verification origin %q", origin)
	}
	validatedEvidence, err := normalizeEvidencePaths(supportingEvidence)
	if err != nil {
		return VerificationCandidate{}, err
	}
	if len(validatedEvidence) == 0 {
		return VerificationCandidate{}, fmt.Errorf("verification candidate requires supporting repository evidence")
	}
	candidate := VerificationCandidate{
		kind:               kind,
		executable:         executable,
		arguments:          validatedArguments,
		workingDirectory:   validatedDirectory,
		origin:             origin,
		supportingEvidence: validatedEvidence,
	}
	candidate.id = candidateIdentity(candidate)
	return candidate, nil
}

func (candidate VerificationCandidate) Id() string         { return candidate.id }
func (candidate VerificationCandidate) Kind() StepKind     { return candidate.kind }
func (candidate VerificationCandidate) Executable() string { return candidate.executable }
func (candidate VerificationCandidate) Arguments() []string {
	return append([]string(nil), candidate.arguments...)
}
func (candidate VerificationCandidate) WorkingDirectory() string { return candidate.workingDirectory }
func (candidate VerificationCandidate) Origin() CandidateOrigin  { return candidate.origin }
func (candidate VerificationCandidate) SupportingEvidence() []source.RepositoryPath {
	return append([]source.RepositoryPath(nil), candidate.supportingEvidence...)
}

// VerificationStep is one required, validated direct process invocation.
type VerificationStep struct {
	id        string
	candidate VerificationCandidate
	timeout   time.Duration
}

func (step VerificationStep) Id() string               { return step.id }
func (step VerificationStep) Kind() StepKind           { return step.candidate.Kind() }
func (step VerificationStep) Executable() string       { return step.candidate.Executable() }
func (step VerificationStep) Arguments() []string      { return step.candidate.Arguments() }
func (step VerificationStep) WorkingDirectory() string { return step.candidate.WorkingDirectory() }
func (step VerificationStep) Origin() CandidateOrigin  { return step.candidate.Origin() }
func (step VerificationStep) Timeout() time.Duration   { return step.timeout }
func (step VerificationStep) SupportingEvidence() []source.RepositoryPath {
	return step.candidate.SupportingEvidence()
}

// VerificationPlan is an immutable ordered set of required Core V0 steps.
type VerificationPlan struct {
	steps []VerificationStep
}

// BuildPlan normalizes deterministic and AI-assisted candidates. A
// deterministic candidate of the same kind always suppresses an AI candidate.
func BuildPlan(
	deterministic []VerificationCandidate,
	assisted []VerificationCandidate,
	stepTimeout time.Duration,
) (VerificationPlan, error) {
	if stepTimeout <= 0 || stepTimeout > 30*time.Minute {
		return VerificationPlan{}, fmt.Errorf("verification step timeout must be between zero and 30 minutes")
	}
	for _, candidate := range deterministic {
		if candidate.id == "" || candidate.origin == OriginAIAssisted {
			return VerificationPlan{}, fmt.Errorf("deterministic candidate provenance is invalid")
		}
	}
	for _, candidate := range assisted {
		if candidate.id == "" || candidate.origin != OriginAIAssisted {
			return VerificationPlan{}, fmt.Errorf("AI-assisted candidate provenance is invalid")
		}
	}

	deterministicKinds := make(map[StepKind]struct{}, len(deterministic))
	selected := make(map[string]VerificationCandidate, len(deterministic)+len(assisted))
	for _, candidate := range deterministic {
		deterministicKinds[candidate.kind] = struct{}{}
		selected[candidate.id] = candidate
	}
	for _, candidate := range assisted {
		if _, authoritative := deterministicKinds[candidate.kind]; authoritative {
			continue
		}
		selected[candidate.id] = candidate
	}
	if len(selected) == 0 {
		return VerificationPlan{}, fmt.Errorf("no viable deterministic verification was discovered")
	}

	candidates := make([]VerificationCandidate, 0, len(selected))
	for _, candidate := range selected {
		candidates = append(candidates, candidate)
	}
	sort.Slice(candidates, func(left int, right int) bool {
		leftOrder, rightOrder := stepKindOrder(candidates[left].kind), stepKindOrder(candidates[right].kind)
		if leftOrder != rightOrder {
			return leftOrder < rightOrder
		}
		if candidates[left].origin != candidates[right].origin {
			return originOrder(candidates[left].origin) < originOrder(candidates[right].origin)
		}
		return candidates[left].id < candidates[right].id
	})
	steps := make([]VerificationStep, len(candidates))
	for index, candidate := range candidates {
		steps[index] = VerificationStep{
			id:        fmt.Sprintf("step-%03d-%s", index+1, strings.TrimPrefix(candidate.id, "candidate-")),
			candidate: candidate,
			timeout:   stepTimeout,
		}
	}
	return VerificationPlan{steps: steps}, nil
}

func (plan VerificationPlan) Steps() []VerificationStep {
	return append([]VerificationStep(nil), plan.steps...)
}

// DiscoveryResult retains deterministic candidates, bounded evidence, and
// whether ambiguity merits optional AI assistance.
type DiscoveryResult struct {
	candidates    []VerificationCandidate
	evidence      []RepositoryEvidence
	needsPlanning bool
	issues        []string
}

func newDiscoveryResult(
	candidates []VerificationCandidate,
	evidence []RepositoryEvidence,
	needsPlanning bool,
	issues []string,
) DiscoveryResult {
	return DiscoveryResult{
		candidates:    append([]VerificationCandidate(nil), candidates...),
		evidence:      append([]RepositoryEvidence(nil), evidence...),
		needsPlanning: needsPlanning,
		issues:        append([]string(nil), issues...),
	}
}

func (result DiscoveryResult) Candidates() []VerificationCandidate {
	return append([]VerificationCandidate(nil), result.candidates...)
}
func (result DiscoveryResult) Evidence() []RepositoryEvidence {
	return append([]RepositoryEvidence(nil), result.evidence...)
}
func (result DiscoveryResult) NeedsPlanning() bool { return result.needsPlanning }
func (result DiscoveryResult) Issues() []string    { return append([]string(nil), result.issues...) }

// PlanningRequest is the bounded provider-independent input to optional AI
// verification discovery.
type PlanningRequest struct {
	projectId    project.ProjectId
	changeId     change.ChangeId
	workspaceId  proposal.WorkspaceId
	baseRevision string
	sourceDigest source.SourceStateDigest
	patchDigest  string
	diffSummary  string
	changedPaths []string
	evidence     []RepositoryEvidence
}

func newPlanningRequest(currentChange change.Change, currentProposal proposal.Proposal, discovery DiscoveryResult) (PlanningRequest, error) {
	artifact, hasArtifact := currentProposal.PatchArtifact()
	if !hasArtifact {
		return PlanningRequest{}, fmt.Errorf("verification planning requires a PatchArtifact")
	}
	workspace := currentProposal.Workspace()
	if currentChange.ProjectId() != workspace.ProjectId() || currentChange.ChangeId() != workspace.ChangeId() {
		return PlanningRequest{}, fmt.Errorf("verification planning Change and Proposal linkage is inconsistent")
	}
	return PlanningRequest{
		projectId:    currentChange.ProjectId(),
		changeId:     currentChange.ChangeId(),
		workspaceId:  workspace.WorkspaceId(),
		baseRevision: workspace.BaseRevision(),
		sourceDigest: workspace.SourceStateDigest(),
		patchDigest:  artifact.PatchDigest(),
		diffSummary:  artifact.DiffSummary(),
		changedPaths: artifact.ChangedPaths(),
		evidence:     discovery.Evidence(),
	}, nil
}

func (request PlanningRequest) ProjectId() project.ProjectId      { return request.projectId }
func (request PlanningRequest) ChangeId() change.ChangeId         { return request.changeId }
func (request PlanningRequest) WorkspaceId() proposal.WorkspaceId { return request.workspaceId }
func (request PlanningRequest) BaseRevision() string              { return request.baseRevision }
func (request PlanningRequest) SourceStateDigest() source.SourceStateDigest {
	return request.sourceDigest
}
func (request PlanningRequest) PatchDigest() string { return request.patchDigest }
func (request PlanningRequest) DiffSummary() string { return request.diffSummary }
func (request PlanningRequest) ChangedPaths() []string {
	return append([]string(nil), request.changedPaths...)
}
func (request PlanningRequest) Evidence() []RepositoryEvidence {
	return append([]RepositoryEvidence(nil), request.evidence...)
}

// PlanningResult is non-deterministic provenance returned by one fresh AI
// planning attempt. Its candidates still require plan validation.
type PlanningResult struct {
	executionAttemptId string
	provider           string
	model              string
	candidates         []VerificationCandidate
}

func NewPlanningResult(executionAttemptId, provider, model string, candidates []VerificationCandidate) (PlanningResult, error) {
	if strings.TrimSpace(executionAttemptId) == "" || strings.TrimSpace(provider) == "" {
		return PlanningResult{}, fmt.Errorf("planning attempt and provider identity are required")
	}
	for _, candidate := range candidates {
		if candidate.origin != OriginAIAssisted {
			return PlanningResult{}, fmt.Errorf("planning result contains non-AI candidate")
		}
	}
	return PlanningResult{
		executionAttemptId: executionAttemptId,
		provider:           provider,
		model:              model,
		candidates:         append([]VerificationCandidate(nil), candidates...),
	}, nil
}

func (result PlanningResult) ExecutionAttemptId() string { return result.executionAttemptId }
func (result PlanningResult) Provider() string           { return result.provider }
func (result PlanningResult) Model() string              { return result.model }
func (result PlanningResult) Candidates() []VerificationCandidate {
	return append([]VerificationCandidate(nil), result.candidates...)
}

// ExecutionOutcome is one normalized deterministic step result.
type ExecutionOutcome string

const (
	OutcomePass           ExecutionOutcome = "PASS"
	OutcomeFail           ExecutionOutcome = "FAIL"
	OutcomeTimeout        ExecutionOutcome = "TIMEOUT"
	OutcomeCancelled      ExecutionOutcome = "CANCELLED"
	OutcomeExecutionError ExecutionOutcome = "EXECUTION_ERROR"
)

// Evidence is immutable normalized output from one actual verification step.
type Evidence struct {
	stepId             string
	kind               StepKind
	executable         string
	arguments          []string
	workingDirectory   string
	resolvedExecutable string
	origin             CandidateOrigin
	supportingEvidence []source.RepositoryPath
	startedAt          time.Time
	completedAt        time.Time
	exitCode           int
	hasExitCode        bool
	outcome            ExecutionOutcome
	standardOutput     string
	standardError      string
	outputTruncated    bool
}

func (evidence Evidence) StepId() string             { return evidence.stepId }
func (evidence Evidence) Kind() StepKind             { return evidence.kind }
func (evidence Evidence) Executable() string         { return evidence.executable }
func (evidence Evidence) Arguments() []string        { return append([]string(nil), evidence.arguments...) }
func (evidence Evidence) WorkingDirectory() string   { return evidence.workingDirectory }
func (evidence Evidence) ResolvedExecutable() string { return evidence.resolvedExecutable }
func (evidence Evidence) Origin() CandidateOrigin    { return evidence.origin }
func (evidence Evidence) SupportingEvidence() []source.RepositoryPath {
	return append([]source.RepositoryPath(nil), evidence.supportingEvidence...)
}
func (evidence Evidence) StartedAt() time.Time   { return evidence.startedAt }
func (evidence Evidence) CompletedAt() time.Time { return evidence.completedAt }
func (evidence Evidence) Duration() time.Duration {
	return evidence.completedAt.Sub(evidence.startedAt)
}
func (evidence Evidence) ExitCode() (int, bool)     { return evidence.exitCode, evidence.hasExitCode }
func (evidence Evidence) Outcome() ExecutionOutcome { return evidence.outcome }
func (evidence Evidence) StandardOutput() string    { return evidence.standardOutput }
func (evidence Evidence) StandardError() string     { return evidence.standardError }
func (evidence Evidence) OutputTruncated() bool     { return evidence.outputTruncated }

// EvidenceSet is immutable authoritative evidence for one M0.6 attempt.
type EvidenceSet struct {
	id                    string
	verificationAttemptId VerificationAttemptId
	projectId             project.ProjectId
	changeId              change.ChangeId
	workspaceId           proposal.WorkspaceId
	patchDigest           string
	sourceDigest          source.SourceStateDigest
	evidence              []Evidence
	passed                bool
}

func (set EvidenceSet) Id() string { return set.id }
func (set EvidenceSet) VerificationAttemptId() VerificationAttemptId {
	return set.verificationAttemptId
}
func (set EvidenceSet) ProjectId() project.ProjectId                { return set.projectId }
func (set EvidenceSet) ChangeId() change.ChangeId                   { return set.changeId }
func (set EvidenceSet) WorkspaceId() proposal.WorkspaceId           { return set.workspaceId }
func (set EvidenceSet) PatchDigest() string                         { return set.patchDigest }
func (set EvidenceSet) SourceStateDigest() source.SourceStateDigest { return set.sourceDigest }
func (set EvidenceSet) Evidence() []Evidence                        { return cloneEvidenceSlice(set.evidence) }
func (set EvidenceSet) Passed() bool                                { return set.passed }

func cloneEvidenceSlice(values []Evidence) []Evidence {
	result := make([]Evidence, len(values))
	for index, value := range values {
		value.arguments = append([]string(nil), value.arguments...)
		value.supportingEvidence = append([]source.RepositoryPath(nil), value.supportingEvidence...)
		result[index] = value
	}
	return result
}

// Result is one complete M0.6 run. Planner provenance never counts as its
// deterministic evidence.
type Result struct {
	attemptId       VerificationAttemptId
	discovery       DiscoveryResult
	plan            VerificationPlan
	planning        PlanningResult
	planningUsed    bool
	planningFailure string
	evidence        EvidenceSet
}

func (result Result) AttemptId() VerificationAttemptId { return result.attemptId }
func (result Result) Discovery() DiscoveryResult       { return result.discovery }
func (result Result) Plan() VerificationPlan           { return result.plan }
func (result Result) PlanningResult() (PlanningResult, bool) {
	return result.planning, result.planningUsed
}
func (result Result) PlanningFailure() string  { return result.planningFailure }
func (result Result) EvidenceSet() EvidenceSet { return result.evidence }
func (result Result) Passed() bool             { return result.evidence.Passed() }

// ValidateResultForDecision proves that one successful deterministic result
// still identifies the exact retained Proposal and Change presented to the
// M0.7 human-decision gate. It performs no source inspection itself.
func ValidateResultForDecision(
	currentChange change.Change,
	currentProposal proposal.Proposal,
	result Result,
) error {
	workspace := currentProposal.Workspace()
	artifact, hasArtifact := currentProposal.PatchArtifact()
	if workspace.State() != proposal.WorkspaceRetained || !hasArtifact {
		return fmt.Errorf("human decision requires a retained Proposal and PatchArtifact")
	}
	if currentChange.ProjectId() != workspace.ProjectId() ||
		currentChange.ChangeId() != workspace.ChangeId() ||
		artifact.ProjectId() != workspace.ProjectId() ||
		artifact.ChangeId() != workspace.ChangeId() ||
		artifact.WorkspaceId() != workspace.WorkspaceId() {
		return fmt.Errorf("human decision Change, Proposal, and PatchArtifact linkage is inconsistent")
	}
	if result.attemptId == "" || result.evidence.Id() == "" {
		return fmt.Errorf("human decision requires a completed deterministic EvidenceSet")
	}
	if err := validateVerificationAttemptId(result.attemptId); err != nil {
		return fmt.Errorf("human decision VerificationAttempt linkage is invalid: %w", err)
	}
	evidenceSet := result.evidence
	if evidenceSet.VerificationAttemptId() != result.attemptId {
		return fmt.Errorf("human decision VerificationAttempt linkage is inconsistent")
	}
	if !evidenceSet.Passed() {
		return fmt.Errorf("human decision requires a passing deterministic EvidenceSet")
	}
	if evidenceSet.ProjectId() != currentChange.ProjectId() ||
		evidenceSet.ChangeId() != currentChange.ChangeId() ||
		evidenceSet.WorkspaceId() != workspace.WorkspaceId() {
		return fmt.Errorf("human decision EvidenceSet identity linkage is inconsistent")
	}
	if evidenceSet.PatchDigest() != artifact.PatchDigest() {
		return fmt.Errorf("human decision EvidenceSet patch digest does not match the retained PatchArtifact")
	}
	if evidenceSet.SourceStateDigest() != artifact.SourceStateDigest() ||
		evidenceSet.SourceStateDigest() != workspace.SourceStateDigest() {
		return fmt.Errorf("human decision EvidenceSet source-state linkage is inconsistent")
	}
	steps := result.plan.Steps()
	evidence := evidenceSet.Evidence()
	if len(steps) == 0 || len(evidence) != len(steps)+1 {
		return fmt.Errorf("human decision EvidenceSet is incomplete for its VerificationPlan")
	}
	patchIntegrityCount := 0
	for _, item := range evidence {
		if item.Outcome() != OutcomePass {
			return fmt.Errorf("human decision EvidenceSet contains non-passing outcome %q", item.Outcome())
		}
		if item.Kind() == KindPatchIntegrity {
			patchIntegrityCount++
		}
	}
	if patchIntegrityCount != 1 {
		return fmt.Errorf("human decision EvidenceSet requires exactly one passing patch-integrity result")
	}
	return nil
}

// AttemptIdGenerator creates opaque Praetor-owned verification identity.
type AttemptIdGenerator func() (VerificationAttemptId, error)

// GenerateVerificationAttemptId creates random 128-bit attempt identity.
func GenerateVerificationAttemptId() (VerificationAttemptId, error) {
	var randomBytes [16]byte
	if _, err := rand.Read(randomBytes[:]); err != nil {
		return "", fmt.Errorf("generate VerificationAttemptId: %w", err)
	}
	return VerificationAttemptId("verification-" + hex.EncodeToString(randomBytes[:])), nil
}

func candidateIdentity(candidate VerificationCandidate) string {
	value := strings.Join([]string{
		string(candidate.kind),
		candidate.executable,
		strings.Join(candidate.arguments, "\x00"),
		candidate.workingDirectory,
		string(candidate.origin),
	}, "\x1f")
	digest := sha256.Sum256([]byte(value))
	return "candidate-" + hex.EncodeToString(digest[:8])
}

func validateExecutable(value string) error {
	if value == "" || strings.TrimSpace(value) != value || len(value) > 256 || !utf8.ValidString(value) {
		return fmt.Errorf("verification executable is invalid")
	}
	if strings.ContainsAny(value, `/\\`) || strings.HasPrefix(value, "-") {
		return fmt.Errorf("verification executable must be a simple program name")
	}
	for _, character := range value {
		if unicode.IsControl(character) || unicode.IsSpace(character) || strings.ContainsRune(";&|`$<>(){}[]!", character) {
			return fmt.Errorf("verification executable contains unsafe syntax")
		}
	}
	switch strings.ToLower(value) {
	case "sh", "bash", "zsh", "fish", "cmd", "cmd.exe", "powershell", "pwsh", "eval", "env", "sudo", "curl", "wget":
		return fmt.Errorf("verification executable %q is not admitted by the M0.6 direct-process gate", value)
	}
	return nil
}

func validateVerificationInvocation(executable string, arguments []string) error {
	requireExact := func(allowed ...[]string) error {
		for _, candidate := range allowed {
			if equalStringSlices(arguments, candidate) {
				return nil
			}
		}
		return fmt.Errorf("verification invocation for %q is not an admitted direct-process template", executable)
	}
	requireNamedTarget := func(prefix string) error {
		if len(arguments) != 2 || arguments[0] != prefix || !isSafeToolTarget(arguments[1]) {
			return fmt.Errorf("verification invocation for %q requires %s plus one safe repository target", executable, prefix)
		}
		return nil
	}
	switch strings.ToLower(executable) {
	case "go":
		return requireExact(
			[]string{"test", "./..."},
			[]string{"build", "./..."},
			[]string{"vet", "./..."},
		)
	case "npm", "pnpm", "yarn":
		if equalStringSlices(arguments, []string{"test"}) {
			return nil
		}
		return requireNamedTarget("run")
	case "make", "task":
		if len(arguments) != 1 || !isSafeToolTarget(arguments[0]) {
			return fmt.Errorf("verification invocation for %q requires one safe repository target", executable)
		}
		return nil
	case "cargo":
		return requireExact([]string{"build"}, []string{"test"}, []string{"check"}, []string{"clippy"})
	case "mvn":
		return requireExact([]string{"test"}, []string{"verify"}, []string{"package"})
	case "gradle":
		return requireExact([]string{"test"}, []string{"check"}, []string{"build"})
	case "dotnet":
		return requireExact([]string{"test"}, []string{"build"})
	case "python", "python3":
		return requireExact([]string{"-m", "pytest"})
	case "pytest", "phpunit", "ruff", "eslint", "tsc":
		return requireExact(nil)
	case "composer":
		return requireNamedTarget("run-script")
	default:
		return fmt.Errorf("verification executable %q is not in the M0.6 direct-process tool set", executable)
	}
}

func isSafeToolTarget(value string) bool {
	if value == "" || len(value) > 128 || strings.HasPrefix(value, "-") {
		return false
	}
	for _, character := range value {
		if !(unicode.IsLetter(character) || unicode.IsDigit(character) || strings.ContainsRune("._:-", character)) {
			return false
		}
	}
	return true
}

func equalStringSlices(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func validateArguments(values []string) ([]string, error) {
	if len(values) > 64 {
		return nil, fmt.Errorf("verification argument vector exceeds 64 entries")
	}
	totalBytes := 0
	result := make([]string, len(values))
	for index, value := range values {
		if len(value) > 4096 || !utf8.ValidString(value) {
			return nil, fmt.Errorf("verification argument %d is invalid", index)
		}
		totalBytes += len(value)
		if totalBytes > 32<<10 {
			return nil, fmt.Errorf("verification argument vector exceeds 32 KiB")
		}
		if strings.ContainsAny(value, "\x00\r\n") || containsShellSyntax(value) {
			return nil, fmt.Errorf("verification argument %d contains unsafe shell syntax", index)
		}
		result[index] = value
	}
	return result, nil
}

func containsShellSyntax(value string) bool {
	for _, syntax := range []string{"&&", "||", ";", "|", "`", "$(`", ">", "<"} {
		if strings.Contains(value, syntax) {
			return true
		}
	}
	return false
}

func normalizeWorkingDirectory(value string) (string, error) {
	if value == "" {
		value = "."
	}
	if strings.TrimSpace(value) != value || strings.Contains(value, `\`) || path.IsAbs(value) {
		return "", fmt.Errorf("verification working directory must be repository-relative")
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == ".." {
			return "", fmt.Errorf("verification working directory contains traversal")
		}
	}
	normalized := path.Clean(value)
	if normalized == ".." || strings.HasPrefix(normalized, "../") {
		return "", fmt.Errorf("verification working directory escapes ProposalWorkspace")
	}
	return normalized, nil
}

func normalizeEvidencePaths(values []string) ([]source.RepositoryPath, error) {
	unique := make(map[source.RepositoryPath]struct{}, len(values))
	for _, value := range values {
		repositoryPath, err := source.NormalizeRepositoryPath(value)
		if err != nil {
			return nil, fmt.Errorf("invalid supporting evidence path: %w", err)
		}
		unique[repositoryPath] = struct{}{}
	}
	result := make([]source.RepositoryPath, 0, len(unique))
	for repositoryPath := range unique {
		result = append(result, repositoryPath)
	}
	sort.Slice(result, func(left, right int) bool { return result[left] < result[right] })
	return result, nil
}

func isKnownOrigin(origin CandidateOrigin) bool {
	switch origin {
	case OriginRepositoryDeclared, OriginDeterministicallyInferred, OriginAIAssisted, OriginUserConfigured:
		return true
	default:
		return false
	}
}

func isKnownStepKind(kind StepKind) bool {
	switch kind {
	case KindBuild, KindTest, KindLint, KindTypeCheck, KindRepositoryCheck, KindPatchIntegrity:
		return true
	default:
		return false
	}
}

func stepKindOrder(kind StepKind) int {
	switch kind {
	case KindBuild:
		return 0
	case KindTypeCheck:
		return 1
	case KindLint:
		return 2
	case KindTest:
		return 3
	case KindRepositoryCheck:
		return 4
	default:
		return 5
	}
}

func originOrder(origin CandidateOrigin) int {
	switch origin {
	case OriginUserConfigured:
		return 0
	case OriginRepositoryDeclared:
		return 1
	case OriginDeterministicallyInferred:
		return 2
	default:
		return 3
	}
}
