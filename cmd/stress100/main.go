package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aether/scn/pkg/cas"
	"github.com/aether/scn/pkg/graph"
	"github.com/aether/scn/pkg/tree"
)

var (
	serverURL = "http://localhost:8089"
	repoName  = "stress-monorepo"
)

type AgentResult struct {
	AgentID      string
	Group        string
	Status       string
	SeqNumber    int64
	DurationMs   int64
	ErrorMessage string
	Conflicts    int
}

func hashBytes(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func compress(raw []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw, err := gzip.NewWriterLevel(&buf, gzip.BestSpeed)
	if err != nil {
		return nil, err
	}
	if _, err := zw.Write(raw); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

type ChunkWire struct {
	Hash           string `json:"hash"`
	UncompressedSz int    `json:"uncompressed_size"`
	DataBase64     string `json:"data_base64"`
}

func uploadBlob(content []byte) (string, error) {
	fullHash := hashBytes(content)
	compressed, err := compress(content)
	if err != nil {
		return "", err
	}

	// 1. Upload chunk
	chunk := ChunkWire{
		Hash:           fullHash,
		UncompressedSz: len(content),
		DataBase64:     base64.StdEncoding.EncodeToString(compressed),
	}
	body, _ := json.Marshal([]ChunkWire{chunk})
	resp, err := http.Post(serverURL+"/api/v1/cas/blobs/batch-put", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("chunk upload failed: %w", err)
	}
	resp.Body.Close()

	// 2. Upload manifest
	manifest := cas.BlobManifest{
		BlobHash:      fullHash,
		TotalSize:     int64(len(content)),
		ChunkHashes:   []string{fullHash},
		IsSingleChunk: true,
	}
	mBody, _ := json.Marshal([]cas.BlobManifest{manifest})
	mResp, err := http.Post(serverURL+"/api/v1/cas/manifests/batch-put", "application/json", bytes.NewReader(mBody))
	if err != nil {
		return "", fmt.Errorf("manifest upload failed: %w", err)
	}
	mResp.Body.Close()

	return fullHash, nil
}

func uploadTree(t *tree.Tree) error {
	body, _ := json.Marshal(t)
	resp, err := http.Post(serverURL+"/api/v1/cas/trees/put", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("tree upload failed (%d): %s", resp.StatusCode, string(msg))
	}
	return nil
}

func main() {
	flag.StringVar(&serverURL, "server", "http://localhost:8089", "Server base URL")
	flag.StringVar(&repoName, "repo", "stress-monorepo", "Target repository name")
	flag.Parse()

	fmt.Println("================================================================================")
	fmt.Println("   PROJECT OPENSCION: 100 CONCURRENT AGENT STRESS TEST HARNESS                  ")
	fmt.Println("================================================================================")

	// 1. Verify server
	resp, err := http.Get(serverURL + "/healthz")
	if err != nil {
		fmt.Printf("Error: Aether server not running at %s: %v\n", serverURL, err)
		os.Exit(1)
	}
	resp.Body.Close()
	fmt.Println("✔ OpenScion control plane server online at " + serverURL)

	// 2. Create stress repo
	createBody, _ := json.Marshal(map[string]string{"name": repoName})
	cResp, err := http.Post(serverURL+"/api/v1/repos/create", "application/json", bytes.NewReader(createBody))
	if err != nil {
		fmt.Printf("Error creating repo: %v\n", err)
		os.Exit(1)
	}
	cResp.Body.Close()

	// Get repo info
	infoResp, err := http.Get(serverURL + "/api/v1/repos/" + repoName + "/info")
	if err != nil {
		fmt.Printf("Error getting repo info: %v\n", err)
		os.Exit(1)
	}
	var repoInfo struct {
		Repo struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"repo"`
		HeadCS struct {
			ID           string `json:"id"`
			RootTreeHash string `json:"root_tree_hash"`
		} `json:"head_changeset"`
	}
	json.NewDecoder(infoResp.Body).Decode(&repoInfo)
	infoResp.Body.Close()

	repoID := repoInfo.Repo.ID
	baseHeadID := repoInfo.HeadCS.ID
	fmt.Printf("✔ Initialized repository %q (ID: %s, Head: %s)\n", repoName, repoID, baseHeadID)

	// 3. Create base codebase
	baseTree := tree.NewTree()

	// File 1: Core API file with 10 distinct functions (for 3-way line merge testing)
	var apiLines []string
	apiLines = append(apiLines, "package api", "", "// Service API endpoints", "")
	for f := 0; f < 10; f++ {
		apiLines = append(apiLines,
			fmt.Sprintf("// Handler%d processes request for section %d", f, f),
			fmt.Sprintf("func Handler%d(req string) string {", f),
			fmt.Sprintf("    return \"original-%d: \" + req", f),
			"}",
			"",
		)
	}
	apiContent := strings.Join(apiLines, "\n")
	apiHash, err := uploadBlob([]byte(apiContent))
	if err != nil {
		fmt.Printf("Error uploading api.go: %v\n", err)
		os.Exit(1)
	}
	baseTree.Entries["services/api/api.go"] = tree.Entry{
		Path: "services/api/api.go",
		Type: tree.TypeBlob,
		Hash: apiHash,
		Size: int64(len(apiContent)),
		Mode: 0644,
	}

	// File 2: Shared Config file (for identical line conflict testing)
	configContent := `{"version": 1, "concurrency_limit": 50, "rate_limit": 1000, "status": "active"}`
	configHash, err := uploadBlob([]byte(configContent))
	if err != nil {
		fmt.Printf("Error uploading settings.json: %v\n", err)
		os.Exit(1)
	}
	baseTree.Entries["config/settings.json"] = tree.Entry{
		Path: "config/settings.json",
		Type: tree.TypeBlob,
		Hash: configHash,
		Size: int64(len(configContent)),
		Mode: 0644,
	}

	// Files 3-82: 80 independent worker services
	for w := 0; w < 80; w++ {
		wPath := fmt.Sprintf("services/worker_%d.go", w)
		wContent := fmt.Sprintf("package services\n\nfunc Worker%d() int {\n    return %d\n}\n", w, w)
		wHash, err := uploadBlob([]byte(wContent))
		if err != nil {
			fmt.Printf("Error uploading %s: %v\n", wPath, err)
			os.Exit(1)
		}
		baseTree.Entries[wPath] = tree.Entry{
			Path: wPath,
			Type: tree.TypeBlob,
			Hash: wHash,
			Size: int64(len(wContent)),
			Mode: 0644,
		}
	}

	baseTree.ComputeHash()
	if err := uploadTree(baseTree); err != nil {
		fmt.Printf("Error uploading base tree: %v\n", err)
		os.Exit(1)
	}

	// Submit base changeset
	baseCS := graph.NewChangeset(
		repoID,
		baseTree.RootHash,
		[]string{baseHeadID},
		"Setup Agent <setup@agent.local>",
		"feat(base): initial 82 microservices structure",
		"claude-code-3.7-sonnet",
		"setup-session",
		"setup-trace",
	)
	subBody, _ := json.Marshal(map[string]any{
		"repo_id":   repoID,
		"changeset": baseCS,
	})
	sResp, err := http.Post(serverURL+"/api/v1/changeset/submit", "application/json", bytes.NewReader(subBody))
	if err != nil {
		fmt.Printf("Error submitting base changeset: %v\n", err)
		os.Exit(1)
	}
	var baseSubmitResult struct {
		Status     string `json:"status"`
		LandedCSID string `json:"landed_changeset_id"`
		SeqNumber  int64  `json:"sequence_number"`
	}
	json.NewDecoder(sResp.Body).Decode(&baseSubmitResult)
	sResp.Body.Close()

	rootHeadID := baseSubmitResult.LandedCSID
	fmt.Printf("✔ Base codebase landed at Sequence #%d (Commit: %s)\n", baseSubmitResult.SeqNumber, rootHeadID)
	fmt.Println("  • Files in base codebase: 82")
	fmt.Println("  • Ready to launch 100 concurrent agents...")
	fmt.Println()

	// 4. Launch 100 concurrent agents
	results := make([]AgentResult, 100)
	var wg sync.WaitGroup
	startBarrier := make(chan struct{})
	suiteStart := time.Now()

	for a := 0; a < 100; a++ {
		wg.Add(1)
		go func(agentIdx int) {
			defer wg.Done()

			agentID := fmt.Sprintf("agent-%03d", agentIdx)
			model := "claude-code-3.7-sonnet"
			if agentIdx%3 == 1 {
				model = "cursor-agent-v2"
			} else if agentIdx%3 == 2 {
				model = "swe-bench-bot"
			}

			// Wait for synchronized burst trigger
			<-startBarrier

			agentStart := time.Now()
			var group string
			agentTree := tree.NewTree()
			for k, v := range baseTree.Entries {
				agentTree.Entries[k] = v
			}

			var commitMsg string

			if agentIdx < 80 {
				// Group 1: 80 agents modify 80 independent worker files
				group = "Group 1: Independent File"
				wPath := fmt.Sprintf("services/worker_%d.go", agentIdx)
				wContent := fmt.Sprintf("package services\n\n// Optimized by %s\nfunc Worker%d() int {\n    return %d * 1000\n}\n", model, agentIdx, agentIdx)
				wHash, err := uploadBlob([]byte(wContent))
				if err != nil {
					results[agentIdx] = AgentResult{AgentID: agentID, Group: group, Status: "UPLOAD_ERR", ErrorMessage: err.Error()}
					return
				}
				agentTree.Entries[wPath] = tree.Entry{
					Path: wPath,
					Type: tree.TypeBlob,
					Hash: wHash,
					Size: int64(len(wContent)),
					Mode: 0644,
				}
				commitMsg = fmt.Sprintf("perf(worker_%d): 1000x optimization by %s", agentIdx, agentID)

			} else if agentIdx < 90 {
				// Group 2: 10 agents modify non-overlapping lines in shared services/api/api.go
				group = "Group 2: Diff3 Shared File"
				funcIdx := agentIdx - 80 // 0 to 9
				var modifiedLines []string
				modifiedLines = append(modifiedLines, "package api", "", "// Service API endpoints", "")
				for f := 0; f < 10; f++ {
					if f == funcIdx {
						modifiedLines = append(modifiedLines,
							fmt.Sprintf("// Handler%d [OPTIMIZED by %s]", f, agentID),
							fmt.Sprintf("func Handler%d(req string) string {", f),
							fmt.Sprintf("    return \"optimized-by-%s: \" + req", agentID),
							"}",
							"",
						)
					} else {
						modifiedLines = append(modifiedLines,
							fmt.Sprintf("// Handler%d processes request for section %d", f, f),
							fmt.Sprintf("func Handler%d(req string) string {", f),
							fmt.Sprintf("    return \"original-%d: \" + req", f),
							"}",
							"",
						)
					}
				}
				mContent := strings.Join(modifiedLines, "\n")
				mHash, err := uploadBlob([]byte(mContent))
				if err != nil {
					results[agentIdx] = AgentResult{AgentID: agentID, Group: group, Status: "UPLOAD_ERR", ErrorMessage: err.Error()}
					return
				}
				agentTree.Entries["services/api/api.go"] = tree.Entry{
					Path: "services/api/api.go",
					Type: tree.TypeBlob,
					Hash: mHash,
					Size: int64(len(mContent)),
					Mode: 0644,
				}
				commitMsg = fmt.Sprintf("feat(api): update Handler%d by %s", funcIdx, agentID)

			} else {
				// Group 3: 10 agents modify EXACT SAME line in config/settings.json
				group = "Group 3: Conflicting Line"
				confContent := fmt.Sprintf(`{"version": 1, "concurrency_limit": %d, "rate_limit": 1000, "status": "active"}`, agentIdx*100)
				cHash, err := uploadBlob([]byte(confContent))
				if err != nil {
					results[agentIdx] = AgentResult{AgentID: agentID, Group: group, Status: "UPLOAD_ERR", ErrorMessage: err.Error()}
					return
				}
				agentTree.Entries["config/settings.json"] = tree.Entry{
					Path: "config/settings.json",
					Type: tree.TypeBlob,
					Hash: cHash,
					Size: int64(len(confContent)),
					Mode: 0644,
				}
				commitMsg = fmt.Sprintf("config: override concurrency to %d by %s", agentIdx*100, agentID)
			}

			agentTree.ComputeHash()
			if err := uploadTree(agentTree); err != nil {
				results[agentIdx] = AgentResult{AgentID: agentID, Group: group, Status: "TREE_ERR", ErrorMessage: err.Error()}
				return
			}

			// All agents branch from the SAME initial root commit!
			cs := graph.NewChangeset(
				repoID,
				agentTree.RootHash,
				[]string{rootHeadID},
				fmt.Sprintf("%s <%s@agent.ai>", agentID, agentID),
				commitMsg,
				model,
				fmt.Sprintf("session-%03d", agentIdx),
				fmt.Sprintf("trace-%03d", agentIdx),
			)

			submitPayload, _ := json.Marshal(map[string]any{
				"repo_id":   repoID,
				"changeset": cs,
			})

			subResp, err := http.Post(serverURL+"/api/v1/changeset/submit", "application/json", bytes.NewReader(submitPayload))
			if err != nil {
				results[agentIdx] = AgentResult{AgentID: agentID, Group: group, Status: "NETWORK_ERR", DurationMs: time.Since(agentStart).Milliseconds(), ErrorMessage: err.Error()}
				return
			}
			defer subResp.Body.Close()

			var subRes struct {
				Status       string `json:"status"`
				LandedCSID   string `json:"landed_changeset_id"`
				SeqNumber    int64  `json:"sequence_number"`
				ErrorMessage string `json:"error_message"`
				Conflicts    []any  `json:"conflicts"`
				DurationMs   int64  `json:"duration_ms"`
			}
			json.NewDecoder(subResp.Body).Decode(&subRes)

			results[agentIdx] = AgentResult{
				AgentID:      agentID,
				Group:        group,
				Status:       subRes.Status,
				SeqNumber:    subRes.SeqNumber,
				DurationMs:   time.Since(agentStart).Milliseconds(),
				ErrorMessage: subRes.ErrorMessage,
				Conflicts:    len(subRes.Conflicts),
			}
		}(a)
	}

	fmt.Println("🚀 Firing burst trigger: 100 concurrent agents submitting to Merge Queue simultaneously...")
	close(startBarrier)
	wg.Wait()
	totalDuration := time.Since(suiteStart)

	fmt.Println()
	fmt.Printf("🏁 All 100 agents completed in %v\n\n", totalDuration)

	// 5. Analyze Results
	mergedCount := 0
	conflictCount := 0
	errorCount := 0
	var mergedDurations []int64

	group1Success := 0
	group2Success := 0
	group3Success := 0
	group3Conflict := 0

	for _, r := range results {
		switch r.Status {
		case "MERGED":
			mergedCount++
			mergedDurations = append(mergedDurations, r.DurationMs)
			if r.Group == "Group 1: Independent File" {
				group1Success++
			} else if r.Group == "Group 2: Diff3 Shared File" {
				group2Success++
			} else if r.Group == "Group 3: Conflicting Line" {
				group3Success++
			}
		case "REBASE_CONFLICT":
			conflictCount++
			if r.Group == "Group 3: Conflicting Line" {
				group3Conflict++
			}
		default:
			errorCount++
		}
	}

	sort.Slice(mergedDurations, func(i, j int) bool { return mergedDurations[i] < mergedDurations[j] })

	var p50, p95, p99, minDur, maxDur int64
	var sumDur int64
	if len(mergedDurations) > 0 {
		minDur = mergedDurations[0]
		maxDur = mergedDurations[len(mergedDurations)-1]
		p50 = mergedDurations[len(mergedDurations)*50/100]
		p95 = mergedDurations[len(mergedDurations)*95/100]
		p99 = mergedDurations[len(mergedDurations)*99/100]
		for _, d := range mergedDurations {
			sumDur += d
		}
	}
	avgDur := float64(0)
	if len(mergedDurations) > 0 {
		avgDur = float64(sumDur) / float64(len(mergedDurations))
	}

	fmt.Println("================================================================================")
	fmt.Println("   100 CONCURRENT AGENTS STRESS TEST RESULTS SCORECARD                          ")
	fmt.Println("================================================================================")
	fmt.Printf("• Total Agents Launched:          100\n")
	fmt.Printf("• Total Merged (Landed to Main):  %d\n", mergedCount)
	fmt.Printf("• Total Rebase Conflicts Caught:  %d\n", conflictCount)
	fmt.Printf("• Total Server/DB Failures:       %d\n", errorCount)
	fmt.Printf("• Throughput:                     %.1f merges/sec\n", float64(mergedCount)/totalDuration.Seconds())
	fmt.Println("--------------------------------------------------------------------------------")
	fmt.Println("DETAILED BREAKDOWN BY WORKLOAD GROUP:")
	fmt.Printf("• Group 1 (80 Agents / Independent Files):\n")
	fmt.Printf("    ✔ %d/80 successfully speculatively rebased and landed.\n", group1Success)
	fmt.Printf("• Group 2 (10 Agents / Non-Overlapping Lines in Same File):\n")
	fmt.Printf("    ✔ %d/10 successfully 3-way line merged (Diff3) and landed.\n", group2Success)
	fmt.Printf("• Group 3 (10 Agents / Conflicting Identical Line in Settings):\n")
	fmt.Printf("    ✔ %d won race and landed; %d properly rejected with REBASE_CONFLICT.\n", group3Success, group3Conflict)
	fmt.Println("--------------------------------------------------------------------------------")
	fmt.Println("LATENCY DISTRIBUTION (End-to-End Client Roundtrip):")
	fmt.Printf("• Min:   %d ms\n", minDur)
	fmt.Printf("• Avg:   %.1f ms\n", avgDur)
	fmt.Printf("• P50:   %d ms\n", p50)
	fmt.Printf("• P95:   %d ms\n", p95)
	fmt.Printf("• P99:   %d ms\n", p99)
	fmt.Printf("• Max:   %d ms\n", maxDur)
	fmt.Println("--------------------------------------------------------------------------------")

	// Print sample timeline from server query
	logResp, err := http.Get(serverURL + "/api/v1/query/log?repo=" + repoName + "&limit=10")
	if err == nil {
		var changesets []graph.Changeset
		json.NewDecoder(logResp.Body).Decode(&changesets)
		logResp.Body.Close()
		fmt.Printf("FINAL COMMIT TIMELINE ON SERVER (Top %d of %d commits):\n", len(changesets), mergedCount+1)
		for _, cs := range changesets {
			fmt.Printf("  • Seq #%-3d  [%s]  %s  (%s)\n", cs.SequenceNumber, cs.ShortID(), cs.Message, cs.Author)
		}
	}
	fmt.Println("================================================================================")
}
