from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware
from fastapi.staticfiles import StaticFiles
from app.database import engine, Base
from app.api import llm_configs, sessions, messages, chat, agents, embedding_configs, interactions, search_config, uploads
from app.services.task.workspace import get_avatars_dir
from app.logger import logger

Base.metadata.create_all(bind=engine)

app = FastAPI(
    title="Private Buddy API",
    description="Private AI Assistant Backend API",
    version="0.0.4"
)

app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)

app.include_router(llm_configs.router)
app.include_router(embedding_configs.router)
app.include_router(sessions.router)
app.include_router(messages.router)
app.include_router(chat.router)
app.include_router(agents.router)
app.include_router(interactions.router)
app.include_router(search_config.router)
app.include_router(uploads.router)

app.mount("/avatars", StaticFiles(directory=str(get_avatars_dir())), name="avatars")


@app.get("/")
def root():
    logger.info("Root endpoint accessed")
    return {"message": "Private Buddy API is running"}


@app.on_event("startup")
async def startup_event():
    logger.info("Application starting up...")


@app.on_event("shutdown")
async def shutdown_event():
    logger.info("Application shutting down...")