package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/sashabaranov/go-openai/jsonschema"

	"private-buddy-server/internal/database"
	"private-buddy-server/internal/model"
	"private-buddy-server/internal/service"
	"private-buddy-server/internal/service/chat/chatcontext"
	"private-buddy-server/internal/service/eventqueue"
	"private-buddy-server/internal/service/llm"
	"private-buddy-server/internal/service/memory"

	applogger "private-buddy-server/internal/logger"
)

// commitRequest represents a request to commit a draft to the messages table.
// Sent through commitCh to serialize message writes across concurrent Works.
type commitRequest struct {
	draft           *model.MessageDraft
	sessionID       int64
	content         string // Final content to write
	hasInteractions int    // HasInteractionsPending, HasInteractionsExists, or HasInteractionsNone
}

// Heartbeat interval constants for the tickless three-phase model.
// Active: agent just completed interaction, context is fresh.
// Steady: session has ongoing activity but agent doesn't participate.
// Dormant: session has been idle for a long time.
const (
	heartbeatActive  = 5 * time.Minute
	heartbeatSteady  = 30 * time.Minute
	heartbeatDormant = 2 * time.Hour
	ticksToSteady    = 3 // Consecutive none → transition to steady
	ticksToDormant   = 6 // Consecutive none → transition to dormant
)

// heartbeatAction represents the self-reflection output from a heartbeat tick.
type heartbeatAction int

const (
	heartbeatNone             heartbeatAction = iota // Nothing to do
	heartbeatProactiveMessage                        // Agent proactively sends a message
	heartbeatContextRefresh                          // Agent refreshes its context summary
)

// heartbeatOutput is the structured output schema for the LLM heartbeat self-reflection.
type heartbeatOutput struct {
	Action          string `json:"action"`
	Reason          string `json:"reason"`
	TargetSessionID int64  `json:"target_session_id"`
	MessageDraft    string `json:"message_draft"`
}

// Proactive message frequency control constants.
// Ensures the agent doesn't spam the user with unsolicited messages.
const (
	proactiveMinIntervalHours = 12 // Minimum hours between proactive messages
	proactiveMinHeartbeats    = 6  // Minimum heartbeat ticks between proactive messages
)

// agentRuntime is the event-driven execution engine for an agent.
// It transforms an Agent from a passive configuration object into an active,
// stateful entity with its own lifecycle.
//
// The runtime runs a single goroutine event loop (for-select + eventCh + heartbeatTimer).
// Work execution runs in separate goroutines, allowing the event loop to remain responsive.
type agentRuntime struct {
	agentID             int64
	workRouter          workRouter
	activeWorks         []*work
	eventCh             <-chan eventqueue.AgentEvent // Read-only channel subscribed from eventqueue.Global
	commitCh            chan commitRequest
	workDoneCh          chan *work
	heartbeatInterval   time.Duration                              // Base heartbeat interval (adaptive)
	idleTicks           int                                        // Consecutive idle heartbeats (for tickless backoff)
	heartbeatTick       int                                        // Total heartbeat ticks (for check scheduling)
	lastProactiveSent   time.Time                                  // Last time a proactive message was sent
	ticksSinceProactive int                                        // Heartbeat ticks since last proactive message
	mu                  sync.Mutex                                 // Protects activeWrites for external queries
	onStatusChange      func(agentID, sessionID int64, status int) // Callback for SSE push
}

// newAgentRuntime creates a new runtime for an agent.
// eventCh is the read-only event channel obtained from eventqueue.Subscribe().
func newAgentRuntime(
	agentID int64,
	workRouter workRouter,
	eventCh <-chan eventqueue.AgentEvent,
	heartbeatInterval time.Duration,
	onStatusChange func(agentID, sessionID int64, status int),
) *agentRuntime {
	return &agentRuntime{
		agentID:           agentID,
		workRouter:        workRouter,
		eventCh:           eventCh,
		commitCh:          make(chan commitRequest, 16),
		workDoneCh:        make(chan *work, 16),
		heartbeatInterval: heartbeatInterval,
		onStatusChange:    onStatusChange,
	}
}

