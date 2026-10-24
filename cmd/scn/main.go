package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aether/scn/pkg/cas"
	"github.com/aether/scn/pkg/client"
	"github.com/aether/scn/pkg/graph"
	"github.com/aether/scn/pkg/merge"
	"github.com/aether/scn/pkg/tree"
	"github.com/aether/scn/pkg/ui"
	"github.com/google/uuid"
)

type Config struct {
	ServerURL      string   `json:"server_url"`
	RepoName       string   `json:"repo_name"`
	RepoID         string   `json:"repo_id"`
	Branch         string   `json:"branch"`
	AgentModel     string   `json:"agent_model"`
	SessionID      string   `json:"session_id"`
	SparsePatterns []string `json:"sparse_patterns"`
}

type Manifest struct {
	BaseChangesetID string     `json:"base_changeset_id"`
	HeadChangesetID string     `json:"head_changeset_id"`
	SequenceNumber  int64      `json:"sequence_number"`
	Tree            *tree.Tree `json:"tree"`
	CommitMessage   string     `json:"commit_message,omitempty"`
	Author          string     `json:"author,omitempty"`
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "init":
		cmdInit(args)
	case "clone":
		cmdClone(args)
	case "status":
		cmdStatus(args)
	case "diff":
		cmdDiff(args)
	case "commit":
		cmdCommit(args)
	case "submit":
		cmdSubmit(args)
	case "log":
		cmdLog(args)
	case "query":
		cmdQuery(args)
	case "profile":
		cmdProfile(args)
	case "gc":
		cmdGC(args)
	case "help", "--help", "-h":
		printUsage()
	default:
		ui.PrintError(fmt.Sprintf("Unknown command %q. Run 'scn help' for available commands.", cmd))
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Printf("%s%sscn - OpenScion: Agent-Scale Version Control System (Anthropic Class)%s\n\n", ui.Bold, ui.BrightCyan, ui.Reset)
	fmt.Println("Usage: scn <command> [options]")
	fmt.Println()
	fmt.Println("Core Commands:")
	fmt.Println("  init [repo-name]              Initialize a new repository workspace")
	fmt.Println("  clone <repo-name> [dir]       Sparse/On-demand clone in sub-seconds")
	fmt.Println("  status                        Show workspace changes, branch, and agent context")
	fmt.Println("  diff                          Inspect local changes against base revision")
	fmt.Println("  commit -m <msg>               Create local changeset with agent provenance")
	fmt.Println("  submit                        Land changeset into linear server merge queue")
	fmt.Println("  log                           View commit DAG with linear sequence numbers")
	fmt.Println()
	fmt.Println("Server-Side Query Commands (Zero-Clone):")
	fmt.Println("  query file <path> [--rev id]  View file content directly from server")
	fmt.Println("  query blame <path>            View server-side computed blame")
	fmt.Println("  query diff <base> <target>    Diff two revisions on the server")
	fmt.Println("  query queue                   Inspect live server merge queue status")
	fmt.Println()
	fmt.Println("Flags:")
	fmt.Println("  --server <url>                Control plane URL (default: http://localhost:8080)")
	fmt.Println("  --agent <model>               Agent model name (e.g. claude-code-3.7-sonnet)")
	fmt.Println("  --sparse <patterns>           Comma-separated sparse path patterns")
	fmt.Println()
}

func getServerURL() string {
	if s := os.Getenv("OPENSCION_SERVER_URL"); s != "" {
		return s
	}
	if s := os.Getenv("AETHER_SERVER_URL"); s != "" {
		return s
	}
	return "http://localhost:8080"
}

