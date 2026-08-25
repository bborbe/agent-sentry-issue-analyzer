// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Command create-tasks publishes one CreateTaskCommand per active Sentry alert
// so the sentry-issue-analyzer triage agent can classify each. It replaces the
// retired standalone sentry-watcher service's emit path: the sentry-watcher
// agent step (this repo) fetches the day's active unresolved alerts via
// scripts/sentry-create-tasks.sh and calls this binary to publish them.
//
// The emitted task shape (title, frontmatter, body, task_identifier) is
// byte-identical to what the retired Go sentry-watcher produced, so controller
// dedup (UUID5 over "sentry:<short-id>:<date>") and the triage input contract
// stay intact.
package main

import (
	"context"
	"encoding/json"
	"os"
	"time"

	agentlib "github.com/bborbe/agent"
	task "github.com/bborbe/agent/command/task"
	"github.com/bborbe/cqrs/base"
	cdb "github.com/bborbe/cqrs/cdb"
	"github.com/bborbe/errors"
	libkafka "github.com/bborbe/kafka"
	"github.com/bborbe/log"
	libsentry "github.com/bborbe/sentry"
	"github.com/bborbe/service"
	"github.com/golang/glog"
	"github.com/google/uuid"
)

func main() {
	app := &application{}
	os.Exit(service.Main(context.Background(), app, &app.SentryDSN, &app.SentryProxy))
}

type application struct {
	SentryDSN   string `required:"false" arg:"sentry-dsn"   env:"SENTRY_DSN"   usage:"SentryDSN"    display:"length"`
	SentryProxy string `required:"false" arg:"sentry-proxy" env:"SENTRY_PROXY" usage:"Sentry Proxy"`

	// AlertsFile is the path to a JSON array of compact alerts as produced by
	// scripts/sentry-create-tasks.sh.
	AlertsFile   string           `required:"true" arg:"alerts-file"   env:"ALERTS_FILE"   usage:"Path to JSON array of compact Sentry alerts"`
	KafkaBrokers libkafka.Brokers `required:"true" arg:"kafka-brokers" env:"KAFKA_BROKERS" usage:"Comma-separated Kafka broker list"`

	// TopicPrefix selects the Kafka topic prefix used for CQRS topic
	// construction (e.g. "develop" / "master"); empty means unprefixed topics.
	TopicPrefix base.TopicPrefix `required:"false" arg:"topic-prefix" env:"TOPIC_PREFIX" usage:"Kafka topic prefix for CQRS topic construction"`

	// TargetVault is the Obsidian vault slug the task-controller materializes
	// the task into (e.g. "personal"). Passed to the CreateCommandSender which
	// substitutes it into each command when unset.
	TargetVault string `required:"false" arg:"target-vault" env:"TARGET_VAULT" usage:"Obsidian vault slug for materialized tasks" default:"personal"`
	// Stage / Assignee / Status / Phase are the per-task frontmatter knobs
	// (same defaults as the retired watcher's TaskConfig).
	Stage    string `required:"false" arg:"stage"        env:"STAGE"        usage:"Frontmatter stage (dev|prod)"               default:"dev"`
	Assignee string `required:"false" arg:"assignee"     env:"ASSIGNEE"     usage:"Frontmatter assignee"                       default:"sentry-issue-analyzer"`
	Status   string `required:"false" arg:"status"       env:"STATUS"       usage:"Frontmatter status"                         default:"in_progress"`
	Phase    string `required:"false" arg:"phase"        env:"PHASE"        usage:"Frontmatter phase"                          default:"planning"`
}

// compactAlert is the compact per-alert JSON shape emitted by
// scripts/sentry-create-tasks.sh. project is the Sentry project slug string.
type compactAlert struct {
	ID        string `json:"id"`
	ShortID   string `json:"shortId"`
	Title     string `json:"title"`
	LastSeen  string `json:"lastSeen"`
	FirstSeen string `json:"firstSeen"`
	Count     int64  `json:"count"`
	Status    string `json:"status"`
	UserCount int64  `json:"userCount"`
	Permalink string `json:"permalink"`
	Project   string `json:"project"`
}

// taskConfig groups the per-task frontmatter envelope settings.
type taskConfig struct {
	Stage    string
	Assignee string
	Status   string
	Phase    string
}

// taskIDNamespace is the UUID5 namespace for sentry-issue-analyzer tasks.
// Stable across releases — changing it would break controller dedup. Must stay
// identical to the retired sentry-watcher's namespace.
var taskIDNamespace = uuid.MustParse("1b06ab0c-eede-481f-b6ee-e91b0708fba9")

