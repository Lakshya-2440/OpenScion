package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aether/scn/pkg/api"
	"github.com/aether/scn/pkg/cas"
	"github.com/aether/scn/pkg/db"
	"github.com/aether/scn/pkg/graph"
	"github.com/aether/scn/pkg/local"
	"github.com/aether/scn/pkg/merge"
	"github.com/aether/scn/pkg/tree"
	"github.com/aether/scn/pkg/ui"
	"github.com/aether/scn/pkg/vfs"
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
	case "checkout", "switch", "restore":
		cmdCheckout(args)
	case "branch":
		cmdBranch(args)
	case "log":
		cmdLog(args)
	case "blame":
		cmdBlame(args)
	case "show":
		cmdShow(args)
	case "clone":
		cmdClone(args)
	case "mount":
		cmdMount(args)
	case "remote":
		cmdRemote(args)
	case "push", "submit":
		cmdPush(args)
	case "pull":
		cmdPull(args)
	case "sync":
		cmdSync(args)
	case "serve":
		cmdServe(args)
	case "gc":
		cmdGC(args)
	case "stash":
		cmdStash(args)
	case "sparse":
		cmdSparse(args)
	case "bench":
		cmdBench(args)
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
	fmt.Println("Core Git-like Commands (Runs directly on machine):")
	fmt.Println("  init [repo-name]              Initialize an OpenScion repository in current directory")
	fmt.Println("  status [-v]                   Show working tree status and categorized changes")
	fmt.Println("  diff [-s|-a] [file]           Inspect colorized diffs (summary, hunk view, or stat)")
	fmt.Println("  commit -m <msg>               Commit changes to local DAG with agent provenance")
	fmt.Println("  branch [name]                 List or create branches")
	fmt.Println("  checkout <path|branch> [--rev #seq] Restore file, switch branch, or restore revision")
	fmt.Println("  log                           View clean commit timeline with linear sequence numbers")
	fmt.Println("  blame <path>                  Show line-by-line blame with sequence numbers and authors")
	fmt.Println("  show [commit-id|#seq]         Display commit details and tree structure")
	fmt.Println("  clone <source> [dir]          Fast sparse clone from local path or remote repository")
	fmt.Println("  mount [dir] [--rev #seq]      Mount virtual filesystem (instant 0-byte ghost tree, lazy fetch)")
	fmt.Println("  stash [push|pop|list|show|drop] Stash dirty working state into CAS stack")
	fmt.Println("  sparse [list|set|add|clear]   Manage sparse checkout patterns and materialization")
	fmt.Println("  bench                         Run reproducible performance benchmarks")
	fmt.Println()
	fmt.Println("Remote & Team Collaboration:")
	fmt.Println("  remote [add|set-url|rm] <url> Configure remote team server (origin)")
	fmt.Println("  push                          Push local changesets to remote merge queue")
	fmt.Println("  pull                          Pull latest changesets and materialize updated files")
	fmt.Println("  sync                          Pull and push in a single command")
	fmt.Println("  serve [--port] [--db URL]     Launch central server (supports SQLite & PostgreSQL/CockroachDB)")
	fmt.Println()
	fmt.Println("Flags:")
	fmt.Println("  --agent <model>               Tag commit with AI agent model (e.g. claude-code-3.7-sonnet)")
	fmt.Println("  --sparse <patterns>           Specify sparse path filter (e.g. 'src/**,config/*')")
	fmt.Println("  --db, --database-url          PostgreSQL or SQLite connection string for server")
	fmt.Println()
}

// parseFlagsFlexible parses flags regardless of whether they appear before, after, or among positional arguments.
// Returns the positional arguments in order.
func parseFlagsFlexible(fs *flag.FlagSet, args []string) []string {
	var flagArgs []string
	var posArgs []string
	stopParsingFlags := false

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if stopParsingFlags {
			posArgs = append(posArgs, arg)
			continue
		}

		if arg == "--" {
			stopParsingFlags = true
			continue
		}

		if strings.HasPrefix(arg, "-") && arg != "-" {
			flagStr := strings.TrimLeft(arg, "-")
			eqIdx := strings.Index(flagStr, "=")
			flagName := flagStr
			if eqIdx != -1 {
				flagName = flagStr[:eqIdx]
			}

			f := fs.Lookup(flagName)
			if f != nil {
				type boolFlag interface {
					IsBoolFlag() bool
				}
				isBool := false
				if bf, ok := f.Value.(boolFlag); ok && bf.IsBoolFlag() {
					isBool = true
				}

				if eqIdx != -1 || isBool {
					flagArgs = append(flagArgs, arg)
				} else {
					flagArgs = append(flagArgs, arg)
					if i+1 < len(args) {
						flagArgs = append(flagArgs, args[i+1])
						i++
					}
				}
			} else {
				flagArgs = append(flagArgs, arg)
			}
		} else {
			posArgs = append(posArgs, arg)
		}
	}

	_ = fs.Parse(flagArgs)
	return posArgs
}

