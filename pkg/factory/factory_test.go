// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package factory_test

import (
	"context"

	agentlib "github.com/bborbe/agent"
	claudelib "github.com/bborbe/agent/claude"
	"github.com/bborbe/cqrs/base"
	kafkamocks "github.com/bborbe/kafka/mocks"
	libtime "github.com/bborbe/time"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/factory"
)

var _ = Describe("CreateAgentProvider", func() {
	var (
		ctx      context.Context
		provider agentlib.AgentProvider
	)

	BeforeEach(func() {
		ctx = context.Background()
		provider = factory.CreateAgentProvider(
			claudelib.ClaudeConfigDir(""),
			claudelib.AgentDir("agent"),
			claudelib.AllowedTools{},
			claudelib.ClaudeModel("sonnet"),
			map[string]string{},
			map[string]string{},
		)
	})

	It("returns a non-nil provider", func() {
		Expect(provider).NotTo(BeNil())
	})

	It("Get returns the domain agent for TaskTypeLLM", func() {
		agent, err := provider.Get(ctx, agentlib.TaskTypeLLM)
		Expect(err).To(BeNil())
		Expect(agent).NotTo(BeNil())
	})

	It("Get returns the liveness agent for TaskTypeHealthcheck", func() {
		agent, err := provider.Get(ctx, agentlib.TaskTypeHealthcheck)
		Expect(err).To(BeNil())
		Expect(agent).NotTo(BeNil())
	})

	It("Get returns the SAME liveness agent for TaskTypeOAuthProbe (alias)", func() {
		healthcheckAgent, err := provider.Get(ctx, agentlib.TaskTypeHealthcheck)
		Expect(err).To(BeNil())
		oauthProbeAgent, err := provider.Get(ctx, agentlib.TaskTypeOAuthProbe)
		Expect(err).To(BeNil())
		Expect(oauthProbeAgent).To(BeIdenticalTo(healthcheckAgent))
	})

	Describe("Get with unknown task_type", func() {
		var err error

		BeforeEach(func() {
			_, err = provider.Get(ctx, agentlib.TaskType("bogus"))
		})

		It("returns an error", func() {
			Expect(err).To(HaveOccurred())
		})

		It("error message contains the unknown task_type literal", func() {
			Expect(err.Error()).To(ContainSubstring("unknown task_type"))
		})

		It("error message contains the offending value quoted", func() {
			Expect(err.Error()).To(ContainSubstring(`"bogus"`))
		})

		It("error message contains the binary name", func() {
			Expect(err.Error()).To(ContainSubstring("agent-sentry-issue-analyzer"))
		})

		It("error message contains the sorted accepted-types list", func() {
			Expect(err.Error()).To(ContainSubstring("[healthcheck llm oauth-probe]"))
		})
	})
})

var _ = Describe("CreateKafkaResultDeliverer", func() {
	It("returns a non-nil ResultDeliverer", func() {
		deliverer := factory.CreateKafkaResultDeliverer(
			nil,
			"",
			"",
			"",
			nil,
		)
		Expect(deliverer).NotTo(BeNil())
	})
})

var _ = DescribeTable(
	"CreateKafkaResultDeliverer publishes to the topic derived from TopicPrefix (golden master)",
	func(topicPrefix base.TopicPrefix, expectedTopic string) {
		fake := &kafkamocks.KafkaSyncProducer{}
		deliverer := factory.CreateKafkaResultDeliverer(
			fake,
			topicPrefix,
			"task-id",
			"# content",
			libtime.NewCurrentDateTime(),
		)
		Expect(deliverer).NotTo(BeNil())

		err := deliverer.DeliverResult(context.Background(), agentlib.AgentResultInfo{
			Status: agentlib.AgentStatusDone,
			Output: "# content",
		})
		Expect(err).NotTo(HaveOccurred())

		Expect(fake.SendMessageCallCount()).To(Equal(1))
		_, msg := fake.SendMessageArgsForCall(0)
		Expect(msg.Topic).To(Equal(expectedTopic))
	},
	Entry("develop prefix", base.TopicPrefix("develop"), "develop-agent-task-v1-request"),
	Entry("master prefix", base.TopicPrefix("master"), "master-agent-task-v1-request"),
	Entry("empty prefix", base.TopicPrefix(""), "agent-task-v1-request"),
)

var _ = Describe("CreateFileResultDeliverer", func() {
	It("returns a non-nil ResultDeliverer", func() {
		deliverer := factory.CreateFileResultDeliverer("/tmp/test-output.md")
		Expect(deliverer).NotTo(BeNil())
	})
})

var _ = Describe("CreateAgent", func() {
	It("returns a non-nil *agentlib.Agent", func() {
		agent := factory.CreateAgent(
			"",
			"",
			nil,
			"",
			nil,
			nil,
		)
		Expect(agent).NotTo(BeNil())
	})
})
