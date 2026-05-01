"""
Database version tracking model.

Records each schema version applied to the database.
Used for upgrade detection and future automated migration support.
"""

from sqlalchemy import Column, Integer, String, DateTime, Text
from datetime import datetime
from app.database import Base
from .base import LOCALTIME


class DBVersion(Base):
    """
    Database version tracking model.

    Each row represents a schema version that has been applied.
    The latest row indicates the current database schema version.
    """

    __tablename__ = "db_versions"

    id = Column(Integer, primary_key=True, autoincrement=True)
    version = Column(String(20), nullable=False)
    description = Column(Text, nullable=False, default='')
    applied_at = Column(DateTime(timezone=True), default=datetime.now, server_default=LOCALTIME, nullable=False)