func cmdInit(args []string) {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	posArgs := parseFlagsFlexible(fs, args)

	repoName := "project"
	if len(posArgs) > 0 {
		repoName = posArgs[0]
	} else {
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

	fmt.Println()
	fmt.Printf("%s✔ Initialized empty OpenScion repository%s in .scion/\n", ui.BrightGreen+ui.Bold, ui.Reset)
	fmt.Printf("  • Repository:  %s%s%s\n", ui.Bold, repo.Config.RepoName, ui.Reset)
	fmt.Printf("  • Branch:      %smain%s (Sequence #1)\n", ui.BrightCyan, ui.Reset)
	fmt.Printf("  • Engine:      Embedded Merkle CAS + ACID SQLite\n")
	fmt.Printf("  • Tracking:    %s%s %s%s\n", ui.BrightPurple, ui.SymAgent, repo.Config.AgentModel, ui.Reset)
	fmt.Printf("  • Agent Rules: %sAuto-generated AGENTS.md & CLAUDE.md%s\n\n", ui.BrightGreen, ui.Reset)
	fmt.Printf("Run %s'scion status'%s or %s'scion commit -m <msg>'%s to begin.\n\n", ui.BrightCyan, ui.Reset, ui.BrightCyan, ui.Reset)
}

func cmdStatus(args []string) {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	verboseFlag := fs.Bool("v", false, "Show all files without truncation")
	parseFlagsFlexible(fs, args)

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

	// Header
	fmt.Printf("\n%sOn branch %s%s%s  •  Sequence %s#%d%s  (head: %s%s%s)\n",
		ui.Dim, ui.Reset+ui.BrightCyan+ui.Bold, repo.Config.Branch, ui.Reset+ui.Dim,
		ui.BrightYellow, headCS.SequenceNumber, ui.Reset+ui.Dim,
		ui.Cyan, headCS.ShortID(), ui.Reset)
	if repo.Config.AgentModel != "" {
		fmt.Printf("%sAgent Context: %s%s %s%s\n\n", ui.Dim, ui.BrightPurple, ui.SymAgent, repo.Config.AgentModel, ui.Reset)
	} else {
		fmt.Println()
	}

	if len(diffs) == 0 {
		fmt.Printf("%snothing to commit, working tree clean%s\n\n", ui.Dim, ui.Reset)
		return
	}

	var added, modified, deleted []tree.DiffEntry
	for _, d := range diffs {
		switch d.ChangeType {
		case tree.ChangeAdded:
			added = append(added, d)
		case tree.ChangeModified:
			modified = append(modified, d)
		case tree.ChangeDeleted:
			deleted = append(deleted, d)
		}
	}

	fmt.Printf("%sChanges to be committed:%s\n", ui.Bold, ui.Reset)
	fmt.Printf("  %s(use \"scion commit -m <msg>\" to commit)%s\n\n", ui.Dim, ui.Reset)

	printSection := func(title, color, prefix string, items []tree.DiffEntry) {
		if len(items) == 0 {
			return
		}
		fmt.Printf("  %s%s (%d):%s\n", ui.Bold, title, len(items), ui.Reset)
		limit := len(items)
		truncated := false
		if !*verboseFlag && limit > 12 {
			limit = 8
			truncated = true
		}
		for i := 0; i < limit; i++ {
			fmt.Printf("    %s%s%s  %s\n", color, prefix, ui.Reset, items[i].Path)
		}
		if truncated {
			fmt.Printf("    %s... and %d more files (run 'scion status -v' to view all)%s\n", ui.Dim, len(items)-limit, ui.Reset)
		}
		fmt.Println()
	}

	printSection("New Files", ui.BrightGreen, "+", added)
	printSection("Modified", ui.BrightYellow, "~", modified)
	printSection("Deleted", ui.Red, "-", deleted)

	fmt.Printf("%sSummary:%s %s%d file(s) staged%s (%d added, %d modified, %d deleted)\n\n",
		ui.Bold, ui.Reset, ui.BrightCyan, len(diffs), ui.Reset, len(added), len(modified), len(deleted))
}

func cmdDiff(args []string) {
	fs := flag.NewFlagSet("diff", flag.ExitOnError)
	statOnly := fs.Bool("s", false, "Show diffstat only")
	showAll := fs.Bool("a", false, "Show full diff without truncation")
	posArgs := parseFlagsFlexible(fs, args)

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

	targetPath := ""
	if len(posArgs) > 0 {
		targetPath = posArgs[0]
	}

	if len(diffs) == 0 {
		fmt.Println("No modifications detected.")
		return
	}

	headTree, _ := repo.DB.GetTree(headCS.RootTreeHash)

	// Filter by target path if specified
	var activeDiffs []tree.DiffEntry
	for _, d := range diffs {
		if targetPath == "" || d.Path == targetPath || strings.HasPrefix(d.Path, targetPath) {
			activeDiffs = append(activeDiffs, d)
		}
	}

	if len(activeDiffs) == 0 {
		fmt.Printf("No changes found matching %q.\n", targetPath)
		return
	}

	// 1. Diff Stat Summary
	fmt.Printf("\n%sDiffstat (%d files):%s\n", ui.Bold, len(activeDiffs), ui.Reset)
	totalAdd := 0
	totalDel := 0

	type fileStat struct {
		path     string
		adds     int
		dels     int
		oldLines []string
		newLines []string
	}
	var stats []fileStat

	for _, d := range activeDiffs {
		var oldLines, newLines []string
		if headTree != nil {
			if oldEntry, ok := headTree.Entries[d.Path]; ok {
				oldData, _ := repo.Store.GetBlobByHash(oldEntry.Hash)
				oldLines = splitLines(string(oldData))
			}
		}
		if currentTree != nil {
			if newEntry, ok := currentTree.Entries[d.Path]; ok {
				newData, _ := repo.Store.GetBlobByHash(newEntry.Hash)
				newLines = splitLines(string(newData))
			}
		}

		adds := 0
		dels := 0
		if len(oldLines) == 0 && len(newLines) > 0 {
			adds = len(newLines)
		} else if len(newLines) == 0 && len(oldLines) > 0 {
			dels = len(oldLines)
		} else {
			// Count differences
			adds = max(0, len(newLines)-len(oldLines))
			dels = max(0, len(oldLines)-len(newLines))
			if adds == 0 && dels == 0 {
				adds = 1
			}
		}
		totalAdd += adds
		totalDel += dels

		stats = append(stats, fileStat{
			path:     d.Path,
			adds:     adds,
			dels:     dels,
			oldLines: oldLines,
			newLines: newLines,
		})

		bar := strings.Repeat("+", min(20, adds)) + strings.Repeat("-", min(10, dels))
		fmt.Printf("  %-38s │ %3d %s%s%s%s%s\n", d.Path, adds+dels, ui.BrightGreen, bar[:min(len(bar), adds)], ui.Red, bar[min(len(bar), adds):], ui.Reset)
	}

	fmt.Printf("  %s%d file(s) changed, %d insertions(+), %d deletions(-)%s\n\n",
		ui.Dim, len(activeDiffs), totalAdd, totalDel, ui.Reset)

	if *statOnly {
		return
	}

	// 2. Clean Per-File Hunk Display
	for _, st := range stats {
		divider := strings.Repeat("─", max(20, 75-len(st.path)-5))
		fmt.Printf("%s─── %s%s%s %s%s\n", ui.Cyan, ui.Bold+ui.White, st.path, ui.Reset+ui.Cyan, divider, ui.Reset)

		// New file addition
		if len(st.oldLines) == 0 {
			fmt.Printf("  %s(new file)%s\n", ui.BrightGreen, ui.Reset)
			displayLimit := len(st.newLines)
			truncated := false
			if !*showAll && targetPath == "" && displayLimit > 15 {
				displayLimit = 10
				truncated = true
			}
			for i := 0; i < displayLimit; i++ {
				fmt.Printf("  %s%4d%s │ %s+%s %s\n", ui.DarkGray, i+1, ui.Reset, ui.BrightGreen, ui.Reset, st.newLines[i])
			}
			if truncated {
				fmt.Printf("  %s... (+%d more lines omitted. Run 'scion diff -a' to view full file)%s\n",
					ui.Dim, len(st.newLines)-displayLimit, ui.Reset)
			}
			fmt.Println()
			continue
		}

		// Deleted file
		if len(st.newLines) == 0 {
			fmt.Printf("  %s(deleted file)%s\n", ui.Red, ui.Reset)
			for i := 0; i < min(10, len(st.oldLines)); i++ {
				fmt.Printf("  %s%4d%s │ %s-%s %s\n", ui.DarkGray, i+1, ui.Reset, ui.Red, ui.Reset, st.oldLines[i])
			}
			fmt.Println()
			continue
		}

		// Modified file: unified hunk view using Myers diff
		edits := merge.MyersDiff(st.oldLines, st.newLines)
		hunks := merge.GenerateUnifiedHunks(edits, 3)
		for _, h := range hunks {
			fmt.Printf("  %s@@ -%d,%d +%d,%d @@%s\n", ui.Cyan, h.OldStart, h.OldCount, h.NewStart, h.NewCount, ui.Reset)
			for _, l := range h.Lines {
				if len(l) == 0 {
					continue
				}
				prefix := l[0]
				content := l[1:]
				switch prefix {
				case '-':
					fmt.Printf("  %s-%s %s\n", ui.Red, ui.Reset, content)
				case '+':
					fmt.Printf("  %s+%s %s\n", ui.BrightGreen, ui.Reset, content)
				default:
					fmt.Printf("  %s  %s%s\n", ui.DarkGray, ui.Reset, content)
				}
			}
		}
		fmt.Println()
	}
}

func cmdCommit(args []string) {
	fs := flag.NewFlagSet("commit", flag.ExitOnError)
	msgFlag := fs.String("m", "", "Commit message (required)")
	agentFlag := fs.String("agent", "", "Override agent model")
	sessionFlag := fs.String("session", "", "Agent session ID")
	traceFlag := fs.String("trace", "", "Prompt trace or task ID")
	parseFlagsFlexible(fs, args)

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

	diffs, _, _, _ := repo.Status()
	fileCount := len(diffs)

	cs, err := repo.Commit(*msgFlag, "", *agentFlag, *sessionFlag, *traceFlag)
	if err != nil {
		ui.PrintError(err.Error())
		os.Exit(1)
	}

	agentTag := ""
	if cs.IsAgentGenerated() {
		agentTag = fmt.Sprintf("  •  %s%s %s%s", ui.BrightPurple, ui.SymAgent, cs.AgentModel, ui.Reset)
	}

	fmt.Printf("\n%s✔ [%s #%d %s]%s %s\n",
		ui.BrightGreen+ui.Bold, repo.Config.Branch, cs.SequenceNumber, cs.ShortID(), ui.Reset+ui.Bold, cs.Message)
	fmt.Printf("   %s%d file(s) changed%s%s\n", ui.Dim, fileCount, ui.Reset, agentTag)
	fmt.Printf("   %sTree root: %s%s\n\n", ui.Dim, cs.RootTreeHash[:min(12, len(cs.RootTreeHash))], ui.Reset)
}

func cmdBranch(args []string) {
	fs := flag.NewFlagSet("branch", flag.ExitOnError)
	deleteFlag := fs.String("d", "", "Delete branch")
	fs.StringVar(deleteFlag, "delete", "", "Delete branch")
	posArgs := parseFlagsFlexible(fs, args)

	repo, err := local.OpenLocalRepo(".")
	if err != nil {
		ui.PrintError(err.Error())
		os.Exit(1)
	}
	defer repo.Close()

	if *deleteFlag != "" {
		if err := repo.DeleteBranch(*deleteFlag); err != nil {
			ui.PrintError(fmt.Sprintf("Failed to delete branch %q: %v", *deleteFlag, err))
			os.Exit(1)
		}
		ui.PrintSuccess(fmt.Sprintf("Deleted branch %q", *deleteFlag))
		return
	}

	if len(posArgs) == 0 {
		branches, err := repo.ListBranches()
		if err != nil {
			ui.PrintError(fmt.Sprintf("Failed to list branches: %v", err))
			os.Exit(1)
		}
		for _, b := range branches {
			prefix := "  "
			color := ui.Reset
			if b.Name == repo.Config.Branch {
				prefix = "* "
				color = ui.BrightGreen + ui.Bold
			}
			fmt.Printf("%s%s%s%s (seq #%d)\n", color, prefix, b.Name, ui.Reset, b.SequenceNumber)
		}
		return
	}

	newBranch := posArgs[0]
	if err := repo.CreateBranch(newBranch); err != nil {
		ui.PrintError(fmt.Sprintf("Failed to create branch: %v", err))
		os.Exit(1)
	}
	ui.PrintSuccess(fmt.Sprintf("Created branch %q", newBranch))
}

func cmdCheckout(args []string) {
	fs := flag.NewFlagSet("checkout", flag.ExitOnError)
	revFlag := fs.String("rev", "", "Commit sequence number (#seq) or ID to restore from")
	branchFlag := fs.Bool("b", false, "Create and switch to a new branch")
	posArgs := parseFlagsFlexible(fs, args)

	repo, err := local.OpenLocalRepo(".")
	if err != nil {
		ui.PrintError(err.Error())
		os.Exit(1)
	}
	defer repo.Close()

	if len(posArgs) == 0 {
		if *revFlag != "" {
			count, err := repo.RestoreAll(*revFlag)
			if err != nil {
				ui.PrintError(fmt.Sprintf("Failed to checkout revision %s: %v", *revFlag, err))
				os.Exit(1)
			}
			ui.PrintSuccess(fmt.Sprintf("Checked out revision %s (%d files materialized)", *revFlag, count))
			return
		}
		ui.PrintError("Usage: scion checkout [-b] <branch> OR scion checkout [--rev #seq] <path>")
		os.Exit(1)
	}

	target := posArgs[0]

	// 1. If -b flag is set, create and switch
	if *branchFlag {
		if err := repo.CreateBranch(target); err != nil {
			ui.PrintError(fmt.Sprintf("Failed to create branch: %v", err))
			os.Exit(1)
		}
		if err := repo.SwitchBranch(target); err != nil {
			ui.PrintError(fmt.Sprintf("Failed to switch to branch: %v", err))
			os.Exit(1)
		}
		ui.PrintSuccess(fmt.Sprintf("Switched to new branch %q", target))
		return
	}

	// 2. Check if target is a branch name
	branches, err := repo.ListBranches()
	if err == nil {
		for _, b := range branches {
			if b.Name == target {
				if err := repo.SwitchBranch(target); err != nil {
					ui.PrintError(fmt.Sprintf("Failed to switch branch: %v", err))
					os.Exit(1)
				}
				ui.PrintSuccess(fmt.Sprintf("Switched to branch %q", target))
				return
			}
		}
	}

	// 3. Otherwise treat target as a file path to restore
	if target == "." {
		count, err := repo.RestoreAll(*revFlag)
		if err != nil {
			ui.PrintError(fmt.Sprintf("Failed to restore workspace: %v", err))
			os.Exit(1)
		}
		ui.PrintSuccess(fmt.Sprintf("Restored working directory (%d files)", count))
		return
	}

	if err := repo.RestoreFile(target, *revFlag); err != nil {
		ui.PrintError(fmt.Sprintf("Checkout failed: %v", err))
		os.Exit(1)
	}
	revName := "HEAD"
	if *revFlag != "" {
		revName = *revFlag
	}
	ui.PrintSuccess(fmt.Sprintf("Restored %q from %s", target, revName))
}

func cmdLog(args []string) {
	fs := flag.NewFlagSet("log", flag.ExitOnError)
	limitFlag := fs.Int("n", 50, "Limit number of commits displayed")
	parseFlagsFlexible(fs, args)

	repo, err := local.OpenLocalRepo(".")
	if err != nil {
		ui.PrintError(err.Error())
		os.Exit(1)
	}
	defer repo.Close()

	changesets, err := repo.Log(*limitFlag)
	if err != nil {
		ui.PrintError(fmt.Sprintf("Failed to query log: %v", err))
		os.Exit(1)
	}

	fmt.Printf("\n%sOpenScion Timeline: %s%s%s (Sequence Graph)%s\n\n",
		ui.Bold, ui.Reset+ui.BrightCyan, repo.Config.RepoName, ui.Bold, ui.Reset)

	for i, cs := range changesets {
		isHead := i == 0
		headTag := ""
		if isHead {
			headTag = fmt.Sprintf(" %s(HEAD -> %s)%s", ui.BrightGreen+ui.Bold, repo.Config.Branch, ui.Reset)
		}

		seqTag := fmt.Sprintf("%s#%-3d%s", ui.BrightYellow+ui.Bold, cs.SequenceNumber, ui.Reset)
		fmt.Printf("%s*%s   %s  %s%s%s%s  %s\n",
			ui.BrightCyan, ui.Reset, seqTag, ui.Cyan, cs.ShortID(), ui.Reset, headTag, time.Unix(cs.CreatedAt, 0).Format("2006-01-02 15:04:05"))

		connector := "│"
		if i == len(changesets)-1 {
			connector = " "
		}

		agentTag := ""
		if cs.IsAgentGenerated() {
			agentTag = fmt.Sprintf("  •  %s%s %s%s", ui.BrightPurple, ui.SymAgent, cs.AgentModel, ui.Reset)
		}

		fmt.Printf("%s%s%s   %s%s%s\n", ui.BrightCyan, connector, ui.Reset, ui.Bold, cs.Message, ui.Reset)
		fmt.Printf("%s%s%s   %s%s%s%s\n", ui.BrightCyan, connector, ui.Reset, ui.Dim, cs.Author, ui.Reset, agentTag)

		if i < len(changesets)-1 {
			fmt.Printf("%s│%s\n", ui.BrightCyan, ui.Reset)
		}
	}
	fmt.Println()
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

	fmt.Printf("\n%sBlame: %s%s\n", ui.Bold+ui.Cyan, relPath, ui.Reset)
	fmt.Printf("%s Line │ Commit   Seq  Author       Code%s\n", ui.Dim, ui.Reset)
	fmt.Printf("%s──────┼─────────────────────────────────────────────────────────────%s\n", ui.Dim, ui.Reset)
	for _, l := range lines {
		fmt.Println(l)
	}
	fmt.Println()
}

func cmdShow(args []string) {
	fs := flag.NewFlagSet("show", flag.ExitOnError)
	allFlag := fs.Bool("all", false, "Show all files without truncation")
	posArgs := parseFlagsFlexible(fs, args)

	repo, err := local.OpenLocalRepo(".")
	if err != nil {
		ui.PrintError(err.Error())
		os.Exit(1)
	}
	defer repo.Close()

	var cs *graph.Changeset
	refArg := ""
	if len(posArgs) > 0 {
		refArg = posArgs[0]
		cs, err = repo.ResolveChangeset(refArg)
	} else {
		cs, err = repo.GetHeadChangeset()
	}

	if err != nil {
		ui.PrintError(fmt.Sprintf("Failed to load commit: %v", err))
		os.Exit(1)
	}

	fmt.Printf("\n%sCommit:   %s%s%s (Sequence %s#%d%s)\n",
		ui.Bold, ui.Cyan, cs.ID, ui.Reset, ui.BrightYellow, cs.SequenceNumber, ui.Reset)
	fmt.Printf("Author:   %s\n", cs.Author)
	if cs.IsAgentGenerated() {
		fmt.Printf("Agent:    %s%s %s%s (Session: %s)\n", ui.BrightPurple, ui.SymAgent, cs.AgentModel, ui.Reset, cs.SessionID)
	}
	fmt.Printf("Date:     %s\n", time.Unix(cs.CreatedAt, 0).Format("2006-01-02 15:04:05"))
	fmt.Printf("Tree:     %s\n\n", cs.RootTreeHash)
	fmt.Printf("    %s%s%s\n\n", ui.Bold, cs.Message, ui.Reset)

	t, err := repo.DB.GetTree(cs.RootTreeHash)
	if err == nil {
		fmt.Printf("%sFiles in Tree (%d):%s\n", ui.Bold, len(t.Entries), ui.Reset)
		sortedPaths := make([]string, 0, len(t.Entries))
		for p := range t.Entries {
			sortedPaths = append(sortedPaths, p)
		}
		sort.Strings(sortedPaths)

		limit := len(sortedPaths)
		truncated := false
		if !*allFlag && limit > 30 {
			limit = 30
			truncated = true
		}

		for i := 0; i < limit; i++ {
			p := sortedPaths[i]
			e := t.Entries[p]
			fmt.Printf("  • %-35s %s%s%s\n", p, ui.Dim, e.Hash[:min(10, len(e.Hash))], ui.Reset)
		}

		if truncated {
			fmt.Printf("\n  %s... and %d more files (run 'scion show --all %s' to view all)%s\n",
				ui.BrightCyan, len(sortedPaths)-30, refArg, ui.Reset)
		}
		fmt.Println()
	}
}

func cmdClone(args []string) {
	fs := flag.NewFlagSet("clone", flag.ExitOnError)
	sparseFlag := fs.String("sparse", "", "Comma-separated sparse path patterns (e.g. 'src/**,config/*')")
	posArgs := parseFlagsFlexible(fs, args)

	if len(posArgs) < 1 {
		ui.PrintError("Usage: scion clone <source-dir|git-url> [target-dir] [--sparse <patterns>]")
		os.Exit(1)
	}

	source := posArgs[0]
	target := filepath.Base(source)
	if len(posArgs) >= 2 {
		target = posArgs[1]
	}

	start := time.Now()

	// Check if source is a Git repository (GitHub, GitLab, Bitbucket, or .git URL)
	isGit := strings.HasSuffix(source, ".git") ||
		strings.Contains(source, "github.com") ||
		strings.Contains(source, "gitlab.com") ||
		strings.Contains(source, "bitbucket.org") ||
		strings.HasPrefix(source, "git@") ||
		strings.HasPrefix(source, "git://")

	if isGit {
		cleanTarget := target
		if cleanTarget == source || cleanTarget == "" || cleanTarget == "." {
			u, err := url.Parse(source)
			if err == nil && u.Path != "" {
				cleanTarget = strings.TrimSuffix(filepath.Base(u.Path), ".git")
			} else {
				cleanTarget = "repo"
			}
		}
		cleanTarget = strings.TrimSuffix(cleanTarget, ".git")
		repoName := filepath.Base(cleanTarget)

		ui.PrintInfo(fmt.Sprintf("Detected Git remote. Cloning %s into OpenScion workspace %q...", source, cleanTarget))

		if _, err := exec.LookPath("git"); err != nil {
			ui.PrintError("Git executable not found in PATH for remote git import.")
			os.Exit(1)
		}

		var cloneArgs []string
		if *sparseFlag != "" {
			// Sparse clone: only download trees and requested sparse directories!
			cloneArgs = []string{"clone", "--depth", "1", "--filter=blob:none", "--sparse", "--quiet", source, cleanTarget}
		} else {
			// Fast stream clone with HTTP/2, single-branch, and no tags
			cloneArgs = []string{"-c", "http.version=HTTP/2", "clone", "--depth", "1", "--no-tags", "--single-branch", "--quiet", source, cleanTarget}
		}

		cloneCmd := exec.Command("git", cloneArgs...)
		cloneCmd.Stdout = os.Stdout
		cloneCmd.Stderr = os.Stderr
		if err := cloneCmd.Run(); err != nil {
			ui.PrintError(fmt.Sprintf("Git clone failed: %v", err))
			os.Exit(1)
		}

		if *sparseFlag != "" {
			sparsePatterns := strings.Split(*sparseFlag, ",")
			var gitDirs []string
			for _, pat := range sparsePatterns {
				cleanPat := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSpace(pat), "/**"), "/*")
				if cleanPat != "" && cleanPat != "*" && cleanPat != "**" {
					gitDirs = append(gitDirs, cleanPat)
				}
			}
			if len(gitDirs) > 0 {
				sparseArgs := append([]string{"-C", cleanTarget, "sparse-checkout", "set"}, gitDirs...)
				_ = exec.Command("git", sparseArgs...).Run()
			}
		}

		targetRepo, err := local.InitLocalRepo(cleanTarget, repoName)
		if err != nil {
			ui.PrintError(fmt.Sprintf("Failed to initialize OpenScion repo: %v", err))
			os.Exit(1)
		}
		defer targetRepo.Close()

		// Move cloned .git into .scion/git_bridge to support bidirectional scion push/pull!
		gitBridgeDir := filepath.Join(targetRepo.ScionDir, "git_bridge")
		_ = os.Rename(filepath.Join(cleanTarget, ".git"), gitBridgeDir)
		_ = os.RemoveAll(filepath.Join(cleanTarget, ".git"))

		// Configure exclude and worktree
		infoDir := filepath.Join(gitBridgeDir, "info")
		_ = os.MkdirAll(infoDir, 0755)
		_ = os.WriteFile(filepath.Join(infoDir, "exclude"), []byte(".scion\n.scion/\n"), 0644)
		_ = exec.Command("git", "--git-dir="+gitBridgeDir, "config", "core.worktree", targetRepo.WorkDir).Run()

		targetRepo.Config.RemoteURL = source
		_ = targetRepo.SaveConfig()

		if *sparseFlag != "" {
			targetRepo.Config.SparsePatterns = strings.Split(*sparseFlag, ",")
			_ = targetRepo.SaveConfig()
		}

		ui.PrintInfo("Ingesting files into Content-Addressable Storage (CAS)...")
		cs, err := targetRepo.Commit("feat: initial import from "+source, "OpenScion Git Importer <git@openscion.local>", "git-importer", "", "")
		if err != nil {
			if strings.Contains(err.Error(), "working tree clean") {
				headCS, _ := targetRepo.GetHeadChangeset()
				seq := int64(1)
				shortID := "empty"
				if headCS != nil {
					seq = headCS.SequenceNumber
					shortID = headCS.ShortID()
				}
				elapsed := time.Since(start).Milliseconds()
				ui.PrintSuccess(fmt.Sprintf("Successfully imported %q into OpenScion in %dms! (Commit #%d %s)",
					cleanTarget, elapsed, seq, shortID))
				return
			}
			ui.PrintError(fmt.Sprintf("Failed to commit imported files: %v", err))
			os.Exit(1)
		}

		elapsed := time.Since(start).Milliseconds()
		ui.PrintSuccess(fmt.Sprintf("Successfully imported %q into OpenScion in %dms! (Commit #%d %s)",
			cleanTarget, elapsed, cs.SequenceNumber, cs.ShortID()))
		return
	}

	// Remote clone via OpenScion control plane HTTP/HTTPS
	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		u, err := url.Parse(source)
		if err != nil {
			ui.PrintError(fmt.Sprintf("Invalid remote URL %s: %v", source, err))
			os.Exit(1)
		}
		repoName := filepath.Base(strings.TrimSuffix(u.Path, "/"))
		if repoName == "" || repoName == "." || repoName == "/" {
			repoName = "repo"
		}
		if len(posArgs) < 2 {
			target = repoName
		}
		serverURL := fmt.Sprintf("%s://%s", u.Scheme, u.Host)

		targetRepo, err := local.InitLocalRepo(target, repoName)
		if err != nil {
			ui.PrintError(fmt.Sprintf("Failed to initialize target workspace: %v", err))
			os.Exit(1)
		}
		defer targetRepo.Close()

		targetRepo.Config.RemoteURL = serverURL
		targetRepo.Config.RepoName = repoName
		if *sparseFlag != "" {
			targetRepo.Config.SparsePatterns = strings.Split(*sparseFlag, ",")
		}
		if err := targetRepo.SaveConfig(); err != nil {
			ui.PrintError(fmt.Sprintf("Failed to save remote config: %v", err))
			os.Exit(1)
		}

		ui.PrintInfo(fmt.Sprintf("Cloning remote repository %q from %s...", repoName, serverURL))
		pullRes, err := targetRepo.Pull()
		if err != nil {
			ui.PrintError(fmt.Sprintf("Failed to pull from remote: %v", err))
			os.Exit(1)
		}

		elapsed := time.Since(start).Milliseconds()
		ui.PrintSuccess(fmt.Sprintf("Cloned into %q (%d files materialized, %d changesets) in %dms!",
			target, pullRes.UpdatedFiles, pullRes.PulledChangesets, elapsed))
		return
	}

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
	sparseTree := headTree
	if *sparseFlag != "" {
		patterns = strings.Split(*sparseFlag, ",")
		sparseTree = headTree.FilterSparse(patterns)
	}

	targetRepo, err := local.InitLocalRepo(target, sourceRepo.Config.RepoName)
	if err != nil {
		ui.PrintError(fmt.Sprintf("Failed to initialize target workspace: %v", err))
		os.Exit(1)
	}
	defer targetRepo.Close()

	if len(patterns) > 0 {
		targetRepo.Config.SparsePatterns = patterns
		_ = targetRepo.SaveConfig()
	}

	// Materialize files only for sparse selection
	for relPath, entry := range sparseTree.Entries {
		data, err := sourceRepo.Store.GetBlobByHash(entry.Hash)
		if err != nil {
			continue
		}
		fullPath := filepath.Join(target, filepath.FromSlash(relPath))
		os.MkdirAll(filepath.Dir(fullPath), 0755)
		os.WriteFile(fullPath, data, os.FileMode(entry.Mode))
		targetRepo.Store.PutBlob(data)
	}

	// Copy full tree and changeset to prevent dropping unmanaged monorepo files
	targetRepo.DB.SaveTree(headTree)
	targetRepo.DB.SaveChangeset(headCS)
	targetRepo.DB.UpdateBranchTip(targetRepo.Config.RepoID, "main", headCS.ID, headCS.SequenceNumber)

	elapsed := time.Since(start).Milliseconds()
	ui.PrintSuccess(fmt.Sprintf("Cloned into %q (%d files materialized) in %dms!", target, len(sparseTree.Entries), elapsed))
}

func cmdServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	port := fs.String("port", "8080", "Server port")
	dataDir := fs.String("data-dir", "./data/server", "Storage directory")
	dbURL := fs.String("db", "", "Database URL: SQLite file path or postgres://user:pass@host:5432/db")
	fs.StringVar(dbURL, "database-url", "", "Alias for --db")
	casDirFlag := fs.String("cas-dir", "", "Custom CAS storage path")
	s3BucketFlag := fs.String("s3-bucket", "", "S3 / MinIO bucket name (or S3_BUCKET env)")
	s3EndpointFlag := fs.String("s3-endpoint", "", "S3 / MinIO endpoint host (or S3_ENDPOINT env)")
	authFlag := fs.String("auth-token", "", "Bearer token required for API access (or SCION_AUTH_TOKEN env)")
	parseFlagsFlexible(fs, args)

	finalDB := *dbURL
	if finalDB == "" {
		finalDB = os.Getenv("DATABASE_URL")
	}
	if finalDB == "" {
		_ = os.MkdirAll(*dataDir, 0755)
		finalDB = filepath.Join(*dataDir, "openscion.db")
	}

	finalCAS := *casDirFlag
	if finalCAS == "" {
		finalCAS = filepath.Join(*dataDir, "cas")
	}
	_ = os.MkdirAll(finalCAS, 0755)

	store, err := cas.NewStore(finalCAS)
	if err != nil {
		log.Fatalf("CAS initialization error: %v", err)
	}

	// Connect S3 / MinIO distributed tier if configured
	var s3Store *cas.S3Store
	if *s3BucketFlag != "" {
		s3Store, err = cas.NewS3Store(cas.S3Config{
			Bucket:          *s3BucketFlag,
			Endpoint:        *s3EndpointFlag,
			AccessKeyID:     os.Getenv("AWS_ACCESS_KEY_ID"),
			SecretAccessKey: os.Getenv("AWS_SECRET_ACCESS_KEY"),
			UseSSL:          !strings.HasPrefix(*s3EndpointFlag, "http://"),
		})
		if err != nil {
			fmt.Printf("%sWarning: S3 init failed: %v%s\n", ui.Yellow, err, ui.Reset)
		}
	} else {
		s3Store, _ = cas.NewS3StoreFromEnv()
	}
	if s3Store != nil {
		store.SetRemote(s3Store)
	}

	database, err := db.Open(finalDB)
	if err != nil {
		log.Fatalf("Database connection error: %v", err)
	}
	defer database.Close()

	server := api.NewServer(database, store)
	if *authFlag != "" {
		server.SetAuthToken(*authFlag)
	}

	fmt.Println()
	fmt.Printf("%s%sOpenScion Team Server Hub%s\n", ui.Bold, ui.BrightCyan, ui.Reset)
	fmt.Println("----------------------------------------------------------------")
	fmt.Printf("• Server Address:   %shttp://0.0.0.0:%s%s\n", ui.BrightGreen, *port, ui.Reset)
	if *authFlag != "" || os.Getenv("SCION_AUTH_TOKEN") != "" || os.Getenv("AETHER_AUTH_TOKEN") != "" {
		fmt.Printf("• Authentication:   %sBearer Token Enabled%s\n", ui.BrightGreen, ui.Reset)
	} else {
		fmt.Printf("• Authentication:   %sOpen (No Auth)%s\n", ui.Yellow, ui.Reset)
	}
	if strings.HasPrefix(finalDB, "postgres://") || strings.HasPrefix(finalDB, "postgresql://") {
		fmt.Printf("• Database Engine:  %sPostgreSQL / CockroachDB Cluster%s\n", ui.BrightPurple, ui.Reset)
	} else {
		fmt.Printf("• Database Engine:  %sSQLite WAL (%s)%s\n", ui.Cyan, finalDB, ui.Reset)
	}
	if s3Store != nil {
		fmt.Printf("• CAS Storage:      %sDistributed S3 / MinIO Cluster%s (local cache: %s)\n", ui.BrightGreen, ui.Reset, finalCAS)
	} else {
		fmt.Printf("• CAS Storage:      %sLocal High-Speed Cache%s (%s)\n", ui.Cyan, ui.Reset, finalCAS)
	}
	fmt.Println()
	fmt.Println("Team members can connect with:")
	fmt.Printf("  %sscion remote add origin http://<this-server-ip>:%s%s\n", ui.BrightCyan, *port, ui.Reset)
	fmt.Printf("  %sscion push%s   # Push commits\n", ui.Bold, ui.Reset)
	fmt.Printf("  %sscion pull%s   # Pull commits\n", ui.Bold, ui.Reset)
	fmt.Println("----------------------------------------------------------------")
	fmt.Println()

	if err := http.ListenAndServe(":"+*port, server); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

