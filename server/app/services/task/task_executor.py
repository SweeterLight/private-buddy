"""
Task executor module - the main entry point for task execution.

This module provides the single public interface for the task system:
    executor = TaskExecutor(db)
    result = await executor.execute(task_requirement, llm_config, ...) -> TaskResult

Design principles:
- Input: task requirement (structured, not raw user message)
- Output: final result (success result or failure with reason)
- Internal isolation: all process info is hidden from the outside
- No pollution of the chat system

The task executor is self-contained and autonomous. It creates its own
Task Loop, LLM client, tools, and context manager for each execution.
Nothing from the internal execution leaks into the chat context.

0.0.5 changes:
- Workspace structure: .meta/ (task.md, notes.md) + output/ (LLM working dir)
- Append-only structured notes.md via write_notes tool
- Context information merged into system prompt
- Task and notes content always provided in context
"""

from pathlib import Path
from typing import List, Optional

from sqlalchemy.orm import Session as DBSession

from app.services.dto.task_result import TaskResult
from app.models.llm_config import LLMConfig
from app.services.task.task_loop import TaskLoop
from app.services.task.llm_client import TaskLLMClient
from app.services.task.context.manager import ContextManager
from app.services.task.tools.base import Tool
from app.services.task.tools.bash import BashTool
from app.services.task.tools.web_search import WebSearchTool
from app.services.task.tools.write_notes import WriteNotesTool
from app.services.task.workspace import (
    get_output_dir,
    init_session_workspace,
    read_task_md,
    read_notes_md,
)
from app.config import get_settings
from app.logger import logger


