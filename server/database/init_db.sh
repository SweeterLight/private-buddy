#!/bin/bash

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SERVER_DIR="$(dirname "$SCRIPT_DIR")"
DATA_DIR="$HOME/PrivateBuddyData/db"
DB_FILE="$DATA_DIR/private_buddy.db"
FULL_INIT_SQL="$SCRIPT_DIR/sql/full_init.sql"
UPGRADE_SQL_DIR="$SCRIPT_DIR/sql/upgrade"

MODE="${1:-init}"

usage() {
    echo "Usage: $0 [init|upgrade]"
    echo ""
    echo "  init    Create a fresh database from the current full schema (default)"
    echo "  upgrade Apply incremental upgrade SQL files to an existing database"
    echo ""
    exit 1
}

if [[ "$MODE" != "init" && "$MODE" != "upgrade" ]]; then
    usage
fi

echo "========================================="
if [ "$MODE" = "init" ]; then
    echo "  Private Buddy Database Initialization"
else
    echo "  Private Buddy Database Upgrade"
fi
echo "========================================="
echo ""

if [ "$MODE" = "init" ]; then
    if [ ! -f "$FULL_INIT_SQL" ]; then
        echo "Error: Full init SQL file not found: $FULL_INIT_SQL"
        exit 1
    fi

    if [ -f "$DB_FILE" ]; then
        echo "Warning: Database file already exists: $DB_FILE"
        read -p "Overwrite existing database? (y/N): " -n 1 -r
        echo ""
        if [[ ! $REPLY =~ ^[Yy]$ ]]; then
            echo "Initialization cancelled"
            exit 0
        fi
        rm -f "$DB_FILE"
        echo "✓ Existing database removed"
    fi

    echo ""
    read -p "Continue to initialize database? (y/N): " -n 1 -r
    echo ""
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        echo "Initialization cancelled"
        exit 0
    fi

    echo ""
    echo "Step 1: Creating data directory..."
    mkdir -p "$DATA_DIR"
    echo "✓ Data directory ready"

    echo ""
    echo "Step 2: Executing full init SQL..."
    if sqlite3 "$DB_FILE" < "$FULL_INIT_SQL"; then
        echo "  ✓ full_init.sql executed successfully"
    else
        echo "  ✗ full_init.sql execution failed"
        exit 1
    fi

    echo ""
    echo "========================================="
    echo "  Database initialization complete!"
    echo "========================================="

else
    if [ ! -f "$DB_FILE" ]; then
        echo "Error: Database file not found: $DB_FILE"
        echo "Run '$0 init' first to create a new database."
        exit 1
    fi

    if [ ! -d "$UPGRADE_SQL_DIR" ]; then
        echo "Error: Upgrade SQL directory not found: $UPGRADE_SQL_DIR"
        exit 1
    fi

    SQL_FILES=($(ls "$UPGRADE_SQL_DIR"/*.sql 2>/dev/null | sort -V))
    if [ ${#SQL_FILES[@]} -eq 0 ]; then
        echo "No upgrade SQL files found. Database is already up to date."
        exit 0
    fi

    echo ""
    echo "Upgrade SQL files to apply:"
    for file in "${SQL_FILES[@]}"; do
        echo "  - $(basename "$file")"
    done
    echo ""

    read -p "Continue to upgrade database? (y/N): " -n 1 -r
    echo ""
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        echo "Upgrade cancelled"
        exit 0
    fi

    echo ""
    echo "Applying upgrade SQL files..."
    for file in "${SQL_FILES[@]}"; do
        filename=$(basename "$file")
        echo "  Executing: $filename"
        if sqlite3 "$DB_FILE" < "$file"; then
            echo "  ✓ $filename applied successfully"
        else
            echo "  ✗ $filename failed"
            exit 1
        fi
    done

    echo ""
    echo "========================================="
    echo "  Database upgrade complete!"
    echo "========================================="
fi

echo ""
echo "Database file: $DB_FILE"
echo ""
echo "Next step: Start server service"
echo "  cd $SERVER_DIR"
echo "  ./start.sh"
echo ""