func cmdInit(args []string) {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	serverFlag := fs.String("server", getServerURL(), "Server URL")
	agentFlag := fs.String("agent", "claude-code-3.7-sonnet", "Agent model identifier")
	fs.Parse(args)

	repoName := "openscion-repo"
	if len(fs.Args()) > 0 {
		repoName = fs.Args()[0]
	}

	cli := client.NewClient(*serverFlag)
	ui.PrintInfo(fmt.Sprintf("Connecting to OpenScion control plane at %s...", *serverFlag))

	// Get or create repo on server
	repoInfo, err := cli.GetRepoInfo(repoName)
	var repoID string
	var headCSID string
	var seq int64

	if err != nil {
		// Attempt creation
		r, err := cli.CreateRepo(repoName)
		if err != nil {
			ui.PrintError(fmt.Sprintf("Failed to initialize repo on server: %v", err))
			os.Exit(1)
		}
		repoID = r.ID
		info, err := cli.GetRepoInfo(repoName)
		if err != nil {
			ui.PrintError(err.Error())
			os.Exit(1)
		}
		headCSID = info.Branch.HeadID
		seq = info.SequenceNumber
	} else {
		repoID = repoInfo.Repo.ID
		headCSID = repoInfo.Branch.HeadID
		seq = repoInfo.SequenceNumber
	}

	// Create .scn directory
	scnDir := ".scn"
	if err := os.MkdirAll(filepath.Join(scnDir, "cache"), 0755); err != nil {
		ui.PrintError(err.Error())
		os.Exit(1)
	}

	cfg := Config{
		ServerURL:      *serverFlag,
		RepoName:       repoName,
		RepoID:         repoID,
		Branch:         "main",
		AgentModel:     *agentFlag,
		SessionID:      uuid.New().String(),
		SparsePatterns: []string{"*"},
	}
	saveConfig(&cfg)

	// Fetch head tree
	headTree, err := cli.GetTree(headCSID)
	if err != nil {
		headTree = tree.NewTree()
		headTree.ComputeHash()
	}

	manifest := Manifest{
		BaseChangesetID: headCSID,
		HeadChangesetID: headCSID,
		SequenceNumber:  seq,
		Tree:            headTree,
	}
	saveManifest(&manifest)

	ui.PrintSuccess(fmt.Sprintf("Initialized empty OpenScion workspace for %q", repoName))
	ui.PrintBox("OpenScion: Workspace Configured", []string{
		fmt.Sprintf("Repository:     %s", repoName),
		fmt.Sprintf("Branch:         main (Sequence #%d)", seq),
		fmt.Sprintf("Server URL:     %s", *serverFlag),
		fmt.Sprintf("Agent Session:  %s", cfg.SessionID),
		fmt.Sprintf("Agent Model:    %s", cfg.AgentModel),
	})
}

