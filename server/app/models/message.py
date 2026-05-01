from sqlalchemy import Column, Integer, String, Text, DateTime
from sqlalchemy.orm import relationship
from datetime import datetime
from app.database import Base
from .base import LOCALTIME


MESSAGE_STATUS_STREAMING = 0
MESSAGE_STATUS_COMPLETED = 1

HAS_INTERACTIONS_PENDING = 0
HAS_INTERACTIONS_EXISTS = 1
HAS_INTERACTIONS_NONE = 2


class Message(Base):
    __tablename__ = "messages"

    id = Column(Integer, primary_key=True, index=True)
    session_id = Column(Integer, nullable=False, index=True)
    role = Column(String(20), nullable=False)
    content = Column(Text, nullable=False)
    status = Column(Integer, default=MESSAGE_STATUS_COMPLETED, nullable=False)
    has_interactions = Column(Integer, default=HAS_INTERACTIONS_NONE, nullable=False)
    created_at = Column(DateTime(timezone=True), default=datetime.now, server_default=LOCALTIME, nullable=False)
    updated_at = Column(DateTime(timezone=True), default=datetime.now, server_default=LOCALTIME, onupdate=datetime.now)

    session = relationship(
        "Session",
        back_populates="messages",
        primaryjoin="Message.session_id == Session.id",
        foreign_keys="Message.session_id"
    )