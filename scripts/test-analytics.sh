#!/bin/bash
# Test analytics features with real data

set -e

echo "=== Dandori Analytics Integration Test ==="
echo ""

# Check if PostgreSQL is available
if ! command -v psql &> /dev/null; then
    echo "psql not found. Install PostgreSQL client or use docker."
    exit 1
fi

# Configuration
DB_HOST="${DANDORI_DB_HOST:-localhost}"
DB_NAME="${DANDORI_DB_NAME:-dandori}"
DB_USER="${DANDORI_DB_USER:-dandori}"
DB_PASSWORD="${DANDORI_DB_PASSWORD:-dandori}"
SERVER_URL="${DANDORI_SERVER_URL:-http://localhost:8080}"

export PGPASSWORD="$DB_PASSWORD"

echo "1. Checking database connection..."
if ! psql -h "$DB_HOST" -U "$DB_USER" -d "$DB_NAME" -c "SELECT 1" > /dev/null 2>&1; then
    echo "   ERROR: Cannot connect to PostgreSQL"
    echo "   Start PostgreSQL: docker-compose up -d postgres"
    exit 1
fi
echo "   Connected to $DB_NAME@$DB_HOST"

echo ""
echo "2. Seeding test data..."
psql -h "$DB_HOST" -U "$DB_USER" -d "$DB_NAME" -f scripts/seed-test-data.sql
echo "   Done"

echo ""
echo "3. Verifying data..."
echo "   Runs:"
psql -h "$DB_HOST" -U "$DB_USER" -d "$DB_NAME" -c \
    "SELECT agent_name, COUNT(*) as runs, SUM(cost_usd)::numeric(10,2) as total_cost FROM runs GROUP BY agent_name ORDER BY total_cost DESC"

echo ""
echo "   Events by layer:"
psql -h "$DB_HOST" -U "$DB_USER" -d "$DB_NAME" -c \
    "SELECT layer, event_type, COUNT(*) FROM events GROUP BY layer, event_type ORDER BY layer, count DESC"

echo ""
echo "4. Testing server API (if running)..."
if curl -s "$SERVER_URL/health" > /dev/null 2>&1; then
    echo "   Server is running at $SERVER_URL"

    echo ""
    echo "   GET /api/analytics/agents"
    curl -s "$SERVER_URL/api/analytics/agents" | jq .

    echo ""
    echo "   GET /api/analytics/cost?group_by=agent"
    curl -s "$SERVER_URL/api/analytics/cost?group_by=agent" | jq .

    echo ""
    echo "   GET /api/analytics/sprints/4"
    curl -s "$SERVER_URL/api/analytics/sprints/4" | jq .

    echo ""
    echo "   GET /api/analytics/agents/compare?agents=alpha,beta,gamma"
    curl -s "$SERVER_URL/api/analytics/agents/compare?agents=alpha,beta,gamma" | jq .
else
    echo "   Server not running. Start with: make build-server && ./bin/dandori-server"
    echo "   Or: docker-compose up -d"
fi

echo ""
echo "5. Testing CLI analytics (if server running)..."
if curl -s "$SERVER_URL/health" > /dev/null 2>&1; then
    export DANDORI_SERVER_URL="$SERVER_URL"

    echo ""
    echo "   dandori analytics agents"
    ./bin/dandori analytics agents 2>/dev/null || echo "   (build CLI first: make build)"

    echo ""
    echo "   dandori analytics cost --group-by agent"
    ./bin/dandori analytics cost --group-by agent 2>/dev/null || true

    echo ""
    echo "   dandori analytics sprint 4"
    ./bin/dandori analytics sprint 4 2>/dev/null || true
fi

echo ""
echo "=== Test Complete ==="