func cmdClone(args []string) {
	fs := flag.NewFlagSet("clone", flag.ExitOnError)
	serverFlag := fs.String("server", getServerURL(), "Server URL")
	sparseFlag := fs.String("sparse", "", "Comma-separated sparse path patterns (e.g. 'src/**,config/*')")
	agentFlag := fs.String("agent", "claude-code-3.7-sonnet", "Agent model identifier")
	fs.Parse(args)

	if len(fs.Args()) < 1 {
		ui.PrintError("Usage: scn clone <repo-name> [target-dir] [--sparse <patterns>]")
		os.Exit(1)
	}

	repoName := fs.Args()[0]
	targetDir := repoName
	if len(fs.Args()) >= 2 {
		targetDir = fs.Args()[1]
	}

	start := time.Now()
	cli := client.NewClient(*serverFlag)

	ui.PrintStep(1, 3, fmt.Sprintf("Querying repository %q from server", repoName))
	info, err := cli.GetRepoInfo(repoName)
	if err != nil {
		ui.PrintError(fmt.Sprintf("Failed to query repo: %v", err))
		os.Exit(1)
	}
	ui.PrintStepDone(1, 3, fmt.Sprintf("Found main branch (Sequence #%d, head: %s)",
		info.SequenceNumber, info.HeadChangeset.ShortID()))

	var patterns []string
	if *sparseFlag != "" {
		patterns = strings.Split(*sparseFlag, ",")
	}

	ui.PrintStep(2, 3, "Resolving Merkle tree with sparse profile")
	t, err := cli.GetSparseTree(info.HeadChangeset.RootTreeHash, patterns)
	if err != nil {
		ui.PrintError(fmt.Sprintf("Failed to get tree: %v", err))
		os.Exit(1)
	}
	ui.PrintStepDone(2, 3, fmt.Sprintf("Selected %d file(s) for local materialization", len(t.Entries)))

	// Create target directory and .scn workspace
	if err := os.MkdirAll(filepath.Join(targetDir, ".scn", "cache"), 0755); err != nil {
		ui.PrintError(err.Error())
		os.Exit(1)
	}

	localCAS, err := cas.NewStore(filepath.Join(targetDir, ".scn", "cache"))
	if err != nil {
		ui.PrintError(err.Error())
		os.Exit(1)
	}

	ui.PrintStep(3, 3, "Streaming required chunks and materializing files")
	// Materialize files
	for relPath, entry := range t.Entries {
		// Download chunks if not in local cache
		fullPath := filepath.Join(targetDir, filepath.FromSlash(relPath))
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			continue
		}

		// Query file blob from server
		fq, err := cli.QueryFile(repoName, relPath, info.HeadChangeset.ID)
		if err != nil {
			continue
		}
		if err := os.WriteFile(fullPath, []byte(fq.ContentText), os.FileMode(entry.Mode)); err != nil {
			ui.PrintError(fmt.Sprintf("Failed to write %s: %v", relPath, err))
		}
		localCAS.PutBlob([]byte(fq.ContentText))
	}
	ui.PrintStepDone(3, 3, "Materialization complete")

	// Save config and manifest
	cfg := Config{
		ServerURL:      *serverFlag,
		RepoName:       repoName,
		RepoID:         info.Repo.ID,
		Branch:         "main",
		AgentModel:     *agentFlag,
		SessionID:      uuid.New().String(),
		SparsePatterns: patterns,
	}
	cfgData, _ := json.MarshalIndent(cfg, "", "  ")
	os.WriteFile(filepath.Join(targetDir, ".scn", "config.json"), cfgData, 0644)

	manifestTree := t
	if fullTree, err := cli.GetTree(info.HeadChangeset.RootTreeHash); err == nil {
		manifestTree = fullTree
	}

	manifest := Manifest{
		BaseChangesetID: info.HeadChangeset.ID,
		HeadChangesetID: info.HeadChangeset.ID,
		SequenceNumber:  info.SequenceNumber,
		Tree:            manifestTree,
	}
	manData, _ := json.MarshalIndent(manifest, "", "  ")
	os.WriteFile(filepath.Join(targetDir, ".scn", "manifest.json"), manData, 0644)

	elapsed := time.Since(start).Milliseconds()
	ui.PrintSuccess(fmt.Sprintf("Cloned %q into ./%s in %dms!", repoName, targetDir, elapsed))
}

