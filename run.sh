#!/bin/bash
set -e

ROOT="$(cd "$(dirname "$0")" && pwd)"

RUN_GUI=true
SEED=false
KEEP_DATA=false

usage() {
  cat <<EOF
Usage: ./run.sh [--cli] [--seed] [--keep-data]

Auto-detects a deployed server via Terraform output (terraform output -raw server_address).
If none is found, falls back to local mode (starts DynamoDB Local + LocalStack S3 + gRPC server).

Flags:
  --cli        Run the CLI client instead of the GUI
  --seed       (deployed mode only) Seed the deployed DynamoDB (real AWS credentials)
  --keep-data  (local mode only) Keep existing local DynamoDB/S3 state

Env:
  SERVER_ADDR        Override detected server address (host:port)
  NEUDFS_SERVER_ADDR Override GUI server target (host:port)
EOF
}

for arg in "$@"; do
  case "$arg" in
    --cli) RUN_GUI=false ;;
    --seed) SEED=true ;;
    --keep-data) KEEP_DATA=true ;;
    -h|--help) usage; exit 0 ;;
  esac
done

# ---------------------------------------------------------------------------
# Resolve server address (deployed vs local)
# ---------------------------------------------------------------------------
strip_ansi() {
  # Remove ANSI escape sequences (common in Terraform warnings).
  sed -E 's/\x1B\[[0-9;]*[[:alpha:]]//g'
}

is_likely_hostport() {
  local v="$1"
  if [ -z "$v" ]; then
    return 1
  fi
  if echo "$v" | rg -q 'Warning:|No outputs found|╷|│'; then
    return 1
  fi
  if echo "$v" | rg -q '://'; then
    return 1
  fi
  if echo "$v" | rg -q '[[:space:]]'; then
    return 1
  fi
  if echo "$v" | rg -q '^[^/]+:[0-9]+$'; then
    return 0
  fi
  return 1
}

MODE="deployed"
ADDR=""

if [ -n "${SERVER_ADDR:-}" ]; then
  ADDR="$SERVER_ADDR"
else
  if [ -d "$ROOT/terraform" ]; then
    ADDR=$(
      cd "$ROOT/terraform" \
        && terraform output -no-color -raw server_address 2>/dev/null \
        | strip_ansi \
        | tr -d '\r' \
        | head -n 1 \
        | xargs \
        || true
    )
    if ! is_likely_hostport "$ADDR"; then
      ADDR=""
    fi
  fi
fi

if [ -z "$ADDR" ]; then
  MODE="local"
  ADDR="127.0.0.1:50051"
fi

echo "==> Mode: $MODE"
echo "==> Server: $ADDR"

# ---------------------------------------------------------------------------
# Local infra (only in local mode)
# ---------------------------------------------------------------------------
SERVER_PID=""
DYNAMO_STARTED=false
S3_STARTED=false

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
  if [ "$S3_STARTED" = true ]; then
    echo "    Stopping LocalStack S3 container..."
    docker stop neudfs-s3 > /dev/null 2>&1 && docker rm neudfs-s3 > /dev/null 2>&1 || true
  else
    echo "    Leaving pre-existing LocalStack S3 running."
  fi
}
trap cleanup EXIT

if [ "$MODE" = "local" ]; then
  # Fake AWS credentials for local DynamoDB / LocalStack
  export AWS_ACCESS_KEY_ID="fake"
  export AWS_SECRET_ACCESS_KEY="fake"
  export AWS_SESSION_TOKEN="fake"
  export AWS_DEFAULT_REGION="us-east-1"
  export DYNAMODB_ENDPOINT="http://localhost:8000"
  export S3_ENDPOINT="http://localhost:4566"
  export S3_BUCKET="neudfs-storage-dev"

  echo "==> Starting local DynamoDB..."
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

  echo "==> Starting local S3 (LocalStack)..."
  if curl -s http://localhost:4566/_localstack/health > /dev/null 2>&1; then
    echo "    LocalStack already running on port 4566, reusing."
  else
    if docker ps -a --format '{{.Names}}' | grep -q '^neudfs-s3$'; then
      CURRENT_IMAGE=$(docker inspect -f '{{.Config.Image}}' neudfs-s3)
      if [ "$CURRENT_IMAGE" != "localstack/localstack:3.8" ]; then
        echo "    Found neudfs-s3 using wrong image ($CURRENT_IMAGE). Recreating..."
        docker rm -f neudfs-s3 > /dev/null
        docker run -d --name neudfs-s3 -p 4566:4566 localstack/localstack:3.8 > /dev/null
      else
        docker start neudfs-s3 > /dev/null
      fi
      S3_STARTED=true
    else
      docker run -d --name neudfs-s3 -p 4566:4566 localstack/localstack:3.8 > /dev/null
      S3_STARTED=true
    fi
  fi

  echo "==> Waiting for LocalStack S3 to be ready..."
  until curl -s http://localhost:4566/_localstack/health > /dev/null 2>&1; do sleep 0.5; done
  echo "    S3 ready."

  echo "==> Ensuring S3 bucket exists..."
  aws --endpoint-url http://localhost:4566 s3 mb s3://neudfs-storage-dev --region us-east-1 > /dev/null 2>&1 || true

  if [ "$KEEP_DATA" = true ]; then
    echo "==> Keeping existing data (--keep-data flag)."
  else
    echo "==> Wiping and re-seeding tables..."
    aws --endpoint-url http://localhost:4566 s3 rm "s3://$S3_BUCKET" --recursive > /dev/null 2>&1 || true
    for table in user classroom_metadata; do
      aws dynamodb delete-table \
        --endpoint-url http://localhost:8000 \
        --table-name "$table" \
        --region us-east-1 > /dev/null 2>&1 || true
    done
    sleep 1
    (cd "$ROOT/test" && go run seed_db.go)
  fi

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

  mkdir -p "$ROOT/.bin"
  echo "==> Building server..."
  go build -o "$ROOT/.bin/server" "$ROOT/server"
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
    if ! kill -0 "$SERVER_PID" 2>/dev/null; then
      echo "    ERROR: Server process died."
      exit 1
    fi
    sleep 0.5
  done
fi

# ---------------------------------------------------------------------------
# Seed deployed DynamoDB (opt-in)
# ---------------------------------------------------------------------------
if [ "$MODE" = "deployed" ] && [ "$SEED" = true ]; then
  echo "==> Seeding deployed DynamoDB (real AWS credentials)..."
  (cd "$ROOT/test" && go run seed_db.go)
  echo "    Seed done."
fi

# ---------------------------------------------------------------------------
# Build + run client / GUI
# ---------------------------------------------------------------------------
mkdir -p "$ROOT/.bin"
if [ "$RUN_GUI" = true ]; then
  echo "==> Building GUI..."
  (cd "$ROOT/gui" && go build -o "$ROOT/.bin/gui" .)
  echo "    Build done."

  export NEUDFS_SERVER_ADDR="$ADDR"
  echo "==> Starting GUI..."
  "$ROOT/.bin/gui"
else
  echo "==> Building client..."
  go build -o "$ROOT/.bin/client" "$ROOT/client"
  echo "    Build done."

  echo "==> Starting client (type Ctrl+D to quit)..."
  echo ""
  "$ROOT/.bin/client" -addr "$ADDR"
fi