// Run starts the agent's event loop. Blocks until context is cancelled.
// The ctx should be the runtime's lifecycle context, created with a cancel
// function stored on the struct for external shutdown via Stop().
func (r *agentRuntime) Run(ctx context.Context) {
	heartbeatTimer := time.NewTimer(r.heartbeatInterval)

	// Start commit handler goroutine
	go r.handleCommits(ctx)

	for {
		select {
		case <-ctx.Done():
			heartbeatTimer.Stop()
			// Drain the timer channel to prevent leak if Stop returned false
			select {
			case <-heartbeatTimer.C:
			default:
			}
			applogger.L.Info("agentRuntime stopped", "agent_id", r.agentID)
			return

		case event := <-r.eventCh:
			// Reset idle counter on event arrival
			r.idleTicks = 0

			// Mark the message as read immediately upon receiving the event.
			// "Read" means the agent is aware of the message — this is separate
			// from "processed" (which happens after the work completes).
			// - For EventTypeNewMessage: the actual user message.
			// - For EventTypeScheduled: the original user message that caused
			//   the alarm (TriggerMessageID), preserving the causal chain.
			if payload, ok := event.Payload.(*eventqueue.NewMessagePayload); ok && payload.MessageID > 0 {
				if err := database.DB.Model(&model.ParticipantSession{}).
					Where("session_id = ? AND participant_type = ? AND participant_id = ?",
						event.SessionID, model.ParticipantTypeAgent, r.agentID).
					Update("last_read_message_id", payload.MessageID).Error; err != nil {
					applogger.L.Warn("failed to advance last_read_message_id on new message event", "error", err)
				}
			}
			if payload, ok := event.Payload.(*eventqueue.ScheduledEventPayload); ok && payload.TriggerMessageID > 0 {
				if err := database.DB.Model(&model.ParticipantSession{}).
					Where("session_id = ? AND participant_type = ? AND participant_id = ? AND last_read_message_id < ?",
						event.SessionID, model.ParticipantTypeAgent, r.agentID, payload.TriggerMessageID).
					Update("last_read_message_id", payload.TriggerMessageID).Error; err != nil {
					applogger.L.Warn("failed to advance last_read_message_id on scheduled event", "error", err)
				}
			}

			// Fast path for scheduled events with action=send_message.
			// Skip the entire LLM pipeline and directly commit the pre-computed
			// message. This is the optimization for simple reminders where the
			// agent already knows exactly what to say when setting the alarm.
			if payload, ok := event.Payload.(*eventqueue.ScheduledEventPayload); ok &&
				payload.Action == model.ScheduledEventActionSendMessage &&
				payload.ActionContent != "" {
				r.handleFastPathSendMessage(event.SessionID, payload)
				r.resetHeartbeatTimer(heartbeatTimer)
				continue
			}

			// Decision: how should the agent respond to this event?
			agent := service.GetAgent(r.agentID)
			llmConfig := service.GetLLMConfig(agent.LLMConfigID)
			decision := Decide(ctx, event, agent, llmConfig)

			switch decision.Action {
			case ActionIgnore:
				applogger.L.Debug("Agent decided to ignore event",
					"agent_id", r.agentID,
					"event_type", event.Type,
					"reasoning", decision.Reasoning,
				)
				r.resetHeartbeatTimer(heartbeatTimer)
				continue

			case ActionDefer:
				applogger.L.Debug("Agent decided to defer event",
					"agent_id", r.agentID,
					"event_type", event.Type,
					"reasoning", decision.Reasoning,
				)
				r.resetHeartbeatTimer(heartbeatTimer)
				continue

			case ActionReplyNow:
				// Simple Q&A: create a Chat Work for immediate reply
				r.routeOrCreateWork(ctx, event)

			case ActionReplyThenWork:
				// Task request: acknowledge first, then execute
				r.handleReplyThenWork(ctx, event, agent, llmConfig)

			case ActionWorkOnly:
				// Continuation of ongoing task: route to active work or create new
				r.routeOrCreateWork(ctx, event)
			}
			r.resetHeartbeatTimer(heartbeatTimer)

		case <-heartbeatTimer.C:
			r.handleHeartbeat(ctx)
			r.resetHeartbeatTimer(heartbeatTimer)

		case doneWork := <-r.workDoneCh:
			r.activeWorks = removeWork(r.activeWorks, doneWork)
			// Only set idle if no other works are active in this session
			if !r.hasActiveWorkInSession(doneWork.sessionID) {
				r.setStatus(doneWork.sessionID, model.ParticipantStatusIdle)
			}
		}
	}
}

// routeOrCreateWork routes the event to an active Work or creates a new one.
// Routing + registration is ATOMIC within the event loop's select case:
// the next event will see the updated activeWorks.
func (r *agentRuntime) routeOrCreateWork(ctx context.Context, event eventqueue.AgentEvent) {
	target := r.workRouter.Route(ctx, event, r.activeWorks)
	if target != nil {
		target.FeedEvent(event)
	} else {
		w := r.newWork(ctx, event, model.WorkTypeChat)
		r.activeWorks = append(r.activeWorks, w)
		go w.Run(ctx)
	}
}

