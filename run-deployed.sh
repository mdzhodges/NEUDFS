#!/bin/bash
set -e

ROOT="$(cd "$(dirname "$0")" && pwd)"

# Parse flags
SEED=false
for arg in "$@"; do
  case $arg in
    --seed) SEED=true ;;
  esac
done

# ---------------------------------------------------------------------------
# Resolve server address
# ---------------------------------------------------------------------------
if [ -n "$SERVER_ADDR" ]; then
  ADDR="$SERVER_ADDR"
else
  echo "==> Fetching server address from Terraform..."
  ADDR=$(cd "$ROOT/terraform" && terraform output -raw server_address 2>/dev/null)
  if [ -z "$ADDR" ]; then
    echo "ERROR: Could not get server_address from terraform output."
    echo "       Run 'terraform apply' first, or set SERVER_ADDR manually:"
    echo "       SERVER_ADDR=<host:port> ./run-deployed.sh"
    exit 1
  fi
fi

echo "==> Server: $ADDR"

# ---------------------------------------------------------------------------
# Seed deployed DynamoDB (opt-in)
# ---------------------------------------------------------------------------
if [ "$SEED" = true ]; then
  echo "==> Seeding deployed DynamoDB (real AWS credentials)..."
  (cd "$ROOT/test" && go run seed_db.go)
  echo "    Seed done."
fi

# ---------------------------------------------------------------------------
# Build client
# ---------------------------------------------------------------------------
mkdir -p "$ROOT/.bin"
echo "==> Building client..."
go build -o "$ROOT/.bin/client" "$ROOT/client"
echo "    Build done."

# ---------------------------------------------------------------------------
# Client
# ---------------------------------------------------------------------------
echo "==> Starting client (type Ctrl+D to quit)..."
echo ""
"$ROOT/.bin/client" -addr "$ADDR"
