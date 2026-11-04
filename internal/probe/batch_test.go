package probe

import (
	"sync"
	"testing"
	"time"

	"github.com/Resinat/Resin/internal/config"
	"github.com/Resinat/Resin/internal/node"
	"github.com/Resinat/Resin/internal/topology"
)

func TestBatch_CancellationUsesSharedConcurrencyLimit(t *testing.T) {
	pool := topology.NewGlobalNodePool(topology.PoolConfig{MaxConsecutiveFailures: func() int { return 3 }})
	hashes := []node.Hash{}
	for _, raw := range []string{`{"type":"a"}`, `{"type":"b"}`, `{"type":"c"}`} {
		hash := node.HashFromRawOptions([]byte(raw))
		pool.AddNodeFromSub(hash, []byte(raw), "sub")
		entry, _ := pool.GetEntry(hash)
		storeOutbound(entry)
		hashes = append(hashes, hash)
	}
	cfg := config.NewDefaultRuntimeConfig()
	cfg.ProbeConcurrency = 1
	started := make(chan struct{}, 3)
	release := make(chan struct{})
	var releaseOnce sync.Once
	manager := NewProbeManager(ProbeConfig{Pool: pool, Concurrency: 3, RuntimeConfig: func() *config.RuntimeConfig { return cfg }, Fetcher: func(node.Hash, string) ([]byte, time.Duration, error) {
		started <- struct{}{}
		<-release
		return nil, time.Millisecond, nil
	}})
	defer manager.Stop()
	defer releaseOnce.Do(func() { close(release) })
	job, err := manager.StartBatch("latency", hashes)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("batch did not start")
	}
	if len(started) != 0 {
		t.Fatal("batch ignored configured concurrency")
	}
	if !manager.CancelBatch(job.ID) {
		t.Fatal("cancel failed")
	}
	releaseOnce.Do(func() { close(release) })
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		status := manager.BatchStatus()
		if !status.Running {
			if status.Completed != 1 || !status.Cancelled {
				t.Fatalf("pending checks continued after cancel: %+v", status)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("cancelled batch did not finish")
}
