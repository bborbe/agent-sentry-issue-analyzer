// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"reflect"
	"testing"
	"time"

	task "github.com/bborbe/agent/command/task"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/format"
	"github.com/onsi/gomega/gexec"
)

func TestCreateTasksSuite(t *testing.T) {
	time.Local = time.UTC
	format.TruncatedDiff = false
	RegisterFailHandler(Fail)
	suiteConfig, reporterConfig := GinkgoConfiguration()
	suiteConfig.Timeout = 60 * time.Second
	RunSpecs(t, "Create-Tasks Suite", suiteConfig, reporterConfig)
}

var _ = Describe("CreateTasks", func() {
	It("Compiles", func() {
		var err error
		_, err = gexec.Build(
			"github.com/bborbe/agent-sentry-issue-analyzer/cmd/create-tasks",
			"-mod=mod",
			"-buildvcs=false",
		)
		Expect(err).NotTo(HaveOccurred())
	})
})

var _ = AfterSuite(func() {
	gexec.CleanupBuildArtifacts()
})

// fakeCreateSender records what the fan-out published, and can be told to fail
// one title so the publish-error arm is exercised without a Kafka broker.
type fakeCreateSender struct {
	sent       []task.CreateCommand
	failTitles map[string]bool
}

func (f *fakeCreateSender) SendCommand(_ context.Context, cmd task.CreateCommand) error {
	if f.failTitles[cmd.Title] {
		return context.DeadlineExceeded
	}
	f.sent = append(f.sent, cmd)
	return nil
}

func (f *fakeCreateSender) titles() []string {
	titles := make([]string, 0, len(f.sent))
	for _, cmd := range f.sent {
		titles = append(titles, cmd.Title)
	}
	return titles
}

// fakeVerdictStore answers the prior-body read per task id, so one alert in a
// run can be refused while its siblings are read normally — the 2026-11-11
// condition, where the vault was unreachable for the whole fan-out.
type fakeVerdictStore struct {
	priors   map[string]string
	readErrs map[string]error
	written  map[string]string
}

func newFakeVerdictStore() *fakeVerdictStore {
	return &fakeVerdictStore{
		priors:   map[string]string{},
		readErrs: map[string]error{},
		written:  map[string]string{},
	}
}

func (f *fakeVerdictStore) ReadTaskBody(_ context.Context, taskID string) (string, error) {
	if err := f.readErrs[taskID]; err != nil {
		return "", err
	}
	return f.priors[taskID], nil
}

