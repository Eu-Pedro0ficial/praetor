package verification

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Eu-Pedro0ficial/praetor/internal/proposal"
	"github.com/Eu-Pedro0ficial/praetor/internal/source"
)

// ProcessInvocation is one already-validated direct process request.
type ProcessInvocation struct {
	ResolvedExecutable string
	Arguments          []string
	Directory          string
	WorkspaceRoot      string
	CanonicalRoot      string
}

// ProcessResult is bounded raw adapter output normalized by the engine.
type ProcessResult struct {
	exitCode        int
	hasExitCode     bool
	standardOutput  []byte
	standardError   []byte
	outputTruncated bool
}

// NewProcessResult constructs bounded process adapter output.
func NewProcessResult(
	exitCode int,
	hasExitCode bool,
	standardOutput []byte,
	standardError []byte,
	outputTruncated bool,
) ProcessResult {
	return ProcessResult{
		exitCode:        exitCode,
		hasExitCode:     hasExitCode,
		standardOutput:  append([]byte(nil), standardOutput...),
		standardError:   append([]byte(nil), standardError...),
		outputTruncated: outputTruncated,
	}
}

func (result ProcessResult) ExitCode() (int, bool) { return result.exitCode, result.hasExitCode }
func (result ProcessResult) StandardOutput() []byte {
	return append([]byte(nil), result.standardOutput...)
}
func (result ProcessResult) StandardError() []byte {
	return append([]byte(nil), result.standardError...)
}
func (result ProcessResult) OutputTruncated() bool { return result.outputTruncated }

// StepRunner is the direct-process Verification Port. Resolve is called for
// every plan step before any child process is started.
type StepRunner interface {
	Resolve(string) (string, error)
	Run(context.Context, ProcessInvocation) (ProcessResult, error)
	MaximumOutputBytes() int
}

// IntegrityVerifier reuses the M0.4/M0.5 proposal/source guard boundary.
type IntegrityVerifier func(proposal.Proposal) error

// Clock supplies deterministic evidence timestamps.
type Clock func() time.Time

// Engine validates and executes one required plan sequentially. It collects
// complete safe evidence unless caller cancellation prevents later steps.
type Engine struct {
	runner    StepRunner
	integrity IntegrityVerifier
	clock     Clock
}

// NewEngine constructs the deterministic execution boundary.
func NewEngine(runner StepRunner, integrity IntegrityVerifier, clock Clock) (*Engine, error) {
	if runner == nil {
		return nil, fmt.Errorf("verification step runner dependency is not configured")
	}
	if runner.MaximumOutputBytes() <= 0 || runner.MaximumOutputBytes() > 1<<20 {
		return nil, fmt.Errorf("verification output limit must be between zero and 1 MiB")
	}
	if integrity == nil {
		return nil, fmt.Errorf("proposal integrity dependency is not configured")
	}
	if clock == nil {
		return nil, fmt.Errorf("verification clock dependency is not configured")
	}
	return &Engine{runner: runner, integrity: integrity, clock: clock}, nil
}

type preparedStep struct {
	step               VerificationStep
	directory          string
	resolvedExecutable string
	workspaceRoot      string
	canonicalRoot      string
}

// VerifyIntegrity exposes the same deterministic guard used before and after
// discovery/planning/execution.
func (engine *Engine) VerifyIntegrity(currentProposal proposal.Proposal) error {
	if engine == nil || engine.integrity == nil {
		return fmt.Errorf("verification engine is not configured")
	}
	return engine.integrity(currentProposal)
}

