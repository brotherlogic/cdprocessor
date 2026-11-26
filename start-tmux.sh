#!/bin/bash

# Ensure the 'cdprocessor' session exists
if ! tmux has-session -t cdprocessor 2>/dev/null; then
  # Create a new session named 'cdprocessor', detached
  cd /workspaces/cdprocessor
  tmux new-session -d -s cdprocessor
fi