func cmdRemote(args []string) {
	repo, err := local.OpenLocalRepo(".")
	if err != nil {
		ui.PrintError(err.Error())
		os.Exit(1)
	}
	defer repo.Close()

	if len(args) == 0 || (len(args) == 1 && (args[0] == "-v" || args[0] == "list")) {
		remote := repo.GetRemote()
		if remote == "" {
			fmt.Println("No remotes configured. Add one with:")
			fmt.Printf("  %sscion remote add origin <server-url>%s\n", ui.Cyan, ui.Reset)
			return
		}
		fmt.Printf("origin  %s (push/pull)\n", remote)
		return
	}

	subCmd := args[0]
	switch subCmd {
	case "add", "set-url":
		if len(args) < 2 {
			ui.PrintError("Usage: scion remote add origin <server-url>")
			os.Exit(1)
		}
		url := args[len(args)-1]
		if err := repo.SetRemote(url); err != nil {
			ui.PrintError(fmt.Sprintf("Failed to set remote: %v", err))
			os.Exit(1)
		}
		ui.PrintSuccess(fmt.Sprintf("Remote 'origin' set to: %s", url))
	case "remove", "rm":
		if err := repo.SetRemote(""); err != nil {
			ui.PrintError(err.Error())
			os.Exit(1)
		}
		ui.PrintSuccess("Remote 'origin' removed.")
	default:
		ui.PrintError(fmt.Sprintf("Unknown remote subcommand %q. Use 'add' or 'set-url'.", subCmd))
		os.Exit(1)
	}
}