func cmdStatus(args []string) {
	cfg, manifest := mustLoadWorkspace()
	localCAS := mustLoadLocalCAS()

	// Build current tree from disk with sparse retention
	currentTree, err := tree.BuildTreeFromDirWithBaseAndSparse(".", localCAS, nil, manifest.Tree, cfg.SparsePatterns)
	if err != nil {
		ui.PrintError(fmt.Sprintf("Failed to scan working directory: %v", err))
		os.Exit(1)
	}

	diffs := tree.Diff(manifest.Tree, currentTree)

	var changesLines []string
	if len(diffs) == 0 {
		changesLines = append(changesLines, fmt.Sprintf("%sWorking tree clean. Nothing to commit.%s", ui.Dim, ui.Reset))
	} else {
		for _, d := range diffs {
			switch d.ChangeType {
			case tree.ChangeAdded:
				changesLines = append(changesLines, fmt.Sprintf("  %s[A]%s %s", ui.BrightGreen, ui.Reset, d.Path))
			case tree.ChangeModified:
				changesLines = append(changesLines, fmt.Sprintf("  %s[M]%s %s", ui.BrightYellow, ui.Reset, d.Path))
			case tree.ChangeDeleted:
				changesLines = append(changesLines, fmt.Sprintf("  %s[D]%s %s", ui.Red, ui.Reset, d.Path))
			}
		}
	}

	lines := []string{
		fmt.Sprintf("Repository:    %s%s%s", ui.Bold, cfg.RepoName, ui.Reset),
		fmt.Sprintf("Branch:        %s%s%s (Sequence #%d)", ui.BrightCyan, cfg.Branch, ui.Reset, manifest.SequenceNumber),
		fmt.Sprintf("Base Commit:   %s%s%s", ui.DarkGray, manifest.BaseChangesetID[:min(12, len(manifest.BaseChangesetID))], ui.Reset),
		fmt.Sprintf("Agent Context: %s%s %s%s (Session: %s)", ui.BrightPurple, ui.SymAgent, cfg.AgentModel, ui.Reset, cfg.SessionID[:8]),
		"",
		fmt.Sprintf("%sChanges:%s", ui.Bold, ui.Reset),
	}
	lines = append(lines, changesLines...)

	ui.PrintBox("OpenScion: Workspace Status", lines)
}

func cmdDiff(args []string) {
	cfg, manifest := mustLoadWorkspace()
	localCAS := mustLoadLocalCAS()

	currentTree, err := tree.BuildTreeFromDirWithBaseAndSparse(".", localCAS, nil, manifest.Tree, cfg.SparsePatterns)
	if err != nil {
		ui.PrintError(err.Error())
		os.Exit(1)
	}

	diffs := tree.Diff(manifest.Tree, currentTree)
	if len(diffs) == 0 {
		fmt.Println("No modifications detected.")
		return
	}

	for _, d := range diffs {
		var oldLines, newLines []string
		if d.OldHash != "" {
			oldData, _ := localCAS.GetBlobByHash(d.OldHash)
			oldLines = strings.Split(string(oldData), "\n")
		}
		if d.NewHash != "" {
			newData, _ := localCAS.GetBlobByHash(d.NewHash)
			newLines = strings.Split(string(newData), "\n")
		}

		diffText := merge.FormatUnifiedDiff(d.Path, oldLines, newLines, true)
		if diffText != "" {
			fmt.Println("\n" + strings.TrimSpace(diffText))
		}
	}
}

func cmdCommit(args []string) {
	fs := flag.NewFlagSet("commit", flag.ExitOnError)
	msgFlag := fs.String("m", "", "Commit message (required)")
	agentFlag := fs.String("agent", "", "Override agent model")
	sessionFlag := fs.String("session", "", "Override session ID")
	traceFlag := fs.String("trace", "", "Task trace ID")
	fs.Parse(args)

	if *msgFlag == "" {
		ui.PrintError("Commit message is required: scn commit -m \"<message>\"")
		os.Exit(1)
	}

	cfg, manifest := mustLoadWorkspace()
	localCAS := mustLoadLocalCAS()

	// Scan working copy and store blobs in local CAS with sparse retention
	currentTree, err := tree.BuildTreeFromDirWithBaseAndSparse(".", localCAS, nil, manifest.Tree, cfg.SparsePatterns)
	if err != nil {
		ui.PrintError(fmt.Sprintf("Failed to build tree: %v", err))
		os.Exit(1)
	}

	diffs := tree.Diff(manifest.Tree, currentTree)
	if len(diffs) == 0 {
		ui.PrintInfo("Nothing to commit (working tree clean)")
		return
	}

	agentModel := cfg.AgentModel
	if *agentFlag != "" {
		agentModel = *agentFlag
	}
	sessionID := cfg.SessionID
	if *sessionFlag != "" {
		sessionID = *sessionFlag
	}

	// Create local changeset
	cs := graph.NewChangeset(
		cfg.RepoID,
		currentTree.RootHash,
		[]string{manifest.HeadChangesetID},
		"Local Agent <agent@aether.local>",
		*msgFlag,
		agentModel,
		sessionID,
		*traceFlag,
	)

	// Update local manifest
	manifest.HeadChangesetID = cs.ID
	manifest.Tree = currentTree
	manifest.CommitMessage = *msgFlag
	manifest.Author = "Local Agent <agent@aether.local>"
	saveManifest(manifest)

	ui.PrintSuccess(fmt.Sprintf("Created local changeset %s%s%s", ui.Bold, cs.ShortID(), ui.Reset))
	fmt.Printf("   ├─ Message: %s\n", cs.Message)
	fmt.Printf("   ├─ Agent:   %s %s\n", ui.SymAgent, cs.AgentModel)
	fmt.Printf("   ├─ Tree:    %s (%d files)\n", cs.RootTreeHash[:min(10, len(cs.RootTreeHash))], len(currentTree.Entries))
	fmt.Printf("   └─ Status:  Ready to land into server queue. Run %s'scn submit'%s\n", ui.BrightCyan, ui.Reset)
}

