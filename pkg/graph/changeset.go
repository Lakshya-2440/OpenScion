package graph

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// Changeset represents a single versioned commit in the repository DAG
type Changeset struct {
	ID             string   `json:"id"`                       // Deterministic hash of changeset contents
	RepoID         string   `json:"repo_id"`                  // Repository identifier
	SequenceNumber int64    `json:"sequence_number"`          // Linear sequence number on main (e.g. 1042)
	RootTreeHash   string   `json:"root_tree_hash"`           // Root Merkle Tree hash
	ParentIDs      []string `json:"parent_ids"`               // Parent changeset IDs
	Author         string   `json:"author"`                   // Author name/email
	Message        string   `json:"message"`                  // Commit message
	AgentModel     string   `json:"agent_model,omitempty"`    // e.g., "claude-code-3.7-sonnet"
	SessionID      string   `json:"session_id,omitempty"`     // Agent runtime session ID
	TaskTraceID    string   `json:"task_trace_id,omitempty"`  // Agent task or prompt trace ID
	CreatedAt      int64    `json:"created_at"`               // Unix timestamp (seconds)
}

// NewChangeset constructs a new changeset and computes its ID
func NewChangeset(
	repoID string,
	rootTreeHash string,
	parentIDs []string,
	author string,
	message string,
	agentModel string,
	sessionID string,
	taskTraceID string,
) *Changeset {
	now := time.Now().Unix()
	cs := &Changeset{
		RepoID:       repoID,
		RootTreeHash: rootTreeHash,
		ParentIDs:    parentIDs,
		Author:       author,
		Message:      message,
		AgentModel:   agentModel,
		SessionID:    sessionID,
		TaskTraceID:  taskTraceID,
		CreatedAt:    now,
	}
	cs.ID = cs.ComputeID()
	return cs
}

// ComputeID calculates the cryptographic hash for this changeset
func (c *Changeset) ComputeID() string {
	h := sha256.New()
	fmt.Fprintf(h, "repo:%s\n", c.RepoID)
	fmt.Fprintf(h, "tree:%s\n", c.RootTreeHash)
	fmt.Fprintf(h, "parents:%s\n", strings.Join(c.ParentIDs, ","))
	fmt.Fprintf(h, "author:%s\n", c.Author)
	fmt.Fprintf(h, "message:%s\n", c.Message)
	fmt.Fprintf(h, "agent:%s\n", c.AgentModel)
	fmt.Fprintf(h, "session:%s\n", c.SessionID)
	fmt.Fprintf(h, "trace:%s\n", c.TaskTraceID)
	fmt.Fprintf(h, "time:%d\n", c.CreatedAt)
	return hex.EncodeToString(h.Sum(nil))
}

// ShortID returns first 8 characters of Changeset ID
func (c *Changeset) ShortID() string {
	if len(c.ID) > 8 {
		return c.ID[:8]
	}
	return c.ID
}

// IsAgentGenerated returns true if this changeset was created by an AI agent
func (c *Changeset) IsAgentGenerated() bool {
	return c.AgentModel != "" && c.AgentModel != "human"
}
