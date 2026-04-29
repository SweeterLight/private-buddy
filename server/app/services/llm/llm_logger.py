"""
LLM callback logger for token usage and latency tracking.

Uses register_configure_hook to register a global callback handler,
so all LangChain LLM calls are automatically tracked without manual
callback passing at each call site.

Thread-safety is ensured by using run_id-keyed dictionaries instead
of instance-level scalar fields, supporting concurrent LLM calls.
"""

import time
from contextvars import ContextVar
from typing import Any, Dict, List, Optional
from uuid import UUID

from langchain_core.callbacks import BaseCallbackHandler
from langchain_core.outputs import LLMResult
from langchain_core.tracers.context import register_configure_hook

from app.logger import logger

# ContextVar holding the global TokenUsageLogger instance.
# register_configure_hook makes LangChain pick up the handler
# from this var on every LLM call automatically.
_token_usage_logger_var: ContextVar[Optional["TokenUsageLogger"]] = ContextVar(
    "token_usage_logger", default=None
)

register_configure_hook(_token_usage_logger_var, inheritable=True)


class TokenUsageLogger(BaseCallbackHandler):
    """
    Global callback handler that logs token usage and latency for each LLM call.

    Registered via register_configure_hook so all LangChain LLM calls
    are automatically tracked. No manual callback passing is needed.

    Thread-safety: uses run_id-keyed dicts to support concurrent calls.
    """

    def __init__(self) -> None:
        self._start_times: Dict[str, float] = {}
        self._models: Dict[str, Optional[str]] = {}

    def on_chat_model_start(
        self,
        serialized: Dict[str, Any],
        messages: List[List[Any]],
        *,
        run_id: UUID,
        parent_run_id: Optional[UUID] = None,
        tags: Optional[List[str]] = None,
        metadata: Optional[Dict[str, Any]] = None,
        **kwargs: Any,
    ) -> None:
        """Called when chat model starts. Records start time and model info by run_id."""
        run_id_str = str(run_id)
        self._start_times[run_id_str] = time.perf_counter()

        invocation_params = kwargs.get("invocation_params", {})
        self._models[run_id_str] = invocation_params.get("model") or invocation_params.get("model_name")

    def on_llm_end(
        self,
        response: LLMResult,
        *,
        run_id: UUID,
        parent_run_id: Optional[UUID] = None,
        tags: Optional[List[str]] = None,
        **kwargs: Any,
    ) -> None:
        """Called when LLM call ends. Logs token usage and latency by run_id."""
        run_id_str = str(run_id)
        start_time = self._start_times.pop(run_id_str, None)
        model = self._models.pop(run_id_str, None)

        latency_ms = 0.0
        if start_time is not None:
            latency_ms = (time.perf_counter() - start_time) * 1000

        if response.llm_output and "token_usage" in response.llm_output:
            parts = ["llm usage", f"latency={latency_ms:.0f}ms"]
            usage = response.llm_output["token_usage"]
            parts.append(f"prompt_tokens: {usage.get('prompt_tokens')}")
            parts.append(f"completion_tokens: {usage.get('completion_tokens')}")
            parts.append(f"total_tokens: {usage.get('total_tokens')}")
            if model:
                parts.append(f"model={model}")
            logger.debug(" | ".join(parts))


# Singleton instance set into the ContextVar at module load time.
# After this, all LangChain LLM calls will automatically trigger
# the callback without any manual config passing.
_token_usage_logger_var.set(TokenUsageLogger())
