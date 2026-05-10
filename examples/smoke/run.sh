#!/usr/bin/env bash
set -euo pipefail

if [ -n "${MEMORY_DB:-}" ]; then
  DB="$MEMORY_DB"
else
  DB="$(mktemp -t memorycore-smoke.XXXXXX.db)"
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

"${CMD[@]}" init-db --db "$DB" --enable-fts=true >/dev/null

SESSION_ID=$("${CMD[@]}" start-session --db "$DB" --channel cli --format id)
EPISODE_ID=$("${CMD[@]}" append-episode --db "$DB" --session "$SESSION_ID" \
  --role user --content "我不喜欢早上八点开会。" --format id)
USER_ID=$("${CMD[@]}" ensure-entity --db "$DB" --id ent_user --name User --type user --format id)
FACT_ID=$("${CMD[@]}" consolidate-fact --db "$DB" \
  --subject "$USER_ID" \
  --predicate dislikes \
  --object-literal "早上八点开会" \
  --summary "用户不喜欢早上八点开会。" \
  --fact-type stable_preference \
  --source-episode "$EPISODE_ID" \
  --confidence explicit \
  --importance 0.7 \
  --valence -0.55 \
  --arousal 0.35 \
  --format id)

BEFORE=$("${CMD[@]}" retrieve --db "$DB" --query "早上八点" --format text)
echo "$BEFORE" | grep -q "$FACT_ID"
echo "$BEFORE" | grep -q "用户不喜欢早上八点开会。"

"${CMD[@]}" forget --db "$DB" --level hard_forget --node-type fact --node-id "$FACT_ID" >/dev/null
"${CMD[@]}" rebuild-search --db "$DB" >/dev/null

AFTER_NODE=$("${CMD[@]}" get-node --db "$DB" --node-type fact --id "$FACT_ID" --include-forgotten --format text)
echo "$AFTER_NODE" | grep -q "visibility_status=forgotten"
echo "$AFTER_NODE" | grep -Eq "searchable=false|searchable=0"
if echo "$AFTER_NODE" | grep -q "早上八点开会"; then
  echo "forgotten semantic content leaked from get-node" >&2
  exit 1
fi

AFTER_LIST=$("${CMD[@]}" list-facts --db "$DB" --format text)
if echo "$AFTER_LIST" | grep -q "$FACT_ID"; then
  echo "forgotten fact leaked from default list-facts" >&2
  exit 1
fi

AFTER_RETRIEVE=$("${CMD[@]}" retrieve --db "$DB" --query "早上八点" --format text)
if echo "$AFTER_RETRIEVE" | grep -q "用户不喜欢早上八点开会。"; then
  echo "forgotten memory leaked after hard_forget" >&2
  exit 1
fi

echo "SMOKE PASS"
