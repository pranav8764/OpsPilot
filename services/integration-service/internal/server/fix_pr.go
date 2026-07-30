package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

type fixPRResult struct {
	URL    string
	Number int64
	Branch string
}

type githubRefResponse struct {
	Object struct {
		SHA string `json:"sha"`
	} `json:"object"`
}

type githubCommitResponse struct {
	SHA  string `json:"sha"`
	Tree struct {
		SHA string `json:"sha"`
	} `json:"tree"`
}

type githubContentResponse struct {
	Content  string `json:"content"`
	Encoding string `json:"encoding"`
}

func (s *IntegrationServer) publishFixPR(ctx context.Context, record *fixApprovalRecord) (*fixPRResult, error) {
	token, err := s.getInstallationToken(record.InstallationID)
	if err != nil {
		return nil, fmt.Errorf("authorize GitHub installation: %w", err)
	}
	return s.publishFixPRWithToken(ctx, record, token)
}

func (s *IntegrationServer) publishFixPRWithToken(ctx context.Context, record *fixApprovalRecord, token string) (*fixPRResult, error) {
	repositoryPath := fmt.Sprintf("/repos/%s/%s", url.PathEscape(record.Owner), url.PathEscape(record.Repository))

	var baseRef githubRefResponse
	if err := s.githubJSON(ctx, http.MethodGet,
		repositoryPath+"/git/ref/heads/"+escapeGitHubPath(record.BaseBranch), token, nil, &baseRef); err != nil {
		return nil, fmt.Errorf("read base branch: %w", err)
	}
	if baseRef.Object.SHA != record.BaseCommitSHA {
		return nil, fmt.Errorf("base branch moved after proposal; expected %s", record.BaseCommitSHA)
	}

	var baseCommit githubCommitResponse
	if err := s.githubJSON(ctx, http.MethodGet,
		repositoryPath+"/git/commits/"+url.PathEscape(record.BaseCommitSHA), token, nil, &baseCommit); err != nil {
		return nil, fmt.Errorf("read base commit: %w", err)
	}
	if baseCommit.Tree.SHA == "" {
		return nil, fmt.Errorf("base commit did not include a tree SHA")
	}

	treeEntries := make([]map[string]string, 0, len(record.Changes))
	for _, change := range record.Changes {
		var current githubContentResponse
		contentEndpoint := repositoryPath + "/contents/" + escapeGitHubPath(change.Path) + "?ref=" + url.QueryEscape(record.BaseCommitSHA)
		if err := s.githubJSON(ctx, http.MethodGet, contentEndpoint, token, nil, &current); err != nil {
			return nil, fmt.Errorf("read proposed file %s: %w", change.Path, err)
		}
		if current.Encoding != "base64" {
			return nil, fmt.Errorf("file %s was not returned as base64", change.Path)
		}
		original, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(current.Content, "\n", ""))
		if err != nil {
			return nil, fmt.Errorf("decode proposed file %s", change.Path)
		}
		digest := sha256.Sum256(original)
		if hex.EncodeToString(digest[:]) != change.OriginalContentHash {
			return nil, fmt.Errorf("file %s changed after proposal", change.Path)
		}

		var blob struct {
			SHA string `json:"sha"`
		}
		if err := s.githubJSON(ctx, http.MethodPost, repositoryPath+"/git/blobs", token,
			map[string]any{"content": change.NewContent, "encoding": "utf-8"}, &blob); err != nil {
			return nil, fmt.Errorf("create blob for %s: %w", change.Path, err)
		}
		treeEntries = append(treeEntries, map[string]string{
			"path": change.Path, "mode": "100644", "type": "blob", "sha": blob.SHA,
		})
	}

	var tree struct {
		SHA string `json:"sha"`
	}
	if err := s.githubJSON(ctx, http.MethodPost, repositoryPath+"/git/trees", token,
		map[string]any{"base_tree": baseCommit.Tree.SHA, "tree": treeEntries}, &tree); err != nil {
		return nil, fmt.Errorf("create fix tree: %w", err)
	}
	var commit githubCommitResponse
	commitMessage := fmt.Sprintf("fix: remediate incident %s", shortID(record.IncidentID))
	if err := s.githubJSON(ctx, http.MethodPost, repositoryPath+"/git/commits", token,
		map[string]any{"message": commitMessage, "tree": tree.SHA, "parents": []string{record.BaseCommitSHA}}, &commit); err != nil {
		return nil, fmt.Errorf("create fix commit: %w", err)
	}
	branch := "opspilot/fix-" + shortID(record.ProposalID)
	if err := s.githubJSON(ctx, http.MethodPost, repositoryPath+"/git/refs", token,
		map[string]any{"ref": "refs/heads/" + branch, "sha": commit.SHA}, nil); err != nil {
		return nil, fmt.Errorf("create fix branch: %w", err)
	}

	body := fmt.Sprintf(
		"## OpsPilot FIX proposal\n\nIncident: `%s`\n\n%s\n\n### Safety evidence\n\n- Base commit pinned: `%s`\n%s- Human approval: `%s`\n\nThis PR is intentionally a draft and must be reviewed and tested before merge.\n",
		record.IncidentID, record.IncidentTitle, record.BaseCommitSHA,
		fixVerificationEvidence(record.VerificationJSON), record.ApprovalID,
	)
	var pullRequest struct {
		HTMLURL string `json:"html_url"`
		Number  int64  `json:"number"`
	}
	if err := s.githubJSON(ctx, http.MethodPost, repositoryPath+"/pulls", token,
		map[string]any{
			"title": fmt.Sprintf("[OpsPilot FIX] %s", record.IncidentTitle),
			"head":  branch, "base": record.BaseBranch, "body": body, "draft": true,
		}, &pullRequest); err != nil {
		return nil, fmt.Errorf("create draft pull request: %w", err)
	}
	return &fixPRResult{URL: pullRequest.HTMLURL, Number: pullRequest.Number, Branch: branch}, nil
}