func cmdSubmit(args []string) {
	cfg, manifest := mustLoadWorkspace()
	localCAS := mustLoadLocalCAS()
	cli := client.NewClient(cfg.ServerURL)

	if manifest.HeadChangesetID == manifest.BaseChangesetID {
		ui.PrintInfo("No new commits to submit. Working copy in sync with base.")
		return
	}

	start := time.Now()
	ui.PrintStep(1, 3, "Uploading new blobs to server CAS")

	// Collect chunks and manifests from current tree
	var chunksToUpload []*cas.BlobChunk
	var manifestsToUpload []*cas.BlobManifest
	for _, entry := range manifest.Tree.Entries {
		man, err := localCAS.GetManifest(entry.Hash)
		if err == nil {
			manifestsToUpload = append(manifestsToUpload, man)
			for _, ch := range man.ChunkHashes {
				chunk, err := localCAS.GetChunk(ch)
				if err == nil {
					chunksToUpload = append(chunksToUpload, chunk)
				}
			}
		}
	}

	if len(manifestsToUpload) > 0 {
		if err := cli.UploadManifests(manifestsToUpload); err != nil {
			ui.PrintError(fmt.Sprintf("CAS manifest upload failed: %v", err))
			os.Exit(1)
		}
	}
	if len(chunksToUpload) > 0 {
		if err := cli.UploadChunks(chunksToUpload); err != nil {
			ui.PrintError(fmt.Sprintf("CAS chunk upload failed: %v", err))
			os.Exit(1)
		}
	}
	ui.PrintStepDone(1, 3, fmt.Sprintf("Uploaded %d chunks, %d manifests to CAS", len(chunksToUpload), len(manifestsToUpload)))

	ui.PrintStep(2, 3, "Registering Merkle tree on server")
	if err := cli.SaveTree(manifest.Tree); err != nil {
		ui.PrintError(fmt.Sprintf("Tree registration failed: %v", err))
		os.Exit(1)
	}
	ui.PrintStepDone(2, 3, "Tree registered")

	ui.PrintStep(3, 3, "Submitting to linear merge queue & executing server-side rebase")
	commitMsg := manifest.CommitMessage
	if commitMsg == "" {
		commitMsg = "Automated Agent Change"
	}
	author := manifest.Author
	if author == "" {
		author = "Local Agent <agent@aether.local>"
	}

	cs := graph.NewChangeset(
		cfg.RepoID,
		manifest.Tree.RootHash,
		[]string{manifest.BaseChangesetID},
		author,
		commitMsg,
		cfg.AgentModel,
		cfg.SessionID,
		"",
	)
	cs.ID = manifest.HeadChangesetID

	result, err := cli.SubmitChangeset(cfg.RepoID, cs)
	if err != nil {
		ui.PrintError(fmt.Sprintf("Submission failed: %v", err))
		os.Exit(1)
	}

	if result.Status == "REBASE_CONFLICT" {
		ui.PrintError(fmt.Sprintf("Server Rebase Conflict: %s", result.ErrorMessage))
		for _, c := range result.Conflicts {
			fmt.Printf("   ✖ %s: %s\n", c.Path, c.Reason)
		}
		os.Exit(1)
	}

	// Successfully landed on main!
	manifest.BaseChangesetID = result.LandedCSID
	manifest.HeadChangesetID = result.LandedCSID
	manifest.SequenceNumber = result.SequenceNumber
	saveManifest(manifest)

	totalMs := time.Since(start).Milliseconds()
	ui.PrintSuccess(fmt.Sprintf("Landed in %smain%s as %sSequence #%d%s [%s] in %dms!",
		ui.BrightCyan, ui.Reset, ui.Bold+ui.BrightGreen, result.SequenceNumber, ui.Reset,
		result.LandedCSID[:min(8, len(result.LandedCSID))], totalMs))
}

