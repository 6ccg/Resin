package probe

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/Resinat/Resin/internal/config"
	"github.com/Resinat/Resin/internal/node"
	"github.com/Resinat/Resin/internal/probe/attributes"
)

var errProbeCancelled = errors.New("probe cancelled before execution")

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

// Acquire the node before a global slot, so another check waiting on the same
// proxy does not consume network capacity. Queue wait is outside probe timing
// and never becomes a failed health observation.
func (m *ProbeManager) acquireNodeSlot(ctx context.Context, batch bool, hash node.Hash) (*node.NodeEntry, func(), error) {
	entry, ok := m.pool.GetEntry(hash)
	if !ok || !entry.HasOutbound() {
		return nil, nil, fmt.Errorf("node outbound not ready")
	}
	if !entry.AcquireProbe(ctx, m.stopCh) {
		return nil, nil, errProbeCancelled
	}
	if !m.acquireSlot(ctx, batch) {
		entry.ReleaseProbe()
		return nil, nil, errProbeCancelled
	}
	release := func() { m.releaseSlot(batch); entry.ReleaseProbe() }
	select {
	case <-ctx.Done():
		release()
		return nil, nil, errProbeCancelled
	case <-m.stopCh:
		release()
		return nil, nil, errProbeCancelled
	default:
	}
	current, ok := m.pool.GetEntry(hash)
	if !ok || current != entry || !entry.HasOutbound() {
		release()
		return nil, nil, fmt.Errorf("node changed before probe execution")
	}
	return entry, release, nil
}

func (m *ProbeManager) recoveryInterval(entry *node.NodeEntry) time.Duration {
	cfg := m.settings()
	return retryInterval(time.Duration(cfg.RecoveryProbeInterval), time.Duration(cfg.RecoveryProbeMaxInterval), max(0, int(entry.FailureCount.Load())-cfg.MaxConsecutiveFailures))
}

func retryInterval(initial, maximum time.Duration, failures int) time.Duration {
	interval := min(initial, maximum)
	for ; failures > 0 && interval < maximum; failures-- {
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
	return m.probeAttributesSync(context.Background(), true, hash)
}

func (m *ProbeManager) probeAttributesSync(ctx context.Context, batch bool, hash node.Hash) (*node.EgressAttributes, error) {
	entry, release, err := m.acquireNodeSlot(ctx, batch, hash)
	if err != nil {
		return nil, err
	}
	defer release()
	return m.probeAttributes(hash, entry)
}

func (m *ProbeManager) probeAttributes(hash node.Hash, entry *node.NodeEntry) (*node.EgressAttributes, error) {
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
			interval = retryInterval(time.Duration(cfg.RecoveryProbeInterval), min(interval, time.Duration(cfg.RecoveryProbeMaxInterval)), max(0, inspection.AttributesFailures-1))
		}
		sameSource := inspection.Attributes != nil && inspection.Attributes.Provider == cfg.EgressAttributesProvider && inspection.Attributes.SourceURL == cfg.EgressAttributesURL && inspection.Attributes.IP == entry.GetEgressIP().String()
		if (sameSource || inspection.Attributes == nil && inspection.AttributesError != "") && inspection.AttributesAttemptAt != nil && time.Since(*inspection.AttributesAttemptAt) < interval {
			return true
		}
		m.enqueuePeriodicProbe(hash, probeTaskKindAttributes, probePriorityNormal)
		return true
	})
}
