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
NLB_DNS=$(terraform output -raw server_address | sed 's|https\?://||')
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
SKIP_K6=false
for arg in "$@"; do
  case $arg in
    --skip-seed) SKIP_SEED=true ;;
    --skip-test) SKIP_TEST=true ;;
    --skip-k6) SKIP_K6=true ;;
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
  if ! kill -0 "$$" 2>/dev/null; then
    echo "    ERROR: Script interrupted."
    exit 1
  fi
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
  NLB_ADDR="$NLB_DNS" TEST_ENV=aws-remote go test ./server -v -count=1 -timeout 120s
  echo ""
  echo "==> Tests complete."
fi

# ─────────────────────────────────────────────────────
# Gather emails for k6
# ─────────────────────────────────────────────────────
echo "==> Gathering student and professor emails..."
aws dynamodb get-item \
  --table-name "$METADATA_TABLE" \
  --key '{"pk":{"S":"CS5010"},"sk":{"S":"class_info"}}' \
  --query 'Item.students.L[*].S' \
  --output json > k6/students.json

PROFESSOR_EMAIL=$(aws dynamodb scan \
  --table-name "$USER_TABLE" \
  --filter-expression "#r = :role" \
  --expression-attribute-names '{"#r":"role"}' \
  --expression-attribute-values '{":role":{"S":"professor"}}' \
  --projection-expression "email" \
  --query 'Items[0].email.S' \
  --output json | tr -d '" \n\r\t')

echo "==> Sample login emails:"
echo "    [professor] $PROFESSOR_EMAIL"
FIRST_STUDENT=$(cat k6/students.json | python3 -c "import sys,json; print(json.load(sys.stdin)[0])" 2>/dev/null || echo "unknown")
echo "    [student]   $FIRST_STUDENT"
echo ""

# ─────────────────────────────────────────────────────
# Run K6 load test
# ─────────────────────────────────────────────────────
if [ "$SKIP_K6" = true ]; then
  echo "==> Skipping K6 Test (--skip-k6 flag)."
else
  echo "==> Running K6 load test..."
  echo "    NLB:       $NLB_DNS"
  echo "    Professor: $PROFESSOR_EMAIL"
  k6 run --out json=k6_results.json \
    -e NLB_ADDR="$NLB_DNS" \
    -e PROFESSOR_EMAIL="$PROFESSOR_EMAIL" \
    k6/load_test.js
fi

echo ""
echo "==> Done! Connect with:"
echo "    ./grpc-client -addr \"$NLB_DNS\""