func cmdPush(args []string) {
	repo, err := local.OpenLocalRepo(".")
	if err != nil {
		ui.PrintError(err.Error())
		os.Exit(1)
	}
	defer repo.Close()

	remote := repo.GetRemote()
	if remote == "" {
		ui.PrintError("No remote server configured.\nRun: scion remote add origin <http://server-address:port>")
		os.Exit(1)
	}

	fmt.Printf("Pushing to %sorigin%s (%s)...\n", ui.Cyan, ui.Reset, remote)
	start := time.Now()
	res, err := repo.Push()
	if err != nil {
		ui.PrintError(fmt.Sprintf("Push failed: %v", err))
		os.Exit(1)
	}

	if res.PushedChangesets == 0 {
		ui.PrintSuccess("Everything up-to-date. No new changesets to push.")
		return
	}

	elapsed := time.Since(start).Milliseconds()
	fmt.Printf("  %s✓%s Uploaded %d CAS chunks (%s)\n", ui.BrightGreen, ui.Reset, res.UploadedChunks, ui.FormatBytes(res.UploadedBytes))
	fmt.Printf("  %s✓%s Pushed %d changesets into remote merge queue\n", ui.BrightGreen, ui.Reset, res.PushedChangesets)
	fmt.Printf("  %s✓%s Landed at linear sequence %s#%d%s: %q (%dms)\n",
		ui.BrightGreen, ui.Reset, ui.BrightYellow+ui.Bold, res.RemoteSequence, ui.Reset, res.Message, elapsed)
}

