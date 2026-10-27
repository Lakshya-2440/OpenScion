package local

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// IsGitRemote returns true if URL points to a standard Git server (GitHub, GitLab, Bitbucket, or .git)
func IsGitRemote(rawURL string) bool {
	u := strings.ToLower(strings.TrimSpace(rawURL))
	return strings.HasSuffix(u, ".git") ||
		strings.Contains(u, "github.com") ||
		strings.Contains(u, "gitlab.com") ||
		strings.Contains(u, "bitbucket.org") ||
		strings.HasPrefix(u, "git@") ||
		strings.HasPrefix(u, "git://")
}

// ensureGitBridge initializes or verifies the internal .scion/git_bridge directory
func (r *LocalRepo) ensureGitBridge(remoteURL string) (string, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return "", fmt.Errorf("git binary not found in PATH")
	}

	gitBridgeDir := filepath.Join(r.ScionDir, "git_bridge")
	if _, err := os.Stat(gitBridgeDir); os.IsNotExist(err) {
		cmdInit := exec.Command("git", "--git-dir="+gitBridgeDir, "--work-tree="+r.WorkDir, "init", "--quiet")
		if out, err := cmdInit.CombinedOutput(); err != nil {
			return "", fmt.Errorf("failed to initialize git bridge: %v (%s)", err, string(out))
		}
		cmdRemote := exec.Command("git", "--git-dir="+gitBridgeDir, "--work-tree="+r.WorkDir, "remote", "add", "origin", remoteURL)
		_ = cmdRemote.Run()
	} else {
		cmdRemote := exec.Command("git", "--git-dir="+gitBridgeDir, "--work-tree="+r.WorkDir, "remote", "set-url", "origin", remoteURL)
		_ = cmdRemote.Run()
	}

	// Always exclude .scion internal files from git tracking
	infoDir := filepath.Join(gitBridgeDir, "info")
	_ = os.MkdirAll(infoDir, 0755)
	_ = os.WriteFile(filepath.Join(infoDir, "exclude"), []byte(".scion\n.scion/\n"), 0644)
	_ = exec.Command("git", "--git-dir="+gitBridgeDir, "config", "core.worktree", r.WorkDir).Run()

	return gitBridgeDir, nil
}

