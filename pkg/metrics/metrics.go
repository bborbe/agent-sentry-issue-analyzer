// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package metrics exports the per-verdict counter the triage and deep steps
// increment.
//
// It is deliberately NOT registered on the shared agent_job_* registry. The
// pushgateway replaces a group's metric families on PUT and overwrites by
// family name on POST, so a counter pushed under the shared
// (agent, task_type) grouping key is overwritten by the next run and can never
// accumulate — see main.go, which pushes this counter through its own
// registry under a per-run grouping key.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

// verdictLabel is the label carrying the rubric verdict value.
const verdictLabel = "verdict"

// knownVerdicts is the closed verdict vocabulary the classifier writes.
// Mirrors the values in pkg/verdict/verdict.go.
//
// Every value is pre-initialized to 0: without it a verdict that has never
// been produced has no series at all, and rate()/increase() over it returns
// no data rather than zero — so a ratio over this counter would silently skip
// absent verdicts instead of reading them as 0. `unanalyzable` is the value
// this metric exists to make visible.
var knownVerdicts = []string{
	"already-tracked",
	"duplicate",
	"noise",
	"not-a-defect",
	"real bug",
	"regression",
	"unanalyzable",
}

// Metrics records the verdict a run produced.
//
//go:generate go run github.com/maxbrunsfeld/counterfeiter/v6@v6.12.2 -o mocks/metrics.go --fake-name Metrics . Metrics
type Metrics interface {
	// RecordVerdict increments the counter for the given verdict value. An
	// empty value (no ## Verdict section parsed) is ignored — an absent
	// verdict is not a verdict, and counting it would inflate the denominator
	// of every ratio computed over this counter.
	RecordVerdict(verdict string)
}

// NewMetrics registers agent_job_verdicts_total on the caller-owned registry
// and returns a Metrics. The caller must NOT pass nil.
//
// Registration failures panic — they are programmer errors caught at startup,
// matching libmetrics.NewJobMetrics.
func NewMetrics(registry *prometheus.Registry) Metrics {
	counter := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "agent_job_verdicts_total",
			Help: "Total number of Sentry analyzer verdicts by rubric verdict value.",
		},
		[]string{verdictLabel},
	)
	registry.MustRegister(counter)
	for _, verdict := range knownVerdicts {
		counter.With(prometheus.Labels{verdictLabel: verdict}).Add(0)
	}
	return &metrics{counter: counter}
}

type metrics struct {
	counter *prometheus.CounterVec
}

func (m *metrics) RecordVerdict(verdict string) {
	if verdict == "" {
		return
	}
	m.counter.With(prometheus.Labels{verdictLabel: verdict}).Inc()
}
