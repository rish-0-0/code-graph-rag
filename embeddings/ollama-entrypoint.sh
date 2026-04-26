#!/bin/sh
# Start ollama in the background, pull the configured model, then keep serving.
set -e
ollama serve &
SERVER_PID=$!

# Wait for the daemon to come up.
for i in $(seq 1 30); do
  if ollama list >/dev/null 2>&1; then
    break
  fi
  sleep 1
done

MODEL="${OLLAMA_MODEL:-nomic-embed-text}"
echo "ollama: ensuring model $MODEL is pulled..."
if ! ollama pull "$MODEL"; then
  # If the model is already present locally, treat that as success.
  if ollama list | awk '{print $1}' | grep -q "^${MODEL}\(:.*\)\?$"; then
    echo "ollama: pull failed but model is already present locally — continuing."
  else
    echo "ollama: FATAL — could not pull $MODEL. Set OLLAMA_MODEL to a valid Ollama-registry name." >&2
    kill "$SERVER_PID" 2>/dev/null
    exit 1
  fi
fi

wait "$SERVER_PID"
