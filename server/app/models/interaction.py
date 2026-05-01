"""
Interaction model for agent-world interaction records.

Each interaction captures one step of the ReAct loop:
- type=1 (request): messages sent to the LLM
- type=2 (response): LLM output including thoughts, tool_calls, finish_reason

Interactions are grouped by (session_id, user_msg_id, agent_msg_id, iteration)
to support both frontend display and debugging.
"""

from sqlalchemy import Column, Integer, Text, DateTime, UniqueConstraint, Index
from datetime import datetime
from app.database import Base
from .base import LOCALTIME


INTERACTION_TYPE_REQUEST = 1
INTERACTION_TYPE_RESPONSE = 2


class Interaction(Base):
    __tablename__ = "interactions"

    id = Column(Integer, primary_key=True, index=True)
    session_id = Column(Integer, nullable=False)
    user_msg_id = Column(Integer, nullable=False)
    agent_msg_id = Column(Integer, nullable=False)
    iteration = Column(Integer, nullable=False)
    type = Column(Integer, nullable=False)
    updated_at = Column(DateTime(timezone=True), default=datetime.now, server_default=LOCALTIME, nullable=False)
    data = Column(Text, nullable=False)
    created_at = Column(DateTime(timezone=True), default=datetime.now, server_default=LOCALTIME, nullable=False)

    __table_args__ = (
        UniqueConstraint(
            "session_id", "user_msg_id", "agent_msg_id", "iteration", "type",
            name="uk_interactions_session_user_agent_iter_type"
        ),
        Index("idx_interactions_session", "session_id"),
        Index("idx_interactions_user_msg", "user_msg_id"),
        Index("idx_interactions_agent_msg", "agent_msg_id"),
        Index("idx_interactions_session_iteration", "session_id", "iteration"),
    )
