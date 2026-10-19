package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aether/scn/pkg/api"
	"github.com/aether/scn/pkg/cas"
	"github.com/aether/scn/pkg/db"
	"github.com/aether/scn/pkg/graph"
	"github.com/aether/scn/pkg/local"
	"github.com/aether/scn/pkg/tree"
	"github.com/aether/scn/pkg/ui"
)

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
	case "status":
		cmdStatus(args)
	case "diff":
		cmdDiff(args)
	case "commit":
		cmdCommit(args)
	case "log":
		cmdLog(args)
	case "blame":
		cmdBlame(args)
	case "show":
		cmdShow(args)
	case "clone":
		cmdClone(args)
	case "serve":
		cmdServe(args)
	case "help", "--help", "-h":
		printUsage()
	default:
		ui.PrintError(fmt.Sprintf("Unknown command %q. Run 'scion help' for usage.", cmd))
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Printf("%s%sOpenScion — Agent-Scale Terminal Version Control System%s\n\n", ui.Bold, ui.BrightCyan, ui.Reset)
	fmt.Println("Usage: scion <command> [options]")
	fmt.Println()
	fmt.Println("Core Git-like Commands (Runs directly in terminal):")
	fmt.Println("  init [repo-name]              Initialize an OpenScion repository in current directory")
	fmt.Println("  status                        Show working tree status, dirty files, and agent context")
	fmt.Println("  diff                          Inspect colorized unified diffs against HEAD")
	fmt.Println("  commit -m <msg>               Commit changes to local DAG with agent provenance")
	fmt.Println("  log                           View commit timeline with linear sequence numbers")
	fmt.Println("  blame <path>                  Show line-by-line blame with sequence numbers and authors")
	fmt.Println("  show [commit-id]              Display commit details and tree structure")
	fmt.Println("  clone <source> [dir]          Fast sparse clone from local path or remote repository")
	fmt.Println()
	fmt.Println("Server & Collaboration:")
	fmt.Println("  serve [--port 8080]           Start an OpenScion central hub server for multi-agent teams")
	fmt.Println()
	fmt.Println("Flags:")
	fmt.Println("  --agent <model>               Tag commit with AI agent model (e.g. claude-code-3.7-sonnet)")
	fmt.Println("  --sparse <patterns>           Specify sparse path filter (e.g. 'src/**,config/*')")
	fmt.Println()
}

func cmdInit(args []string) {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	fs.Parse(args)

	repoName := "project"
	if len(fs.Args()) > 0 {
		repoName = fs.Args()[0]
	} else {
		// Use current directory name
		wd, err := os.Getwd()
		if err == nil {
			repoName = filepath.Base(wd)
		}
	}

	repo, err := local.InitLocalRepo(".", repoName)
	if err != nil {
		ui.PrintError(fmt.Sprintf("Failed to initialize repository: %v", err))
		os.Exit(1)
	}
	defer repo.Close()

	ui.PrintSuccess(fmt.Sprintf("Initialized empty OpenScion repository in .scion/"))
	ui.PrintBox("OpenScion: Repository Initialized", []string{
		fmt.Sprintf("Repository:     %s%s%s", ui.Bold, repo.Config.RepoName, ui.Reset),
		fmt.Sprintf("Branch:         %smain%s (Sequence #1)", ui.BrightCyan, ui.Reset),
		fmt.Sprintf("Engine:         Embedded Merkle CAS + SQLite DAG", ),
		fmt.Sprintf("Agent Tracking: %s%s %s%s", ui.BrightPurple, ui.SymAgent, repo.Config.AgentModel, ui.Reset),
		"",
		fmt.Sprintf("Run %s'scion status'%s or %s'scion commit'%s to begin.", ui.BrightCyan, ui.Reset, ui.BrightCyan, ui.Reset),
	})
}

func cmdStatus(args []string) {
	repo, err := local.OpenLocalRepo(".")
	if err != nil {
		ui.PrintError(err.Error())
		os.Exit(1)
	}
	defer repo.Close()

	diffs, _, headCS, err := repo.Status()
	if err != nil {
		ui.PrintError(fmt.Sprintf("Status error: %v", err))
		os.Exit(1)
	}

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
		fmt.Sprintf("Repository:    %s%s%s", ui.Bold, repo.Config.RepoName, ui.Reset),
		fmt.Sprintf("Branch:        %s%s%s (Sequence #%d)", ui.BrightCyan, repo.Config.Branch, ui.Reset, headCS.SequenceNumber),
		fmt.Sprintf("Head Commit:   %s%s%s", ui.DarkGray, headCS.ShortID(), ui.Reset),
		fmt.Sprintf("Agent Context: %s%s %s%s", ui.BrightPurple, ui.SymAgent, repo.Config.AgentModel, ui.Reset),
		"",
		fmt.Sprintf("%sChanges:%s", ui.Bold, ui.Reset),
	}
	lines = append(lines, changesLines...)

	ui.PrintBox("OpenScion: Workspace Status", lines)
}

