#!/usr/bin/env bash
# Security Simulation Script — gunakan untuk TESTING server sendiri saja!
set -e

TARGET="${1:-http://localhost:8081}"
PASS=0
FAIL=0

green() { echo -e "\033[32m[PASS]\033[0m $1"; }
red()   { echo -e "\033[31m[FAIL]\033[0m $1"; }
info()  { echo -e "\033[36m[INFO]\033[0m $1"; }

echo ""
echo "╔═══════════════════════════════════════════════╗"
echo "║    SECURITY SIMULATION — 7 Layer Stress Test   ║"
echo "║    Target: $TARGET"
echo "╚═══════════════════════════════════════════════╝"
echo ""

# ─── Layer 1: Rate Limiting (DDoS) ───
info "Layer 1 — Rate Limiting (DDoS: 200 concurrent requests)"
BLOCKED=0; ALLOWED=0
for i in $(seq 1 200); do
  (curl -s -o /dev/null -w "%{http_code}" --max-time 5 "$TARGET/api/proxy?url=https://example.com/video.mp4" 2>/dev/null || echo "000") > /tmp/rl_$i.txt &
done
wait
for i in $(seq 1 200); do
  CODE=$(cat /tmp/rl_$i.txt 2>/dev/null || echo "000")
  [ "$CODE" = "429" ] && BLOCKED=$((BLOCKED+1))
  [ "$CODE" != "429" ] && [ "$CODE" != "000" ] && ALLOWED=$((ALLOWED+1))
  rm -f /tmp/rl_$i.txt
done
info "Allowed: $ALLOWED, Blocked: $BLOCKED"
if [ "$BLOCKED" -gt 0 ]; then
  green "Rate limiter blocked $BLOCKED requests"
  PASS=$((PASS+1))
else
  red "Rate limiter did not block any requests"
  FAIL=$((FAIL+1))
fi

# ─── Layer 2: SSRF Protection ───
info "Layer 2 — SSRF Protection"
for URL in \
  "http://localhost:8081/secret" \
  "http://127.0.0.1:22/" \
  "http://10.0.0.1/admin" \
  "http://192.168.1.1/config" \
  "http://169.254.169.254/latest/meta-data" \
  "http://[::1]:8081"; do
  ENCODED=$(python3 -c "import urllib.parse; print(urllib.parse.quote('$URL'))" 2>/dev/null || echo "$URL")
  STATUS=$(curl -s -o /dev/null -w "%{http_code}" --max-time 3 "$TARGET/api/proxy?url=$ENCODED" 2>/dev/null || echo "000")
  if [ "$STATUS" = "403" ]; then
    green "SSRF blocked: $(echo "$URL" | head -c 40)... → $STATUS"
    PASS=$((PASS+1))
  else
    red "SSRF NOT blocked: $(echo "$URL" | head -c 40)... → $STATUS"
    FAIL=$((FAIL+1))
  fi
done

# ─── Layer 3: Input Validation ───
info "Layer 3 — Input Validation"
for URL in \
  'https://evil.com?cmd=;rm%20-rf%20/' \
  'https://x.com?id=$(cat%20/etc/passwd)' \
  'https://x.com?id=`id`' \
  'https://x.com?a=1|whoami'; do
  STATUS=$(curl -s -o /dev/null -w "%{http_code}" --max-time 3 "$TARGET/api/proxy?url=$URL" 2>/dev/null || echo "000")
  if [ "$STATUS" = "400" ]; then
    green "Injection blocked: $(echo "$URL" | head -c 40)... → $STATUS"
    PASS=$((PASS+1))
  else
    red "Injection NOT blocked: $(echo "$URL" | head -c 40)... → $STATUS"
    FAIL=$((FAIL+1))
  fi
done

# ─── Layer 4: Security Headers ───
info "Layer 4 — Security Headers"
HEADERS=$(curl -s -I --max-time 3 "$TARGET/api/stream/video?url=" 2>/dev/null)
for H in "X-Content-Type-Options: nosniff" "X-Frame-Options: DENY" "X-XSS-Protection: 1; mode=block" "Referrer-Policy: strict-origin-when-cross-origin"; do
  NAME="${H%%:*}"
  VAL="${H#*: }"
  if echo "$HEADERS" | grep -qi "^$NAME:.*$VAL"; then
    green "Header $NAME"
    PASS=$((PASS+1))
  else
    red "Header $NAME missing"
    FAIL=$((FAIL+1))
  fi
done

# ─── Layer 5: Large Payload ───
info "Layer 5 — Large Payload"
LARGE=$(python3 -c "print('a'*10000)" 2>/dev/null || printf 'a%.0s' $(seq 10000))
STATUS=$(curl -s -o /dev/null -w "%{http_code}" --max-time 5 -G --data-urlencode "url=https://x.com/$LARGE" "$TARGET/api/proxy" 2>/dev/null || echo "000")
if [ "$STATUS" = "400" ] || [ "$STATUS" = "414" ] || [ "$STATUS" = "431" ]; then
  green "Large URL rejected: $STATUS"
  PASS=$((PASS+1))
else
  red "Large URL accepted: $STATUS"
  FAIL=$((FAIL+1))
fi

# ─── Layer 6: Invalid Schemes ───
info "Layer 6 — Scheme Validation"
for SCHEME in "ftp" "file" "data" "javascript"; do
  STATUS=$(curl -s -o /dev/null -w "%{http_code}" --max-time 3 "$TARGET/api/proxy?url=$SCHEME://evil.com/file" 2>/dev/null || echo "000")
  if [ "$STATUS" = "400" ] || [ "$STATUS" = "403" ]; then
    green "Blocked: $SCHEME:// → $STATUS"
    PASS=$((PASS+1))
  else
    red "NOT blocked: $SCHEME:// → $STATUS"
    FAIL=$((FAIL+1))
  fi
done

# ─── Layer 7: Server Stability ───
info "Layer 7 — Server Stability (server masih hidup?)"
STATUS=$(curl -s -o /dev/null -w "%{http_code}" --max-time 5 "$TARGET/api/proxy?url=https://example.com/video.mp4" 2>/dev/null || echo "000")
if [ "$STATUS" != "000" ]; then
  green "Server masih merespon setelah semua serangan — Status: $STATUS"
  PASS=$((PASS+1))
else
  red "Server tidak merespon!"
  FAIL=$((FAIL+1))
fi

# ─── Final Report ───
echo ""
echo "╔═══════════════════════════════════════════════╗"
echo "║              FINAL REPORT                      ║"
echo "╠═══════════════════════════════════════════════╣"
echo "║  PASSED: $PASS"
echo "║  FAILED: $FAIL"
echo "╚═══════════════════════════════════════════════╝"

[ "$FAIL" -gt 0 ] && exit 1 || exit 0