class TaskExecutor:
    """
    Self-contained task execution service.

    This is the only public interface of the task module.
    It accepts a task requirement and an LLM configuration,
    runs the task loop internally, and returns a TaskResult.

    Usage:
        executor = TaskExecutor(db)
        result = await executor.execute(
            task_requirement="Find the latest Python release version",
            llm_config=llm_config,
            session_id=1,
        )
        # result.status == "success" or "failure"
        # result.result or result.reason
    """

    def __init__(self, db: DBSession):
        """
        Initialize TaskExecutor with database session.

        Args:
            db: Database session for writing interaction records and loading configs.
        """
        self._db = db

    async def execute(
        self,
        task_requirement: str,
        llm_config: LLMConfig,
        max_iterations: Optional[int] = None,
        workspace: Optional[Path] = None,
        delivery_type: Optional[str] = None,
        session_id: Optional[int] = None,
        user_msg_id: Optional[int] = None,
        agent_msg_id: Optional[int] = None,
    ) -> TaskResult:
        """
        Execute a task and return the result.

        This method is the single entry point for task execution.
        It creates all necessary components internally and runs
        the task loop to completion.

        Args:
            task_requirement: The rewritten task description to execute.
            llm_config: LLM configuration for the task.
            max_iterations: Override for max loop iterations.
            workspace: Ignored (workspace is session-based).
            delivery_type: Expected delivery type ("text" or "file").
                          Affects the system prompt to guide the task.
            session_id: Session ID for interaction records and workspace.
            user_msg_id: User message ID that triggered execution.
            agent_msg_id: Agent message ID for the result target.

        Returns:
            TaskResult with status, result (on success), reason (on failure),
            and notes (for generating user-friendly response on failure).
        """
        settings = get_settings()
        effective_max_iterations = max_iterations or settings.task_max_iterations

        logger.info(
            f"TaskExecutor.execute: task_len={len(task_requirement)}, "
            f"model={llm_config.model_id}, "
            f"max_iterations={effective_max_iterations}, "
            f"delivery_type={delivery_type}, "
            f"session_id={session_id}, agent_msg_id={agent_msg_id}"
        )

        try:
            # 1. Initialize workspace structure
            if not session_id:
                return TaskResult(
                    status="failure",
                    reason="session_id is required for task execution",
                )

            init_session_workspace(session_id, task_requirement)

            # 2. Get working directory (output/ is the LLM's workspace)
            working_dir = get_output_dir(session_id)

            # 3. Read workspace files (always provided in context)
            task_content = read_task_md(session_id)
            notes_content = read_notes_md(session_id)

            # 4. Get iteration window
            iteration_window = settings.context_window_iterations

            # 5. Build system prompt (static, basic rules only)
            has_web_search = self._has_web_search(db=self._db)
            effective_system_prompt = self._build_system_prompt(
                working_dir=working_dir,
                delivery_type=delivery_type,
                has_web_search=has_web_search,
            )

            # 6. Create tools
            tools = self._create_tools(
                session_id=session_id,
                working_dir=working_dir,
            )

            # 7. Create ContextManager with window
            context = ContextManager(
                system_prompt=effective_system_prompt,
                iteration_window=iteration_window,
                task_content=task_content,
                notes_content=notes_content,
            )

            # 8. Create LLM client
            tool_schemas = [t.schema for t in tools]
            llm_client = TaskLLMClient(
                llm_config=llm_config,
                tool_schemas=tool_schemas,
            )

            try:
                # 9. Run task loop
                task_loop = TaskLoop(
                    llm_client=llm_client,
                    llm_config=llm_config,
                    tools=tools,
                    context_manager=context,
                    max_iterations=effective_max_iterations,
                    db=self._db,
                    session_id=session_id,
                    user_msg_id=user_msg_id,
                    agent_msg_id=agent_msg_id,
                )

                loop_result = await task_loop.run()

                # Read final notes for result (useful for failure response generation)
                final_notes = read_notes_md(session_id)

                return TaskResult(
                    status=loop_result["status"],
                    result=loop_result.get("result"),
                    reason=loop_result.get("reason"),
                    notes=final_notes,
                )
            finally:
                await llm_client.close()

        except Exception as e:
            logger.error(
                f"TaskExecutor.execute unexpected error: {str(e)}",
                exc_info=True,
            )
            return TaskResult(
                status="failure",
                reason=f"Unexpected error during task execution: {str(e)}",
            )

    def _has_web_search(self, db: DBSession) -> bool:
        """
        Check if web search tool is available.

        Args:
            db: Database session for loading search config.

        Returns:
            True if web search is configured and available.
        """
        from app.services.search import SearchService
        search_config = SearchService.get_config(db)
        return bool(search_config and search_config.is_available())

    def _create_tools(
        self,
        session_id: int,
        working_dir: Path,
    ) -> List[Tool]:
        """
        Create the tool set for the task.

        Args:
            session_id: Session ID for workspace operations.
            working_dir: The output/ directory as LLM's working directory.

        Returns:
            List of Tool instances.
        """
        tools: List[Tool] = [
            BashTool(workspace=working_dir),
            WriteNotesTool(session_id=session_id),
        ]

        from app.services.search import SearchService
        search_config = SearchService.get_config(self._db)
        if search_config and search_config.is_available():
            tools.append(WebSearchTool(search_config=search_config))
            logger.info("WebSearchTool added to task tools")
        else:
            logger.info("WebSearchTool not available (not configured or disabled)")

        return tools

    @staticmethod
    def _build_system_prompt(
        working_dir: Path,
        delivery_type: Optional[str] = None,
        has_web_search: bool = True,
    ) -> str:
        """
        Build a system prompt based on working directory and delivery type.

        System prompt contains only static basic rules. Dynamic info
        (memory boundary, notes.md length, workspace structure) is
        injected via the context prompt by ContextManager.

        Args:
            working_dir: The task's output/ working directory.
            delivery_type: Expected delivery type ("text" or "file").
            has_web_search: Whether web_search tool is available.

        Returns:
            A system prompt string tailored to the task configuration.
        """
        parts = [
            "You are a helpful AI agent that can execute tasks using tools.",
            "",
            "Available tools:",
            "- bash: Execute shell commands in your working directory",
            "- write_notes: Append structured entries to your notes.md",
        ]

        if has_web_search:
            parts.append("- web_search: Search the web for information")

        parts.extend([
            "",
            "CRITICAL: Before calling any tool, you MUST first explain your reasoning",
            "in the content field. Describe what you plan to do and why.",
            "Only after explaining your thought process, make the tool call.",
            "",
            "Always verify your actions by checking the results.",
            "",
            f"Your working directory is: {working_dir}",
            "All files you create MUST be within this directory.",
            "Do not write files to any other location.",
        ])

        if delivery_type == "file":
            parts.extend([
                "",
                "DELIVERY TYPE: file",
                "The user expects file deliverables (code, documents, etc.).",
                "Create the required files in your working directory.",
                "When finished, list all created files and provide a summary.",
            ])
        elif delivery_type == "text":
            parts.extend([
                "",
                "DELIVERY TYPE: text",
                "The user expects a text answer as the deliverable.",
                "Provide a clear, concise text response.",
                "You may use tools to gather information, but the final",
                "output should be a direct text answer.",
            ])

        parts.extend([
            "",
            "When the task is complete, provide a clear and concise summary of what was accomplished.",
            "If the task cannot be completed, explain why and what was attempted.",
        ])

        return "\n".join(parts)