func cmdPull(args []string) {
	repo, err := local.OpenLocalRepo(".")
	if err != nil {
		ui.PrintError(err.Error())
		os.Exit(1)
	}
	defer repo.Close()

	remote := repo.GetRemote()
	if remote == "" {
		ui.PrintError("No remote server configured.\nRun: scion remote add origin <http://server-address:port>")
		os.Exit(1)
	}

	fmt.Printf("Pulling from %sorigin%s (%s)...\n", ui.Cyan, ui.Reset, remote)
	start := time.Now()
	res, err := repo.Pull()
	if err != nil {
		ui.PrintError(fmt.Sprintf("Pull failed: %v", err))
		os.Exit(1)
	}

	if res.PulledChangesets == 0 {
		ui.PrintSuccess(fmt.Sprintf("Already up-to-date at sequence #%d.", res.NewSequence))
		return
	}

	elapsed := time.Since(start).Milliseconds()
	fmt.Printf("  %s✓%s Downloaded %d changesets and %d CAS chunks (%s)\n",
		ui.BrightGreen, ui.Reset, res.PulledChangesets, res.DownloadedChunks, ui.FormatBytes(res.DownloadedBytes))
	fmt.Printf("  %s✓%s Materialized %d updated files in workspace\n", ui.BrightGreen, ui.Reset, res.UpdatedFiles)
	fmt.Printf("  %s✓%s Fast-forwarded to sequence %s#%d%s (%dms)\n",
		ui.BrightGreen, ui.Reset, ui.BrightYellow+ui.Bold, res.NewSequence, ui.Reset, elapsed)
}

