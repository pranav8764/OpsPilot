package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	integrationv1 "github.com/opspilot/gen/proto/integration/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fixProposalChange struct {
	Path                string `json:"path"`
	OriginalContentHash string `json:"original_content_hash"`
	NewContent          string `json:"new_content"`
	Reason              string `json:"reason"`
	Additions           int    `json:"additions"`
	Deletions           int    `json:"deletions"`
}

type fixApprovalRecord struct {
	ApprovalID       string
	WorkflowID       string
	ProposalID       string
	IncidentID       string
	IncidentTitle    string
	Owner            string
	Repository       string
	BaseBranch       string
	InstallationID   int64
	BaseCommitSHA    string
	UnifiedDiff      string
	VerificationJSON []byte
	Changes          []fixProposalChange
}

func (s *IntegrationServer) DecideFixApproval(ctx context.Context, req *integrationv1.DecideFixApprovalRequest) (*integrationv1.DecideFixApprovalResponse, error) {
	if req.UserId == "" || req.WorkspaceId == "" || req.ProjectId == "" || req.ApprovalId == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id, workspace_id, project_id, and approval_id are required")
	}
	decision := strings.ToLower(strings.TrimSpace(req.Decision))
	if decision != "approve" && decision != "reject" {
		return nil, status.Error(codes.InvalidArgument, "decision must be approve or reject")
	}
	if len(req.Reason) > 2000 {
		return nil, status.Error(codes.InvalidArgument, "decision reason exceeds 2000 characters")
	}
	if err := s.requireWorkspaceRole(ctx, req.WorkspaceId, req.UserId, "owner", "admin"); err != nil {
		return nil, err
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to start approval transaction")
	}
	defer tx.Rollback(ctx)

	var record fixApprovalRecord
	var approvalStatus, actionType, payloadHash string
	var actionPayload, changesJSON []byte
	var expiresAt time.Time
	err = tx.QueryRow(ctx, `
		SELECT approval.id, approval.workflow_id, approval.status, approval.action_type,
		       approval.action_payload, approval.payload_hash, approval.expires_at,
		       proposal.id, proposal.incident_id, proposal.base_commit_sha,
		       proposal.proposed_changes, proposal.unified_diff, proposal.verification,
		       incident.title, repository.owner, repository.name, repository.selected_branch,
		       installation.installation_id
		FROM approvals AS approval
		JOIN fix_proposals AS proposal
		  ON proposal.workflow_id = approval.workflow_id
		 AND proposal.id = (approval.action_payload->>'proposal_id')::uuid
		JOIN incidents AS incident ON incident.id = proposal.incident_id
		JOIN repository_connections AS repository
		  ON repository.project_id = proposal.project_id AND repository.workspace_id = proposal.workspace_id
		JOIN github_installations AS installation ON installation.id = repository.github_installation_id
		WHERE approval.id = $1 AND approval.workspace_id = $2 AND approval.project_id = $3
		FOR UPDATE OF approval
	`, req.ApprovalId, req.WorkspaceId, req.ProjectId).Scan(
		&record.ApprovalID, &record.WorkflowID, &approvalStatus, &actionType,
		&actionPayload, &payloadHash, &expiresAt,
		&record.ProposalID, &record.IncidentID, &record.BaseCommitSHA,
		&changesJSON, &record.UnifiedDiff, &record.VerificationJSON,
		&record.IncidentTitle, &record.Owner, &record.Repository, &record.BaseBranch,
		&record.InstallationID,
	)
	if err == pgx.ErrNoRows {
		return nil, status.Error(codes.NotFound, "fix approval not found in project")
	}
	if err != nil {
		s.logger.Error("load fix approval failed", "error", err, "approval_id", req.ApprovalId)
		return nil, status.Error(codes.Internal, "failed to load fix approval")
	}
	if approvalStatus != "pending" {
		return nil, status.Errorf(codes.FailedPrecondition, "approval is already %s", approvalStatus)
	}
	if time.Now().After(expiresAt) {
		if _, err := tx.Exec(ctx, "UPDATE approvals SET status = 'expired' WHERE id = $1", req.ApprovalId); err != nil {
			return nil, status.Error(codes.Internal, "failed to expire approval")
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, status.Error(codes.Internal, "failed to persist approval expiry")
		}
		return nil, status.Error(codes.FailedPrecondition, "approval has expired")
	}
	if actionType != "github.create_fix_pr" || !validApprovalPayload(actionPayload, payloadHash, record.ProposalID, record.BaseCommitSHA) {
		return nil, status.Error(codes.FailedPrecondition, "approval payload integrity check failed")
	}
	if err := json.Unmarshal(changesJSON, &record.Changes); err != nil || len(record.Changes) == 0 || len(record.Changes) > 5 {
		return nil, status.Error(codes.FailedPrecondition, "stored fix proposal is invalid")
	}
	for _, change := range record.Changes {
		if !validFixPath(change.Path) || change.OriginalContentHash == "" {
			return nil, status.Error(codes.FailedPrecondition, "stored fix proposal violates path policy")
		}
	}

	if decision == "reject" {
		_, err = tx.Exec(ctx, `
			UPDATE approvals SET status = 'rejected', decided_by = $1, decision_reason = $2, decided_at = NOW()
			WHERE id = $3
		`, req.UserId, req.Reason, req.ApprovalId)
		if err == nil {
			_, err = tx.Exec(ctx, "UPDATE fix_proposals SET status = 'rejected' WHERE id = $1", record.ProposalID)
		}
		if err == nil {
			_, err = tx.Exec(ctx, "UPDATE workflows SET status = 'cancelled' WHERE id = $1", record.WorkflowID)
		}
		if err == nil {
			_, err = tx.Exec(ctx, `
				UPDATE workflow_steps SET status = 'skipped', finished_at = NOW()
				WHERE workflow_id = $1 AND step_name = 'approve_and_publish_draft_pr'
			`, record.WorkflowID)
		}
		if err == nil {
			_, err = tx.Exec(ctx, `
				INSERT INTO audit_logs (workspace_id, user_id, action, resource_type, resource_id, metadata)
				VALUES ($1, $2, 'fix.approval.rejected', 'approval', $3, $4::jsonb)
			`, req.WorkspaceId, req.UserId, req.ApprovalId, mustJSON(map[string]string{"reason": req.Reason}))
		}
		if err != nil {
			return nil, status.Error(codes.Internal, "failed to reject approval")
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, status.Error(codes.Internal, "failed to commit approval rejection")
		}
		return &integrationv1.DecideFixApprovalResponse{
			ApprovalId: req.ApprovalId, ApprovalStatus: "rejected",
		}, nil
	}

	_, err = tx.Exec(ctx, `
		UPDATE approvals SET status = 'approved', decided_by = $1, decision_reason = $2, decided_at = NOW()
		WHERE id = $3
	`, req.UserId, req.Reason, req.ApprovalId)
	if err != nil || tx.Commit(ctx) != nil {
		return nil, status.Error(codes.Internal, "failed to record approval")
	}

	toolID, err := s.startFixPRToolCall(ctx, req, &record)
	if err != nil {
		return nil, err
	}
	result, err := s.publishFixPR(ctx, &record)
	if err != nil {
		s.failFixPRExecution(ctx, req, &record, toolID, err)
		s.logger.Error("draft fix PR publish failed", "error", err, "approval_id", req.ApprovalId)
		return nil, status.Error(codes.Unavailable, "approved fix could not be published to GitHub")
	}
	if err := s.completeFixPRExecution(ctx, req, &record, toolID, result); err != nil {
		s.logger.Error("draft PR created but completion state could not be persisted", "error", err, "approval_id", req.ApprovalId, "pr_url", result.URL)
		return nil, status.Error(codes.Internal, "draft PR was created but workflow reconciliation is required")
	}
	return &integrationv1.DecideFixApprovalResponse{
		ApprovalId:        req.ApprovalId,
		ApprovalStatus:    "executed",
		PullRequestUrl:    result.URL,
		PullRequestNumber: result.Number,
		Branch:            result.Branch,
	}, nil
}

