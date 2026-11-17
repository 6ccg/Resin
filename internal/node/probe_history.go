package node

import "time"

// Keep the API/JSON representation separate from the retained samples. Target,
// kind and error strings repeat within a node's two bounded diagnostic windows.
type probeHistory struct {
	samples []storedProbe
	texts   []probeText
}

type storedProbe struct {
	checkedAt  time.Time
	durationMs float64
	latencyMs  float64
	httpStatus int
	kind       uint16
	target     uint16
	errorText  uint16
	success    bool
	general    bool
}

type probeText struct {
	value string
	refs  uint16
}

func (h *probeHistory) record(observation ProbeObservation, general bool) {
	count := 0
	oldest := -1
	for i, sample := range h.samples {
		if sample.general == general {
			if oldest < 0 {
				oldest = i
			}
			count++
		}
	}
	if count >= ProbeWindowSize {
		sample := h.samples[oldest]
		h.releaseText(sample.kind)
		h.releaseText(sample.target)
		h.releaseText(sample.errorText)
		copy(h.samples[oldest:], h.samples[oldest+1:])
		h.samples = h.samples[:len(h.samples)-1]
	}

	h.samples = append(h.samples, storedProbe{
		checkedAt:  observation.CheckedAt,
		durationMs: observation.DurationMs,
		latencyMs:  observation.LatencyMs,
		httpStatus: observation.HTTPStatus,
		kind:       h.retainText(observation.Kind),
		target:     h.retainText(observation.Target),
		errorText:  h.retainText(observation.Error),
		success:    observation.Success,
		general:    general,
	})
}

// Text IDs are one-based, with zero for empty strings. Each window holds at
// most 50 samples, so even three different strings per sample fit in uint16.
func (h *probeHistory) retainText(value string) uint16 {
	if value == "" {
		return 0
	}
	free := -1
	for i := range h.texts {
		text := &h.texts[i]
		if text.refs == 0 {
			if free < 0 {
				free = i
			}
		} else if text.value == value {
			text.refs++
			return uint16(i + 1)
		}
	}
	text := probeText{value: value, refs: 1}
	if free >= 0 {
		h.texts[free] = text
		return uint16(free + 1)
	}
	h.texts = append(h.texts, text)
	return uint16(len(h.texts))
}

func (h *probeHistory) releaseText(id uint16) {
	if id == 0 {
		return
	}
	text := &h.texts[id-1]
	text.refs--
	if text.refs == 0 {
		text.value = ""
	}
}

func (h *probeHistory) text(id uint16) string {
	if id == 0 {
		return ""
	}
	return h.texts[id-1].value
}

func (h *probeHistory) snapshot() []ProbeObservation {
	checks := make([]ProbeObservation, len(h.samples))
	for i, sample := range h.samples {
		checks[i] = ProbeObservation{
			Kind:       h.text(sample.kind),
			Target:     h.text(sample.target),
			CheckedAt:  sample.checkedAt,
			Success:    sample.success,
			DurationMs: sample.durationMs,
			LatencyMs:  sample.latencyMs,
			HTTPStatus: sample.httpStatus,
			Error:      h.text(sample.errorText),
		}
	}
	return checks
}