// pushGit bridges OpenScion changesets to a remote Git repository (e.g. GitHub)
func (r *LocalRepo) pushGit(remoteURL string) (*SyncResult, error) {
	gitBridgeDir, err := r.ensureGitBridge(remoteURL)
	if err != nil {
		return nil, err
	}

	headCS, err := r.GetHeadChangeset()
	if err != nil {
		return nil, fmt.Errorf("cannot resolve HEAD changeset: %w", err)
	}

	// 1. Stage all working files in git bridge index
	cmdAdd := exec.Command("git", "--git-dir="+gitBridgeDir, "--work-tree="+r.WorkDir, "add", "-A")
	if out, err := cmdAdd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("git add failed: %v (%s)", err, string(out))
	}

	// 2. Commit in git bridge with OpenScion provenance
	commitMsg := fmt.Sprintf("%s\n\nOpenScion-Seq: #%d\nOpenScion-ID: %s", headCS.Message, headCS.SequenceNumber, headCS.ShortID())
	if headCS.AgentModel != "" {
		commitMsg += fmt.Sprintf("\nOpenScion-Agent: %s", headCS.AgentModel)
	}

	authorName := "OpenScion User"
	authorEmail := "user@openscion.local"
	if headCS.Author != "" {
		if strings.Contains(headCS.Author, "<") && strings.Contains(headCS.Author, ">") {
			parts := strings.SplitN(headCS.Author, "<", 2)
			authorName = strings.TrimSpace(parts[0])
			authorEmail = strings.TrimSuffix(strings.TrimSpace(parts[1]), ">")
		} else {
			authorName = headCS.Author
		}
	}

	cmdCommit := exec.Command("git", "--git-dir="+gitBridgeDir, "--work-tree="+r.WorkDir, "commit", "-m", commitMsg, "--allow-empty")
	cmdCommit.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME="+authorName,
		"GIT_AUTHOR_EMAIL="+authorEmail,
		"GIT_COMMITTER_NAME="+authorName,
		"GIT_COMMITTER_EMAIL="+authorEmail,
	)
	_ = cmdCommit.Run()

	// 3. Resolve target branch
	branch := r.Config.Branch
	if branch == "" {
		cmdBranch := exec.Command("git", "--git-dir="+gitBridgeDir, "rev-parse", "--abbrev-ref", "HEAD")
		if out, err := cmdBranch.Output(); err == nil {
			b := strings.TrimSpace(string(out))
			if b != "" && b != "HEAD" {
				branch = b
			}
		}
	}
	if branch == "" {
		branch = "main"
	}

	// 4. Push to remote Git repository
	cmdPush := exec.Command("git", "--git-dir="+gitBridgeDir, "--work-tree="+r.WorkDir, "push", "origin", "HEAD:"+branch)
	if _, err := cmdPush.CombinedOutput(); err != nil {
		cmdPushU := exec.Command("git", "--git-dir="+gitBridgeDir, "--work-tree="+r.WorkDir, "push", "-u", "origin", "HEAD:"+branch)
		if outU, errU := cmdPushU.CombinedOutput(); errU != nil {
			cmdPushHead := exec.Command("git", "--git-dir="+gitBridgeDir, "--work-tree="+r.WorkDir, "push", "-u", "origin", "HEAD")
			if outH, errH := cmdPushHead.CombinedOutput(); errH != nil {
				return nil, fmt.Errorf("git push failed: %v\n%s\n%s", errH, string(outU), string(outH))
			}
		}
	}

	return &SyncResult{
		PushedChangesets: 1,
		RemoteSequence:   headCS.SequenceNumber,
		LandedCommit:     headCS.ID,
		Message:          fmt.Sprintf("Pushed OpenScion changeset #%d to Git remote %s", headCS.SequenceNumber, remoteURL),
	}, nil
}

// pullGit pulls changes from a remote Git repository into OpenScion
func (r *LocalRepo) pullGit(remoteURL string) (*PullResult, error) {
	gitBridgeDir, err := r.ensureGitBridge(remoteURL)
	if err != nil {
		return nil, err
	}

	branch := r.Config.Branch
	if branch == "" {
		cmdBranch := exec.Command("git", "--git-dir="+gitBridgeDir, "rev-parse", "--abbrev-ref", "HEAD")
		if out, err := cmdBranch.Output(); err == nil {
			b := strings.TrimSpace(string(out))
			if b != "" && b != "HEAD" {
				branch = b
			}
		}
	}
	if branch == "" {
		branch = "main"
	}

	cmdPull := exec.Command("git", "--git-dir="+gitBridgeDir, "--work-tree="+r.WorkDir, "pull", "--quiet", "origin", branch)
	if out, err := cmdPull.CombinedOutput(); err != nil {
		cmdPull2 := exec.Command("git", "--git-dir="+gitBridgeDir, "--work-tree="+r.WorkDir, "pull", "--quiet")
		if out2, err2 := cmdPull2.CombinedOutput(); err2 != nil {
			return nil, fmt.Errorf("git pull failed: %v (%s / %s)", err, string(out), string(out2))
		}
	}

	// Re-commit imported files into OpenScion DAG if changed
	cs, err := r.commitUnlocked("Merge from Git remote "+remoteURL, "Git Remote <git@remote>", "git-sync", "", "")
	if err != nil {
		// Working tree was clean or no changes
		headCS, _ := r.GetHeadChangeset()
		seq := int64(1)
		if headCS != nil {
			seq = headCS.SequenceNumber
		}
		return &PullResult{NewSequence: seq}, nil
	}

	return &PullResult{
		PulledChangesets: 1,
		NewSequence:      cs.SequenceNumber,
	}, nil
}