func cmdLog(args []string) {
	cfg, _ := mustLoadWorkspace()
	cli := client.NewClient(cfg.ServerURL)

	limit := 20
	changesets, err := cli.QueryLog(cfg.RepoName, limit)
	if err != nil {
		ui.PrintError(fmt.Sprintf("Failed to query log: %v", err))
		os.Exit(1)
	}

	fmt.Printf("%s%sCommit Graph: %s (Linear Main Timeline)%s\n\n", ui.Bold, ui.BrightCyan, cfg.RepoName, ui.Reset)
	for i, cs := range changesets {
		isHead := i == 0
		headTag := ""
		if isHead {
			headTag = fmt.Sprintf(" %s(HEAD -> main)%s", ui.BrightGreen+ui.Bold, ui.Reset)
		}

		seqTag := fmt.Sprintf("%s[#%d]%s", ui.BrightYellow, cs.SequenceNumber, ui.Reset)
		fmt.Printf("%s*  %s %s%s %s\n", ui.Cyan, seqTag, cs.ShortID(), headTag, cs.Message)

		agentInfo := ""
		if cs.IsAgentGenerated() {
			agentInfo = fmt.Sprintf(" %s[%s]%s", ui.BrightPurple, cs.AgentModel, ui.Reset)
		}
		tm := time.Unix(cs.CreatedAt, 0).Format("2006-01-02 15:04:05")
		fmt.Printf("%s|  %sAuthor: %s%s%s%s | %s\n", ui.Cyan, ui.DarkGray, cs.Author, agentInfo, ui.DarkGray, ui.Reset, tm)
		if i < len(changesets)-1 {
			fmt.Printf("%s|%s\n", ui.Cyan, ui.Reset)
		}
	}
}

func cmdQuery(args []string) {
	if len(args) < 1 {
		ui.PrintError("Usage: scn query [file|blame|diff|queue] ...")
		os.Exit(1)
	}

	sub := args[0]
	cfg, _ := mustLoadWorkspace()
	cli := client.NewClient(cfg.ServerURL)

	switch sub {
	case "file":
		if len(args) < 2 {
			ui.PrintError("Usage: scn query file <path> [--rev <id>]")
			os.Exit(1)
		}
		path := args[1]
		rev := "HEAD"
		if len(args) >= 4 && args[2] == "--rev" {
			rev = args[3]
		}
		res, err := cli.QueryFile(cfg.RepoName, path, rev)
		if err != nil {
			ui.PrintError(err.Error())
			os.Exit(1)
		}
		fmt.Println(res.ContentText)

	case "blame":
		if len(args) < 2 {
			ui.PrintError("Usage: scn query blame <path>")
			os.Exit(1)
		}
		path := args[1]
		res, err := cli.QueryBlame(cfg.RepoName, path)
		if err != nil {
			ui.PrintError(err.Error())
			os.Exit(1)
		}
		for _, l := range res.Lines {
			agentTag := ""
			if l.AgentModel != "" {
				agentTag = fmt.Sprintf("[%s]", l.AgentModel)
			}
			fmt.Printf("%s%4d%s | %s#%-4d%s %s%-8s%s %-15s %s\n",
				ui.DarkGray, l.LineNumber, ui.Reset,
				ui.BrightYellow, l.CommitSeq, ui.Reset,
				ui.Cyan, l.CommitID, ui.Reset,
				agentTag,
				l.Content)
		}

	case "queue":
		status, err := cli.GetQueueStatus(cfg.RepoName)
		if err != nil {
			ui.PrintError(err.Error())
			os.Exit(1)
		}
		fmt.Println("\033[1;36m=== Live Merge Queue Status ===\033[0m")
		for k, v := range status {
			fmt.Printf("  %-25s: %v\n", k, v)
		}

	default:
		ui.PrintError(fmt.Sprintf("Unknown query subcommand: %s", sub))
	}
}

