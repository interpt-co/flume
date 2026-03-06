#!/usr/bin/env bash
set -euo pipefail

# Local end-to-end test for flume collector + aggregator.
# Creates fake CRI container logs, runs both components, opens the browser.
#
# Usage: ./scripts/local-e2e.sh [--no-browser]

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LOGDIR="/tmp/flume-test-logs"
CONFIG="/tmp/flume-collector.yaml"
PIDS=()

cleanup() {
    echo ""
    echo "==> Shutting down..."
    for pid in "${PIDS[@]}"; do
        kill "$pid" 2>/dev/null || true
    done
    rm -rf "$LOGDIR" "$CONFIG"
    echo "==> Cleaned up. Done."
}
trap cleanup EXIT INT TERM

# --- Setup ---

rm -rf "$LOGDIR"
mkdir -p "$LOGDIR"

cat > "$CONFIG" << 'EOF'
collector:
  logDir: /tmp/flume-test-logs
  bufferSize: 5000
  aggregator:
    addr: localhost:9090
  patterns:
    - name: all
      selector:
        matchLabels: {}
    - name: api
      selector:
        matchLabels:
          app: api-gateway
    - name: workers
      selector:
        matchLabels:
          app: worker
EOF

echo "==> Config written to $CONFIG"
echo "==> Log directory: $LOGDIR"

# --- Build ---

echo "==> Building flume (frontend + backend)..."
cd "$ROOT"
make build

# --- Start aggregator ---

echo "==> Starting aggregator on :8080 (gRPC :9090)..."
./bin/flume aggregator --port 8080 --grpc-port 9090 --verbose &
PIDS+=($!)
sleep 1

# --- Start collector ---

echo "==> Starting collector..."
./bin/flume collector --config "$CONFIG" --verbose &
PIDS+=($!)
sleep 1

# --- Log generators ---

