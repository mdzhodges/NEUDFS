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
SKIP_GRAFANA=false
for arg in "$@"; do
  case $arg in
    --skip-seed)    SKIP_SEED=true ;;
    --skip-test)    SKIP_TEST=true ;;
    --skip-k6)      SKIP_K6=true ;;
    --skip-grafana) SKIP_GRAFANA=true ;;
  esac
done

# ─────────────────────────────────────────────────────
# Grafana + InfluxDB
# ─────────────────────────────────────────────────────
if [ "$SKIP_GRAFANA" = true ]; then
  echo "==> Skipping Grafana/InfluxDB (--skip-grafana flag)."
  INFLUX_OUT=""
else
  echo "==> Starting InfluxDB and Grafana containers..."
  if ! docker info > /dev/null 2>&1; then
    echo "    WARNING: Docker not running — skipping Grafana setup."
    INFLUX_OUT=""
  else
    if ! docker ps --format '{{.Names}}' | grep -q '^influxdb$'; then
      docker rm -f influxdb > /dev/null 2>&1 || true
      docker run -d --name influxdb -p 8086:8086 \
        -v influxdb-k6-data:/var/lib/influxdb \
        influxdb:1.8 > /dev/null
    fi
    if ! docker ps --format '{{.Names}}' | grep -q '^grafana$'; then
      docker rm -f grafana > /dev/null 2>&1 || true
      docker run -d --name grafana -p 3000:3000 \
        -e GF_AUTH_ANONYMOUS_ENABLED=true \
        -e GF_AUTH_ANONYMOUS_ORG_ROLE=Admin \
        grafana/grafana > /dev/null
    fi
    echo "    InfluxDB: http://localhost:8086"
    echo "    Grafana:  http://localhost:3000  (import dashboard ID 2587)"
    echo "    Waiting for InfluxDB to be ready..."
    for i in $(seq 1 20); do
      if curl -s http://localhost:8086/ping > /dev/null 2>&1; then
        echo "    InfluxDB ready."
        break
      fi
      sleep 1
    done
    INFLUX_OUT="--out influxdb=http://localhost:8086/k6"
  fi
fi

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

PROFESSOR_EMAIL=$(aws dynamodb get-item \
  --table-name "$METADATA_TABLE" \
  --key '{"pk":{"S":"CS5010"},"sk":{"S":"class_info"}}' \
  --query 'Item.professor.S' \
  --output text)

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
  echo "    Dashboard: http://localhost:5665"
  K6_WEB_DASHBOARD=true \
  K6_WEB_DASHBOARD_EXPORT=k6_report.html \
  k6 run --out json=k6_results.json \
    ${INFLUX_OUT} \
    -e NLB_ADDR="$NLB_DNS" \
    -e PROFESSOR_EMAIL="$PROFESSOR_EMAIL" \
    k6/load_test.js
fi

echo ""
echo "==> Done! Connect with:"
echo "    ./grpc-client -addr \"$NLB_DNS\""
echo "==> K6 report saved to: $ROOT/k6_report.html"