func cmdDiff(args []string) {
	repo, err := local.OpenLocalRepo(".")
	if err != nil {
		ui.PrintError(err.Error())
		os.Exit(1)
	}
	defer repo.Close()

	diffs, currentTree, headCS, err := repo.Status()
	if err != nil {
		ui.PrintError(err.Error())
		os.Exit(1)
	}

	if len(diffs) == 0 {
		fmt.Println("No modifications detected.")
		return
	}

	headTree, _ := repo.DB.GetTree(headCS.RootTreeHash)

	for _, d := range diffs {
		fmt.Printf("\n%s--- a/%s%s\n", ui.Red, d.Path, ui.Reset)
		fmt.Printf("%s+++ b/%s%s\n", ui.Green, d.Path, ui.Reset)

		var oldLines, newLines []string
		if headTree != nil {
			if oldEntry, ok := headTree.Entries[d.Path]; ok {
				oldData, _ := repo.Store.GetBlobByHash(oldEntry.Hash)
				oldLines = strings.Split(string(oldData), "\n")
			}
		}
		if currentTree != nil {
			if newEntry, ok := currentTree.Entries[d.Path]; ok {
				newData, _ := repo.Store.GetBlobByHash(newEntry.Hash)
				newLines = strings.Split(string(newData), "\n")
			}
		}

		fmt.Printf("%s@@ -1,%d +1,%d @@%s\n", ui.Cyan, len(oldLines), len(newLines), ui.Reset)
		for _, l := range oldLines {
			if l != "" {
				fmt.Printf("%s-%s%s\n", ui.Red, l, ui.Reset)
			}
		}
		for _, l := range newLines {
			if l != "" {
				fmt.Printf("%s+%s%s\n", ui.Green, l, ui.Reset)
			}
		}
	}
}

func cmdCommit(args []string) {
	fs := flag.NewFlagSet("commit", flag.ExitOnError)
	msgFlag := fs.String("m", "", "Commit message (required)")
	agentFlag := fs.String("agent", "", "Override agent model")
	traceFlag := fs.String("trace", "", "Prompt trace or task ID")
	fs.Parse(args)

	if *msgFlag == "" {
		ui.PrintError("Commit message is required: scion commit -m \"<message>\"")
		os.Exit(1)
	}

	repo, err := local.OpenLocalRepo(".")
	if err != nil {
		ui.PrintError(err.Error())
		os.Exit(1)
	}
	defer repo.Close()

	cs, err := repo.Commit(*msgFlag, "", *agentFlag, "", *traceFlag)
	if err != nil {
		ui.PrintError(err.Error())
		os.Exit(1)
	}

	agentTag := ""
	if cs.IsAgentGenerated() {
		agentTag = fmt.Sprintf(" %s[%s]", ui.SymAgent, cs.AgentModel)
	}

	ui.PrintSuccess(fmt.Sprintf("[%s #%d %s] %s", repo.Config.Branch, cs.SequenceNumber, cs.ShortID(), cs.Message))
	fmt.Printf("   ├─ Author: %s%s\n", cs.Author, agentTag)
	fmt.Printf("   └─ Tree:   %s\n", cs.RootTreeHash[:min(10, len(cs.RootTreeHash))])
}

