from sqlalchemy import Column, Integer, String, DateTime
from sqlalchemy.orm import relationship
from datetime import datetime
from app.database import Base
from .base import LOCALTIME


SESSION_STATUS_STREAMING = 0
SESSION_STATUS_IDLE = 1


class Session(Base):
    __tablename__ = "sessions"

    id = Column(Integer, primary_key=True, index=True)
    title = Column(String(255), nullable=False, default='')
    agent_id = Column(Integer, nullable=False, index=True)
    status = Column(Integer, default=SESSION_STATUS_IDLE, nullable=False)
    created_at = Column(DateTime(timezone=True), default=datetime.now, server_default=LOCALTIME, nullable=False)
    updated_at = Column(DateTime(timezone=True), default=datetime.now, server_default=LOCALTIME, onupdate=datetime.now, nullable=False)

    messages = relationship(
        "Message",
        back_populates="session",
        primaryjoin="Session.id == Message.session_id",
        foreign_keys="Message.session_id"
    )