generate_api_logs() {
    local file="$LOGDIR/api-gateway-7f8d9c-abc12_default_gateway-aabbccdd11223344.log"
    local i=0
    local endpoints=("/api/users" "/api/orders" "/api/products" "/api/health" "/api/auth/login" "/api/search")
    local methods=("GET" "POST" "PUT" "DELETE" "PATCH")
    local statuses=("200" "200" "200" "201" "204" "400" "401" "404" "500")
    while true; do
        i=$((i + 1))
        local ts
        ts=$(date -u +%Y-%m-%dT%H:%M:%S.%NZ)
        local method=${methods[$((RANDOM % ${#methods[@]}))]}
        local endpoint=${endpoints[$((RANDOM % ${#endpoints[@]}))]}
        local status=${statuses[$((RANDOM % ${#statuses[@]}))]}
        local latency=$((RANDOM % 800 + 5))
        echo "${ts} stdout F {\"level\":\"info\",\"method\":\"${method}\",\"path\":\"${endpoint}\",\"status\":${status},\"latency_ms\":${latency},\"request_id\":\"req-$(printf '%04x' $i)\"}" >> "$file"
        sleep "0.$((RANDOM % 5 + 1))"
    done
}

generate_worker_logs() {
    local file="$LOGDIR/task-worker-5c4b3a_production_worker-eeff00112233.log"
    local i=0
    local tasks=("email.send" "report.generate" "image.resize" "cache.rebuild" "data.export" "notification.push")
    while true; do
        i=$((i + 1))
        local ts
        ts=$(date -u +%Y-%m-%dT%H:%M:%S.%NZ)
        local task=${tasks[$((RANDOM % ${#tasks[@]}))]}
        local duration=$((RANDOM % 5000 + 100))
        if ((RANDOM % 10 == 0)); then
            echo "${ts} stderr F {\"level\":\"error\",\"task\":\"${task}\",\"job_id\":${i},\"error\":\"timeout after ${duration}ms\",\"retry\":true}" >> "$file"
        else
            echo "${ts} stdout F {\"level\":\"info\",\"task\":\"${task}\",\"job_id\":${i},\"duration_ms\":${duration},\"status\":\"completed\"}" >> "$file"
        fi
        sleep "0.$((RANDOM % 8 + 2))"
    done
}

generate_noisy_logs() {
    local file="$LOGDIR/nginx-ingress-ctrl-9d8e7f_ingress-nginx_controller-deadbeefcafe.log"
    local ips=("10.0.1.15" "10.0.2.30" "10.0.3.45" "192.168.1.100" "172.16.0.50")
    while true; do
        local ts
        ts=$(date -u +%Y-%m-%dT%H:%M:%S.%NZ)
        local ip=${ips[$((RANDOM % ${#ips[@]}))]}
        local code=$((RANDOM % 3 == 0 ? 302 : 200))
        echo "${ts} stdout F ${ip} - - [$(date -u +%d/%b/%Y:%H:%M:%S\ +0000)] \"GET /healthz HTTP/1.1\" ${code} 0 \"-\" \"kube-probe/1.28\"" >> "$file"
        sleep "0.$((RANDOM % 3 + 1))"
    done
}

generate_multiline_logs() {
    # Simulates a Java app with occasional stack traces using CRI partial lines
    local file="$LOGDIR/payment-svc-2a3b4c_default_payment-1122334455667788.log"
    local i=0
    while true; do
        i=$((i + 1))
        local ts
        ts=$(date -u +%Y-%m-%dT%H:%M:%S.%NZ)
        if ((RANDOM % 8 == 0)); then
            # Multiline stack trace using CRI partial (P) lines
            echo "${ts} stderr P java.lang.NullPointerException: payment failed" >> "$file"
            sleep 0.01
            echo "${ts} stderr P     at com.example.PaymentService.process(PaymentService.java:$((RANDOM % 200 + 10)))" >> "$file"
            sleep 0.01
            echo "${ts} stderr P     at com.example.OrderHandler.handle(OrderHandler.java:$((RANDOM % 100 + 5)))" >> "$file"
            sleep 0.01
            echo "${ts} stderr F     at com.example.Main.main(Main.java:42)" >> "$file"
        else
            local amount=$((RANDOM % 10000 + 100))
            echo "${ts} stdout F {\"level\":\"info\",\"service\":\"payment\",\"tx_id\":\"tx-$(printf '%06x' $i)\",\"amount\":${amount},\"currency\":\"EUR\",\"status\":\"ok\"}" >> "$file"
        fi
        sleep "0.$((RANDOM % 6 + 3))"
    done
}

echo "==> Starting log generators (4 simulated pods)..."
generate_api_logs &
PIDS+=($!)
generate_worker_logs &
PIDS+=($!)
generate_noisy_logs &
PIDS+=($!)
generate_multiline_logs &
PIDS+=($!)

echo ""
echo "============================================"
echo "  Flume local E2E test running!"
echo ""
echo "  Web UI:     http://localhost:8080"
echo "  Aggregator: localhost:8080 (HTTP) / :9090 (gRPC)"
echo "  Log dir:    $LOGDIR"
echo ""
echo "  Simulated pods:"
echo "    - api-gateway (default/gateway)     -> JSON API logs"
echo "    - task-worker  (production/worker)   -> async job logs"
echo "    - nginx-ingress (ingress-nginx/ctrl) -> access logs"
echo "    - payment-svc  (default/payment)     -> multiline stack traces"
echo ""
echo "  Patterns configured: all, api, workers"
echo "  (Without K8s API, only 'all' matches — labels are empty)"
echo ""
echo "  Press Ctrl+C to stop."
echo "============================================"
echo ""

# Open browser unless --no-browser
if [[ "${1:-}" != "--no-browser" ]]; then
    if command -v xdg-open &>/dev/null; then
        xdg-open "http://localhost:8080" 2>/dev/null &
    elif command -v open &>/dev/null; then
        open "http://localhost:8080" &
    fi
fi

# Wait for any child to exit
wait