func cmdLog(args []string) {
	repo, err := local.OpenLocalRepo(".")
	if err != nil {
		ui.PrintError(err.Error())
		os.Exit(1)
	}
	defer repo.Close()

	changesets, err := repo.Log(50)
	if err != nil {
		ui.PrintError(fmt.Sprintf("Failed to query log: %v", err))
		os.Exit(1)
	}

	fmt.Printf("%s%sOpenScion Timeline: %s (Sequence Graph)%s\n\n", ui.Bold, ui.BrightCyan, repo.Config.RepoName, ui.Reset)
	for i, cs := range changesets {
		isHead := i == 0
		headTag := ""
		if isHead {
			headTag = fmt.Sprintf(" %s(HEAD -> %s)%s", ui.BrightGreen+ui.Bold, repo.Config.Branch, ui.Reset)
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

func cmdBlame(args []string) {
	if len(args) < 1 {
		ui.PrintError("Usage: scion blame <path>")
		os.Exit(1)
	}

	repo, err := local.OpenLocalRepo(".")
	if err != nil {
		ui.PrintError(err.Error())
		os.Exit(1)
	}
	defer repo.Close()

	relPath := args[0]
	lines, err := repo.Blame(relPath)
	if err != nil {
		ui.PrintError(err.Error())
		os.Exit(1)
	}

	for _, l := range lines {
		fmt.Println(l)
	}
}

func cmdShow(args []string) {
	repo, err := local.OpenLocalRepo(".")
	if err != nil {
		ui.PrintError(err.Error())
		os.Exit(1)
	}
	defer repo.Close()

	var cs *graph.Changeset
	if len(args) > 0 {
		cs, err = repo.DB.GetChangeset(args[0])
	} else {
		cs, err = repo.GetHeadChangeset()
	}

	if err != nil {
		ui.PrintError(fmt.Sprintf("Failed to load commit: %v", err))
		os.Exit(1)
	}

	fmt.Printf("%sCommit:   %s (Sequence #%d)%s\n", ui.Bold+ui.BrightYellow, cs.ID, cs.SequenceNumber, ui.Reset)
	fmt.Printf("Author:   %s\n", cs.Author)
	if cs.IsAgentGenerated() {
		fmt.Printf("Agent:    %s %s (Session: %s)\n", ui.SymAgent, cs.AgentModel, cs.SessionID)
	}
	fmt.Printf("Date:     %s\n", time.Unix(cs.CreatedAt, 0).Format("2006-01-02 15:04:05"))
	fmt.Printf("Tree:     %s\n\n", cs.RootTreeHash)
	fmt.Printf("    %s\n\n", cs.Message)

	t, err := repo.DB.GetTree(cs.RootTreeHash)
	if err == nil {
		fmt.Printf("%sFiles in Tree (%d):%s\n", ui.Bold, len(t.Entries), ui.Reset)
		for p, e := range t.Entries {
			fmt.Printf("  • %-35s %s\n", p, e.Hash[:10])
		}
	}
}

func cmdClone(args []string) {
	fs := flag.NewFlagSet("clone", flag.ExitOnError)
	sparseFlag := fs.String("sparse", "", "Comma-separated sparse path patterns (e.g. 'src/**,config/*')")
	fs.Parse(args)

	if len(fs.Args()) < 1 {
		ui.PrintError("Usage: scion clone <source-dir> [target-dir] [--sparse <patterns>]")
		os.Exit(1)
	}

	source := fs.Args()[0]
	target := filepath.Base(source)
	if len(fs.Args()) >= 2 {
		target = fs.Args()[1]
	}

	start := time.Now()
	sourceRepo, err := local.OpenLocalRepo(source)
	if err != nil {
		ui.PrintError(fmt.Sprintf("Failed to open source repo %s: %v", source, err))
		os.Exit(1)
	}
	defer sourceRepo.Close()

	headCS, err := sourceRepo.GetHeadChangeset()
	if err != nil {
		ui.PrintError(err.Error())
		os.Exit(1)
	}

	headTree, err := sourceRepo.DB.GetTree(headCS.RootTreeHash)
	if err != nil {
		ui.PrintError(err.Error())
		os.Exit(1)
	}

	var patterns []string
	if *sparseFlag != "" {
		patterns = strings.Split(*sparseFlag, ",")
		headTree = headTree.FilterSparse(patterns)
	}

	targetRepo, err := local.InitLocalRepo(target, sourceRepo.Config.RepoName)
	if err != nil {
		ui.PrintError(fmt.Sprintf("Failed to initialize target workspace: %v", err))
		os.Exit(1)
	}
	defer targetRepo.Close()

	// Materialize files
	for relPath, entry := range headTree.Entries {
		data, err := sourceRepo.Store.GetBlobByHash(entry.Hash)
		if err != nil {
			continue
		}
		fullPath := filepath.Join(target, filepath.FromSlash(relPath))
		os.MkdirAll(filepath.Dir(fullPath), 0755)
		os.WriteFile(fullPath, data, os.FileMode(entry.Mode))
		targetRepo.Store.PutBlob(data)
	}

	// Copy changesets and tree
	targetRepo.DB.SaveTree(headTree)
	targetRepo.DB.SaveChangeset(headCS)
	targetRepo.DB.UpdateBranchTip(targetRepo.Config.RepoID, "main", headCS.ID, headCS.SequenceNumber)

	elapsed := time.Since(start).Milliseconds()
	ui.PrintSuccess(fmt.Sprintf("Cloned into %q (%d files materialized) in %dms!", target, len(headTree.Entries), elapsed))
}

func cmdServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	port := fs.String("port", "8080", "Server port")
	dataDir := fs.String("data-dir", "./data/server", "Storage directory")
	fs.Parse(args)

	os.MkdirAll(*dataDir, 0755)
	dbPath := filepath.Join(*dataDir, "openscion.db")

	store, err := cas.NewStore(filepath.Join(*dataDir, "cas"))
	if err != nil {
		log.Fatalf("CAS error: %v", err)
	}
	database, err := db.Open(dbPath)
	if err != nil {
		log.Fatalf("Database error: %v", err)
	}
	defer database.Close()

	server := api.NewServer(database, store)
	fmt.Printf("%s%sOpenScion Server Hub listening on http://0.0.0.0:%s%s\n", ui.Bold, ui.BrightCyan, *port, ui.Reset)
	http.ListenAndServe(":"+*port, server)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