// handleReplyThenWork implements the ActionReplyThenWork pattern:
//  1. Generate a brief acknowledgment reply via LLM
//  2. Commit the acknowledgment as a message
//  3. Create a Task Work for the actual execution
//
// The acknowledgment is generated synchronously in the event loop to ensure
// the user sees an immediate response. The Task Work runs asynchronously.
// Message ordering is guaranteed by the serialized commitCh mechanism.
func (r *agentRuntime) handleReplyThenWork(ctx context.Context, event eventqueue.AgentEvent, agent *model.Agent, llmConfig *model.LLMConfig) {
	sessionID := event.SessionID

	// Generate acknowledgment reply via LLM
	messageContent := extractMessageContent(event)
	ackContent := r.generateAcknowledgment(ctx, messageContent, agent, llmConfig)

	// Create and commit the acknowledgment draft
	var agentLastReadID int64
	var ps model.ParticipantSession
	if err := database.DB.Where("session_id = ? AND participant_type = ? AND participant_id = ?",
		sessionID, model.ParticipantTypeAgent, r.agentID).First(&ps).Error; err == nil {
		agentLastReadID = ps.LastReadMessageID
	}

	ackDraft := &model.MessageDraft{
		AgentID:           r.agentID,
		SessionID:         sessionID,
		Status:            model.DraftStatusBuilding,
		LastReadMessageID: agentLastReadID,
	}
	if err := database.DB.Create(ackDraft).Error; err != nil {
		applogger.L.Error("Failed to create acknowledgment draft",
			"agent_id", r.agentID, "session_id", sessionID, "error", err)
		// Fall back: create task work without acknowledgment
		w := r.newWork(ctx, event, model.WorkTypeTask)
		r.activeWorks = append(r.activeWorks, w)
		go w.Run(ctx)
		return
	}

	// Commit the acknowledgment message
	r.commitCh <- commitRequest{
		draft:           ackDraft,
		sessionID:       sessionID,
		content:         ackContent,
		hasInteractions: model.HasInteractionsNone,
	}

	applogger.L.Info("Acknowledgment committed, creating task work",
		"agent_id", r.agentID,
		"session_id", sessionID,
		"ack_draft_id", ackDraft.ID,
	)

	// Create Task Work for the actual execution
	w := r.newWork(ctx, event, model.WorkTypeTask)
	r.activeWorks = append(r.activeWorks, w)
	go w.Run(ctx)
}

// generateAcknowledgment produces a brief acknowledgment message via LLM.
// Uses a short, deterministic prompt to generate a concise confirmation.
func (r *agentRuntime) generateAcknowledgment(ctx context.Context, messageContent string, agent *model.Agent, llmConfig *model.LLMConfig) string {
	// Truncate long messages for the acknowledgment prompt
	userMsg := messageContent
	if len(userMsg) > 200 {
		userMsg = userMsg[:200] + "..."
	}

	prompt := fmt.Sprintf("The person you are talking to said: \"%s\"\n\nGenerate a brief, natural acknowledgment in the same language as their message. Keep it to 1-2 short sentences. Examples: \"Got it, I'll work on that\", \"Sure, let me handle that\", \"On it!\". Do not repeat their message or add unnecessary details.", userMsg)

	chatModel := llm.NewChatModelWithTemperature(
		llmConfig.BaseURL, llmConfig.APIKey, llmConfig.ModelID, llm.TemperatureDeterministic,
	)

	result, err := chatModel.Chat(ctx, []llm.Message{
		{Role: "user", Content: prompt},
	})
	if err != nil {
		applogger.L.Error("Failed to generate acknowledgment, using default",
			"agent_id", r.agentID, "error", err)
		return "Got it, I'll work on that."
	}

	return result
}

// newWork creates a new Work from an event, persists it to the database,
// and sets the agent status to working.
func (r *agentRuntime) newWork(ctx context.Context, event eventqueue.AgentEvent, workType int) *work {
	sessionID := event.SessionID

	description := extractDescription(event)

	// Create draft for this work, snapshotting the agent's current read position
	// as the context boundary. Messages up to this ID were visible when the
	// work started, ensuring preprocessing and context assembly have the
	// correct conversation history.
	var agentLastReadID int64
	var ps model.ParticipantSession
	if err := database.DB.Where("session_id = ? AND participant_type = ? AND participant_id = ?",
		sessionID, model.ParticipantTypeAgent, r.agentID).First(&ps).Error; err == nil {
		agentLastReadID = ps.LastReadMessageID
	}

	draft := &model.MessageDraft{
		AgentID:           r.agentID,
		SessionID:         sessionID,
		Status:            model.DraftStatusBuilding,
		LastReadMessageID: agentLastReadID,
	}
	if err := database.DB.Create(draft).Error; err != nil {
		applogger.L.Error("Failed to create draft", "agent_id", r.agentID, "session_id", sessionID, "error", err)
		draft = nil
	}

	// Persist work to database
	workRecord := &model.Work{
		AgentID:     r.agentID,
		SessionID:   sessionID,
		DraftID:     nilDraftID(draft),
		Type:        workType,
		Description: description,
		Status:      model.WorkStatusRunning,
	}
	if err := database.DB.Create(workRecord).Error; err != nil {
		applogger.L.Error("Failed to create work", "agent_id", r.agentID, "session_id", sessionID, "error", err)
	}

	w := &work{
		ID:             workRecord.ID,
		agent:          r,
		sessionID:      sessionID,
		draft:          draft,
		workType:       workType,
		description:    description,
		maxIterations:  90, // Default max iterations for task works
		pendingEvents:  make(chan eventqueue.AgentEvent, 32),
		initialPayload: event.Payload,
	}

	r.setStatus(sessionID, model.ParticipantStatusWorking)
	return w
}

