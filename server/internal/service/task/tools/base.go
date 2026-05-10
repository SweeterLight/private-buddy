// Package tools provides the tool abstractions and implementations for the task agent system.
//
// All tools must implement the Tool interface, which requires a unique name,
// a function definition schema, and an execute method.
// Tools are registered by name and provide their schema for LLM tool calling.
//
// Available tools:
//   - BashTool: Execute shell commands within a workspace
//   - WriteNotesTool: Append structured entries to agent's notes
//   - WebSearchTool: Search the web for information (Tavily provider)
package tools

import "private-buddy-server/internal/service/llm"

// Tool is the interface that all agent tools must implement.
// Each tool has a unique name, a function definition schema,
// and an execute method that performs the actual work.
type Tool interface {
	// Name returns the unique identifier for this tool.
	Name() string
	// Schema returns the function definition schema for this tool.
	Schema() llm.FunctionDefinition
	// Execute runs the tool with the given arguments and returns the result as a string.
	Execute(args map[string]interface{}) (string, error)
}
