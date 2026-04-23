"""
Context assembly module for building LLM input messages.

This module handles the assembly of context components into a unified message
format for LLM processing. It implements the "one big message" pattern where
character settings, background story, and recent messages are combined into
a single prompt.

The assembly process includes:
- Character settings (personality, style, identity) integration
- Background story formatting with metadata
- Recent message formatting with sequence numbers
- User state injection as natural language in instruction area

Template Design:
- Uses narrative-style section headers instead of bracketed labels
- "Background context from earlier" and "Recent conversation" create temporal flow
- Metadata (message numbers) preserved for debugging and context clarity
- User state placed in instruction area (after narrative, before response directive)
  to preserve narrative flow while guiding response strategy
"""

from typing import List, Dict, Any, Optional
from langchain_core.messages import HumanMessage, AIMessage, BaseMessage
from app.logger import logger


class ContextAssemblyService:
    """
    Service for assembling context components into LLM-ready messages.
    
    This service implements the context assembly strategy where:
    - Character settings define the agent's personality and style
    - Summary and recent messages are decoupled (may overlap)
    - Metadata labels help LLM understand the scope of each component
    - Background story provides compressed historical context
    - Recent messages provide precise details for current context
    - User state guides response strategy via natural language instruction
    """
    
    # Template for full context with background story and character settings
    ONE_BIG_MESSAGE_TEMPLATE = """{character_section}Background context from earlier in the conversation (messages 1-{summary_version}):

{background_story}

---

Recent conversation (messages {recent_start}-{recent_end}):

{dialog_section}

---

{user_state_instruction}Please respond directly to the user. Do not use parenthetical action descriptions or non-verbal content."""

    # Template for simple context without background story (V < N case)
    ONE_BIG_MESSAGE_NO_STORY_TEMPLATE = """{character_section}Conversation record (messages {recent_start}-{recent_end}):

{dialog_section}

---

{user_state_instruction}Please respond directly to the user. Do not use parenthetical action descriptions or non-verbal content."""

    @staticmethod
    def _format_character_section(character_settings: Optional[str]) -> str:
        """
        Format character settings section for the prompt.
        
        Args:
            character_settings: Agent's personality, style, and identity settings
            
        Returns:
            Formatted character section string
        """
        if not character_settings:
            return ""
        return f"[Your Character]\n{character_settings}\n\n---\n\n"

    @staticmethod
    def _format_user_state_instruction(user_state_description: Optional[str]) -> str:
        """
        Format user state as natural language instruction.
        
        User state is placed in the instruction area (after narrative sections,
        before response directive) to preserve narrative flow while guiding
        the LLM's response strategy.
        
        Args:
            user_state_description: Natural language description of user state,
                e.g. "The user appears frustrated, is seeking help with a problem,
                and is likely at work on desktop."
            
        Returns:
            Formatted instruction string, or empty string if no user state
        """
        if not user_state_description:
            return ""
        return f"{user_state_description}\nAdjust your response tone, detail level, and strategy accordingly.\n\n"

    @staticmethod
    def assemble_context(
        character_settings: Optional[str],
        background_story: Optional[str],
        recent_messages: List[Dict[str, Any]],
        summary_version: Optional[int] = None,
        recent_start: int = 1,
        recent_end: int = 1,
        user_state_description: Optional[str] = None
    ) -> List[BaseMessage]:
        """
        Assemble context into one big message for LLM processing.
        
        This method combines character settings, background story, and recent messages
        into a unified message format. The background story and recent messages
        are decoupled - they may overlap in coverage.
        
        Args:
            character_settings: Agent's personality, style, and identity settings
            background_story: Background narrative from summary + RAG segments
            recent_messages: Recent completed messages (including trigger_message as the latest)
            summary_version: Version number of the summary (covers messages 1 to summary_version)
            recent_start: Starting message sequence number for recent messages
            recent_end: Ending message sequence number for recent messages
            user_state_description: Natural language description of inferred user state,
                placed in instruction area to guide response strategy
            
        Returns:
            List of LangChain messages ready for LLM processing
        """
        messages = []

        # Format character settings section
        character_section = ContextAssemblyService._format_character_section(character_settings)

        # Format user state instruction
        user_state_instruction = ContextAssemblyService._format_user_state_instruction(user_state_description)

        # Format recent messages into dialog section
        dialog_lines = []
        for msg in recent_messages:
            role = "User" if msg["role"] == "user" else "You"
            dialog_lines.append(f"{role}: {msg['content']}")
        dialog_section = "\n".join(dialog_lines)

        # Choose template based on whether background story exists
        if background_story and summary_version:
            one_big_message = ContextAssemblyService.ONE_BIG_MESSAGE_TEMPLATE.format(
                character_section=character_section,
                background_story=background_story,
                dialog_section=dialog_section,
                summary_version=summary_version,
                recent_start=recent_start,
                recent_end=recent_end,
                user_state_instruction=user_state_instruction
            )
        else:
            one_big_message = ContextAssemblyService.ONE_BIG_MESSAGE_NO_STORY_TEMPLATE.format(
                character_section=character_section,
                dialog_section=dialog_section,
                recent_start=recent_start,
                recent_end=recent_end,
                user_state_instruction=user_state_instruction
            )

        # Add the one big message as a HumanMessage
        messages.append(HumanMessage(content=one_big_message))

        logger.info(f"Assembled context with {len(messages)} messages, user_state: {user_state_description is not None}")
        return messages

