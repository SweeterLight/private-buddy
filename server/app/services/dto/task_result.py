"""
Task result DTO for task execution output.

This is the shared data structure returned by TaskExecutor and consumed
by the chat context assembly. It exists as an independent DTO because
it is referenced by multiple service modules (task and chat).
"""

from typing import Optional

from pydantic import BaseModel


class TaskResult(BaseModel):
    """
    The final output of a task execution.

    A result is always produced - either a successful outcome
    or a failure with a reason. Both are legitimate outcomes.

    Attributes:
        status: "success" or "failure"
        result: The task output (present on success)
        reason: The failure explanation (present on failure)
        notes: The final notes content (for generating user-friendly response)
    """

    status: str
    result: Optional[str] = None
    reason: Optional[str] = None
    notes: Optional[str] = None