// deriveTaskID returns a UUID5 derived deterministically from the (short-id,
// date) pair — byte-identical to the retired sentry-watcher's DeriveTaskID.
// Same (short-id, date) → same task id → controller dedup makes re-emit a
// no-op; a new date for the same issue → fresh daily task.
func deriveTaskID(shortID, date string) uuid.UUID {
	return uuid.NewSHA1(taskIDNamespace, []byte("sentry:"+shortID+":"+date))
}

// deriveDate derives the UTC calendar date (YYYY-MM-DD) of the lastSeen
// RFC3339 timestamp — byte-identical to the retired sentry-watcher's IssueDate.
func deriveDate(ctx context.Context, lastSeen string) (string, error) {
	if lastSeen == "" {
		return "", errors.Errorf(ctx, "issue has no lastSeen timestamp")
	}
	t, err := time.Parse(time.RFC3339, lastSeen)
	if err != nil {
		return "", errors.Wrapf(ctx, err, "parse lastSeen %q", lastSeen)
	}
	return t.UTC().Format("2006-01-02"), nil
}

// buildCreateCommand assembles the CreateTaskCommand for one active Sentry
// alert — shape-identical to the retired sentry-watcher's BuildCreateCommand
// (title/frontmatter/body rendered from the default templates). date is the
// UTC calendar date of the alert's last activity.
func buildCreateCommand(alert compactAlert, date string, cfg taskConfig) task.CreateCommand {
	taskIDStr := deriveTaskID(alert.ShortID, date).String()
	title := "Analyze Sentry issue " + alert.ShortID + " - " + date
	body := "# Analyze Sentry issue " + alert.ShortID + " - " + date + "\n\n" +
		"Active unresolved Sentry alert (last activity " + date + ") in project " + alert.Project + ".\n" +
		"- Issue: " + alert.ShortID + "\n" +
		"- Project: " + alert.Project + "\n" +
		"- Title: " + alert.Title + "\n" +
		"- URL: " + alert.Permalink
	return task.CreateCommand{
		TaskIdentifier: agentlib.TaskIdentifier(taskIDStr),
		Title:          title,
		Frontmatter: agentlib.TaskFrontmatter{
			"task_type":       "sentry-issue-analyzer",
			"assignee":        cfg.Assignee,
			"phase":           cfg.Phase,
			"status":          cfg.Status,
			"stage":           cfg.Stage,
			"task_identifier": taskIDStr,
			"title":           title,
			"short_id":        alert.ShortID,
			"project":         alert.Project,
			"date":            date,
			"issue_url":       alert.Permalink,
		},
		Body: body,
	}
}

// readAlerts loads the compact alerts JSON array from disk.
func readAlerts(ctx context.Context, path string) ([]compactAlert, error) {
	data, err := os.ReadFile(
		path,
	) // #nosec G304 -- alerts-file from trusted CLI input (the constrained script's temp file)
	if err != nil {
		return nil, errors.Wrap(ctx, err, "read alerts file")
	}
	var alerts []compactAlert
	if err := json.Unmarshal(data, &alerts); err != nil {
		return nil, errors.Wrap(ctx, err, "parse alerts file")
	}
	return alerts, nil
}

func (a *application) Run(ctx context.Context, _ libsentry.Client) error {
	alerts, err := readAlerts(ctx, a.AlertsFile)
	if err != nil {
		return errors.Wrap(ctx, err, "read alerts")
	}

	syncProducer, err := libkafka.NewSyncProducerWithName(
		ctx,
		a.KafkaBrokers,
		"sentry-watcher-agent-step",
	)
	if err != nil {
		return errors.Wrap(ctx, err, "create sync producer")
	}
	defer func() {
		if cerr := syncProducer.Close(); cerr != nil {
			glog.Warningf("close kafka sync producer: %v", cerr)
		}
	}()

	sender := cdb.NewCommandObjectSender(syncProducer, a.TopicPrefix, log.DefaultSamplerFactory)
	createSender := task.NewCreateCommandSender(sender, a.TargetVault)
	cfg := taskConfig{
		Stage:    a.Stage,
		Assignee: a.Assignee,
		Status:   a.Status,
		Phase:    a.Phase,
	}

	failed := 0
	for _, alert := range alerts {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		date, err := deriveDate(ctx, alert.LastSeen)
		if err != nil {
			return errors.Wrapf(ctx, err, "derive date for %s", alert.ShortID)
		}
		cmd := buildCreateCommand(alert, date, cfg)
		if err := createSender.SendCommand(ctx, cmd); err != nil {
			glog.Errorf(
				"publish create-task failed taskID=%s title=%q err=%v",
				string(cmd.TaskIdentifier),
				cmd.Title,
				err,
			)
			failed++
			continue
		}
	}
	if failed > 0 {
		return errors.Errorf(
			ctx,
			"%d of %d create-task commands failed to publish",
			failed,
			len(alerts),
		)
	}
	glog.V(2).Infof("published %d create-task commands", len(alerts))
	return nil
}
