package eventsourcing

import (
	"math"
	"time"
)

// PluginCapability describes a single high-level ability that an agent-backed plugin can provide.
type PluginCapability struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// PluginMetadata captures descriptive, human facing details about a plugin so the orchestrator
// can ground its decisions in more than a bare name.
type PluginMetadata struct {
	Name           string
	Summary        string
	UsageHint      string
	Capabilities   []PluginCapability
	Examples       []string
	Tags           []string
	Maintainer     string
	Source         string
	DefaultTimeout time.Duration
	Safety         string
	Reliability    string
	Lifecycle      string
	ModelAsset     string
}

// PluginMetadataSnapshot is a serialization friendly representation of PluginMetadata.
type PluginMetadataSnapshot struct {
	Name                  string             `json:"name"`
	Summary               string             `json:"summary,omitempty"`
	UsageHint             string             `json:"usage_hint,omitempty"`
	Capabilities          []PluginCapability `json:"capabilities,omitempty"`
	Examples              []string           `json:"examples,omitempty"`
	Tags                  []string           `json:"tags,omitempty"`
	Maintainer            string             `json:"maintainer,omitempty"`
	Source                string             `json:"source,omitempty"`
	DefaultTimeoutSeconds int                `json:"default_timeout_seconds,omitempty"`
	Safety                string             `json:"safety,omitempty"`
	Reliability           string             `json:"reliability,omitempty"`
	Lifecycle             string             `json:"lifecycle,omitempty"`
	ModelAsset            string             `json:"model_asset,omitempty"`
}

// PluginTelemetry tracks cumulative runtime behaviour of a plugin so the orchestrator
// can take reliability into account.
type PluginTelemetry struct {
	Invocations          int     `json:"invocations"`
	Successes            int     `json:"successes"`
	Failures             int     `json:"failures"`
	Timeouts             int     `json:"timeouts"`
	Panics               int     `json:"panics"`
	AverageLatencyMillis float64 `json:"average_latency_millis"`
	LastLatencyMillis    float64 `json:"last_latency_millis"`
	LastError            string  `json:"last_error,omitempty"`
	LastInvocation       string  `json:"last_invocation,omitempty"`
}

// PluginSnapshot fuses metadata and telemetry into a single payload that can be persisted or broadcast.
type PluginSnapshot struct {
	Metadata  PluginMetadataSnapshot `json:"metadata"`
	Telemetry PluginTelemetry        `json:"telemetry"`
}

// PluginMetadataProvider can be implemented by plugins that wish to provide richer descriptors.
type PluginMetadataProvider interface {
	Metadata() PluginMetadata
}

// PluginInvocationResult captures the outcome of a single plugin execution for telemetry aggregation.
type PluginInvocationResult struct {
	Timestamp time.Time
	Duration  time.Duration
	Success   bool
	TimedOut  bool
	Panicked  bool
	Error     string
}

// DefaultPluginMetadata returns a conservative metadata stub when a plugin does not provide details.
func DefaultPluginMetadata(name string) PluginMetadata {
	return PluginMetadata{
		Name:        name,
		Summary:     "No metadata supplied.",
		UsageHint:   "Use when the orchestrator explicitly knows this agent.",
		Tags:        []string{"unspecified"},
		Maintainer:  "unknown",
		Safety:      "unknown",
		Reliability: "untested",
		Lifecycle:   "unknown",
	}
}

// Snapshot converts the richer metadata struct into a serialization safe snapshot.
func (m PluginMetadata) Snapshot() PluginMetadataSnapshot {
	return PluginMetadataSnapshot{
		Name:                  m.Name,
		Summary:               m.Summary,
		UsageHint:             m.UsageHint,
		Capabilities:          m.Capabilities,
		Examples:              m.Examples,
		Tags:                  m.Tags,
		Maintainer:            m.Maintainer,
		Source:                m.Source,
		DefaultTimeoutSeconds: int(m.DefaultTimeout.Seconds()),
		Safety:                m.Safety,
		Reliability:           m.Reliability,
		Lifecycle:             m.Lifecycle,
		ModelAsset:            m.ModelAsset,
	}
}

// SuccessRate returns the success ratio of the plugin on the interval [0,1].
func (t PluginTelemetry) SuccessRate() float64 {
	if t.Invocations == 0 {
		return 0
	}
	return float64(t.Successes) / float64(t.Invocations)
}

// Merge aggregates a new invocation result into existing telemetry counters.
func (t PluginTelemetry) Merge(result PluginInvocationResult) PluginTelemetry {
	t.Invocations++
	if result.Success {
		t.Successes++
	} else {
		t.Failures++
	}
	if result.TimedOut {
		t.Timeouts++
	}
	if result.Panicked {
		t.Panics++
	}

	latencyMillis := float64(result.Duration.Microseconds()) / 1000.0
	if latencyMillis < 0 || math.IsNaN(latencyMillis) || math.IsInf(latencyMillis, 0) {
		latencyMillis = 0
	}
	t.LastLatencyMillis = latencyMillis
	if t.AverageLatencyMillis == 0 {
		t.AverageLatencyMillis = latencyMillis
	} else {
		// Simple exponential moving average to avoid unbounded totals.
		const smoothing = 0.3
		t.AverageLatencyMillis = smoothing*latencyMillis + (1-smoothing)*t.AverageLatencyMillis
	}

	if result.Error != "" {
		t.LastError = result.Error
	} else if result.TimedOut {
		t.LastError = "timed out"
	} else if result.Panicked {
		t.LastError = "panic recovered"
	} else if !result.Success {
		t.LastError = "unknown failure"
	} else {
		t.LastError = ""
	}

	t.LastInvocation = result.Timestamp.UTC().Format(time.RFC3339Nano)
	return t
}
