// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/format"
)

func TestSuite(t *testing.T) {
	time.Local = time.UTC
	format.TruncatedDiff = false
	RegisterFailHandler(Fail)
	suiteConfig, reporterConfig := GinkgoConfiguration()
	suiteConfig.Timeout = 60 * time.Second
	RunSpecs(t, "Create-Tasks Suite", suiteConfig, reporterConfig)
}

var _ = Describe("create-tasks task builder", func() {
	var (
		ctx context.Context
		cfg taskConfig
	)

	BeforeEach(func() {
		ctx = context.Background()
		cfg = taskConfig{
			Stage:    "dev",
			Assignee: "sentry-issue-analyzer",
			Status:   "in_progress",
			Phase:    "planning",
		}
	})

	alert := compactAlert{
		ID:        "42",
		ShortID:   "SENTRY-TEST-1",
		Title:     "Example unresolved error",
		LastSeen:  "2026-08-23T04:00:00Z",
		FirstSeen: "2026-08-20T01:00:00Z",
		Count:     12,
		Status:    "unresolved",
		UserCount: 3,
		Permalink: "https://bborbe.sentry.io/organizations/bborbe/issues/42/",
		Project:   "nuke-prod",
	}

	It("derives the same TaskIdentifier for the same (shortId, date)", func() {
		cmd1 := buildCreateCommand(alert, "2026-08-23", cfg)
		cmd2 := buildCreateCommand(alert, "2026-08-23", cfg)
		Expect(cmd1.TaskIdentifier).To(Equal(cmd2.TaskIdentifier))
	})

	It("derives the deterministic UUID5 task id for (shortId, date)", func() {
		cmd := buildCreateCommand(alert, "2026-08-23", cfg)
		Expect(uuid.MustParse(string(cmd.TaskIdentifier)).Version()).To(BeEquivalentTo(5))
	})

	It("derives a new TaskIdentifier when the date changes", func() {
		cmd1 := buildCreateCommand(alert, "2026-08-23", cfg)
		cmd2 := buildCreateCommand(alert, "2026-08-24", cfg)
		Expect(cmd1.TaskIdentifier).NotTo(Equal(cmd2.TaskIdentifier))
	})

	It("builds the exact watcher title", func() {
		cmd := buildCreateCommand(alert, "2026-08-23", cfg)
		Expect(cmd.Title).To(Equal("Analyze Sentry issue SENTRY-TEST-1 - 2026-08-23"))
	})

	It("frontmatter has all required keys", func() {
		cmd := buildCreateCommand(alert, "2026-08-23", cfg)
		for _, key := range []string{
			"task_type", "assignee", "phase", "status", "stage",
			"task_identifier", "title", "short_id", "project", "date", "issue_url",
		} {
			Expect(cmd.Frontmatter).To(HaveKey(key))
		}
		Expect(cmd.Frontmatter["task_type"]).To(Equal("sentry-issue-analyzer"))
		Expect(cmd.Frontmatter["short_id"]).To(Equal("SENTRY-TEST-1"))
		Expect(cmd.Frontmatter["project"]).To(Equal("nuke-prod"))
		Expect(cmd.Frontmatter["date"]).To(Equal("2026-08-23"))
		Expect(cmd.Frontmatter["issue_url"]).To(Equal(alert.Permalink))
		Expect(cmd.Frontmatter["task_identifier"]).To(Equal(string(cmd.TaskIdentifier)))
	})

	It("derives the UTC calendar date from lastSeen", func() {
		d, err := deriveDate(ctx, "2026-08-23T23:30:00+02:00")
		Expect(err).NotTo(HaveOccurred())
		Expect(d).To(Equal("2026-08-23"))
		d, err = deriveDate(ctx, "2026-08-24T01:30:00+02:00")
		Expect(err).NotTo(HaveOccurred())
		Expect(d).To(Equal("2026-08-23"))
	})

	It("fails to derive a date from an empty or malformed lastSeen", func() {
		_, err := deriveDate(ctx, "")
		Expect(err).To(HaveOccurred())
		_, err = deriveDate(ctx, "not-a-time")
		Expect(err).To(HaveOccurred())
	})
})