func cmdSync(args []string) {
	fmt.Printf("\n%sSynchronizing with origin...%s\n", ui.Bold+ui.Cyan, ui.Reset)
	cmdPull(args)
	cmdPush(args)
}


func splitLines(s string) []string {
	if s == "" {
		return []string{}
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	lines := strings.Split(s, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func cmdMount(args []string) {
	fs := flag.NewFlagSet("mount", flag.ExitOnError)
	revFlag := fs.String("rev", "", "Commit sequence number (#1) or ID to mount (default: HEAD)")
	posArgs := parseFlagsFlexible(fs, args)

	repo, err := local.OpenLocalRepo(".")
	if err != nil {
		ui.PrintError(err.Error())
		os.Exit(1)
	}
	defer repo.Close()

	var cs *graph.Changeset
	if *revFlag != "" {
		cs, err = repo.ResolveChangeset(*revFlag)
	} else {
		cs, err = repo.GetHeadChangeset()
	}

	if err != nil {
		ui.PrintError(fmt.Sprintf("Failed to resolve commit: %v", err))
		os.Exit(1)
	}

	t, err := repo.DB.GetTree(cs.RootTreeHash)
	if err != nil {
		ui.PrintError(fmt.Sprintf("Failed to load Merkle tree for commit #%d: %v", cs.SequenceNumber, err))
		os.Exit(1)
	}

	mountPoint := "./mnt"
	if len(posArgs) > 0 {
		mountPoint = posArgs[0]
	}

	opts := vfs.MountOptions{
		MountPoint: mountPoint,
		CommitSeq:  cs.SequenceNumber,
		ReadOnly:   true,
	}

	commitTime := time.Unix(cs.CreatedAt, 0)
	if err := vfs.Mount(t, repo.Store, commitTime, opts); err != nil {
		ui.PrintError(fmt.Sprintf("VFS mount error: %v", err))
		os.Exit(1)
	}
}

func cmdBench(args []string) {
	fmt.Printf("%s%s⚡ OpenScion Benchmark Suite%s\n", ui.Bold, ui.BrightCyan, ui.Reset)
	fmt.Printf("Measuring throughput and latency across Diff Engine, Content Storage, and Merge Queue...\n\n")

	cmd := exec.Command("go", "test", "-bench=.", "-benchmem", "./pkg/merge", "./pkg/queue")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		ui.PrintError(fmt.Sprintf("Benchmark execution encountered an error: %v", err))
		os.Exit(1)
	}
	fmt.Printf("\n%s✔ Benchmarks completed successfully.%s\n", ui.BrightGreen+ui.Bold, ui.Reset)
}

func cmdGC(args []string) {
	repo, err := local.OpenLocalRepo(".")
	if err != nil {
		ui.PrintError(err.Error())
		os.Exit(1)
	}
	defer repo.Close()

	deleted, freedBytes, err := repo.GC()
	if err != nil {
		ui.PrintError(fmt.Sprintf("Garbage collection failed: %v", err))
		os.Exit(1)
	}

	ui.PrintSuccess(fmt.Sprintf("Garbage collection complete: pruned %d unreferenced object(s), freed %.2f KB",
		deleted, float64(freedBytes)/1024.0))
}

func cmdStash(args []string) {
	fs := flag.NewFlagSet("stash", flag.ExitOnError)
	msgFlag := fs.String("m", "", "Stash message")
	posArgs := parseFlagsFlexible(fs, args)

	repo, err := local.OpenLocalRepo(".")
	if err != nil {
		ui.PrintError(err.Error())
		os.Exit(1)
	}
	defer repo.Close()

	sub := "push"
	if len(posArgs) > 0 {
		sub = posArgs[0]
		posArgs = posArgs[1:]
	}

	switch sub {
	case "push", "save":
		stash, err := repo.SaveStash(*msgFlag)
		if err != nil {
			ui.PrintError(fmt.Sprintf("Cannot stash: %v", err))
			os.Exit(1)
		}
		ui.PrintSuccess(fmt.Sprintf("Saved working directory and index state %s", stash.Message))

	case "list":
		stashes, err := repo.ListStashes()
		if err != nil {
			ui.PrintError(fmt.Sprintf("Failed to list stashes: %v", err))
			os.Exit(1)
		}
		if len(stashes) == 0 {
			return
		}
		for i, s := range stashes {
			fmt.Printf("stash@{%d}: %s\n", i, s.Message)
		}

	case "pop", "apply":
		index := 0
		if len(posArgs) > 0 {
			arg := posArgs[0]
			arg = strings.TrimPrefix(arg, "stash@{")
			arg = strings.TrimSuffix(arg, "}")
			if idx, err := strconv.Atoi(arg); err == nil {
				index = idx
			}
		}

		var stash *db.Stash
		if sub == "pop" {
			stash, err = repo.PopStash(index)
		} else {
			stash, err = repo.ApplyStash(index)
		}
		if err != nil {
			ui.PrintError(fmt.Sprintf("Failed to %s stash: %v", sub, err))
			os.Exit(1)
		}
		if sub == "pop" {
			ui.PrintSuccess(fmt.Sprintf("Dropped stash@{%d} and applied: %s", index, stash.Message))
		} else {
			ui.PrintSuccess(fmt.Sprintf("Applied stash@{%d}: %s", index, stash.Message))
		}

	case "drop":
		index := 0
		if len(posArgs) > 0 {
			arg := posArgs[0]
			arg = strings.TrimPrefix(arg, "stash@{")
			arg = strings.TrimSuffix(arg, "}")
			if idx, err := strconv.Atoi(arg); err == nil {
				index = idx
			}
		}
		stash, err := repo.DropStash(index)
		if err != nil {
			ui.PrintError(fmt.Sprintf("Failed to drop stash: %v", err))
			os.Exit(1)
		}
		ui.PrintSuccess(fmt.Sprintf("Dropped stash@{%d}: %s", index, stash.Message))

	case "show":
		index := 0
		if len(posArgs) > 0 {
			arg := posArgs[0]
			arg = strings.TrimPrefix(arg, "stash@{")
			arg = strings.TrimSuffix(arg, "}")
			if idx, err := strconv.Atoi(arg); err == nil {
				index = idx
			}
		}
		diffs, err := repo.ShowStash(index)
		if err != nil {
			ui.PrintError(fmt.Sprintf("Failed to show stash: %v", err))
			os.Exit(1)
		}
		for _, d := range diffs {
			fmt.Printf("  %s %s\n", d.ChangeType, d.Path)
		}

	default:
		// If unknown subcommand with no flag, treat as saving stash with that message
		stash, err := repo.SaveStash(sub)
		if err != nil {
			ui.PrintError(fmt.Sprintf("Cannot stash: %v", err))
			os.Exit(1)
		}
		ui.PrintSuccess(fmt.Sprintf("Saved working directory and index state %s", stash.Message))
	}
}

func cmdSparse(args []string) {
	repo, err := local.OpenLocalRepo(".")
	if err != nil {
		ui.PrintError(err.Error())
		os.Exit(1)
	}
	defer repo.Close()

	if len(args) == 0 || args[0] == "list" {
		patterns := repo.Config.SparsePatterns
		if len(patterns) == 0 || (len(patterns) == 1 && patterns[0] == "*") {
			fmt.Printf("Sparse checkout is %sdisabled%s (full working tree active).\n", ui.Dim, ui.Reset)
			return
		}
		fmt.Printf("Active sparse checkout patterns (%d):\n", len(patterns))
		for _, p := range patterns {
			fmt.Printf("  • %s%s%s\n", ui.Cyan, p, ui.Reset)
		}
		return
	}

	sub := args[0]
	subArgs := args[1:]

	switch sub {
	case "set":
		if len(subArgs) == 0 {
			ui.PrintError("Usage: scion sparse set <pattern1,pattern2...> or <pattern1> [pattern2...]")
			os.Exit(1)
		}
		var patterns []string
		for _, arg := range subArgs {
			parts := strings.Split(arg, ",")
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p != "" {
					patterns = append(patterns, p)
				}
			}
		}
		count, err := repo.SetSparsePatterns(patterns)
		if err != nil {
			ui.PrintError(fmt.Sprintf("Failed to set sparse patterns: %v", err))
			os.Exit(1)
		}
		ui.PrintSuccess(fmt.Sprintf("Set sparse patterns (%d active); materialized %d files.", len(patterns), count))

	case "add":
		if len(subArgs) == 0 {
			ui.PrintError("Usage: scion sparse add <pattern1> [pattern2...]")
			os.Exit(1)
		}
		patterns := repo.Config.SparsePatterns
		if len(patterns) == 1 && patterns[0] == "*" {
			patterns = nil
		}
		for _, arg := range subArgs {
			parts := strings.Split(arg, ",")
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p != "" {
					patterns = append(patterns, p)
				}
			}
		}
		count, err := repo.SetSparsePatterns(patterns)
		if err != nil {
			ui.PrintError(fmt.Sprintf("Failed to add sparse patterns: %v", err))
			os.Exit(1)
		}
		ui.PrintSuccess(fmt.Sprintf("Updated sparse patterns (%d active); materialized %d files.", len(patterns), count))

	case "clear", "disable":
		count, err := repo.SetSparsePatterns([]string{"*"})
		if err != nil {
			ui.PrintError(fmt.Sprintf("Failed to clear sparse patterns: %v", err))
			os.Exit(1)
		}
		ui.PrintSuccess(fmt.Sprintf("Disabled sparse checkout; materialized full working copy (%d files).", count))

	default:
		ui.PrintError(fmt.Sprintf("Unknown sparse subcommand %q. Usage: scion sparse [list|set|add|clear]", sub))
		os.Exit(1)
	}
}

