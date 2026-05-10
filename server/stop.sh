#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PID_FILE="$SCRIPT_DIR/.pid"

# Try to stop via PID file first
if [ -f "$PID_FILE" ]; then
    PID=$(cat "$PID_FILE")
    if kill -0 "$PID" 2>/dev/null; then
        echo "Stopping server (PID: $PID)..."
        kill "$PID"
        sleep 2
        if kill -0 "$PID" 2>/dev/null; then
            echo "Force killing server..."
            kill -9 "$PID"
        fi
        echo "Server stopped"
    else
        echo "Stale PID file (process $PID not running)"
    fi
    rm -f "$PID_FILE"
fi

# Fallback: find and kill process by port
PORT="${PORT:-8000}"
PID_BY_PORT=$(lsof -ti :"$PORT" 2>/dev/null || true)
if [ -n "$PID_BY_PORT" ]; then
    echo "Found process occupying port $PORT (PID: $PID_BY_PORT), killing..."
    kill $PID_BY_PORT 2>/dev/null || true
    sleep 2
    PID_BY_PORT=$(lsof -ti :"$PORT" 2>/dev/null || true)
    if [ -n "$PID_BY_PORT" ]; then
        echo "Force killing process on port $PORT..."
        kill -9 $PID_BY_PORT 2>/dev/null || true
    fi
    echo "Port $PORT freed"
fi
