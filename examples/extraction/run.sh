#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

if [ -n "${MEMORY_DB:-}" ]; then
  DB="$MEMORY_DB"
else
  DB="$(mktemp -t memorycore-extraction.XXXXXX.db)"
fi

resolve_go() {
  if command -v go >/dev/null 2>&1; then
    command -v go
    return
  fi
  if command -v bash >/dev/null 2>&1; then
    FOUND="$(bash -lc 'command -v go' 2>/dev/null || true)"
    if [ -n "$FOUND" ]; then
      printf '%s\n' "$FOUND"
      return
    fi
  fi
  echo "go not found" >&2
  exit 127
}

GO_BIN="${GO_BIN:-$(resolve_go)}"
CMD=("$GO_BIN" run ./cmd/memoryctl)
WORKDIR="$(mktemp -d -t memorycore-extraction-smoke.XXXXXX)"

render_response() {
  local template="$1"
  local output="$2"
  sed -e "s/req_example/$REQUEST_ID/g" "$template" > "$output"
}

"${CMD[@]}" init-db --db "$DB" --enable-fts=true >/dev/null
"${CMD[@]}" start-session --db "$DB" --id session_seed --channel cli --format id >/dev/null
"${CMD[@]}" append-episode --db "$DB" --id ep_seed --session session_seed \
  --role user --content "我不喜欢早上八点开会。" --format id >/dev/null
"${CMD[@]}" ensure-entity --db "$DB" --id ent_user --name User --type user --format id >/dev/null

"${CMD[@]}" extract-request \
  --db "$DB" \
  --session session_seed \
  --trigger session_end \
  --format json > "$WORKDIR/request.json"

REQUEST_ID="$(sed -n 's/.*"request_id":"\([^"]*\)".*/\1/p' "$WORKDIR/request.json")"
if [ -z "$REQUEST_ID" ]; then
  echo "failed to parse request_id" >&2
  exit 1
fi

render_response "examples/extraction/explicit_preference_response.json" "$WORKDIR/explicit_response.json"
"${CMD[@]}" extract-dry-run --db "$DB" --request "$WORKDIR/request.json" --response "$WORKDIR/explicit_response.json" --format text | grep -q "f1"
"${CMD[@]}" extract-apply --db "$DB" --request "$WORKDIR/request.json" --response "$WORKDIR/explicit_response.json" --format json | grep -q '"status":"applied"'
"${CMD[@]}" retrieve --db "$DB" --query "早上八点" --format text | grep -q "用户不喜欢早上八点开会。"

"${CMD[@]}" extract-request \
  --db "$DB" \
  --session session_seed \
  --trigger manual_forget \
  --format json > "$WORKDIR/forget_request.json"
REQUEST_ID="$(sed -n 's/.*"request_id":"\([^"]*\)".*/\1/p' "$WORKDIR/forget_request.json")"
render_response "examples/extraction/manual_forget_response.json" "$WORKDIR/forget_response.json"
"${CMD[@]}" extract-dry-run --db "$DB" --request "$WORKDIR/forget_request.json" --response "$WORKDIR/forget_response.json" --format text | grep -q "route_to=forget_manager"

"${CMD[@]}" extract-request \
  --db "$DB" \
  --session session_seed \
  --trigger session_end \
  --format json > "$WORKDIR/agent_request.json"
REQUEST_ID="$(sed -n 's/.*"request_id":"\([^"]*\)".*/\1/p' "$WORKDIR/agent_request.json")"
render_response "examples/extraction/agent_affect_reject_response.json" "$WORKDIR/agent_response.json"
if "${CMD[@]}" extract-dry-run --db "$DB" --request "$WORKDIR/agent_request.json" --response "$WORKDIR/agent_response.json" --format text --fail-on-reject > "$WORKDIR/agent_dry_run.txt" 2>&1; then
  echo "agent affect dry-run unexpectedly passed with --fail-on-reject" >&2
  exit 1
fi
grep -q "agent_affect_boundary" "$WORKDIR/agent_dry_run.txt"

echo "SMOKE PASS"
