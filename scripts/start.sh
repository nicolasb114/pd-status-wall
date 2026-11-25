#!/bin/bash
# Start the status wall.
#
# Keeping the app's data next to this file is the point of the script: by
# default the app stores config in ./data relative to whatever directory
# it was launched from, and a Finder double-click runs from your home
# folder - so launching it that way finds no settings and generates a new
# admin password every time. Pinning DATA_DIR here means your setup is
# wherever this file is, and moving the folder moves everything with it.

cd "$(dirname "$0")" || exit 1
export DATA_DIR="$(pwd)/data"

BIN="$(pwd)/pd-status-wall"
if [ ! -x "$BIN" ]; then
  BIN="$(command -v pd-status-wall)"
fi

if [ -z "$BIN" ] || [ ! -x "$BIN" ]; then
  echo "Could not find the pd-status-wall program."
  echo
  echo "Keep this file in the same folder as the pd-status-wall binary,"
  echo "or install it with:"
  echo "    brew install nicolasb114/tap/pd-status-wall"
  echo
  read -r -p "Press Return to close."
  exit 1
fi

echo "Starting the status wall..."
echo "  settings: $DATA_DIR"
echo
echo "Your browser will open automatically."
echo "Leave this window open while it runs. Press Ctrl+C to stop."
echo

"$BIN"

status=$?
if [ $status -ne 0 ]; then
  echo
  echo "The status wall stopped (exit code $status). The reason should be above."
  read -r -p "Press Return to close."
fi
