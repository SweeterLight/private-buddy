"""
Task Loop implementation for the task execution system.

Implements the ReAct (Thought -> Action -> Observation) pattern:
1. LLM receives the current context and decides what to do
2. If tool_calls: execute tools and feed results back
3. If stop: return the final content as result
4. Repeat until stop or max_iterations reached

The loop records every iteration to the interactions table
for observability and frontend display.

0.0.5 changes:
- Agent can voluntarily call write_notes tool
- Checkpoint only triggered when distance from last voluntary write >= window - 1
- This avoids redundant forced writes when agent is already writing notes
"""

import json
from typing import Dict, List, Optional

from sqlalchemy.orm import Session as DBSession

from app.models.interaction import Interaction, INTERACTION_TYPE_REQUEST, INTERACTION_TYPE_RESPONSE
from app.models.llm_config import LLMConfig
from app.services.task.llm_client import TaskLLMClient
from app.services.task.context.manager import ContextManager
from app.services.task.tools.base import Tool
from app.services.task.tools.write_notes import WriteNotesTool
from app.services.task.workspace import trim_notes_md, read_notes_md
from app.logger import logger


DEFAULT_MAX_ITERATIONS = 90


class TaskLoop:
    """
    ReAct-style task loop for autonomous task execution.

    The loop iterates:
    - Call LLM with current context (window-controlled by ContextManager)
    - If LLM returns tool_calls: execute tools, append results, continue
    - If LLM returns stop: deliver the content
    - If max_iterations reached: deliver failure with reason

    Every iteration is recorded to the interactions table with:
    - type=1 (request): the messages sent to the LLM
    - type=2 (response): the LLM output (content, tool_calls, finish_reason)

    Notes checkpoint strategy:
    - Agent can voluntarily call write_notes at any time
    - Forced checkpoint only when distance from last voluntary write >= window - 1
    - This respects agent's autonomy while ensuring memory persistence
    - Final iteration always writes notes if task not completed
    """

    def __init__(
        self,
        llm_client: TaskLLMClient,
        llm_config: LLMConfig,
        tools: List[Tool],
        context_manager: ContextManager,
        max_iterations: int = DEFAULT_MAX_ITERATIONS,
        db: Optional[DBSession] = None,
        session_id: Optional[int] = None,
        user_msg_id: Optional[int] = None,
        agent_msg_id: Optional[int] = None,
    ):
        """
        Initialize the agent loop.

        Args:
            llm_client: LLM client with tool binding support.
            llm_config: LLM configuration for creating checkpoint client.
            tools: List of available tools.
            context_manager: ContextManager with window control.
            max_iterations: Maximum number of loop iterations.
            db: Database session for writing interaction records.
                If None, interactions are not persisted.
            session_id: Session ID for interaction records and notes.md refresh.
            user_msg_id: User message ID that triggered execution.
            agent_msg_id: Agent message ID for the delivery target.
        """
        self._llm_client = llm_client
        self._llm_config = llm_config
        self._tool_registry: Dict[str, Tool] = {t.name: t for t in tools}
        self._context_manager = context_manager
        self._max_iterations = max_iterations
        self._db = db
        self._session_id = session_id
        self._user_msg_id = user_msg_id
        self._agent_msg_id = agent_msg_id

        # Prepare write_notes tool and client for checkpoints
        self._write_notes_tool = WriteNotesTool(session_id=session_id) if session_id else None
        self._checkpoint_client: Optional[TaskLLMClient] = None

        # Track last voluntary write_notes call by agent
        # Used to determine when forced checkpoint is needed
        self._last_notes_iteration: int = 0

    def _write_interaction(self, iteration: int, interaction_type: int, data: dict) -> None:
        """
        Write an interaction record to the database.

        Silently skips if database session is not configured.

        Args:
            iteration: The iteration number.
            interaction_type: INTERACTION_TYPE_REQUEST or INTERACTION_TYPE_RESPONSE.
            data: The data payload to store as JSON.
        """
        if not self._db or not self._session_id:
            return

        try:
            record = Interaction(
                session_id=self._session_id,
                user_msg_id=self._user_msg_id or 0,
                agent_msg_id=self._agent_msg_id or 0,
                iteration=iteration,
                type=interaction_type,
                data=json.dumps(data, ensure_ascii=False),
            )
            self._db.add(record)
            self._db.commit()
        except Exception as e:
            logger.error(f"Failed to write interaction record: {e}")
            if self._db:
                self._db.rollback()

    async def run(self) -> Dict[str, str]:
        """
        Execute the agent loop.

        This is the main entry point. It runs the ReAct loop until:
        - LLM returns a stop response (success)
        - Max iterations reached (failure, after writing notes)

        The task requirement is already injected via ContextManager
        (as part of the fixed task.md content), so it is not passed
        as a parameter here.

        Returns:
            Dict with:
            - status: "success" or "failure"
            - result: Final content (on success)
            - reason: Failure reason (on failure)
        """
        logger.info(
            f"TaskLoop starting: "
            f"max_iterations={self._max_iterations}, "
            f"tools={list(self._tool_registry.keys())}, "
            f"session_id={self._session_id}, agent_msg_id={self._agent_msg_id}"
        )

        for iteration in range(1, self._max_iterations + 1):
            logger.info(f"TaskLoop iteration {iteration}/{self._max_iterations}")

            # Re-read and trim notes before building messages
            if self._session_id:
                trim_notes_md(self._session_id)
                self._context_manager.refresh_notes(
                    read_notes_md(self._session_id)
                )

            # Build messages with window control
            messages = self._context_manager.build_messages()

            # Determine if this iteration requires notes checkpoint
            is_checkpoint = self._is_checkpoint_iteration(iteration)
            is_final = (iteration == self._max_iterations)

            if is_checkpoint or is_final:
                result = await self._run_notes_iteration(
                    iteration=iteration,
                    messages=messages,
                    is_final=is_final,
                )
                if result["status"] == "failure":
                    return result
                # Checkpoint completed, continue to next iteration
                continue

            # Normal iteration with all tools available
            self._write_interaction(
                iteration=iteration,
                interaction_type=INTERACTION_TYPE_REQUEST,
                data={"messages": messages},
            )

            try:
                response = await self._llm_client.invoke(messages)
            except Exception as e:
                logger.error(f"TaskLoop LLM error at iteration {iteration}: {str(e)}")
                return {
                    "status": "failure",
                    "reason": f"LLM invocation failed at iteration {iteration}: {str(e)}",
                }

            finish_reason = response["finish_reason"]
            content = response.get("content", "") or ""
            tool_calls = response.get("tool_calls", []) or []

            self._write_interaction(
                iteration=iteration,
                interaction_type=INTERACTION_TYPE_RESPONSE,
                data={
                    "content": content,
                    "tool_calls": tool_calls,
                    "finish_reason": finish_reason,
                },
            )

            if finish_reason == "stop":
                logger.debug(
                    f"TaskLoop completed at iteration {iteration}: "
                    f"content={repr(content[:500])}"
                )
                
                # Update notes before returning success
                # This ensures notes reflects the final state for future modifications
                await self._update_notes_on_success(iteration, content, messages)
                
                return {"status": "success", "result": content}

            if finish_reason == "tool_calls":
                thoughts = content

                if thoughts:
                    logger.info(
                        f"TaskLoop thoughts [iteration {iteration}]: {thoughts[:500]}"
                    )

                # Build assistant message
                assistant_msg: Dict[str, object] = {
                    "role": "assistant",
                    "tool_calls": tool_calls,
                }
                if thoughts:
                    assistant_msg["content"] = thoughts

                # Execute tools and collect results
                tool_results = []
                has_write_notes = False
                for tc in tool_calls:
                    tool_name = tc["function"]["name"]
                    tool_args = json.loads(tc["function"]["arguments"])

                    logger.info(f"Executing tool: {tool_name}, args: {json.dumps(tool_args, ensure_ascii=False)[:200]}")

                    # Track if agent voluntarily calls write_notes
                    if tool_name == "write_notes":
                        has_write_notes = True

                    tool_result = await self._execute_tool_call(tc)

                    logger.debug(
                        f"TaskLoop tool result: tool={tool_name}, "
                        f"result={repr(tool_result[:500])}"
                    )
                    tool_results.append({
                        "role": "tool",
                        "tool_call_id": tc["id"],
                        "content": tool_result,
                    })

                # Update last voluntary write_notes iteration
                if has_write_notes:
                    self._last_notes_iteration = iteration
                    logger.info(f"Agent voluntarily called write_notes at iteration {iteration}")

                # Add iteration to context manager (as a group)
                self._context_manager.add_iteration(assistant_msg, tool_results)

                continue

            if finish_reason == "length":
                # LLM output was truncated, tool_calls may be incomplete
                # Do NOT execute tool_calls - they could be malformed
                logger.warning(f"TaskLoop finish_reason='length' at iteration {iteration}")

                # Build assistant message with whatever content we got
                # This preserves agent's partial output for context
                assistant_msg: Dict[str, object] = {"role": "assistant"}
                if content:
                    assistant_msg["content"] = content
                if tool_calls:
                    assistant_msg["tool_calls"] = tool_calls

                # Add iteration to context manager (without executing tools)
                self._context_manager.add_iteration(assistant_msg, [])

                # Add a user message to inform agent about truncation
                # Agent can then reorganize and continue
                self._context_manager.add_iteration(
                    {"role": "user", "content": "[System] Your previous response was truncated due to length limits. Your tool calls were NOT executed. Please continue with a more concise response."},
                    []
                )

                continue

            # Unknown finish_reason, log and continue
            logger.warning(
                f"TaskLoop unexpected finish_reason='{finish_reason}' at iteration {iteration}, continuing"
            )
            continue

        # Should not reach here, but just in case
        logger.warning(f"TaskLoop reached max_iterations={self._max_iterations}")
        return {
            "status": "failure",
            "reason": f"Task did not complete within {self._max_iterations} iterations",
        }

    def _is_checkpoint_iteration(self, iteration: int) -> bool:
        """
        Check if this iteration should be a forced notes checkpoint.

        Checkpoint is triggered when:
        - Distance from last voluntary write_notes >= window - 1
        - This respects agent's autonomy while ensuring memory persistence

        Final iteration is handled separately.

        Args:
            iteration: Current iteration number.

        Returns:
            True if this is a checkpoint iteration.
        """
        if iteration == self._max_iterations:
            return False  # Final iteration handled separately

        window = self._context_manager.iteration_window
        distance = iteration - self._last_notes_iteration

        # Force checkpoint when agent hasn't written notes for window-1 iterations
        return distance >= window

    async def _run_notes_iteration(
        self,
        iteration: int,
        messages: List[Dict],
        is_final: bool,
    ) -> Dict[str, str]:
        """
        Run a notes checkpoint or final notes iteration.

        During this iteration, only write_notes tool is available.
        The agent must use it to persist information.

        Args:
            iteration: Current iteration number.
            messages: Messages to send to LLM.
            is_final: Whether this is the final iteration.

        Returns:
            Dict with status and optional reason on failure.
        """
        if not self._write_notes_tool:
            logger.error("Cannot run notes iteration: write_notes_tool not initialized")
            if is_final:
                return {
                    "status": "failure",
                    "reason": "Task did not complete within max iterations",
                }
            return {"status": "success"}

        # Create checkpoint client if not exists
        if not self._checkpoint_client:
            self._checkpoint_client = TaskLLMClient(
                llm_config=self._llm_config,
                tool_schemas=[self._write_notes_tool.schema],
            )

        iteration_type = "final" if is_final else "checkpoint"
        logger.info(f"Running {iteration_type} notes iteration {iteration}")

        # Append checkpoint/final message
        if is_final:
            checkpoint_msg = """[Final Iteration - Save Your Progress]
You have reached the maximum number of iterations.
The task could not be completed in time.

MANDATORY: You must save your progress now using the write_notes tool.
This is the ONLY tool available to you.

Use write_notes to APPEND entries to your NOTES:
- entry_type: "progress" for current status
- entry_type: "finding" for key discoveries
- entry_type: "decision" for choices made

Example:
{
  "entry_type": "progress",
  "content": "Completed X, Y. Still need to do Z.",
  "references": ["result.json"]
}

Your notes will help the next execution continue from where you left off."""
        else:
            checkpoint_msg = """[Memory Checkpoint Required]
You have reached the limit of your working memory.
The oldest iterations are now invisible to you.

MANDATORY: You must write your notes now using the write_notes tool.
This is the ONLY tool available to you in this iteration.

Use write_notes to APPEND entries to your NOTES:
- entry_type: "progress" for current status and next steps
- entry_type: "finding" for key discoveries
- entry_type: "decision" for choices made and why
- entry_type: "observation" for important things noticed

Each entry is APPENDED, not overwritten. Include file references when relevant.

After writing notes, you will regain access to all tools."""

        messages_with_checkpoint = messages + [
            {"role": "user", "content": checkpoint_msg}
        ]

        self._write_interaction(
            iteration=iteration,
            interaction_type=INTERACTION_TYPE_REQUEST,
            data={"messages": messages_with_checkpoint, "is_checkpoint": True},
        )

        try:
            response = await self._checkpoint_client.invoke(messages_with_checkpoint)
        except Exception as e:
            logger.error(f"Notes iteration LLM error: {str(e)}")
            if is_final:
                return {
                    "status": "failure",
                    "reason": f"Task did not complete within max iterations",
                }
            return {
                "status": "failure",
                "reason": f"Notes iteration LLM invocation failed: {str(e)}",
            }

        finish_reason = response["finish_reason"]
        content = response.get("content", "") or ""
        tool_calls = response.get("tool_calls", []) or []

        self._write_interaction(
            iteration=iteration,
            interaction_type=INTERACTION_TYPE_RESPONSE,
            data={
                "content": content,
                "tool_calls": tool_calls,
                "finish_reason": finish_reason,
                "is_checkpoint": True,
            },
        )

        if finish_reason == "tool_calls":
            # Execute write_notes tool
            tool_results = []
            for tc in tool_calls:
                tool_name = tc["function"]["name"]
                tool_call_id = tc["id"]

                if tool_name != "write_notes":
                    logger.warning(f"Notes iteration: unexpected tool call '{tool_name}', ignoring")
                    tool_results.append({
                        "role": "tool",
                        "tool_call_id": tool_call_id,
                        "content": f"Error: tool '{tool_name}' is not available during notes iteration",
                    })
                    continue

                tool_args = json.loads(tc["function"]["arguments"])
                logger.info(f"Notes iteration: executing write_notes")

                result = await self._write_notes_tool.execute(**tool_args)
                logger.info(f"Notes iteration: write_notes result: {result[:200]}")

                tool_results.append({
                    "role": "tool",
                    "tool_call_id": tool_call_id,
                    "content": result,
                })

            # Update last notes iteration (forced checkpoint counts)
            self._last_notes_iteration = iteration

            # Refresh notes in context manager
            if self._session_id:
                self._context_manager.refresh_notes(
                    read_notes_md(self._session_id)
                )

            # Build assistant message
            assistant_msg: Dict[str, object] = {
                "role": "assistant",
                "tool_calls": tool_calls,
            }
            if content:
                assistant_msg["content"] = content

            self._context_manager.add_iteration(assistant_msg, tool_results)

        logger.info(f"Notes iteration {iteration} completed")

        if is_final:
            return {
                "status": "failure",
                "reason": "Task did not complete within max iterations. Notes have been saved for next execution.",
            }

        return {"status": "success"}

    async def _update_notes_on_success(
        self,
        iteration: int,
        final_content: str,
        messages: List[Dict],
    ) -> None:
        """
        Update notes after task completion.

        This ensures notes reflects the final state for future
        modifications or follow-up tasks.

        Args:
            iteration: Current iteration number.
            final_content: The final response content.
            messages: The messages that led to completion.
        """
        if not self._write_notes_tool:
            logger.warning("Cannot update notes on success: write_notes_tool not initialized")
            return

        # Create checkpoint client if not exists
        if not self._checkpoint_client:
            self._checkpoint_client = TaskLLMClient(
                llm_config=self._llm_config,
                tool_schemas=[self._write_notes_tool.schema],
            )

        logger.info(f"Updating notes after successful completion at iteration {iteration}")

        success_msg = """[Task Completed - Update Your Notes]
The task has been completed successfully.

Please update your notes to reflect the final state.
Use write_notes to APPEND a summary entry:

{
  "entry_type": "progress",
  "content": "Task completed. Summary of what was done...",
  "references": ["file1.py", "file2.json"]
}

This will help you continue work if the user requests changes later."""

        messages_with_update = messages + [
            {"role": "user", "content": success_msg}
        ]

        try:
            response = await self._checkpoint_client.invoke(messages_with_update)
        except Exception as e:
            logger.error(f"Notes update on success failed: {str(e)}")
            return

        finish_reason = response["finish_reason"]
        content = response.get("content", "") or ""
        tool_calls = response.get("tool_calls", []) or []

        if finish_reason == "tool_calls":
            for tc in tool_calls:
                tool_name = tc["function"]["name"]
                if tool_name != "write_notes":
                    continue

                tool_args = json.loads(tc["function"]["arguments"])
                logger.info(f"Success notes update: executing write_notes")

                result = await self._write_notes_tool.execute(**tool_args)
                logger.info(f"Success notes update: write_notes result: {result[:200]}")

            # Refresh notes in context manager
            if self._session_id:
                self._context_manager.refresh_notes(
                    read_notes_md(self._session_id)
                )

        logger.info("Notes updated after successful completion")

    async def _execute_tool_call(self, tool_call: Dict) -> str:
        """
        Execute a single tool call and return the result.

        Args:
            tool_call: Dict with id, function.name, function.arguments.

        Returns:
            Tool execution result as a string.
        """
        tool_name = tool_call["function"]["name"]
        arguments_str = tool_call["function"]["arguments"]

        try:
            arguments = json.loads(arguments_str)
        except json.JSONDecodeError as e:
            logger.error(f"Invalid tool arguments JSON: {arguments_str}")
            return f"Error: invalid arguments format - {str(e)}"

        tool = self._tool_registry.get(tool_name)
        if not tool:
            logger.error(f"Unknown tool: {tool_name}")
            return f"Error: unknown tool '{tool_name}'"

        logger.info(f"Executing tool: {tool_name}, args: {arguments_str[:200]}")

        try:
            result = await tool.execute(**arguments)
            return result
        except Exception as e:
            logger.error(
                f"Tool execution error: tool={tool_name}, error={str(e)}",
                exc_info=True,
            )
            return f"Error executing tool '{tool_name}': {str(e)}"
