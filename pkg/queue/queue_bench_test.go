package queue

import (
	"fmt"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/aether/scn/pkg/cas"
	"github.com/aether/scn/pkg/db"
	"github.com/aether/scn/pkg/graph"
	"github.com/aether/scn/pkg/tree"
)

func BenchmarkMergeQueue_Sequential(b *testing.B) {
	tempDir := b.TempDir()
	dbPath := filepath.Join(tempDir, "bench_seq.db")
	database, _ := db.Open(dbPath)
	defer database.Close()

	store, _ := cas.NewStore(filepath.Join(tempDir, "cas"))
	repo, _ := database.CreateRepo("bench-repo")
	q := NewMergeQueue(database, store)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		branch, _ := database.GetBranch(repo.ID, "main")
		data := []byte(fmt.Sprintf("bench content %d\n", i))
		m, _ := store.PutBlob(data)

		tr := tree.NewTree()
		tr.Entries[fmt.Sprintf("file_%d.txt", i)] = tree.Entry{
			Path: fmt.Sprintf("file_%d.txt", i),
			Type: tree.TypeBlob,
			Hash: m.BlobHash,
			Size: m.TotalSize,
			Mode: 0644,
		}
		tr.ComputeHash()
		_ = database.SaveTree(tr)

		cs := graph.NewChangeset(repo.ID, tr.RootHash, []string{branch.HeadID}, "Benchmarker", fmt.Sprintf("commit %d", i), "", "", "")
		res, err := q.Submit(repo.ID, cs)
		if err != nil || res.Status != StatusMerged {
			b.Fatalf("submit failed at iter %d: %v", i, err)
		}
	}
}

func BenchmarkMergeQueue_ConcurrentThroughput(b *testing.B) {
	tempDir := b.TempDir()
	dbPath := filepath.Join(tempDir, "bench_conc.db")
	database, _ := db.Open(dbPath)
	defer database.Close()

	store, _ := cas.NewStore(filepath.Join(tempDir, "cas"))
	repo, _ := database.CreateRepo("bench-repo")

	branch, _ := database.GetBranch(repo.ID, "main")
	initialHead := branch.HeadID

	const concurrency = 20
	latencies := make([]time.Duration, b.N)
	var mu sync.Mutex

	b.ResetTimer()
	b.SetParallelism(concurrency)

	idx := 0
	b.RunParallel(func(pb *testing.PB) {
		q := NewMergeQueue(database, store)
		for pb.Next() {
			mu.Lock()
			curIdx := idx
			idx++
			mu.Unlock()

			data := []byte(fmt.Sprintf("worker file %d\n", curIdx))
			m, _ := store.PutBlob(data)

			tr := tree.NewTree()
			tr.Entries[fmt.Sprintf("worker_%d.txt", curIdx)] = tree.Entry{
				Path: fmt.Sprintf("worker_%d.txt", curIdx),
				Type: tree.TypeBlob,
				Hash: m.BlobHash,
				Size: m.TotalSize,
				Mode: 0644,
			}
			tr.ComputeHash()
			_ = database.SaveTree(tr)

			cs := graph.NewChangeset(repo.ID, tr.RootHash, []string{initialHead}, "Worker", fmt.Sprintf("worker commit %d", curIdx), "", "", "")

			start := time.Now()
			res, err := q.Submit(repo.ID, cs)
			elapsed := time.Since(start)

			if err == nil && res.Status == StatusMerged && curIdx < len(latencies) {
				mu.Lock()
				latencies[curIdx] = elapsed
				mu.Unlock()
			}
		}
	})

	b.StopTimer()

	// Calculate p50, p95, p99 if we recorded measurements
	var valid []time.Duration
	for _, l := range latencies {
		if l > 0 {
			valid = append(valid, l)
		}
	}

	if len(valid) > 10 {
		sort.Slice(valid, func(i, j int) bool { return valid[i] < valid[j] })
		p50 := valid[len(valid)*50/100]
		p95 := valid[len(valid)*95/100]
		p99 := valid[len(valid)*99/100]
		b.ReportMetric(float64(p50.Microseconds())/1000.0, "p50_ms")
		b.ReportMetric(float64(p95.Microseconds())/1000.0, "p95_ms")
		b.ReportMetric(float64(p99.Microseconds())/1000.0, "p99_ms")
	}
}