// Execute validates the complete plan before running any process, then emits
// one immutable Evidence value per actual step plus patch integrity evidence.
func (engine *Engine) Execute(
	ctx context.Context,
	plan VerificationPlan,
	currentProposal proposal.Proposal,
) ([]Evidence, error) {
	if engine == nil {
		return nil, fmt.Errorf("verification engine is required")
	}
	if ctx == nil {
		return nil, fmt.Errorf("verification context is required")
	}
	if err := engine.VerifyIntegrity(currentProposal); err != nil {
		return nil, fmt.Errorf("pre-execution patch integrity failed: %w", err)
	}
	prepared, err := engine.prepare(plan, currentProposal)
	if err != nil {
		return nil, err
	}
	evidence := make([]Evidence, 0, len(prepared)+1)
	for _, preparedValue := range prepared {
		if ctx.Err() != nil {
			break
		}
		evidence = append(evidence, engine.executeStep(ctx, preparedValue))
	}
	evidence = append(evidence, engine.executePatchIntegrity(currentProposal))
	return evidence, nil
}

func (engine *Engine) prepare(plan VerificationPlan, currentProposal proposal.Proposal) ([]preparedStep, error) {
	steps := plan.Steps()
	if len(steps) == 0 {
		return nil, fmt.Errorf("empty VerificationPlan cannot execute")
	}
	if len(steps) > 64 {
		return nil, fmt.Errorf("VerificationPlan exceeds 64 steps")
	}
	workspace := currentProposal.Workspace()
	artifact, hasArtifact := currentProposal.PatchArtifact()
	if workspace.State() != proposal.WorkspaceRetained || !hasArtifact ||
		artifact.WorkspaceId() != workspace.WorkspaceId() ||
		artifact.ProjectId() != workspace.ProjectId() ||
		artifact.ChangeId() != workspace.ChangeId() {
		return nil, fmt.Errorf("verification requires a linked retained PatchArtifact")
	}
	workspaceRoot, err := resolveExistingDirectory(workspace.Root(), "ProposalWorkspace")
	if err != nil {
		return nil, err
	}
	canonicalRoot, err := resolveExistingDirectory(workspace.CanonicalRoot(), "canonical repository")
	if err != nil {
		return nil, err
	}
	seenIdentities := make(map[string]struct{}, len(steps))
	prepared := make([]preparedStep, 0, len(steps))
	for _, step := range steps {
		if strings.TrimSpace(step.Id()) == "" {
			return nil, fmt.Errorf("VerificationStep identity is required")
		}
		if _, duplicate := seenIdentities[step.Id()]; duplicate {
			return nil, fmt.Errorf("duplicate VerificationStep identity %q", step.Id())
		}
		seenIdentities[step.Id()] = struct{}{}
		if step.Timeout() <= 0 || step.Timeout() > 30*time.Minute {
			return nil, fmt.Errorf("VerificationStep %q has invalid timeout", step.Id())
		}
		directory, err := resolveStepDirectory(workspaceRoot, step.WorkingDirectory())
		if err != nil {
			return nil, fmt.Errorf("VerificationStep %q: %w", step.Id(), err)
		}
		insideCanonical, err := pathWithin(canonicalRoot, directory)
		if err != nil {
			return nil, err
		}
		if insideCanonical {
			return nil, fmt.Errorf("VerificationStep %q targets canonical source", step.Id())
		}
		if err := validateSupportingEvidence(workspaceRoot, step.SupportingEvidence()); err != nil {
			return nil, fmt.Errorf("VerificationStep %q: %w", step.Id(), err)
		}
		resolvedExecutable, err := engine.runner.Resolve(step.Executable())
		if err != nil {
			return nil, fmt.Errorf("resolve VerificationStep %q executable %q: %w", step.Id(), step.Executable(), err)
		}
		if strings.TrimSpace(resolvedExecutable) == "" {
			return nil, fmt.Errorf("VerificationStep %q executable resolved empty", step.Id())
		}
		resolvedPath, err := filepath.Abs(resolvedExecutable)
		if err != nil {
			return nil, fmt.Errorf("normalize VerificationStep %q executable: %w", step.Id(), err)
		}
		executableSelectedFromWorkspace, err := pathWithin(workspaceRoot, resolvedPath)
		if err != nil {
			return nil, err
		}
		if executableSelectedFromWorkspace {
			return nil, fmt.Errorf("VerificationStep %q executable was selected from ProposalWorkspace", step.Id())
		}
		executableSelectedFromCanonical, err := pathWithin(canonicalRoot, resolvedPath)
		if err != nil {
			return nil, err
		}
		if executableSelectedFromCanonical {
			return nil, fmt.Errorf("VerificationStep %q executable was selected from canonical source", step.Id())
		}
		resolvedPath, err = filepath.EvalSymlinks(resolvedPath)
		if err != nil {
			return nil, fmt.Errorf("resolve VerificationStep %q executable symlinks: %w", step.Id(), err)
		}
		executableInfo, err := os.Stat(resolvedPath)
		if err != nil || !executableInfo.Mode().IsRegular() {
			return nil, fmt.Errorf("VerificationStep %q executable is not a regular file", step.Id())
		}
		executableInsideWorkspace, err := pathWithin(workspaceRoot, resolvedPath)
		if err != nil {
			return nil, err
		}
		if executableInsideWorkspace {
			return nil, fmt.Errorf("VerificationStep %q executable resolves inside ProposalWorkspace", step.Id())
		}
		executableInsideCanonical, err := pathWithin(canonicalRoot, resolvedPath)
		if err != nil {
			return nil, err
		}
		if executableInsideCanonical {
			return nil, fmt.Errorf("VerificationStep %q executable resolves inside canonical source", step.Id())
		}
		prepared = append(prepared, preparedStep{
			step:               step,
			directory:          directory,
			resolvedExecutable: resolvedPath,
			workspaceRoot:      workspaceRoot,
			canonicalRoot:      canonicalRoot,
		})
	}
	return prepared, nil
}

