"""
Context manager for the agent's internal message history.

Manages the message list within a single task execution using a
"fixed part + dynamic part" architecture with iteration window control.

Fixed part (always fully included):
- system prompt: basic rules + context information
- Task content: task requirements (system-managed)
- Notes content: agent's structured working notes (system-managed)

Dynamic part (window-controlled):
- Recent interaction rounds (assistant + tool messages)
- Only the last w iterations are visible to the LLM
- Older iterations are discarded from context

Context information is merged into the system prompt so the agent
always sees it as top-level instructions.
"""

from typing import Any, Dict, List

from app.config import get_settings
from app.logger import logger


class ContextManager:
    """
    Manages the internal message history for a single task execution.

    Messages follow the OpenAI chat completion format:
    - system: { role, content }
    - user: { role, content }
    - assistant: { role, content } or { role, tool_calls }
    - tool: { role, tool_call_id, content }

    Window applies to the dynamic part only. The fixed part
    (system prompt with context info, Task, Notes) is always
    fully included because these are essential prerequisites for
    the agent's work.
    """

    def __init__(
        self,
        system_prompt: str,
        iteration_window: int,
        task_content: str,
        notes_content: str,
    ):
        """
        Initialize the context manager.

        Args:
            system_prompt: Static system prompt (basic rules).
                Context information will be appended to this at
                build time, so the agent always sees it as part
                of the system-level instructions.
            iteration_window: Number of recent iterations to keep visible.
                Older iterations are discarded from context.
            task_content: Full content of task requirements.
            notes_content: Full content of agent's notes.
        """
        self._system_prompt = system_prompt
        self._iteration_window = iteration_window
        self._task_content = task_content
        self._notes_content = notes_content
        self._total_iterations = 0
        self._dynamic_messages: List[List[Dict[str, Any]]] = []

    @property
    def iteration_window(self) -> int:
        """Get the iteration window size."""
        return self._iteration_window

    def refresh_notes(self, new_notes_content: str) -> None:
        """
        Update notes content (agent may have appended via write_notes tool).

        Args:
            new_notes_content: The updated notes content.
        """
        self._notes_content = new_notes_content

    def add_iteration(
        self,
        assistant_msg: Dict[str, Any],
        tool_results: List[Dict[str, Any]],
    ) -> None:
        """
        Add a complete iteration (assistant message + tool results).

        An iteration is a group of messages that must be kept together
        to maintain conversation coherence. The assistant message and
        its associated tool results are always included or excluded
        as a unit.

        Args:
            assistant_msg: The assistant message dict (with content
                and/or tool_calls).
            tool_results: List of tool result message dicts.
        """
        group = [assistant_msg] + tool_results
        self._dynamic_messages.append(group)
        self._total_iterations += 1

    def build_messages(self) -> List[Dict[str, Any]]:
        """
        Assemble final message list for LLM call.

        Order:
        1. system prompt (basic rules + context information)
        2. user: Task content
        3. user: Notes content
        4. dynamic messages (recent iterations within window)

        Window applies to dynamic part only; fixed part is always
        fully included.

        Returns:
            List of message dicts ready for LLM invocation.
        """
        # Step 1: Take the last w iterations
        window = self._iteration_window
        visible = self._dynamic_messages[-window:] if self._dynamic_messages else []
        visible_iterations = len(visible)
        invisible_iterations = self._total_iterations - visible_iterations

        # Step 2: Build system prompt with context information
        full_system_prompt = self._build_full_system_prompt(
            visible_iterations=visible_iterations,
            invisible_iterations=invisible_iterations,
            notes_length=len(self._notes_content),
        )

        # Step 3: Assemble
        messages: List[Dict[str, Any]] = [
            {"role": "system", "content": full_system_prompt},
            {"role": "user", "content": f"[Task]\n{self._task_content}"},
            {"role": "user", "content": f"[Your Notes]\n{self._notes_content}"},
        ]
        for group in visible:
            messages.extend(group)

        logger.debug(
            f"ContextManager.build_messages: "
            f"total_iterations={self._total_iterations}, "
            f"visible_iterations={visible_iterations}, "
            f"window={window}, "
            f"total_messages={len(messages)}"
        )

        return messages

    def _build_full_system_prompt(
        self,
        visible_iterations: int,
        invisible_iterations: int,
        notes_length: int,
    ) -> str:
        """
        Build the full system prompt by merging static rules with
        dynamic context information.

        Context information is appended to the system prompt so the
        agent always sees it as system-level instructions rather
        than a separate user message that may be overlooked.

        Args:
            visible_iterations: Number of iterations within the window.
            invisible_iterations: Number of iterations outside the window.
            notes_length: Current character count of notes.

        Returns:
            Complete system prompt string.
        """
        settings = get_settings()
        notes_max_chars = settings.notes_max_chars

        context_parts = [
            "",
            "[Context Information]",
            f"Your working memory is limited. You can see the last {self._iteration_window} iterations.",
            f"This task has produced {self._total_iterations} iterations total, {invisible_iterations} of which are outside your visible range.",
            "",
            f"Your NOTES are currently {notes_length} chars (max: {notes_max_chars} chars).",
        ]

        if notes_length > notes_max_chars * 0.8:
            context_parts.append("WARNING: Your NOTES are approaching the size limit. Consider consolidating older entries.")

        context_parts.extend([
            "",
            "[Understanding Current State]",
            "To understand the current project state:",
            "- Use 'ls -la' to see files in your working directory",
            "- Use 'cat <filename>' to read file contents",
            "- Use 'find . -type f' to discover all files",
            "- Check your NOTES (provided above) for previous progress",
            "",
            "[NOTES Usage Guide]",
            "The write_notes tool appends structured entries to your notes.",
            "",
            "Entry types:",
            "- observation: Something you discovered",
            "- decision: A choice you made (explain why)",
            "- finding: A key result or conclusion",
            "- correction: A fix to a previous entry (use conflicts_with)",
            "- progress: Current status and next steps",
            "",
            "Best practices:",
            "- Each entry is APPENDED, not overwritten",
            "- Write CONCISE entries — notes have a size limit",
            "- Only write IMPORTANT information — skip trivial or obvious facts",
            "- Ask: would losing this information hurt the task? If not, skip it",
            "- Include file references when relevant",
            "- Use conflicts_with when correcting earlier decisions",
            "- Write self-contained entries (future LLM calls have no memory)",
        ])

        return self._system_prompt + "\n".join(context_parts)
