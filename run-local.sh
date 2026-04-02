#!/bin/bash
set -e

ROOT="$(cd "$(dirname "$0")" && pwd)"

# Fake AWS credentials for local DynamoDB
export AWS_ACCESS_KEY_ID="fake"
export AWS_SECRET_ACCESS_KEY="fake"
export AWS_SESSION_TOKEN="fake"
export DYNAMODB_ENDPOINT="http://localhost:8000"

echo "==> Starting local DynamoDB..."
docker run -d --name neudfs-dynamo -p 8000:8000 amazon/dynamodb-local > /dev/null 2>&1 || {
  echo "    (container already running or name taken, skipping)"
}

echo "==> Waiting for DynamoDB to be ready..."
until curl -s http://localhost:8000 > /dev/null 2>&1; do
  sleep 0.5
done
echo "    DynamoDB ready."

echo "==> Seeding tables..."
(cd "$ROOT/test" && go run scripts.go)

echo "==> Starting gRPC server (background)..."
(cd "$ROOT/server" && go run . &)
SERVER_PID=$!
sleep 2

echo "==> Running client..."
(cd "$ROOT/client" && go run main.go)

echo "==> Done. Stopping server (PID $SERVER_PID)..."
kill $SERVER_PID 2>/dev/null || true

echo "==> Stopping DynamoDB container..."
docker stop neudfs-dynamo > /dev/null && docker rm neudfs-dynamo > /dev/null
