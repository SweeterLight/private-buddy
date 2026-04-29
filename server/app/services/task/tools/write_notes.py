"""
Write notes tool for persisting agent's working memory.

This tool implements an append-only, structured notes system.
Each note entry has:
- Timestamp: when the entry was written
- Type: observation/decision/finding/correction/progress
- Content: the main note text
- References: optional links to workspace files
- Conflict marker: optional note about conflicting with a previous entry

Design principles:
- Append-only: never overwrite, always add new entries
- Structured: consistent format for traceability
- Traceable: each entry can reference files and mark conflicts
- LLM stateless: each LLM call is independent, notes bridge the gap

The notes are stored in a system-managed location that the agent
should not directly access. Use this tool to interact with notes.
"""

from typing import Any, Dict, List, Literal, Optional

from app.services.task.tools.base import Tool
from app.services.task.workspace import append_note
from app.logger import logger

NOTE_TYPES = Literal["observation", "decision", "finding", "correction", "progress"]


class WriteNotesTool(Tool):
    """
    Tool for appending structured entries to agent's notes.

    This tool uses an append-only design to preserve the history
    of the agent's reasoning and decisions. Each entry is timestamped
    and typed for easy navigation.

    Unlike the previous overwrite design, this approach:
    - Preserves all previous entries (no information loss)
    - Allows marking conflicts with earlier entries
    - Supports file references for traceability
    - Creates an auditable decision trail
    """

    def __init__(self, session_id: int):
        """
        Initialize the write notes tool.

        Args:
            session_id: The session ID for determining the workspace path.
        """
        self._session_id = session_id

    @property
    def name(self) -> str:
        return "write_notes"

    @property
    def schema(self) -> Dict[str, Any]:
        return {
            "type": "function",
            "function": {
                "name": "write_notes",
                "description": (
                    "Append a structured entry to your NOTES. "
                    "This ADDS a new entry, it does NOT overwrite. "
                    "Use this to persist important information for future steps. "
                    "\n\n"
                    "IMPORTANT: Notes have a size limit. Only write IMPORTANT entries. "
                    "Skip trivial or obvious information. "
                    "Focus on key facts that future steps MUST know — "
                    "critical discoveries, important decisions, and essential state. "
                    "When in doubt, ask: would losing this information hurt the task? "
                    "If not, skip it."
                    "\n\n"
                    "Entry types:\n"
                    "- observation: Something you discovered or noticed\n"
                    "- decision: A choice you made and why\n"
                    "- finding: A key result or conclusion\n"
                    "- correction: A fix or change to a previous entry (use conflicts_with)\n"
                    "- progress: Current status and next steps\n"
                    "\n"
                    "Always include:\n"
                    "- Concise, self-contained content\n"
                    "- File references when relevant (paths relative to your working directory)\n"
                    "- Conflict markers when correcting earlier decisions"
                ),
                "parameters": {
                    "type": "object",
                    "properties": {
                        "entry_type": {
                            "type": "string",
                            "enum": ["observation", "decision", "finding", "correction", "progress"],
                            "description": "The type of this note entry",
                        },
                        "content": {
                            "type": "string",
                            "description": (
                                "The main content of this note. "
                                "Be CONCISE — only include information that is "
                                "IMPORTANT to preserve for future steps."
                            ),
                        },
                        "references": {
                            "type": "array",
                            "items": {"type": "string"},
                            "description": (
                                "Optional list of file paths this note relates to. "
                                "Use paths relative to your working directory. "
                                "Example: ['result.json', 'src/main.py']"
                            ),
                        },
                        "conflicts_with": {
                            "type": "string",
                            "description": (
                                "Optional timestamp or description of a previous entry "
                                "that this entry corrects or supersedes. "
                                "Example: '2024-05-20 14:30:00' or 'the decision about X'"
                            ),
                        },
                    },
                    "required": ["entry_type", "content"],
                },
            },
        }

    async def execute(
        self,
        entry_type: NOTE_TYPES,
        content: str,
        references: Optional[List[str]] = None,
        conflicts_with: Optional[str] = None,
    ) -> str:
        """
        Append a structured entry to notes.

        Args:
            entry_type: Type of the note entry.
            content: The main content of the note.
            references: Optional list of file paths this note relates to.
            conflicts_with: Optional identifier of conflicting entry.

        Returns:
            Success message with entry details.
        """
        try:
            append_note(
                session_id=self._session_id,
                entry_type=entry_type,
                content=content,
                references=references,
                conflicts_with=conflicts_with,
            )

            ref_count = len(references) if references else 0
            conflict_marker = " (with conflict marker)" if conflicts_with else ""

            logger.info(
                f"WriteNotesTool: appended {entry_type} entry for session {self._session_id}, "
                f"content_len={len(content)}, refs={ref_count}{conflict_marker}"
            )

            return (
                f"Successfully appended {entry_type} entry to your NOTES. "
                f"Content: {len(content)} chars, References: {ref_count}{conflict_marker}"
            )

        except Exception as e:
            logger.error(f"WriteNotesTool error: {e}")
            return f"Error appending to notes: {str(e)}"
