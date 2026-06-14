package runtime

import (
	"context"
	"fmt"
	"strings"
	"time"

	"private-buddy-server/internal/database"
	"private-buddy-server/internal/model"
	"private-buddy-server/internal/service"
	"private-buddy-server/internal/service/llm"
	"private-buddy-server/internal/service/memory"

	applogger "private-buddy-server/internal/logger"
)

// Heartbeat check frequency constants. Each check runs at a different cadence
// to match the cognitive time scale of the task.
const (
	obligationCheckInterval    = 3 // Every 3 heartbeat ticks
	memoryDensityCheckInterval = 6 // Every 6 heartbeat ticks
)

// checkObligations runs obligation check: evaluates whether the agent has
// outstanding commitments that need attention.
func (r *agentRuntime) checkObligations(ctx context.Context) {
	agent := service.GetAgent(r.agentID)
	llmConfig := service.GetLLMConfig(agent.LLMConfigID)
	if llmConfig == nil {
		return
	}

	// Query pending scheduled events
	var pendingEvents []model.ScheduledEvent
	if err := database.DB.Where("agent_id = ? AND status = ?", r.agentID, model.ScheduledEventPending).
		Order("trigger_at ASC").Limit(10).Find(&pendingEvents).Error; err != nil {
		applogger.L.Warn("failed to load pending scheduled events", "agent_id", r.agentID, "error", err)
		return
	}

	// Build obligation prompt
	var eventDescs []string
	for _, se := range pendingEvents {
		timeUntil := se.TriggerAt.Sub(timeNow()).Round(time.Minute)
		overdue := ""
		if timeUntil < 0 {
			overdue = " [OVERDUE]"
		}
		eventDescs = append(eventDescs, fmt.Sprintf(
			"- Scheduled event #%d: \"%s\" at %s (in %s)%s",
			se.ID, se.Message, se.TriggerAt.Format("15:04"), timeUntil, overdue,
		))
	}

	if len(eventDescs) == 0 {
		applogger.L.Debug("obligation check: no pending obligations",
			"agent_id", r.agentID,
		)
		return
	}

	prompt := fmt.Sprintf(`You are an agent's obligation-checking subsystem. Review your pending commitments and decide what to do.

Agent role: %s

Pending scheduled events:
%s

For each event, evaluate:
1. Is the event overdue? If so, should it be executed now or deferred?
2. If deferred, when should it be re-evaluated?
3. Does the person need to be reminded about any pending event?

Return your assessment. Default to no action unless you are confident action is needed.`,
		agent.Description, strings.Join(eventDescs, "\n"))

	chatModel := llm.NewChatModelWithTemperature(
		llmConfig.BaseURL, llmConfig.APIKey, llmConfig.ModelID, llm.TemperatureDeterministic,
	)

	result, err := chatModel.Chat(ctx, []llm.Message{
		{Role: "user", Content: prompt},
	})

	if err != nil {
		applogger.L.Error("obligation check LLM call failed",
			"agent_id", r.agentID, "error", err)
		return
	}

	applogger.L.Info("obligation check completed",
		"agent_id", r.agentID,
		"pending_events", len(pendingEvents),
		"result_len", len(result),
	)
}

// checkMemoryDensity runs memory density check: detects when enough long-term
// observations have accumulated around an entity to trigger EntityProfile
// generation.
func (r *agentRuntime) checkMemoryDensity(ctx context.Context) {
	triggered := memory.CheckProfileDensity(ctx, r.agentID)
	if triggered > 0 {
		applogger.L.Info("memory density check: EntityProfile generation triggered",
			"agent_id", r.agentID,
			"profiles_triggered", triggered,
		)
	} else {
		applogger.L.Debug("memory density check: no profiles triggered",
			"agent_id", r.agentID,
		)
	}
}

// timeNow is a package-level variable for testability.
var timeNow = func() time.Time { return time.Now() }
