# Database Scripts Directory

This directory contains database initialization scripts and SQL files for the Private Buddy project.

## Directory Structure

```
database/
├── init_db.sh         # Database initialization/upgrade script
├── sql/
│   ├── full_init.sql  # Full schema for fresh database initialization
│   └── upgrade/       # Incremental upgrade SQL files
│                       # (empty for 0.0.8 — breaking change, no upgrade path)
└── README.md
```

## Usage

### Fresh Database Initialization

```bash
cd server/database
./init_db.sh          # or ./init_db.sh init
```

This script will:
1. Create `$HOME/PrivateBuddyData/db/` directory if it doesn't exist
2. Execute SQL files in `sql/init/` directory
3. Create SQLite database file at `$HOME/PrivateBuddyData/db/private_buddy.db`
4. Display execution progress and results

If the database file already exists, the script will ask for confirmation before overwriting.

### Database Upgrade

```bash
cd server/database
./init_db.sh upgrade
```

This script will:
1. Check that the database file exists
2. Execute SQL files in `sql/upgrade/` directory in version order
3. Apply incremental schema changes to the existing database

If no upgrade SQL files are found, it reports that the database is already up to date.

### Prerequisites

1. `sqlite3` command available in terminal

### Database Location

The SQLite database file is stored at `~/PrivateBuddyData/db/private_buddy.db`. The application also uses `Base.metadata.create_all()` to ensure all tables exist on startup, so manual initialization is optional.

### Data Directory

All application data is unified under `~/PrivateBuddyData/`:

```
~/PrivateBuddyData/
    db/                 -- SQLite database (private_buddy.db)
    chroma/             -- ChromaDB vector store
    workspace/          -- Agent task workspace
    avatars/            -- Agent avatar images
```

The `DATA_ROOT` can be configured via `.env` file (defaults to `~/PrivateBuddyData`).

## SQL File Management

### Full Init SQL (`sql/full_init.sql`)

Contains the **complete schema** for the current version. Updated with each release to reflect the full database structure. Used for fresh database creation only.

### Upgrade SQL (`sql/upgrade/`)

Contains **incremental** schema changes between versions. Each file represents the delta from one version to the next.

**Naming convention:** `major.minor.patch.sql` (e.g., `0.0.9.sql`, `0.1.0.sql`)

**Execution order:** Files are sorted by version number (`sort -V`) and applied sequentially.

### Adding a New Version

1. Modify tables as needed
2. Create an incremental SQL file in `sql/upgrade/` (e.g., `0.0.9.sql`)
3. Update `sql/full_init.sql` to reflect all changes
4. Add comments at the beginning of the upgrade file to describe changes
5. The upgrade SQL should also insert a record into `db_versions`

**Example upgrade file:**

```sql
-- 0.0.9 Schema
-- Changes: Add user preferences table

CREATE TABLE IF NOT EXISTS user_preferences (
    ...
);

INSERT INTO db_versions (version, description)
VALUES ('0.0.9', 'Add user preferences table');
```

## Database Structure

### Table Structure

**llm_configs** - LLM configuration table
- Stores LLM configuration information, including model name, API keys, etc.

**embedding_configs** - Embedding configuration table
- Stores embedding model configuration for RAG retrieval

**agents** - Agent configuration table
- Stores Agent configuration, associated with LLM configuration and character settings

**sessions** - Session table
- Stores session information, associated with Agent

**messages** - Message table
- Stores message records, associated with session

**interactions** - Interaction table
- Stores agent-world interaction records (LLM request/response per iteration)

**historical_summaries** - Historical summary table
- Stores conversation summaries and cached narratives

**search_config** - Search configuration table
- Single record (id=1) for search engine configuration

**db_versions** - Database version tracking table
- Records each schema version applied to the database
- Used for upgrade detection and future automated migration support

### Index Optimization

**agents table:**
- `idx_agents_llm_config_id` - LLM configuration association query
- `idx_agents_updated_at` - Sort by update time

**sessions table:**
- `idx_sessions_created_at` - Sort by creation time
- `idx_sessions_status` - Filter by status
- `idx_sessions_agent_id` - Query by Agent ID
- `idx_sessions_agent_updated` - Composite index, optimize agent session list query

**messages table:**
- `idx_messages_session_id` - Query by session ID
- `idx_messages_created_at` - Sort by creation time
- `idx_messages_status` - Filter by status
- `idx_messages_session_created` - Composite index, optimize message history query
- `idx_messages_session_status` - Composite index, optimize streaming message query

**interactions table:**
- `idx_interactions_session` - Query by session ID
- `idx_interactions_user_msg` - Query by user message ID
- `idx_interactions_agent_msg` - Query by agent message ID
- `idx_interactions_session_iteration` - Composite index, optimize iteration query

## Database Design Principles

This project follows these database design principles:

1. **No foreign key constraints** - Data constraints should be enforced at application layer
2. **No nullable fields** - All fields should have explicit NOT NULL constraints
3. **Use indexes for query optimization** - Create indexes for frequently queried fields
4. **Prefer composite indexes** - Create composite indexes based on query patterns
5. **Avoid redundant indexes** - Remove duplicate and unnecessary indexes

## Important Notes

1. **Backup data** - Always backup before performing any database operations (copy the .db file)
2. **Test environment** - Validate database changes in test environment first
3. **Version control** - Commit new SQL files to version control system
4. **Idempotency** - SQL files should support repeated execution (use IF NOT EXISTS, etc.)

## Troubleshooting

### Database File Not Created

Check the following:
1. Is `~/PrivateBuddyData/db/` directory writable
2. Is `sqlite3` command available
3. Check application logs for errors

### SQL Execution Failed

1. Check SQL syntax for SQLite compatibility
2. Confirm if table or column already exists
3. Note: SQLite has limited ALTER TABLE support (no DROP COLUMN before 3.35.0)

### Connection Issues

1. Check `DATA_ROOT` in `.env` file
2. Ensure the database file path is correct
3. Check file permissions