// handleFastPathSendMessage handles the fast path for scheduled events with
// action=send_message. It directly creates a message with the pre-computed
// content, skipping the entire LLM pipeline (no context engineering, no
// inference, no tool calls). This is the optimization for simple reminders.
//
// The method still creates a draft (for audit trail) and commits through the
// serialized commitCh to maintain message ordering. No Work object is created,
// so the agent status transitions are handled inline:
//   - working → (commit) → idle
func (r *agentRuntime) handleFastPathSendMessage(sessionID int64, payload *eventqueue.ScheduledEventPayload) {
	applogger.L.Info("Fast path: sending pre-computed message for scheduled event",
		"agent_id", r.agentID,
		"session_id", sessionID,
		"scheduled_event_id", payload.ScheduledEventID,
	)

	// Get agent's current read position
	var agentLastReadID int64
	var ps model.ParticipantSession
	if err := database.DB.Where("session_id = ? AND participant_type = ? AND participant_id = ?",
		sessionID, model.ParticipantTypeAgent, r.agentID).First(&ps).Error; err == nil {
		agentLastReadID = ps.LastReadMessageID
	}

	// Create draft for audit trail
	draft := &model.MessageDraft{
		AgentID:           r.agentID,
		SessionID:         sessionID,
		Status:            model.DraftStatusBuilding,
		LastReadMessageID: agentLastReadID,
	}
	if err := database.DB.Create(draft).Error; err != nil {
		applogger.L.Error("Failed to create draft for fast path message",
			"agent_id", r.agentID, "session_id", sessionID, "error", err)
		return
	}

	// Set status to working before committing
	r.setStatus(sessionID, model.ParticipantStatusWorking)

	// Commit the pre-computed message through the serialized channel.
	// This ensures message ordering is preserved even if a normal work
	// is committing at the same time.
	r.commitCh <- commitRequest{
		draft:           draft,
		sessionID:       sessionID,
		content:         payload.ActionContent,
		hasInteractions: model.HasInteractionsNone,
	}

	// Set status back to idle. The commitCh is buffered and handleCommits
	// processes it asynchronously, but the status transition is safe because
	// commitDraft does not modify status — it only updates last_active_at
	// and last_read_message_id. The SSE push from commitDraft will arrive
	// at the client after this status change, which is the correct order.
	r.setStatus(sessionID, model.ParticipantStatusIdle)

	applogger.L.Info("Fast path message dispatched",
		"agent_id", r.agentID,
		"session_id", sessionID,
		"draft_id", draft.ID,
		"scheduled_event_id", payload.ScheduledEventID,
	)
}

// setStatus updates the agent's ParticipantSession.Status in the database
// and fires the SSE callback if the status actually changed.
func (r *agentRuntime) setStatus(sessionID int64, status int) {
	// Read current status from DB to detect changes
	var ps model.ParticipantSession
	err := database.DB.Where(
		"session_id = ? AND participant_type = ? AND participant_id = ?",
		sessionID, model.ParticipantTypeAgent, r.agentID,
	).First(&ps).Error

	if err != nil {
		applogger.L.Error("Failed to read participant status",
			"agent_id", r.agentID, "session_id", sessionID, "error", err)
		return
	}

	if ps.Status == status {
		return // No change, skip update and callback
	}

	// Persist new status to database
	if err := database.DB.Model(&model.ParticipantSession{}).
		Where("session_id = ? AND participant_type = ? AND participant_id = ?",
			sessionID, model.ParticipantTypeAgent, r.agentID).
		Update("status", status).Error; err != nil {
		applogger.L.Error("Failed to update participant status",
			"agent_id", r.agentID, "session_id", sessionID, "error", err)
		return
	}

	// Fire SSE callback for status change
	if r.onStatusChange != nil {
		r.onStatusChange(r.agentID, sessionID, status)
	}
}

// hasActiveWorkInSession checks whether any active work exists for the
// given session. Used to determine if the agent can transition to idle
// when a work completes.
func (r *agentRuntime) hasActiveWorkInSession(sessionID int64) bool {
	for _, w := range r.activeWorks {
		if w.sessionID == sessionID {
			return true
		}
	}
	return false
}

// resetHeartbeatTimer resets the heartbeat timer with tickless adaptive intervals.
//
// Three-phase model (inspired by Linux NOHZ):
//   - Active (5min): agent just interacted, context is fresh
//   - Steady (30min): session has activity but agent doesn't participate
//   - Dormant (2h): session has been idle for a long time
//
// Events reset idleTicks to 0, naturally returning to the active phase.
// Consecutive "none" self-reflections increment idleTicks, transitioning
// through steady to dormant.
func (r *agentRuntime) resetHeartbeatTimer(timer *time.Timer) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}

	interval := r.adjustHeartbeatInterval()
	timer.Reset(interval)
}

// adjustHeartbeatInterval computes the current heartbeat interval based on
// the idle tick counter. The interval grows as the agent stays idle longer.
func (r *agentRuntime) adjustHeartbeatInterval() time.Duration {
	switch {
	case r.idleTicks == 0:
		return heartbeatActive
	case r.idleTicks <= ticksToSteady:
		return heartbeatActive
	case r.idleTicks <= ticksToDormant:
		return heartbeatSteady
	default:
		return heartbeatDormant
	}
}

