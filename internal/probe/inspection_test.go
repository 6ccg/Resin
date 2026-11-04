package probe

import (
	"errors"
	"testing"
	"time"

	"github.com/Resinat/Resin/internal/config"
	"github.com/Resinat/Resin/internal/node"
	"github.com/Resinat/Resin/internal/topology"
)

func TestAttributeProbe_SourceFailureDoesNotChangeHealth(t *testing.T) {
	pool:=topology.NewGlobalNodePool(topology.PoolConfig{MaxConsecutiveFailures:func()int{return 3}})
	hash:=node.HashFromRawOptions([]byte(`{"type":"attributes"}`));pool.AddNodeFromSub(hash,nil,"sub");entry,_:=pool.GetEntry(hash);storeOutbound(entry);entry.CircuitOpenSince.Store(0);entry.FailureCount.Store(2)
	cfg:=config.NewDefaultRuntimeConfig()
	manager:=NewProbeManager(ProbeConfig{Pool:pool,RuntimeConfig:func()*config.RuntimeConfig{return cfg},Fetcher:func(node.Hash,string)([]byte,time.Duration,error){return nil,0,errors.New("provider rate limited")}})
	if _,err:=manager.ProbeAttributesSync(hash);err==nil{t.Fatal("provider failure not reported")}
	if entry.FailureCount.Load()!=2||entry.IsCircuitOpen(){t.Fatal("provider failure changed node health")}
	inspection:=entry.Inspection();if inspection.AttributesAttemptAt==nil||inspection.AttributesError!="provider rate limited"{t.Fatalf("failure state not stored: %+v",inspection)}
}
