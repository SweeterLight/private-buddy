from sqlalchemy import Column, Integer, String, DateTime, Text
from sqlalchemy.orm import relationship
from datetime import datetime
from app.database import Base
from .base import LOCALTIME


class LLMConfig(Base):
    __tablename__ = "llm_configs"

    id = Column(Integer, primary_key=True, index=True)
    name = Column(String(100), nullable=False)
    model_id = Column(String(100), nullable=False)
    base_url = Column(String(255), nullable=False)
    api_key = Column(String(255), nullable=False)
    description = Column(Text, nullable=False, default='')
    created_at = Column(DateTime(timezone=True), default=datetime.now, server_default=LOCALTIME, nullable=False)
    updated_at = Column(DateTime(timezone=True), default=datetime.now, server_default=LOCALTIME, onupdate=datetime.now, nullable=False)

    agents = relationship(
        "Agent",
        back_populates="llm_config",
        primaryjoin="LLMConfig.id == Agent.llm_config_id",
        foreign_keys="Agent.llm_config_id"
    )