// handleHeartbeat processes a heartbeat tick for agent self-reflection.
//
// The heartbeat is the agent's self-preservation mechanism, not the session's.
// It is the agent's way of asking: "Is there anything I should be doing?"
//
// Responsiveness is guaranteed by interrupts (events). Heartbeat only
// handles proactivity. Even an agent with no active sessions should
// run heartbeats (they'll just return "none").
//
// Self-reflection flow:
//  1. If agent has active works, skip (works drive themselves)
//  2. Increment heartbeat tick counter
//  3. Run scheduled heartbeat checks
//  4. Load all sessions the agent participates in
//  5. For each session, check for unread messages and proactive opportunities
//  6. Use LLM to decide whether to act (none / proactive_message)
//  7. Execute the decided action
func (r *agentRuntime) handleHeartbeat(ctx context.Context) {
	if len(r.activeWorks) > 0 {
		// Agent is busy — no self-reflection needed
		return
	}

	r.heartbeatTick++

	// Obligation check (every 3 ticks)
	if r.heartbeatTick%obligationCheckInterval == 0 {
		r.checkObligations(ctx)
	}

	// Memory density check (every 6 ticks)
	if r.heartbeatTick%memoryDensityCheckInterval == 0 {
		r.checkMemoryDensity(ctx)
	}

	// Load all sessions this agent participates in
	var sessions []model.ParticipantSession
	if err := database.DB.Where("participant_type = ? AND participant_id = ?",
		model.ParticipantTypeAgent, r.agentID).Find(&sessions).Error; err != nil {
		applogger.L.Error("heartbeat: failed to load agent sessions", "error", err)
		r.idleTicks++
		return
	}

	if len(sessions) == 0 {
		r.idleTicks++
		return
	}

	// Check each session for unread messages (last_read_message_id is advanced
	// by commitDraft, so agent's own messages won't appear as unread)
	var sessionsWithActivity []model.ParticipantSession
	for _, ps := range sessions {
		var maxMsgID int64
		if err := database.DB.Model(&model.Message{}).
			Where("session_id = ?", ps.SessionID).
			Select("COALESCE(MAX(id), 0)").
			Scan(&maxMsgID).Error; err != nil {
			applogger.L.Warn("heartbeat: failed to scan max message ID", "session_id", ps.SessionID, "error", err)
			continue
		}

		if maxMsgID > ps.LastReadMessageID {
			sessionsWithActivity = append(sessionsWithActivity, ps)
		}
	}

	// No unread messages in any session — increment idle counter
	if len(sessionsWithActivity) == 0 {
		r.idleTicks++
		applogger.L.Debug("Agent heartbeat (idle)",
			"agent_id", r.agentID,
			"idle_ticks", r.idleTicks,
		)
		return
	}

	// Use LLM self-reflection to decide what to do
	agent := service.GetAgent(r.agentID)
	llmConfig := service.GetLLMConfig(agent.LLMConfigID)
	output := r.selfReflect(ctx, agent, llmConfig, sessionsWithActivity)

	// Advance last_read_message_id for all reflected-upon sessions.
	// The self-reflection IS the act of reading — the agent has acknowledged
	// these messages exist and decided what (not) to do. Without this,
	// the same unread messages trigger infinite self-reflection loops,
	// wasting LLM calls every heartbeat tick.
	r.markSessionsAsRead(sessionsWithActivity)

	switch output.Action {
	case "proactive_message":
		r.executeProactiveMessage(ctx, output)
	default:
		r.idleTicks++
		r.ticksSinceProactive++
		applogger.L.Debug("Agent heartbeat self-reflection: none",
			"agent_id", r.agentID,
			"idle_ticks", r.idleTicks,
		)
	}
}

// markSessionsAsRead advances last_read_message_id to the current max message
// ID in each session. This prevents infinite self-reflection loops where the
// same "unread" messages repeatedly trigger heartbeat LLM calls.
//
// The self-reflection IS the act of reading — the agent has acknowledged
// these messages and decided what (not) to do. Not advancing would mean
// the agent keeps "re-reading" the same messages every heartbeat tick.
func (r *agentRuntime) markSessionsAsRead(sessions []model.ParticipantSession) {
	for _, ps := range sessions {
		var maxMsgID int64
		if err := database.DB.Model(&model.Message{}).
			Where("session_id = ?", ps.SessionID).
			Select("COALESCE(MAX(id), 0)").
			Scan(&maxMsgID).Error; err != nil {
			applogger.L.Warn("markSessionsAsRead: failed to scan max message ID", "session_id", ps.SessionID, "error", err)
			continue
		}

		if maxMsgID > ps.LastReadMessageID {
			if err := database.DB.Model(&model.ParticipantSession{}).
				Where("session_id = ? AND participant_type = ? AND participant_id = ? AND last_read_message_id < ?",
					ps.SessionID, model.ParticipantTypeAgent, r.agentID, maxMsgID).
				Update("last_read_message_id", maxMsgID).Error; err != nil {
				applogger.L.Warn("markSessionsAsRead: failed to update last_read_message_id", "session_id", ps.SessionID, "error", err)
			}

			applogger.L.Debug("Heartbeat: advanced last_read_message_id",
				"agent_id", r.agentID,
				"session_id", ps.SessionID,
				"new_last_read_id", maxMsgID,
			)
		}
	}
}

