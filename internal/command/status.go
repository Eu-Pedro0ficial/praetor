package command

// StatusSnapshot is the single presentation-neutral projection consumed by
// both the status command and the terminal sidebar.
type StatusSnapshot struct {
	Project             string
	Repository          string
	GitRepository       string
	Change              string
	Proposal            string
	ProposalBase        string
	ProviderAdapter     string
	ProviderVendor      string
	ProviderModel       string
	Verification        string
	VerificationAttempt string
	HumanDecision       string
	HumanActor          string
	Session             string
}

// StatusSnapshot returns one coherent view of retained session state.
func (session *Session) StatusSnapshot() StatusSnapshot {
	snapshot := StatusSnapshot{
		Project:       "none",
		Repository:    "none",
		GitRepository: "true",
		Change:        "none",
		Proposal:      "none",
		Verification:  "none",
		HumanDecision: "none",
		Session:       "active",
		ProviderModel: "provider default",
	}
	if session == nil {
		snapshot.GitRepository = "false"
		snapshot.Session = "none"
		return snapshot
	}
	registration := session.Registration()
	snapshot.Project = string(registration.ProjectId)
	snapshot.Repository = registration.RepositoryRoot
	if currentChange, ok := session.CurrentChange(); ok {
		snapshot.Change = string(currentChange.ChangeId()) + " (" + string(currentChange.State()) + ")"
	}
	if currentProposal, ok := session.CurrentProposal(); ok {
		workspace := currentProposal.Workspace()
		snapshot.Proposal = string(workspace.WorkspaceId()) + " (" + string(workspace.State()) + ")"
		snapshot.ProposalBase = workspace.BaseRevision()
	}
	selection := session.ProviderSelection()
	snapshot.ProviderAdapter = string(selection.ProviderIdentifier())
	if descriptor, ok := session.SelectedProviderDescriptor(); ok {
		snapshot.ProviderVendor = descriptor.Vendor()
	}
	if model, ok := selection.ModelIdentifier(); ok {
		snapshot.ProviderModel = string(model)
	}
	if result, ok := session.LastVerification(); ok {
		snapshot.VerificationAttempt = string(result.AttemptId())
		snapshot.Verification = "FAIL"
		if result.Passed() {
			snapshot.Verification = "PASS"
		}
	}
	if decision, ok := session.LastDecision(); ok {
		snapshot.HumanDecision = string(decision.Kind())
		snapshot.HumanActor = string(decision.Actor())
	}
	return snapshot
}