func (engine *Engine) executeStep(ctx context.Context, prepared preparedStep) Evidence {
	startedAt := engine.clock().UTC()
	stepContext, cancel := context.WithTimeout(ctx, prepared.step.Timeout())
	processResult, runError := engine.runner.Run(stepContext, ProcessInvocation{
		ResolvedExecutable: prepared.resolvedExecutable,
		Arguments:          prepared.step.Arguments(),
		Directory:          prepared.directory,
		WorkspaceRoot:      prepared.workspaceRoot,
		CanonicalRoot:      prepared.canonicalRoot,
	})
	stepContextError := stepContext.Err()
	cancel()
	completedAt := engine.clock().UTC()
	if completedAt.Before(startedAt) {
		completedAt = startedAt
	}

	outcome := OutcomePass
	exitCode, hasExitCode := processResult.ExitCode()
	switch {
	case errors.Is(ctx.Err(), context.Canceled):
		outcome = OutcomeCancelled
	case errors.Is(stepContextError, context.DeadlineExceeded):
		outcome = OutcomeTimeout
	case errors.Is(stepContextError, context.Canceled):
		outcome = OutcomeCancelled
	case runError != nil && hasExitCode:
		outcome = OutcomeFail
	case runError != nil:
		outcome = OutcomeExecutionError
	case !hasExitCode || exitCode != 0:
		outcome = OutcomeFail
	}
	standardOutput, outputTrimmed := sanitizeOutput(processResult.StandardOutput(), engine.runner.MaximumOutputBytes())
	standardError, errorTrimmed := sanitizeOutput(processResult.StandardError(), engine.runner.MaximumOutputBytes())
	return Evidence{
		stepId:             prepared.step.Id(),
		kind:               prepared.step.Kind(),
		executable:         prepared.step.Executable(),
		arguments:          prepared.step.Arguments(),
		workingDirectory:   prepared.step.WorkingDirectory(),
		resolvedExecutable: prepared.resolvedExecutable,
		origin:             prepared.step.Origin(),
		supportingEvidence: prepared.step.SupportingEvidence(),
		startedAt:          startedAt,
		completedAt:        completedAt,
		exitCode:           exitCode,
		hasExitCode:        hasExitCode,
		outcome:            outcome,
		standardOutput:     standardOutput,
		standardError:      standardError,
		outputTruncated:    processResult.OutputTruncated() || outputTrimmed || errorTrimmed,
	}
}

