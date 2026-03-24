#!/bin/bash
# Seed script: creates test sessions via the API for quick PoC validation.
# Usage: ./scripts/seed.sh [API_BASE_URL]

API="${1:-http://localhost:8080}"

echo "=== Creating bearer token session ==="
curl -s -X POST "$API/api/sessions" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "my-api-session",
    "target_host": "httpbin.org",
    "auth_type": "bearer",
    "credentials": {
      "token": "my-secret-jwt-token-here"
    }
  }' | jq .

echo ""
echo "=== Creating cookie session ==="
curl -s -X POST "$API/api/sessions" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "my-cookie-session",
    "target_host": "example.com",
    "auth_type": "cookie",
    "credentials": {
      "session_id": "abc123",
      "csrf_token": "xyz789"
    }
  }' | jq .

echo ""
echo "=== Listing sessions ==="
curl -s "$API/api/sessions" | jq .