// selfReflect uses LLM to decide whether the agent should take proactive action
// based on unread messages and session context.
//
// The prompt is intentionally conservative: "only speak up if there is genuine
// incremental value." Early iterations should err on the side of silence rather
// than disturbing the user.
func (r *agentRuntime) selfReflect(ctx context.Context, agent *model.Agent, llmConfig *model.LLMConfig, sessions []model.ParticipantSession) heartbeatOutput {
	if llmConfig == nil {
		return heartbeatOutput{Action: "none"}
	}

	// Build session summaries for the self-reflection prompt
	var sessionDescs []string
	for _, ps := range sessions {
		// Get the latest unread message content (truncated)
		var lastMsg model.Message
		if err := database.DB.Where("session_id = ? AND id > ?", ps.SessionID, ps.LastReadMessageID).
			Order("id DESC").First(&lastMsg).Error; err != nil {
			continue
		}

		content := lastMsg.Content
		if len(content) > 100 {
			content = content[:100] + "..."
		}

		timeSinceActive := time.Since(ps.LastActiveAt).Round(time.Minute)
		sessionDescs = append(sessionDescs, fmt.Sprintf(
			"- Session %d: last active %s ago, latest unread: \"%s\"",
			ps.SessionID, timeSinceActive, content,
		))
	}

	if len(sessionDescs) == 0 {
		return heartbeatOutput{Action: "none"}
	}

	prompt := fmt.Sprintf(`You are an AI agent's self-reflection subsystem. Based on the following structured state, decide your action.

Agent role: %s

Sessions with unread messages:
%s

Decision rules:
- "none": No action needed. Default choice.
- "proactive_message": You have genuinely valuable NEW information the person doesn't know yet (e.g., a long task completed, new findings, important updates). NOT greetings, NOT confirmation of existence.

Three thresholds for proactive_message:
1. INCREMENTAL VALUE: Can you provide information the person doesn't already have and would find useful?
2. TIMING: Based on last_active_at, is the person likely awake and receptive?
3. FREQUENCY: Don't message if you've spoken recently without being prompted.

IMPORTANT: Err on the side of silence. Only choose proactive_message if you would regret NOT informing them.`,
		agent.Description, strings.Join(sessionDescs, "\n"))

	chatModel := llm.NewChatModelWithTemperature(
		llmConfig.BaseURL, llmConfig.APIKey, llmConfig.ModelID, llm.TemperatureDeterministic,
	)

	result, err := chatModel.ChatWithJSONSchema(ctx, []llm.Message{
		{Role: "user", Content: prompt},
	}, llm.JSONSchemaDefinition{
		Name:        "HeartbeatReflection",
		Description: "Agent's structured self-reflection decision on whether to take proactive action",
		Strict:      true,
		Schema: jsonschema.Definition{
			Type: jsonschema.Object,
			Properties: map[string]jsonschema.Definition{
				"action": {
					Type: jsonschema.String,
					Enum: []string{"none", "proactive_message"},
					Description: "The action to take: " +
						"'none' for no action, " +
						"'proactive_message' if you have genuinely valuable new information",
				},
				"reason": {
					Type:        jsonschema.String,
					Description: "Brief explanation of why this action was chosen, referencing the three thresholds if applicable",
				},
				"target_session_id": {
					Type:        jsonschema.Number,
					Description: "If proactive_message, the session ID to send the message to. Omit for other actions.",
				},
				"message_draft": {
					Type:        jsonschema.String,
					Description: "If proactive_message, the draft message content. Omit for other actions.",
				},
			},
			Required: []string{"action", "reason"},
		},
	})

	if err != nil {
		applogger.L.Error("Heartbeat self-reflection LLM call failed",
			"agent_id", r.agentID, "error", err)
		return heartbeatOutput{Action: "none"}
	}

	var output heartbeatOutput
	if err := json.Unmarshal([]byte(result), &output); err != nil {
		applogger.L.Error("Heartbeat self-reflection output parse failed",
			"agent_id", r.agentID, "error", err)
		return heartbeatOutput{Action: "none"}
	}

	applogger.L.Info("Heartbeat self-reflection result",
		"agent_id", r.agentID,
		"action", output.Action,
		"reason", output.Reason,
		"target_session_id", output.TargetSessionID,
	)

	return output
}

