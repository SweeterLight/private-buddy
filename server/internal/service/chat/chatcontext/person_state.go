package chatcontext

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sashabaranov/go-openai/jsonschema"

	"private-buddy-server/internal/model"
	"private-buddy-server/internal/service/llm"

	applogger "private-buddy-server/internal/logger"
)

// personStateInferencePrompt is the LLM prompt template for inferring the current state of the person you are talking to.
// It takes three parameters: agent_name, character_settings, recent_messages (formatted dialog text).
// The role context ensures the LLM correctly interprets the conversation as role-playing
// rather than treating casual questions (e.g., "Are you asleep?") as needing real-time information.
const personStateInferencePrompt = `You are %s, %s. You are inferring the current state of the person you are talking to.

Based on the following recent conversation, infer the current state of the person you are talking to.

Recent conversation:
%s

Analyze their emotional tone, conversational purpose, and any clues about their physical situation.

Also determine if their request requires interaction with the external world:
- Set needs_world_interaction=true if the request needs: real-time information (news, weather, stock prices), 
  file operations (create, modify, delete files), code execution, web searches, or any tool usage.
- Set needs_world_interaction=false if the LLM can answer directly from its training data 
  (general knowledge, advice, explanations, casual conversation).

IMPORTANT: You are the person named above(%s). Questions directed at you (e.g., "Are you asleep?", "How are you?", "What do you think?") are casual chat and do NOT require world interaction. Only set needs_world_interaction=true when the person explicitly asks you to perform actions that require tools or external data.`

// PersonState represents the inferred person state from conversation context.
//
// Three-dimensional model:
//   - Emotion: user's current emotional state (affects response tone)
//   - Purpose: user's current conversational goal (affects response content direction)
//   - Situation: user's physical context (affects response constraints)
//
// Intent type is implicitly derived from purpose + situation, not modeled separately.
//
// Field descriptions serve dual purpose:
//  1. Guide LLM structured output generation
//  2. Provide natural language fragments for prompt template assembly
type PersonState struct {
	Emotion               string `json:"emotion"`
	Purpose               string `json:"purpose"`
	Situation             string `json:"situation"`
	NeedsWorldInteraction bool   `json:"needs_world_interaction"`
}

// emotionDescriptions maps emotion codes to natural language descriptions.
var emotionDescriptions = map[string]string{
	"calm":       "calm and relaxed",
	"anxious":    "anxious or worried",
	"frustrated": "frustrated or impatient",
	"urgent":     "under time pressure or in urgency",
	"curious":    "curious and exploratory",
}

// purposeDescriptions maps purpose codes to natural language descriptions.
var purposeDescriptions = map[string]string{
	"seek_help":         "seeking help with a problem",
	"seek_advice":       "looking for advice or recommendations",
	"seek_confirmation": "seeking confirmation or validation",
	"express_feeling":   "expressing feelings without expecting solutions",
	"casual_chat":       "engaging in casual conversation",
}

// ToNaturalLanguage converts the structured person state into a natural language description
// suitable for injection into the prompt's instruction area.
// personName is the actual name of the person (empty = no profile set).
func (ps *PersonState) ToNaturalLanguage(personName string) string {
	emotionDesc := ps.Emotion
	if desc, ok := emotionDescriptions[ps.Emotion]; ok {
		emotionDesc = desc
	}
	purposeDesc := ps.Purpose
	if desc, ok := purposeDescriptions[ps.Purpose]; ok {
		purposeDesc = desc
	}

	subject := personName

	parts := []string{
		fmt.Sprintf("%s appears %s", subject, emotionDesc),
		fmt.Sprintf("is %s", purposeDesc),
	}
	if ps.Situation != "" && ps.Situation != "unknown" {
		parts = append(parts, fmt.Sprintf("and is likely %s", ps.Situation))
	}
	if ps.NeedsWorldInteraction {
		parts = append(parts, "and needs to interact with the external world (tools, real-time data, or file operations)")
	}

	return strings.Join(parts, ", ") + "."
}