func validApprovalPayload(raw []byte, expectedHash, proposalID, baseCommitSHA string) bool {
	var payload map[string]any
	if json.Unmarshal(raw, &payload) != nil {
		return false
	}
	canonical, err := json.Marshal(payload)
	if err != nil {
		return false
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:]) == expectedHash &&
		fmt.Sprint(payload["proposal_id"]) == proposalID &&
		fmt.Sprint(payload["base_commit_sha"]) == baseCommitSHA
}

func validFixPath(path string) bool {
	lower := strings.ToLower(path)
	return path != "" && !strings.HasPrefix(path, "/") && !strings.Contains(path, "..") &&
		!strings.HasPrefix(lower, ".github/workflows/") && !strings.HasPrefix(lower, ".git/") &&
		!strings.HasPrefix(lower, ".env") && !strings.HasSuffix(lower, ".pem") &&
		!strings.HasSuffix(lower, ".key")
}

func mustJSON(value any) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func (s *IntegrationServer) startFixPRToolCall(ctx context.Context, req *integrationv1.DecideFixApprovalRequest, record *fixApprovalRecord) (string, error) {
	var toolID string
	idempotencyKey := "github.create_fix_pr:" + record.ApprovalID
	err := s.db.QueryRow(ctx, `
		INSERT INTO tool_calls
			(workspace_id, project_id, workflow_id, approval_id, tool_name, risk_level,
			 request_summary, status, idempotency_key, started_at)
		VALUES ($1, $2, $3, $4, 'github.create_fix_pr', 'high', $5::jsonb, 'running', $6, NOW())
		ON CONFLICT (idempotency_key) DO NOTHING
		RETURNING id
	`, req.WorkspaceId, req.ProjectId, record.WorkflowID, record.ApprovalID,
		mustJSON(map[string]any{"file_count": len(record.Changes), "base_commit_sha": record.BaseCommitSHA}),
		idempotencyKey).Scan(&toolID)
	if err == pgx.ErrNoRows {
		return "", status.Error(codes.Aborted, "fix PR tool call has already been attempted")
	}
	if err != nil {
		return "", status.Error(codes.Internal, "failed to start audited tool call")
	}
	return toolID, nil
}

