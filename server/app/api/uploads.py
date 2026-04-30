"""
Upload API endpoints.

Provides file upload functionality for agent avatars and other resources.
"""

import time
from fastapi import APIRouter, UploadFile, File, HTTPException
from pathlib import Path

from app.services.task.workspace import get_avatars_dir
from app.logger import logger

router = APIRouter(prefix="/api/uploads", tags=["uploads"])

ALLOWED_EXTENSIONS = {".jpg", ".jpeg", ".png", ".webp"}
MAX_FILE_SIZE = 2 * 1024 * 1024  # 2MB


@router.post("/avatar")
async def upload_avatar(
    agent_id: int,
    file: UploadFile = File(...)
):
    """
    Upload an avatar image for an agent.

    Accepts jpg, jpeg, png, webp formats. Max size 2MB.
    Returns the relative filename stored under PrivateBuddyData/avatars/.
    """
    if not file.filename:
        raise HTTPException(status_code=400, detail="No filename provided")

    ext = Path(file.filename).suffix.lower()
    if ext not in ALLOWED_EXTENSIONS:
        raise HTTPException(
            status_code=400,
            detail=f"Invalid file type. Allowed: {', '.join(ALLOWED_EXTENSIONS)}"
        )

    content = await file.read()
    if len(content) > MAX_FILE_SIZE:
        raise HTTPException(
            status_code=400,
            detail=f"File too large. Max size: {MAX_FILE_SIZE // (1024 * 1024)}MB"
        )

    filename = f"{agent_id}_{int(time.time())}{ext}"
    avatars_dir = get_avatars_dir()
    file_path = avatars_dir / filename

    with open(file_path, "wb") as f:
        f.write(content)

    logger.info(f"Avatar uploaded: {filename} for agent {agent_id}")
    return {"filename": filename}
