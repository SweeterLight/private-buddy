"""
Data Transfer Objects (DTO) for inter-service communication.

DTOs are pure data structures (Pydantic BaseModel) used to transfer data
between service modules. They are independent of both database models
and API schemas, serving as clean contracts for service boundaries.
"""

from app.services.dto.task_result import TaskResult

__all__ = [
    "TaskResult",
]
