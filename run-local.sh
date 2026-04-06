#!/bin/bash
set -e

ROOT="$(cd "$(dirname "$0")" && pwd)"

# Fake AWS credentials for local DynamoDB
export AWS_ACCESS_KEY_ID="fake"
export AWS_SECRET_ACCESS_KEY="fake"
export AWS_SESSION_TOKEN="fake"
export AWS_DEFAULT_REGION="us-east-1"
export DYNAMODB_ENDPOINT="http://localhost:8000"

# Parse flags
KEEP_DATA=false
for arg in "$@"; do
  case $arg in
    --keep-data) KEEP_DATA=true ;;
  esac
done

# ---------------------------------------------------------------------------
# DynamoDB
# ---------------------------------------------------------------------------
echo "==> Starting local DynamoDB..."
DYNAMO_STARTED=false
if curl -s http://localhost:8000 > /dev/null 2>&1; then
  echo "    DynamoDB already running on port 8000, reusing."
elif docker ps -a --format '{{.Names}}' | grep -q '^neudfs-dynamo$'; then
  docker start neudfs-dynamo > /dev/null
  DYNAMO_STARTED=true
else
  docker run -d --name neudfs-dynamo -p 8000:8000 amazon/dynamodb-local > /dev/null
  DYNAMO_STARTED=true
fi

echo "==> Waiting for DynamoDB to be ready..."
until curl -s http://localhost:8000 > /dev/null 2>&1; do sleep 0.5; done
echo "    DynamoDB ready."

# ---------------------------------------------------------------------------
# Wipe and re-seed (default), or keep existing data with --keep-data
# ---------------------------------------------------------------------------
if [ "$KEEP_DATA" = true ]; then
  echo "==> Keeping existing data (--keep-data flag)."
else
  echo "==> Wiping and re-seeding tables..."
  for table in user classroom_metadata; do
    aws dynamodb delete-table \
      --endpoint-url http://localhost:8000 \
      --table-name "$table" \
      --region us-east-1 > /dev/null 2>&1 || true
  done
  sleep 1
  (cd "$ROOT/test" && go run seed_db.go)
fi

# Print a valid login email so you know what to type
echo ""
echo "==> Sample login emails from seeded data:"
for role_pair in "professor:professor" "TA:ta" "student:student"; do
  role="${role_pair%%:*}"
  label="${role_pair##*:}"
  aws dynamodb scan \
    --endpoint-url http://localhost:8000 \
    --region us-east-1 \
    --table-name user \
    --filter-expression "#r = :role" \
    --expression-attribute-names '{"#r":"role"}' \
    --expression-attribute-values "{\":role\":{\"S\":\"$role\"}}" \
    --projection-expression "email" \
    --max-items 1 \
    --query 'Items[*].email.S' \
    --output text 2>/dev/null | tr '\t' '\n' | grep -v '^None$' | grep -v '^$' | sed "s/^/    [$label] /" || true
done
echo ""

# ---------------------------------------------------------------------------
# gRPC server
# ---------------------------------------------------------------------------
SERVER_PID=""

cleanup() {
  echo ""
  echo "==> Cleaning up..."
  if [ -n "$SERVER_PID" ] && kill -0 "$SERVER_PID" 2>/dev/null; then
    echo "    Stopping server (PID $SERVER_PID)..."
    kill "$SERVER_PID" 2>/dev/null || true
  fi
  if [ "$DYNAMO_STARTED" = true ]; then
    echo "    Stopping DynamoDB container..."
    docker stop neudfs-dynamo > /dev/null 2>&1 && docker rm neudfs-dynamo > /dev/null 2>&1 || true
  else
    echo "    Leaving pre-existing DynamoDB running."
  fi
}
trap cleanup EXIT

echo "==> Building binaries..."
go build -o "$ROOT/.bin/server" "$ROOT/server" &
go build -o "$ROOT/.bin/client" "$ROOT/client" &
wait
echo "    Build done."

echo "==> Starting gRPC server..."
if nc -z localhost 50051 2>/dev/null; then
  echo "    Port 50051 already in use — killing old server..."
  kill $(lsof -ti :50051) 2>/dev/null || true
  sleep 0.5
fi
"$ROOT/.bin/server" &
SERVER_PID=$!

echo "    Waiting for server on :50051..."
for i in $(seq 1 40); do
  if nc -z localhost 50051 2>/dev/null; then
    echo "    Server ready."
    break
  fi
  sleep 0.5
done

# ---------------------------------------------------------------------------
# Client
# ---------------------------------------------------------------------------
echo "==> Starting client (type 'exit' or Ctrl+D to quit)..."
"$ROOT/.bin/client"
