package main

import "testing"

func TestProjectQueryPath(t *testing.T) {
	valid := "/api/v1/workspaces/workspace-1/projects/project-1/query"
	if !isProjectQueryPath(valid) {
		t.Fatalf("expected valid project query path: %s", valid)
	}
	invalid := []string{
		"/api/v1/workspaces/workspace-1/projects/query",
		"/api/v1/workspaces/workspace-1/projects/project-1/query/extra",
		"/api/v1/workspaces/workspace-1/projects/project-1/repository",
	}
	for _, path := range invalid {
		if isProjectQueryPath(path) {
			t.Fatalf("expected invalid project query path: %s", path)
		}
	}
}

func TestGitHubInstallSessionPath(t *testing.T) {
	valid := "/api/v1/workspaces/workspace-1/integrations/github/install-session"
	if !isGitHubInstallSessionPath(valid) {
		t.Fatalf("expected valid GitHub install-session path: %s", valid)
	}
	if isGitHubInstallSessionPath(valid + "/extra") {
		t.Fatal("extra segment must not match GitHub install-session route")
	}
}

func TestLogProviderPaths(t *testing.T) {
	connections := "/api/v1/workspaces/workspace-1/integrations/log-providers"
	if !isLogProviderConnectionsPath(connections) || isLogProviderConnectionsPath(connections+"/extra") {
		t.Fatal("log-provider connection route matching is incorrect")
	}
	collection := "/api/v1/workspaces/workspace-1/projects/project-1/incidents/collect"
	if !isProjectLogCollectionPath(collection) || isProjectLogCollectionPath(collection+"/extra") {
		t.Fatal("provider log collection route matching is incorrect")
	}
}

func TestProjectIncidentsPath(t *testing.T) {
	valid := "/api/v1/workspaces/workspace-1/projects/project-1/incidents"
	if !isProjectIncidentsPath(valid) || isProjectIncidentsPath(valid+"/extra") {
		t.Fatal("project incident history route matching is incorrect")
	}
}

func TestProjectDeploymentAnalysisPaths(t *testing.T) {
	base := "/api/v1/workspaces/workspace-1/projects/project-1/deployment/"
	if !isProjectDeploymentReadinessPath(base+"readiness") || !isProjectDeploymentGeneratePath(base+"generate") {
		t.Fatal("deployment analysis route matching is incorrect")
	}
	if isProjectDeploymentReadinessPath(base+"generate") || isProjectDeploymentGeneratePath(base+"readiness") {
		t.Fatal("deployment analysis routes must not overlap")
	}
}

func TestWorkspaceUsagePath(t *testing.T) {
	valid := "/api/v1/workspaces/workspace-1/usage"
	if !isWorkspaceUsagePath(valid) || isWorkspaceUsagePath(valid+"/extra") {
		t.Fatal("workspace usage route matching is incorrect")
	}
}

func TestProjectPath(t *testing.T) {
	valid := "/api/v1/workspaces/workspace-1/projects/project-1"
	if !isProjectPath(valid) || isProjectPath(valid+"/extra") {
		t.Fatal("single-project route matching is incorrect")
	}
}

func TestProjectsPath(t *testing.T) {
	valid := "/api/v1/workspaces/workspace-1/projects"
	if !isProjectsPath(valid) || !isProjectsPath(valid+"/") {
		t.Fatal("workspace project collection route should match")
	}
	invalid := []string{
		"/projects",
		"/api/v1/workspaces/workspace-1/projects/project-1",
		"/api/v1/workspaces/workspace-1/other/projects",
	}
	for _, path := range invalid {
		if isProjectsPath(path) {
			t.Fatalf("unexpected project collection route match: %s", path)
		}
	}
}

func TestWorkspaceSettingsPath(t *testing.T) {
	valid := "/api/v1/workspaces/workspace-1/settings"
	if !isWorkspaceSettingsPath(valid) || isWorkspaceSettingsPath(valid+"/extra") {
		t.Fatal("workspace settings route matching is incorrect")
	}
}

func TestProjectLogAnalysisPath(t *testing.T) {
	valid := "/api/v1/workspaces/workspace-1/projects/project-1/incidents/analyze"
	if !isProjectLogAnalysisPath(valid) {
		t.Fatalf("expected valid project log analysis path: %s", valid)
	}
	if isProjectLogAnalysisPath(valid + "/extra") {
		t.Fatal("extra path segment must not match log analysis route")
	}
}

func TestIncidentFixProposalPath(t *testing.T) {
	valid := "/api/v1/workspaces/workspace-1/projects/project-1/incidents/incident-1/fix-proposal"
	if !isIncidentFixProposalPath(valid) {
		t.Fatalf("expected valid incident fix proposal path: %s", valid)
	}
	if isIncidentFixProposalPath("/api/v1/workspaces/workspace-1/projects/project-1/incidents/analyze") {
		t.Fatal("log analysis route must not match a fix proposal")
	}
}

func TestFixApprovalDecisionPath(t *testing.T) {
	valid := "/api/v1/workspaces/workspace-1/projects/project-1/approvals/approval-1/decision"
	if !isFixApprovalDecisionPath(valid) {
		t.Fatalf("expected valid approval decision path: %s", valid)
	}
	if isFixApprovalDecisionPath(valid + "/extra") {
		t.Fatal("extra segment must not match approval decision route")
	}
}