// executeProactiveMessage sends a proactive message generated by the heartbeat
// self-reflection to the target session. It enforces the frequency gate before
// sending: the agent must not have sent a proactive message within the last
// 12 hours or within the last 6 heartbeat ticks.
func (r *agentRuntime) executeProactiveMessage(ctx context.Context, output heartbeatOutput) {
	// Frequency gate: check if too soon since last proactive message
	if !r.canSendProactiveMessage() {
		applogger.L.Info("Proactive message suppressed by frequency gate",
			"agent_id", r.agentID,
			"session_id", output.TargetSessionID,
			"ticks_since_last", r.ticksSinceProactive,
		)
		return
	}

	// Validate the target session exists and the agent is a participant
	if output.TargetSessionID <= 0 {
		applogger.L.Error("Proactive message has no target session",
			"agent_id", r.agentID)
		return
	}

	if strings.TrimSpace(output.MessageDraft) == "" {
		applogger.L.Warn("Proactive message draft is empty",
			"agent_id", r.agentID, "session_id", output.TargetSessionID)
		return
	}

	// Create the message directly (no draft, since this is spontaneous)
	msg := model.Message{
		SessionID:       output.TargetSessionID,
		Role:            model.MessageRoleAssistant,
		Content:         output.MessageDraft,
		Status:          model.MessageStatusCompleted,
		HasInteractions: model.HasInteractionsNone,
	}
	if err := database.DB.Create(&msg).Error; err != nil {
		applogger.L.Error("Failed to create proactive message",
			"agent_id", r.agentID,
			"session_id", output.TargetSessionID,
			"error", err,
		)
		return
	}

	// Update agent's last_active_at and last_read_message_id
	if err := database.DB.Model(&model.ParticipantSession{}).
		Where("session_id = ? AND participant_type = ? AND participant_id = ?",
			output.TargetSessionID, model.ParticipantTypeAgent, r.agentID).
		Updates(map[string]interface{}{
			"last_active_at":       time.Now(),
			"last_read_message_id": msg.ID,
		}).Error; err != nil {
		applogger.L.Warn("failed to update participant session after proactive send", "error", err)
	}

	// Update frequency tracking
	r.lastProactiveSent = time.Now()
	r.ticksSinceProactive = 0
	r.idleTicks = 0

	// Submit to the event vectorization service for embedding + observation.
	memory.SubmitVectorization(memory.VectorizationTask{
		MessageID: msg.ID,
		SessionID: msg.SessionID,
		Content:   msg.Content,
	})

	// Push message event to SSE clients
	pushMessageEvent(output.TargetSessionID, msg.ID, msg.Content, msg.HasInteractions)

	// Trigger summary generation if needed
	chatcontext.MaybeTriggerSummary(ctx, output.TargetSessionID, r.agentID)

	applogger.L.Info("Proactive message sent",
		"agent_id", r.agentID,
		"session_id", output.TargetSessionID,
		"message_id", msg.ID,
		"reason", output.Reason,
	)
}

// canSendProactiveMessage checks whether the frequency gate allows a
// proactive message. Returns true if both the time-based and tick-based
// thresholds are satisfied.
func (r *agentRuntime) canSendProactiveMessage() bool {
	if !r.lastProactiveSent.IsZero() {
		if time.Since(r.lastProactiveSent).Hours() < proactiveMinIntervalHours {
			return false
		}
	}
	if r.ticksSinceProactive < proactiveMinHeartbeats {
		return false
	}
	return true
}

// handleCommits processes draft commit requests from the commitCh.
// Runs in a separate goroutine to serialize message writes.
func (r *agentRuntime) handleCommits(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case req := <-r.commitCh:
			r.commitDraft(ctx, req)
		}
	}
}

// commitDraft atomically commits a draft to the messages table.
// This is the only path through which agent messages enter the messages table.
func (r *agentRuntime) commitDraft(ctx context.Context, req commitRequest) {
	draft := req.draft
	if draft == nil {
		applogger.L.Error("commitDraft called with nil draft")
		return
	}

	// Create the message from the draft content
	msg := model.Message{
		SessionID:       draft.SessionID,
		Role:            model.MessageRoleAssistant,
		Content:         req.content,
		Status:          model.MessageStatusCompleted,
		HasInteractions: req.hasInteractions,
		DraftID:         &draft.ID,
	}
	if err := database.DB.Create(&msg).Error; err != nil {
		applogger.L.Error("Failed to commit draft to messages",
			"draft_id", draft.ID,
			"session_id", draft.SessionID,
			"error", err,
		)
		return
	}

	// Update draft status and content
	if err := database.DB.Model(&model.MessageDraft{}).Where("id = ?", draft.ID).Updates(map[string]interface{}{
		"status":  model.DraftStatusCommitted,
		"content": req.content,
	}).Error; err != nil {
		applogger.L.Error("commitDraft: failed to update draft", "draft_id", draft.ID, "error", err)
		return
	}

	// Update agent's last_active_at and last_read_message_id in the participant session.
	// The agent has "read" everything up to and including its own message,
	// since it produced it based on all prior context.
	if err := database.DB.Model(&model.ParticipantSession{}).
		Where("session_id = ? AND participant_type = ? AND participant_id = ? AND last_read_message_id < ?",
			draft.SessionID, model.ParticipantTypeAgent, r.agentID, msg.ID).
		Updates(map[string]interface{}{
			"last_active_at":       time.Now(),
			"last_read_message_id": msg.ID,
		}).Error; err != nil {
		applogger.L.Warn("commitDraft: failed to update participant session", "draft_id", draft.ID, "error", err)
	}

	applogger.L.Info("Draft committed to messages",
		"draft_id", draft.ID,
		"message_id", msg.ID,
		"session_id", draft.SessionID,
	)

	// Submit to the event vectorization service for embedding + observation.
	memory.SubmitVectorization(memory.VectorizationTask{
		MessageID: msg.ID,
		SessionID: msg.SessionID,
		Content:   msg.Content,
	})

	// Push message event to SSE clients
	pushMessageEvent(draft.SessionID, msg.ID, msg.Content, msg.HasInteractions)

	// Trigger summary generation if needed (sender-agnostic, based on message count)
	chatcontext.MaybeTriggerSummary(ctx, draft.SessionID, r.agentID)
}