func (s *IntegrationServer) githubJSON(ctx context.Context, method, endpoint, token string, body any, target any) error {
	var requestBody *bytes.Reader
	if body == nil {
		requestBody = bytes.NewReader(nil)
	} else {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		requestBody = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, githubAPIBaseURL+endpoint, requestBody)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "opspilot-integration-service")
	request.Header.Set("X-GitHub-Api-Version", s.cfg.GitHubAPIVersion)
	response, err := githubHTTPClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("GitHub %s %s returned status %d (request %s)", method, endpoint, response.StatusCode, response.Header.Get("x-github-request-id"))
	}
	if target != nil && response.StatusCode != http.StatusNoContent {
		if err := json.NewDecoder(response.Body).Decode(target); err != nil {
			return err
		}
	}
	return nil
}

// fixVerificationEvidence renders the verification record the AI worker actually
// produced, as Markdown bullet lines ending in a newline.
//
// The previous PR body asserted "Secret/path/static checks: passed" and
// "Repository tests: not run" as literal text, regardless of what happened. That
// is a safety claim on a pull request that proposes a code change, so it has to
// come from the recorded result or say plainly that it is unknown.
func fixVerificationEvidence(verificationJSON []byte) string {
	var verification map[string]any
	if len(verificationJSON) == 0 || json.Unmarshal(verificationJSON, &verification) != nil {
		return "- Verification record: unavailable\n"
	}

	var lines []string
	check := func(label, key string) {
		if value, ok := verification[key].(string); ok && value != "" {
			lines = append(lines, fmt.Sprintf("- %s: `%s`", label, value))
		}
	}
	check("Path policy", "path_policy")
	check("Secret scan", "secret_scan")
	check("Static validation", "static_validation")

	if tests, ok := verification["tests"].(map[string]any); ok {
		status, _ := tests["status"].(string)
		if status == "" {
			status = "unknown"
		}
		line := fmt.Sprintf("- Repository tests: `%s`", status)
		if reason, ok := tests["reason"].(string); ok && reason != "" {
			line += fmt.Sprintf(" (%s)", reason)
		}
		lines = append(lines, line)
	} else {
		lines = append(lines, "- Repository tests: `unknown`")
	}

	verified, _ := verification["verified_fix"].(bool)
	if verified {
		lines = append(lines, "- Sandbox verified the patched tree: `yes`")
	} else {
		lines = append(lines, "- Sandbox verified the patched tree: `no`")
	}

	if len(lines) == 0 {
		return "- Verification record: unavailable\n"
	}
	return strings.Join(lines, "\n") + "\n"
}

func escapeGitHubPath(value string) string {
	parts := strings.Split(value, "/")
	for index := range parts {
		parts[index] = url.PathEscape(parts[index])
	}
	return strings.Join(parts, "/")
}

func shortID(value string) string {
	clean := strings.ReplaceAll(value, "-", "")
	if len(clean) > 12 {
		return clean[:12]
	}
	return clean
}