func (f *fakeVerdictStore) WriteRecord(_ context.Context, shortID, content string) error {
	f.written[shortID] = content
	return nil
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
		Status:    "unresolved",
		Permalink: "https://bborbe.sentry.io/organizations/bborbe/issues/42/",
		Project:   "nuke-prod",
	}

	It("derives the same TaskIdentifier for the same (shortId, date)", func() {
		cmd1 := buildCreateCommand(alert, "2026-08-23", renderBody(alert, "2026-08-23"), cfg)
		cmd2 := buildCreateCommand(alert, "2026-08-23", renderBody(alert, "2026-08-23"), cfg)
		Expect(cmd1.TaskIdentifier).To(Equal(cmd2.TaskIdentifier))
	})

	It("derives the deterministic UUID5 task id for (shortId, date)", func() {
		cmd := buildCreateCommand(alert, "2026-08-23", renderBody(alert, "2026-08-23"), cfg)
		Expect(uuid.MustParse(string(cmd.TaskIdentifier)).Version()).To(BeEquivalentTo(5))
	})

	It("derives a new TaskIdentifier when the date changes", func() {
		cmd1 := buildCreateCommand(alert, "2026-08-23", renderBody(alert, "2026-08-23"), cfg)
		cmd2 := buildCreateCommand(alert, "2026-08-24", renderBody(alert, "2026-08-24"), cfg)
		Expect(cmd1.TaskIdentifier).NotTo(Equal(cmd2.TaskIdentifier))
	})

	It("builds the exact collector title", func() {
		cmd := buildCreateCommand(alert, "2026-08-23", renderBody(alert, "2026-08-23"), cfg)
		Expect(cmd.Title).To(Equal("Analyze Sentry issue SENTRY-TEST-1 - 2026-08-23"))
	})

	It("frontmatter has all required keys", func() {
		cmd := buildCreateCommand(alert, "2026-08-23", renderBody(alert, "2026-08-23"), cfg)
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

var _ = Describe("deriveStage", func() {
	DescribeTable(
		"derives the stage from short_id or project",
		func(shortID, project, fallback, want string) {
			alert := compactAlert{ShortID: shortID, Project: project}
			Expect(deriveStage(alert, fallback)).To(Equal(want))
		},
		Entry("NUKE-PROD short_id", "NUKE-PROD-9K", "nuke-prod", "dev", "prod"),
		Entry("NUKE-DEV short_id", "NUKE-DEV-A4", "nuke-dev", "dev", "dev"),
		Entry("nuke-prod project slug", "SENTRY-TEST-1", "nuke-prod", "dev", "prod"),
		Entry("nuke-dev project slug", "SENTRY-TEST-2", "nuke-dev", "dev", "dev"),
		Entry("unknown short_id and project falls back", "SENTRY-TEST-3", "other", "dev", "dev"),
		Entry(
			"unknown short_id and project falls back to custom stage",
			"SENTRY-TEST-4",
			"other",
			"prod",
			"prod",
		),
	)

	It("stamps the derived stage into the built task frontmatter", func() {
		localCfg := taskConfig{
			Stage:    "dev",
			Assignee: "sentry-issue-analyzer",
			Status:   "in_progress",
			Phase:    "planning",
		}
		prodAlert := compactAlert{
			ShortID: "NUKE-PROD-9K",
			Project: "nuke-prod",
		}
		cmd := buildCreateCommand(
			prodAlert,
			"2026-08-30",
			renderBody(prodAlert, "2026-08-30"),
			localCfg,
		)
		Expect(cmd.Frontmatter["stage"]).To(Equal("prod"))
	})
})

var _ = Describe("dispatchAlerts", func() {
	// The fan-out contract the fix must not break: a retention refusal narrows
	// what may be WRITTEN (one alert's create command), never what may be
	// DISPATCHED (the remaining alerts in the same run). A fix that returns an
	// error out of the loop instead — the shape the fail-open was written to
	// avoid — fails these specs.
	var (
		ctx context.Context
		cfg taskConfig
		now time.Time
	)

	BeforeEach(func() {
		ctx = context.Background()
		cfg = taskConfig{
			Stage:    "dev",
			Assignee: "sentry-issue-analyzer",
			Status:   "in_progress",
			Phase:    "planning",
		}
		now = time.Date(2026, 11, 11, 15, 0, 0, 0, time.UTC)
	})

	newAlert := func(shortID string) compactAlert {
		return compactAlert{
			ID:        shortID,
			ShortID:   shortID,
			Title:     "Example unresolved error",
			LastSeen:  "2026-11-11T04:00:00Z",
			Status:    "unresolved",
			Permalink: "https://bborbe.sentry.io/organizations/bborbe/issues/1/",
			Project:   "nuke-dev",
		}
	}

	taskIDOf := func(alert compactAlert) string {
		return deriveTaskID(alert.ShortID, "2026-11-11").String()
	}

	It("publishes every alert when every retention read succeeds", func() {
		alerts := []compactAlert{
			newAlert("OCTOPUS-DEV-1A"),
			newAlert("OCTOPUS-DEV-2B"),
			newAlert("OCTOPUS-DEV-3C"),
		}
		store := newFakeVerdictStore()
		sender := &fakeCreateSender{}

		published, failed, refused, err := dispatchAlerts(ctx, alerts, cfg, store, sender, now)

		Expect(err).NotTo(HaveOccurred())
		Expect(published).To(Equal(3))
		Expect(failed).To(Equal(0))
		Expect(refused).To(Equal(0))
		Expect(sender.titles()).To(HaveLen(3))
	})

	It("skips only the refused alert and still publishes the rest", func() {
		alerts := []compactAlert{
			newAlert("OCTOPUS-DEV-1A"),
			newAlert("OCTOPUS-DEV-2B"),
			newAlert("OCTOPUS-DEV-3C"),
		}
		store := newFakeVerdictStore()
		store.readErrs[taskIDOf(alerts[1])] = context.DeadlineExceeded
		sender := &fakeCreateSender{}

		published, failed, refused, err := dispatchAlerts(ctx, alerts, cfg, store, sender, now)

		Expect(err).NotTo(HaveOccurred())
		Expect(published).To(Equal(2))
		Expect(failed).To(Equal(0))
		Expect(refused).To(Equal(1))
		Expect(sender.titles()).To(Equal([]string{
			"Analyze Sentry issue OCTOPUS-DEV-1A - 2026-11-11",
			"Analyze Sentry issue OCTOPUS-DEV-3C - 2026-11-11",
		}))
	})

	// SC1 at the caller: the refused alert's command is never sent, so no body
	// that could replace an analysed task ever reaches the controller.
	It("sends no command at all for a refused alert", func() {
		alerts := []compactAlert{newAlert("OCTOPUS-PROD-8W")}
		store := newFakeVerdictStore()
		store.readErrs[taskIDOf(alerts[0])] = context.DeadlineExceeded
		sender := &fakeCreateSender{}

		published, _, refused, err := dispatchAlerts(ctx, alerts, cfg, store, sender, now)

		Expect(err).NotTo(HaveOccurred())
		Expect(published).To(Equal(0))
		Expect(refused).To(Equal(1))
		Expect(sender.sent).To(BeEmpty())
	})

	// SC5 at the caller: the success path is unchanged — a reachable read still
	// splices the prior verdict into the published body.
	It("publishes the spliced body when the retention read succeeds", func() {
		alerts := []compactAlert{newAlert("OCTOPUS-PROD-8W")}
		store := newFakeVerdictStore()
		store.priors[taskIDOf(alerts[0])] = "## Verdict\n\nverdict: real bug\n"
		sender := &fakeCreateSender{}

		_, _, refused, err := dispatchAlerts(ctx, alerts, cfg, store, sender, now)

		Expect(err).NotTo(HaveOccurred())
		Expect(refused).To(Equal(0))
		Expect(sender.sent).To(HaveLen(1))
		Expect(sender.sent[0].Body).To(ContainSubstring("verdict: real bug"))
		Expect(store.written).To(HaveKey("OCTOPUS-PROD-8W"))
	})

	It("keeps publishing after a publish failure", func() {
		alerts := []compactAlert{
			newAlert("OCTOPUS-DEV-1A"),
			newAlert("OCTOPUS-DEV-2B"),
		}
		sender := &fakeCreateSender{failTitles: map[string]bool{
			"Analyze Sentry issue OCTOPUS-DEV-1A - 2026-11-11": true,
		}}

		published, failed, refused, err := dispatchAlerts(
			ctx, alerts, cfg, newFakeVerdictStore(), sender, now,
		)

		Expect(err).NotTo(HaveOccurred())
		Expect(published).To(Equal(1))
		Expect(failed).To(Equal(1))
		Expect(refused).To(Equal(0))
	})
})

var _ = Describe("application defaults", func() {
	// The executor resolves an agent Config by the exact `assignee` string and
	// silently skips unknown names (`skipped_unknown_assignee`). The value is now
	// sourced solely from the collector lane's env: block, so the tag must stay
	// required with NO default: a default would be a second copy of the name, and
	// an unset value would stamp an empty assignee the executor silently drops —
	// task filed, nothing claims it, nothing errors.
	It("requires assignee with no default, so an unset value fails loudly", func() {
		field, ok := reflect.TypeOf(application{}).FieldByName("Assignee")
		Expect(ok).To(BeTrue())
		Expect(field.Tag.Get("required")).To(Equal("true"))
		Expect(field.Tag.Get("default")).To(BeEmpty())
	})

	It("defaults stage to dev as the non-derivable fallback", func() {
		field, ok := reflect.TypeOf(application{}).FieldByName("Stage")
		Expect(ok).To(BeTrue())
		Expect(field.Tag.Get("default")).To(Equal("dev"))
	})
})