// GetActiveWorkCount returns the number of currently active works.
// Used for monitoring and debugging.
func (r *agentRuntime) GetActiveWorkCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.activeWorks)
}

// removeWork removes a work from the active works slice.
func removeWork(works []*work, target *work) []*work {
	for i, w := range works {
		if w == target {
			return append(works[:i], works[i+1:]...)
		}
	}
	return works
}

// nilDraftID returns nil if draft is nil, otherwise returns a pointer to draft.ID.
func nilDraftID(draft *model.MessageDraft) *int64 {
	if draft == nil {
		return nil
	}
	return &draft.ID
}

// extractDescription extracts a short description from an event for work routing.
func extractDescription(event eventqueue.AgentEvent) string {
	if event.Payload == nil {
		return "Process event"
	}
	// For new message events, use the message content as description
	type messagePayload interface{ GetMessageContent() string }
	if mp, ok := event.Payload.(messagePayload); ok {
		content := mp.GetMessageContent()
		if len(content) > 200 {
			return content[:200]
		}
		return content
	}
	return "Process event"
}

// pushMessageEvent pushes a message event to SSE clients.
// This is a package-level function that will be connected to the
// handler's ConnectionManager during integration.
var pushMessageEvent = func(sessionID, messageID int64, content string, hasInteractions int) {
	// Default no-op; will be overridden during integration
	applogger.L.Debug("pushMessageEvent called (not integrated)",
		"session_id", sessionID,
		"message_id", messageID,
	)
}

// pushSSEEvent pushes a raw SSE event to all clients of a session.
// Used for notifications and other non-message events.
var pushSSEEvent = func(sessionID int64, data string) {
	// Default no-op; will be overridden during integration
	applogger.L.Debug("pushSSEEvent called (not integrated)",
		"session_id", sessionID,
	)
}

// userFriendlyErrorMsg is the default error message shown to users when
// server-side processing fails.
const userFriendlyErrorMsg = "Sorry, something went wrong on the server. Please try again later."

// recoverActiveWorks loads active works from the database for agent recovery
// after a service restart. Abandoned works are marked and no new Work objects
// are returned since mid-execution resumption is not supported.
func recoverActiveWorks(agentID int64, runtime *agentRuntime) []*work {
	var workRecords []model.Work
	if err := database.DB.Where("agent_id = ? AND status = ?", agentID, model.WorkStatusRunning).Find(&workRecords).Error; err != nil {
		applogger.L.Error("recoverActiveWorks: failed to load work records", "agent_id", agentID, "error", err)
		return nil
	}

	for _, wr := range workRecords {
		// Mark recovered works as abandoned since we can't resume mid-execution
		if err := database.DB.Model(&model.Work{}).Where("id = ?", wr.ID).
			Update("status", model.WorkStatusAbandoned).Error; err != nil {
			applogger.L.Error("recoverActiveWorks: failed to mark work as abandoned", "work_id", wr.ID, "error", err)
		}

		if wr.DraftID != nil {
			if err := database.DB.Model(&model.MessageDraft{}).Where("id = ?", *wr.DraftID).
				Update("status", model.DraftStatusDiscarded).Error; err != nil {
				applogger.L.Error("recoverActiveWorks: failed to discard draft", "draft_id", *wr.DraftID, "error", err)
			}
		}

		// Reset participant status to idle so the frontend doesn't show stuck "responding"
		if err := database.DB.Model(&model.ParticipantSession{}).
			Where("session_id = ? AND participant_type = ? AND participant_id = ?",
				wr.SessionID, model.ParticipantTypeAgent, agentID).
			Update("status", model.ParticipantStatusIdle).Error; err != nil {
			applogger.L.Error("recoverActiveWorks: failed to reset participant status",
				"session_id", wr.SessionID, "agent_id", agentID, "error", err)
		}

		applogger.L.Info("Recovered work marked as abandoned",
			"work_id", wr.ID,
			"agent_id", agentID,
			"session_id", wr.SessionID,
		)
	}

	return nil
}

// createAgentRuntime creates and initializes an agentRuntime struct without starting
// the event loop. Loads the agent's LLM config, subscribes to the event queue,
// and recovers abandoned works from a previous run.
func createAgentRuntime(agentID int64, onStatusChange func(agentID, sessionID int64, status int)) *agentRuntime {
	agent := service.GetAgent(agentID)
	var llmConfig *model.LLMConfig
	if agent != nil {
		llmConfig = service.GetLLMConfig(agent.LLMConfigID)
	}

	router := newSemanticWorkRouter(llmConfig)
	eventCh := eventqueue.Subscribe(agentID)

	runtime := newAgentRuntime(agentID, router, eventCh, 30*time.Second, onStatusChange)

	// Recover any abandoned works from previous run
	recoverActiveWorks(agentID, runtime)

	return runtime
}
