#!/bin/bash
set -e

ROOT="$(cd "$(dirname "$0")" && pwd)"

# Clear local env vars so we hit real AWS
unset DYNAMODB_ENDPOINT
unset S3_ENDPOINT

# Get outputs from Terraform
echo "==> Reading Terraform outputs..."
cd "$ROOT/terraform"
S3_BUCKET=$(terraform output -raw s3_bucket_id)
NLB_DNS=$(terraform output -raw server_address)
USER_TABLE=$(terraform output -raw user_table_name)
METADATA_TABLE=$(terraform output -raw metadata_table_name)

export S3_BUCKET
export DYNAMODB_USER_TABLE="$USER_TABLE"
export DYNAMODB_METADATA_TABLE="$METADATA_TABLE"

echo "    S3 Bucket:      $S3_BUCKET"
echo "    NLB Address:    $NLB_DNS"
echo "    User Table:     $USER_TABLE"
echo "    Metadata Table: $METADATA_TABLE"
cd "$ROOT"

# Parse flags
SKIP_SEED=false
SKIP_TEST=false
for arg in "$@"; do
  case $arg in
    --skip-seed) SKIP_SEED=true ;;
    --skip-test) SKIP_TEST=true ;;
  esac
done

# ─────────────────────────────────────────────────────
# Seed
# ─────────────────────────────────────────────────────
if [ "$SKIP_SEED" = true ]; then
  echo "==> Skipping seed (--skip-seed flag)."
else
  echo "==> Seeding production database..."
  cd "$ROOT/test" && go run seed_db.go
  cd "$ROOT"
  echo "    Seed complete."
fi

# ─────────────────────────────────────────────────────
# Wait for healthy targets
# ─────────────────────────────────────────────────────
echo "==> Checking ECS target health..."
TG_ARN=$(aws elbv2 describe-target-groups --query 'TargetGroups[?starts_with(TargetGroupName, `neudfs`)].TargetGroupArn' --output text | head -1)

for i in $(seq 1 20); do
  HEALTH=$(aws elbv2 describe-target-health --target-group-arn "$TG_ARN" --query 'TargetHealthDescriptions[0].TargetHealth.State' --output text)
  if [ "$HEALTH" = "healthy" ]; then
    echo "    Targets healthy."
    break
  fi
  echo "    Target state: $HEALTH (attempt $i/20)..."
  if [ "$i" = "20" ]; then
    echo "    ERROR: Targets not healthy after 60 seconds."
    exit 1
  fi
  sleep 3
done

# ─────────────────────────────────────────────────────
# Run tests against AWS
# ─────────────────────────────────────────────────────
if [ "$SKIP_TEST" = true ]; then
  echo "==> Skipping tests (--skip-test flag)."
else
  echo "==> Running tests against AWS..."
  NLB_ADDR=$NLB_DNS TEST_ENV=aws-remote go test ./server -v -count=1 -timeout 120s
  echo ""
  echo "==> Tests complete."
fi

# ─────────────────────────────────────────────────────
# Print connection info
# ─────────────────────────────────────────────────────
echo ""
echo "==> Deployment ready!"
echo "    Connect with:"
echo "    ./grpc-client -addr \"$NLB_DNS\""
echo ""

# Print sample emails
echo "==> Sample login emails:"
for role in professor student; do
  EMAIL=$(aws dynamodb scan \
    --table-name "$USER_TABLE" \
    --filter-expression "#r = :role" \
    --expression-attribute-names '{"#r":"role"}' \
    --expression-attribute-values "{\":role\":{\"S\":\"$role\"}}" \
    --projection-expression "email" \
    --max-items 1 \
    --query 'Items[0].email.S' \
    --output text 2>/dev/null)
  if [ "$EMAIL" != "None" ] && [ -n "$EMAIL" ]; then
    echo "    [$role] $EMAIL"
  fi
done
echo ""