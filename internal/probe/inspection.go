package probe

import (
	"context"
	"fmt"
	"net/netip"
	"time"

	"github.com/Resinat/Resin/internal/config"
	"github.com/Resinat/Resin/internal/node"
	"github.com/Resinat/Resin/internal/probe/attributes"
)

func IsAttributeProvider(id string) bool { _, ok := attributes.Lookup(id); return ok }

func (m *ProbeManager) settings() *config.RuntimeConfig {
	if m.runtimeConfig != nil {
		if cfg := m.runtimeConfig(); cfg != nil {
			return cfg
		}
	}
	cfg := config.NewDefaultRuntimeConfig()
	cfg.ProbeConcurrency = m.workerCount
	cfg.EgressAttributesEnabled = false
	return cfg
}

// When both classes have work, reserve half of the slots for manual batches.
// Either class can use spare capacity when the other has no waiting tasks.
func (m *ProbeManager) acquireSlot(ctx context.Context, batch bool) bool {
	m.slotMu.Lock()
	if batch {
		m.slotBatchWaiting++
	} else {
		m.slotNormalWaiting++
	}
	m.slotMu.Unlock()
	defer func() {
		m.slotMu.Lock()
		if batch {
			m.slotBatchWaiting--
		} else {
			m.slotNormalWaiting--
		}
		close(m.slotChanged)
		m.slotChanged = make(chan struct{})
		m.slotMu.Unlock()
	}()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-m.stopCh:
			return false
		default:
		}
		m.slotMu.Lock()
		limit := max(1, m.settings().ProbeConcurrency)
		reserved := max(1, limit/2)
		allowed := m.slotBatchWaiting == 0 || m.slotBatchActive >= reserved
		if batch {
			allowed = m.slotBatchActive < reserved || m.slotNormalWaiting == 0
		}
		if m.slotsActive < limit && allowed {
			m.slotsActive++
			if batch {
				m.slotBatchActive++
			}
			m.slotMu.Unlock()
			return true
		}
		changed := m.slotChanged
		m.slotMu.Unlock()
		select {
		case <-ctx.Done():
			return false
		case <-m.stopCh:
			return false
		case <-changed:
		}
	}
}

func (m *ProbeManager) releaseSlot(batch bool) {
	m.slotMu.Lock()
	m.slotsActive--
	if batch {
		m.slotBatchActive--
	}
	close(m.slotChanged)
	m.slotChanged = make(chan struct{})
	m.slotMu.Unlock()
}

func (m *ProbeManager) recoveryInterval(entry *node.NodeEntry) time.Duration {
	cfg := m.settings()
	interval, maximum := time.Duration(cfg.RecoveryProbeInterval), time.Duration(cfg.RecoveryProbeMaxInterval)
	for failures := max(0, int(entry.FailureCount.Load())-cfg.MaxConsecutiveFailures); failures > 0 && interval < maximum; failures-- {
		if interval > maximum/2 {
			return maximum
		}
		interval *= 2
	}
	return min(interval, maximum)
}

func (m *ProbeManager) refreshEntry(hash node.Hash, entry *node.NodeEntry) {
	if m.resolveEntry == nil {
		return
	}
	ips, err := m.resolveEntry(entry.Endpoint().Host)
	if err == nil && len(ips) > 0 {
		entry.SetEntryIPs(ips)
		m.pool.NotifyInspectionChanged(hash)
	}
}

func (m *ProbeManager) ProbeAttributesSync(hash node.Hash) (*node.EgressAttributes, error) {
	entry, ok := m.pool.GetEntry(hash)
	if !ok || !entry.HasOutbound() {
		return nil, fmt.Errorf("node outbound not ready")
	}
	if m.fetcher == nil {
		return nil, fmt.Errorf("probe fetcher not configured")
	}
	select {
	case <-m.stopCh:
		return nil, fmt.Errorf("probe manager stopped")
	default:
	}
	cfg := m.settings()
	provider, ok := attributes.Lookup(cfg.EgressAttributesProvider)
	if !ok {
		return nil, fmt.Errorf("unsupported attribute provider: %s", cfg.EgressAttributesProvider)
	}
	m.refreshEntry(hash, entry)
	body, _, err := m.fetcher(hash, cfg.EgressAttributesURL)
	var result *node.EgressAttributes
	if err == nil {
		result, err = provider.Parse(body)
	}
	if err != nil {
		entry.SetAttributes(nil, err)
		m.pool.NotifyInspectionChanged(hash)
		return nil, err
	}
	result.Provider, result.SourceURL, result.CheckedAt = provider.ID(), cfg.EgressAttributesURL, time.Now()
	ip, _ := netip.ParseAddr(result.IP)
	region := result.CountryCode
	m.pool.UpdateNodeEgressIP(hash, &ip, &region)
	entry.SetAttributes(result, nil)
	m.pool.NotifyInspectionChanged(hash)
	return result, nil
}

func (m *ProbeManager) scanAttributes() {
	cfg := m.settings()
	if !cfg.ProbeEnabled || !cfg.EgressAttributesEnabled {
		return
	}
	subLookup := m.pool.MakeSubLookup()
	m.pool.Range(func(hash node.Hash, entry *node.NodeEntry) bool {
		select {
		case <-m.stopCh:
			return false
		default:
		}
		if !entry.HasOutbound() || entry.IsDisabledBySubscriptions(subLookup) || entry.IsCircuitOpen() {
			return true
		}
		inspection := entry.Inspection()
		interval := time.Duration(cfg.EgressAttributesInterval)
		if inspection.AttributesError != "" {
			interval = min(interval, time.Duration(cfg.RecoveryProbeInterval))
		}
		sameSource := inspection.Attributes != nil && inspection.Attributes.Provider == cfg.EgressAttributesProvider && inspection.Attributes.SourceURL == cfg.EgressAttributesURL && inspection.Attributes.IP == entry.GetEgressIP().String()
		if (sameSource || inspection.Attributes == nil && inspection.AttributesError != "") && inspection.AttributesAttemptAt != nil && time.Since(*inspection.AttributesAttemptAt) < interval {
			return true
		}
		m.enqueueProbe(hash, probeTaskKindAttributes, probePriorityNormal)
		return true
	})
}
