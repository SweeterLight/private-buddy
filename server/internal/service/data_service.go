package service

import (
	"private-buddy-server/internal/database"
	"private-buddy-server/internal/model"

	applogger "private-buddy-server/internal/logger"
)

// GetSession retrieves a session by ID. Returns nil if not found.
func GetSession(sessionID int64) *model.Session {
	var session model.Session
	if err := database.DB.First(&session, sessionID).Error; err != nil {
		applogger.L.Error("Session not found", "session_id", sessionID, "error", err)
		return nil
	}
	return &session
}

// GetAgent retrieves an agent by ID. Returns nil if not found.
func GetAgent(agentID int64) *model.Agent {
	var agent model.Agent
	if err := database.DB.First(&agent, agentID).Error; err != nil {
		applogger.L.Error("Agent not found", "agent_id", agentID, "error", err)
		return nil
	}
	return &agent
}

// GetLLMConfig retrieves an LLM config by ID. Returns nil if not found.
func GetLLMConfig(llmConfigID int64) *model.LLMConfig {
	var config model.LLMConfig
	if err := database.DB.First(&config, llmConfigID).Error; err != nil {
		applogger.L.Error("LLM config not found", "llm_config_id", llmConfigID, "error", err)
		return nil
	}
	return &config
}

// GetSearchConfig retrieves the search configuration. Creates a default if not found.
func GetSearchConfig() *model.SearchConfig {
	var config model.SearchConfig
	if err := database.DB.Where("id = ?", 1).First(&config).Error; err != nil {
		applogger.L.Warn("SearchConfig not found, creating default")
		config = model.SearchConfig{
			Provider:    "tavily",
			APIKey:      "",
			Description: "",
			IsActive:    false,
		}
		database.DB.Create(&config)
	}
	return &config
}

// UpdateSearchConfig updates the search configuration with non-nil fields.
func UpdateSearchConfig(provider, apiKey, description *string, isActive *bool) *model.SearchConfig {
	config := GetSearchConfig()

	updates := make(map[string]interface{})
	if provider != nil {
		updates["provider"] = *provider
	}
	if apiKey != nil {
		updates["api_key"] = *apiKey
	}
	if description != nil {
		updates["description"] = *description
	}
	if isActive != nil {
		updates["is_active"] = *isActive
	}

	if len(updates) > 0 {
		database.DB.Model(config).Updates(updates)
		database.DB.First(config, 1)
	}

	applogger.L.Info("SearchConfig updated",
		"provider", config.Provider,
		"is_active", config.IsActive,
		"has_api_key", config.APIKey != "",
	)
	return config
}