func cmdProfile(args []string) {
	cfg, _ := mustLoadWorkspace()
	if len(args) == 0 || args[0] == "list" {
		fmt.Println("Active Sparse Patterns for this workspace:")
		for _, p := range cfg.SparsePatterns {
			fmt.Printf("  • %s\n", p)
		}
		return
	}
	if args[0] == "set" && len(args) >= 2 {
		cfg.SparsePatterns = strings.Split(args[1], ",")
		saveConfig(cfg)
		ui.PrintSuccess(fmt.Sprintf("Updated sparse patterns to: %s", args[1]))
	}
}

func mustLoadWorkspace() (*Config, *Manifest) {
	cfgData, err := os.ReadFile(filepath.Join(".scn", "config.json"))
	if err != nil {
		ui.PrintError("Not an OpenScion workspace (no .scn/config.json found). Run 'scn init' or 'scn clone'.")
		os.Exit(1)
	}
	var cfg Config
	json.Unmarshal(cfgData, &cfg)

	manData, err := os.ReadFile(filepath.Join(".scn", "manifest.json"))
	if err != nil {
		ui.PrintError("Corrupt workspace: missing .scn/manifest.json")
		os.Exit(1)
	}
	var manifest Manifest
	json.Unmarshal(manData, &manifest)

	return &cfg, &manifest
}

func mustLoadLocalCAS() *cas.Store {
	store, err := cas.NewStore(filepath.Join(".scn", "cache"))
	if err != nil {
		ui.PrintError(fmt.Sprintf("Failed to open local CAS cache: %v", err))
		os.Exit(1)
	}
	return store
}

func saveConfig(c *Config) {
	data, _ := json.MarshalIndent(c, "", "  ")
	os.WriteFile(filepath.Join(".scn", "config.json"), data, 0644)
}

func saveManifest(m *Manifest) {
	data, _ := json.MarshalIndent(m, "", "  ")
	os.WriteFile(filepath.Join(".scn", "manifest.json"), data, 0644)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func cmdGC(args []string) {
	_, manifest := mustLoadWorkspace()
	localCAS := mustLoadLocalCAS()

	reachable := make(map[string]bool)
	if manifest.Tree != nil {
		for _, entry := range manifest.Tree.Entries {
			reachable[entry.Hash] = true
			man, err := localCAS.GetManifest(entry.Hash)
			if err == nil {
				reachable[man.BlobHash] = true
				for _, ch := range man.ChunkHashes {
					reachable[ch] = true
				}
			}
		}
	}

	deleted, freedBytes, err := localCAS.GC(reachable)
	if err != nil {
		ui.PrintError(fmt.Sprintf("Garbage collection failed: %v", err))
		os.Exit(1)
	}

	ui.PrintSuccess(fmt.Sprintf("Garbage collection complete: pruned %d unreferenced object(s), freed %.2f KB",
		deleted, float64(freedBytes)/1024.0))
}
