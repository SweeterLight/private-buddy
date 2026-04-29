# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).


## [0.0.5] - 2026-04-30

### Added
- **Finite Working Memory**: iteration window for context visibility, older iterations discarded directly
- **Reader-Oriented Notes**: structured append-only notes.md via write_notes tool, bridging LLM statelessness
- **Forced Checkpoint**: mandatory notes write when distance from last write reaches window boundary
- **Workspace Structure**: `.meta/` (task.md + notes.md) + `output/` two-channel isolation
- **Task Requirement Rewriting**: ambiguous user messages rewritten into clear, self-contained task descriptions

### Changed
- Refactored `agent/` to `task/` for naming consistency with task execution semantics
- Moved chat context logic under `chat/` and created shared DTO module to eliminate circular imports


## [0.0.4] - 2026-04-26

### Added
- **Agent Execution System**: ReAct pattern with minimal tool set (Bash + Web Search)
- **Interaction Boundary**: separate storage for agent-world interactions, isolated from user conversation
- **Search Engine Integration**: configurable Tavily/DuckDuckGo with automatic tool availability detection


## [0.0.3] - 2026-04-24

### Added
- **Narrative Optimization**: internal focalization for background story, cohesive section transitions
- **User State Inference**: three-dimensional model (emotion, purpose, situation) for response strategy guidance


## [0.0.2] - 2026-04-22

### Added
- Context engineering: automatic conversation summary and background narrative generation
- Smart query preprocessing: query classification, rewriting, and clarification
- Character settings: customizable agent personality and style
- RAG integration: retrieve relevant historical context for better responses

### Changed
- Improved context assembly with decoupled summary and recent messages
- Optimized LLM prompts for better multilingual support


## [0.0.1] - 2026-04-17

### Added
- Basic chat functionality with AI agents
- Agent and LLM configuration management
- Session and message history
- SSE streaming for chat responses
- Multi-language support (English and Chinese)