func (engine *Engine) executePatchIntegrity(currentProposal proposal.Proposal) Evidence {
	startedAt := engine.clock().UTC()
	err := engine.integrity(currentProposal)
	completedAt := engine.clock().UTC()
	if completedAt.Before(startedAt) {
		completedAt = startedAt
	}
	outcome := OutcomePass
	exitCode := 0
	standardError := ""
	if err != nil {
		outcome = OutcomeFail
		exitCode = 1
		standardError, _ = sanitizeOutput([]byte(err.Error()), engine.runner.MaximumOutputBytes())
	}
	artifact, _ := currentProposal.PatchArtifact()
	supportingEvidence := make([]source.RepositoryPath, 0, len(artifact.ChangedPaths()))
	for _, changedPath := range artifact.ChangedPaths() {
		if repositoryPath, pathError := source.NormalizeRepositoryPath(changedPath); pathError == nil {
			supportingEvidence = append(supportingEvidence, repositoryPath)
		}
	}
	return Evidence{
		stepId:             "step-patch-integrity",
		kind:               KindPatchIntegrity,
		executable:         "praetor-patch-integrity",
		workingDirectory:   ".",
		origin:             OriginDeterministicallyInferred,
		supportingEvidence: supportingEvidence,
		startedAt:          startedAt,
		completedAt:        completedAt,
		exitCode:           exitCode,
		hasExitCode:        true,
		outcome:            outcome,
		standardError:      standardError,
	}
}

func resolveExistingDirectory(value string, label string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%s path is required", label)
	}
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", label, err)
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve %s symlinks: %w", label, err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("inspect %s: %w", label, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", label)
	}
	return filepath.Clean(resolved), nil
}

func resolveStepDirectory(workspaceRoot string, relativeDirectory string) (string, error) {
	directory := filepath.Join(workspaceRoot, filepath.FromSlash(relativeDirectory))
	resolved, err := resolveExistingDirectory(directory, "verification working directory")
	if err != nil {
		return "", err
	}
	inside, err := pathWithin(workspaceRoot, resolved)
	if err != nil {
		return "", err
	}
	if !inside {
		return "", fmt.Errorf("verification working directory escapes ProposalWorkspace")
	}
	return resolved, nil
}

func validateSupportingEvidence(workspaceRoot string, paths []source.RepositoryPath) error {
	if len(paths) == 0 {
		return fmt.Errorf("supporting repository evidence is required")
	}
	for _, repositoryPath := range paths {
		candidate := filepath.Join(workspaceRoot, filepath.FromSlash(string(repositoryPath)))
		info, err := os.Lstat(candidate)
		if err != nil {
			return fmt.Errorf("supporting evidence %q is unavailable: %w", repositoryPath, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("supporting evidence %q is not a regular file", repositoryPath)
		}
		resolved, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			return fmt.Errorf("resolve supporting evidence %q: %w", repositoryPath, err)
		}
		inside, err := pathWithin(workspaceRoot, resolved)
		if err != nil {
			return err
		}
		if !inside {
			return fmt.Errorf("supporting evidence %q escapes ProposalWorkspace", repositoryPath)
		}
	}
	return nil
}

func pathWithin(parent string, candidate string) (bool, error) {
	relative, err := filepath.Rel(parent, candidate)
	if err != nil {
		return false, fmt.Errorf("compare verification path boundary: %w", err)
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))), nil
}

func sanitizeOutput(value []byte, limit int) (string, bool) {
	truncated := false
	if len(value) > limit {
		value = value[:limit]
		truncated = true
	}
	text := strings.ToValidUTF8(string(value), "�")
	if !utf8.ValidString(string(value)) {
		truncated = true
	}
	lines := strings.Split(text, "\n")
	for index, line := range lines {
		lower := strings.ToLower(line)
		for _, marker := range []string{"password", "secret", "token", "api_key", "api-key", "authorization", "credential"} {
			if strings.Contains(lower, marker) {
				lines[index] = "[REDACTED]"
				break
			}
		}
	}
	return strings.Join(lines, "\n"), truncated
}
