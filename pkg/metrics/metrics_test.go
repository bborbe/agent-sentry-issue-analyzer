// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package metrics_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/metrics"
)

// gatherVerdicts reads agent_job_verdicts_total out of the registry as a
// verdict-value -> counter map.
func gatherVerdicts(registry *prometheus.Registry) map[string]float64 {
	families, err := registry.Gather()
	Expect(err).NotTo(HaveOccurred())
	out := map[string]float64{}
	for _, family := range families {
		if family.GetName() != "agent_job_verdicts_total" {
			continue
		}
		for _, metric := range family.GetMetric() {
			value := ""
			for _, label := range metric.GetLabel() {
				if label.GetName() == "verdict" {
					value = label.GetValue()
				}
			}
			out[value] = metric.GetCounter().GetValue()
		}
	}
	return out
}

var _ = Describe("Metrics", func() {
	var (
		registry *prometheus.Registry
		subject  metrics.Metrics
	)

	BeforeEach(func() {
		registry = prometheus.NewRegistry()
		subject = metrics.NewMetrics(registry)
	})

	It("exposes a single counter named agent_job_verdicts_total", func() {
		// The _total suffix is mandatory — Prometheus panics at registration
		// without it, so a rename here would be caught by this assertion first.
		families, err := registry.Gather()
		Expect(err).NotTo(HaveOccurred())
		Expect(families).To(HaveLen(1))
		Expect(families[0].GetName()).To(Equal("agent_job_verdicts_total"))
		Expect(families[0].GetType().String()).To(Equal("COUNTER"))
	})

	It("pre-initializes every known verdict to zero", func() {
		// Without pre-initialization an unseen verdict has no series at all, and
		// a ratio over this counter would silently skip it rather than read it
		// as 0 — so the unanalyzable rate would look like no-data on a healthy
		// day instead of 0.
		values := gatherVerdicts(registry)
		Expect(values).To(HaveLen(7))
		for _, verdict := range []string{
			"already-tracked",
			"duplicate",
			"noise",
			"not-a-defect",
			"real bug",
			"regression",
			"unanalyzable",
		} {
			Expect(values).To(HaveKeyWithValue(verdict, 0.0))
		}
	})

	DescribeTable("increments only the named verdict",
		func(recorded string) {
			subject.RecordVerdict(recorded)
			values := gatherVerdicts(registry)
			Expect(values).To(HaveKeyWithValue(recorded, 1.0))
			for verdict, value := range values {
				if verdict != recorded {
					Expect(value).To(BeZero(), "verdict %q moved", verdict)
				}
			}
		},
		Entry("unanalyzable — the value this metric exists to expose", "unanalyzable"),
		Entry("real bug", "real bug"),
		Entry("noise", "noise"),
	)

	It("accumulates across calls so a rise is observable run over run", func() {
		subject.RecordVerdict("unanalyzable")
		subject.RecordVerdict("unanalyzable")
		subject.RecordVerdict("noise")
		values := gatherVerdicts(registry)
		Expect(values).To(HaveKeyWithValue("unanalyzable", 2.0))
		Expect(values).To(HaveKeyWithValue("noise", 1.0))
	})

	It("ignores an empty verdict", func() {
		// An absent ## Verdict section is not a verdict. Counting it would
		// inflate the denominator of every ratio computed over this counter.
		subject.RecordVerdict("")
		Expect(gatherVerdicts(registry)).NotTo(HaveKey(""))
	})

	It("records a verdict outside the known vocabulary", func() {
		// The vocabulary is closed today, but a new value must not be silently
		// dropped — it has to appear so the gap is visible.
		subject.RecordVerdict("brand-new-verdict")
		Expect(gatherVerdicts(registry)).To(HaveKeyWithValue("brand-new-verdict", 1.0))
	})
})