// formatRecentMessages formats recent messages into text for the inference prompt.
// userName is the actual name of the other party, agentName is the agent's own name.
func formatRecentMessages(recentMessages []model.Message, userName, agentName string) string {
	userRole := userName
	var lines []string
	for _, msg := range recentMessages {
		role := userRole
		if msg.Role != model.MessageRoleUser {
			role = agentName
		}
		lines = append(lines, fmt.Sprintf("%s: %s", role, msg.Content))
	}
	return strings.Join(lines, "\n")
}

// InferPersonState infers the user's current state from recent conversation messages.
// Uses TemperatureDeterministic for consistent, deterministic outputs.
// userName is the actual name of the person being talked to, agentName is the agent's own name.
// characterSettings provides the agent's role context to prevent misinterpretation of casual questions.
// Returns nil if inference fails, allowing the chat flow to continue without person state.
func InferPersonState(
	ctx context.Context,
	llmConfig *model.LLMConfig,
	recentMessages []model.Message,
	userName string,
	agentName string,
	characterSettings string,
) *PersonState {
	if len(recentMessages) == 0 {
		return nil
	}

	chatModel := llm.NewChatModelWithTemperature(llmConfig.BaseURL, llmConfig.APIKey, llmConfig.ModelID, llm.TemperatureDeterministic)

	dialogText := formatRecentMessages(recentMessages, userName, agentName)
	prompt := fmt.Sprintf(personStateInferencePrompt, agentName, characterSettings, dialogText, agentName)

	result, err := chatModel.ChatWithJSONSchema(ctx, []llm.Message{
		{Role: "user", Content: prompt},
	}, llm.JSONSchemaDefinition{
		Name:        "PersonState",
		Description: "Infer the person's current state from conversation context",
		Strict:      true,
		Schema: jsonschema.Definition{
			Type: jsonschema.Object,
			Properties: map[string]jsonschema.Definition{
				"emotion": {
					Type: jsonschema.String,
					Enum: []string{"calm", "anxious", "frustrated", "urgent", "curious"},
					Description: "The person's current emotional state: " +
						"'calm' for relaxed or neutral, " +
						"'anxious' for worried or uneasy, " +
						"'frustrated' for annoyed or impatient (e.g. repeated failed attempts), " +
						"'urgent' for time-pressured or emergency, " +
						"'curious' for inquisitive or exploratory",
				},
				"purpose": {
					Type: jsonschema.String,
					Enum: []string{"seek_help", "seek_advice", "seek_confirmation", "express_feeling", "casual_chat"},
					Description: "The person's current conversational goal: " +
						"'seek_help' for needing a solution or fix, " +
						"'seek_advice' for wanting recommendations or guidance, " +
						"'seek_confirmation' for validating a decision or understanding, " +
						"'express_feeling' for sharing emotions without expecting solutions, " +
						"'casual_chat' for social or non-goal-oriented conversation",
				},
				"situation": {
					Type:        jsonschema.String,
					Description: "Brief natural language description of the person's physical context if inferable from the conversation, such as time of day, device, environment, or activity. Use 'unknown' if not inferable. Examples: 'at work on desktop', 'late evening on mobile', 'in a meeting', 'commuting'",
				},
				"needs_world_interaction": {
					Type:        jsonschema.Boolean,
					Description: "Whether the person's request requires interaction with the external world: true if the request needs tools, real-time information, file operations, or any action beyond the LLM's parametric knowledge; false if the LLM can answer directly from its training data",
				},
			},
			Required: []string{"emotion", "purpose", "situation", "needs_world_interaction"},
		},
	})

	if err != nil {
		applogger.L.Error("Failed to infer person state", "error", err)
		return nil
	}

	if result != "" {
		var state PersonState
		if err := json.Unmarshal([]byte(result), &state); err == nil {
			applogger.L.Info("Inferred person state",
				"emotion", state.Emotion,
				"purpose", state.Purpose,
				"situation", state.Situation,
				"needs_world_interaction", state.NeedsWorldInteraction,
			)
			return &state
		}
	}

	return nil
}