func (s *IntegrationServer) failFixPRExecution(ctx context.Context, req *integrationv1.DecideFixApprovalRequest, record *fixApprovalRecord, toolID string, executionErr error) {
	message := executionErr.Error()
	if len(message) > 1000 {
		message = message[:1000]
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		s.logger.Error("failed to start fix failure transaction", "error", err)
		return
	}
	defer tx.Rollback(ctx)
	statements := []struct {
		query string
		args  []any
	}{
		{"UPDATE tool_calls SET status = 'failed', error_code = 'GITHUB_PUBLISH_FAILED', error_message = $1, finished_at = NOW() WHERE id = $2", []any{message, toolID}},
		{"UPDATE approvals SET status = 'failed' WHERE id = $1", []any{record.ApprovalID}},
		{"UPDATE fix_proposals SET status = 'failed' WHERE id = $1", []any{record.ProposalID}},
		{"UPDATE workflows SET status = 'failed', error_code = 'GITHUB_PUBLISH_FAILED', error_message = $1 WHERE id = $2", []any{message, record.WorkflowID}},
	}
	for _, statement := range statements {
		if _, err := tx.Exec(ctx, statement.query, statement.args...); err != nil {
			s.logger.Error("failed to update fix PR failure state", "error", err, "approval_id", record.ApprovalID)
			return
		}
	}
	if err := tx.Commit(ctx); err != nil {
		s.logger.Error("failed to persist fix PR failure", "error", err, "approval_id", record.ApprovalID)
	}
}

func (s *IntegrationServer) completeFixPRExecution(ctx context.Context, req *integrationv1.DecideFixApprovalRequest, record *fixApprovalRecord, toolID string, result *fixPRResult) error {
	response := mustJSON(map[string]any{"pr_url": result.URL, "pr_number": result.Number, "branch": result.Branch, "draft": true})
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("start fix completion transaction: %w", err)
	}
	defer tx.Rollback(ctx)
	statements := []struct {
		query string
		args  []any
	}{
		{"UPDATE tool_calls SET status = 'succeeded', response_summary = $1::jsonb, finished_at = NOW() WHERE id = $2", []any{response, toolID}},
		{"UPDATE approvals SET status = 'executed', executed_at = NOW() WHERE id = $1", []any{record.ApprovalID}},
		{"UPDATE fix_proposals SET status = 'published' WHERE id = $1", []any{record.ProposalID}},
		{"UPDATE workflows SET status = 'succeeded', output = $1::jsonb WHERE id = $2", []any{response, record.WorkflowID}},
		{`UPDATE workflow_steps SET status = 'succeeded', output = $1::jsonb, finished_at = NOW()
		  WHERE workflow_id = $2 AND step_name = 'approve_and_publish_draft_pr'`, []any{response, record.WorkflowID}},
		{`INSERT INTO audit_logs (workspace_id, user_id, action, resource_type, resource_id, metadata)
		  VALUES ($1, $2, 'fix.pr.created', 'approval', $3, $4::jsonb)`, []any{req.WorkspaceId, req.UserId, record.ApprovalID, response}},
	}
	for _, statement := range statements {
		if _, err := tx.Exec(ctx, statement.query, statement.args...); err != nil {
			return fmt.Errorf("persist fix completion state: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit fix completion state: %w", err)
	}
	return nil
}
