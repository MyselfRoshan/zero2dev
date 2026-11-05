#!/usr/bin/env bash
# End-to-end smoke test: builds nothing, runs dist/zero2dev against a temp home.
# Requires: bash, curl, python3. Usage: make build && bash scripts/smoke.sh
set -u
cd "$(dirname "$0")/.."
BIN=${BIN:-dist/zero2dev}
[ -x "$BIN" ] || { echo "build first: make build"; exit 2; }

TMP=$(mktemp -d)
PORT=$(python3 -c 'import socket;s=socket.socket();s.bind(("127.0.0.1",0));print(s.getsockname()[1]);s.close()')
SVCPORT=$(python3 -c 'import socket;s=socket.socket();s.bind(("127.0.0.1",0));print(s.getsockname()[1]);s.close()')
# Portable mode: run a COPY of the binary with no ZERO2DEV_HOME; the data
# folder must appear next to the copy. Every user-profile style location is
# pointed at an empty fake dir that must still be empty at the end.
FAKE="$TMP/fakehome"; mkdir -p "$FAKE" "$TMP/prog"
cp "$BIN" "$TMP/prog/zero2dev"
BIN="$TMP/prog/zero2dev"
DATA="$TMP/prog/data"
unset ZERO2DEV_HOME
export HOME="$FAKE" USERPROFILE="$FAKE" APPDATA="$FAKE" LOCALAPPDATA="$FAKE" \
  XDG_DATA_HOME="$FAKE" XDG_CONFIG_HOME="$FAKE" XDG_CACHE_HOME="$FAKE" TMPDIR="$FAKE"
export ZERO2DEV_PORT=$PORT ZERO2DEV_NO_BROWSER=1
mkdir -p "$TMP/www" "$TMP/app"; echo "<h1>hi</h1>" > "$TMP/www/index.html"
FAILS=0
ok()   { echo "  ok   $1"; }
bad()  { echo "  FAIL $1"; FAILS=$((FAILS+1)); }
check() { # desc expected actual
  if [ "$2" = "$3" ]; then ok "$1"; else bad "$1 (expected '$2', got '$3')"; fi
}

"$BIN" >"$TMP/daemon.log" 2>&1 &
DPID=$!
cleanup() { kill "$DPID" 2>/dev/null; wait "$DPID" 2>/dev/null; kill "$SSEPID" 2>/dev/null; rm -rf "$TMP"; }
SSEPID=0
trap cleanup EXIT
for i in $(seq 1 50); do [ -s "$DATA/token" ] && curl -s -o /dev/null "http://127.0.0.1:$PORT/" && break; sleep 0.1; done
TOKEN=$(tr -d '\n' < "$DATA/token")
B="http://127.0.0.1:$PORT"
A=(-H "Authorization: Bearer $TOKEN")
code() { curl -s --max-time 5 -o /dev/null -w '%{http_code}' "$@"; }
jget() { python3 -c "import sys,json;d=json.load(sys.stdin);print(eval(sys.argv[1]))" "$1"; }

echo "== portable layout"
[ -d "$DATA" ] && ok "data/ created next to the binary" || bad "no $DATA"
for d in tmp caddy/data caddy/config logs config; do [ -d "$DATA/$d" ] && ok "data/$d" || bad "data/$d missing"; done
[ -d "$(dirname "$DATA")/bin" ] && ok "bin/ (runtimes) next to data/" || bad "bin/ missing next to data/"
check "status reports portable" "exe True" "$(curl -s "${A[@]}" $B/api/status | jget "d['homeSource']+' '+str(d['portable'])")"
check "status version" 0.1.0 "$(curl -s "${A[@]}" $B/api/status | jget "d['version']")"
grep -q "(portable, next to the program)" "$TMP/daemon.log" && ok "startup message names the data folder" || bad "no data folder message"

echo "== auth and hygiene"
check "401 without token" 401 "$(code $B/api/status)"
check "401 with wrong token" 401 "$(code -H 'Authorization: Bearer nope' $B/api/status)"
check "200 with token" 200 "$(code "${A[@]}" $B/api/status)"
check "403 forbidden_host (Host: evil.com)" 403 "$(code "${A[@]}" -H 'Host: evil.com' $B/api/status)"
check "403 forbidden_host on static" 403 "$(code -H 'Host: evil.com' $B/)"
check "403 forbidden_origin" 403 "$(code "${A[@]}" -H 'Origin: http://evil.com' $B/api/status)"
check "static UI needs no token" 200 "$(code $B/)"
check "SPA fallback" 200 "$(code $B/some/route)"

echo "== service lifecycle"
cat > "$TMP/app/server.py" <<PY
import http.server, sys
print("server booting", flush=True)
http.server.ThreadingHTTPServer(("127.0.0.1", $SVCPORT), http.server.SimpleHTTPRequestHandler).serve_forever()
PY
BODY=$(python3 -c "import json;print(json.dumps({'name':'py','kind':'custom','command':'python3','args':['-u','server.py'],'cwd':'$TMP/app','port':$SVCPORT,'healthPath':'/'}))")
# open the SSE stream with a ticket
TICKET=$(curl -s "${A[@]}" -X POST $B/api/events/ticket | jget "d['ticket']")
curl -sN "$B/api/events?ticket=$TICKET" > "$TMP/sse.txt" 2>/dev/null &
SSEPID=$!; sleep 0.5   # let the stream consume the ticket before we try to reuse it
check "ticket is single use" 401 "$(code "$B/api/events?ticket=$TICKET")"
sleep 0.3
SID=$(curl -s "${A[@]}" -X POST -d "$BODY" $B/api/services | jget "d['id']")
check "service created" svc_ "${SID:0:4}"


check "start 202" 202 "$(code "${A[@]}" -X POST $B/api/services/$SID/start)"
STATE=""
for i in $(seq 1 60); do
  STATE=$(curl -s "${A[@]}" $B/api/services/$SID | jget "d['state']")
  [ "$STATE" = running ] && break; sleep 0.2
done
check "state running" running "$STATE"
check "service really serves" 200 "$(code http://127.0.0.1:$SVCPORT/)"
sleep 0.3
LOGS=$(curl -s "${A[@]}" "$B/api/services/$SID/logs?tail=50" | jget "' | '.join(l['stream']+':'+l['text'] for l in d['lines'])")
case "$LOGS" in *"stdout:server booting"*) ok "logs contain stdout line";; *) bad "logs: $LOGS";; esac
check "port conflict on second start of same port" 409 "$(code "${A[@]}" -X POST $B/api/services/$SID/start)"
check "update while running is 409" 409 "$(code "${A[@]}" -X PUT -d "$BODY" $B/api/services/$SID)"
check "stop 202" 202 "$(code "${A[@]}" -X POST $B/api/services/$SID/stop)"
for i in $(seq 1 60); do
  STATE=$(curl -s "${A[@]}" $B/api/services/$SID | jget "d['state']")
  [ "$STATE" = stopped ] && break; sleep 0.2
done
check "state stopped" stopped "$STATE"
check "port released after stop" 000 "$(code --max-time 1 http://127.0.0.1:$SVCPORT/)"
kill "$SSEPID" 2>/dev/null; sleep 0.2
for ev in services.changed service.status log.line; do
  grep -q "^event: $ev" "$TMP/sse.txt" && ok "SSE delivered $ev" || bad "SSE missing $ev"
done
grep -q '"state":"running"' "$TMP/sse.txt" && ok "SSE service.status running" || bad "SSE no running status"

echo "== sites"
SITE=$(curl -s "${A[@]}" -X POST -d "{\"name\":\"Www\",\"domain\":\"www.localhost\",\"type\":\"static\",\"root\":\"$TMP/www\"}" $B/api/sites)
SITEID=$(echo "$SITE" | jget "d['id']")
check "site url" "http://www.localhost:8080" "$(echo "$SITE" | jget "d['url']")"
CF=$(cat "$DATA/config/Caddyfile")
case "$CF" in *"http://www.localhost:8080 {"*"root * $TMP/www"*"file_server"*) ok "Caddyfile has static site";; *) bad "Caddyfile: $CF";; esac
check "invalid domain rejected" 400 "$(code "${A[@]}" -X POST -d '{"name":"x","domain":"x.com","type":"proxy","upstreamPort":1}' $B/api/sites)"

echo "== cleanup"
check "delete site" 204 "$(code "${A[@]}" -X DELETE $B/api/sites/$SITEID)"
check "delete service" 204 "$(code "${A[@]}" -X DELETE $B/api/services/$SID)"
check "log file removed" "" "$(ls "$DATA/logs" 2>/dev/null)"
COUNTS=$(curl -s "${A[@]}" $B/api/status | jget "sum(d['counts'].values())")
check "all counts zero" 0 "$COUNTS"

echo "== shutdown stops children"
curl -s "${A[@]}" -X POST -d "$(python3 -c "import json;print(json.dumps({'name':'sleeper','kind':'custom','command':'sleep','args':['300'],'cwd':'$TMP/app'}))")" $B/api/services > "$TMP/sl.json"
SL=$(jget "d['id']" < "$TMP/sl.json")
curl -s -o /dev/null "${A[@]}" -X POST $B/api/services/$SL/start
sleep 1
SLPID=$(curl -s "${A[@]}" $B/api/services/$SL | jget "d['pid']")
kill -0 "$SLPID" 2>/dev/null && ok "child $SLPID alive" || bad "child not alive"
kill -TERM "$DPID"; wait "$DPID" 2>/dev/null
sleep 0.3
kill -0 "$SLPID" 2>/dev/null && bad "child outlived the daemon" || ok "child stopped on daemon shutdown"

echo "== nothing written outside the data folder"
check "fake HOME/APPDATA/XDG/TMPDIR still empty" "" "$(ls -A "$FAKE")"

echo
if [ "$FAILS" -eq 0 ]; then echo "SMOKE PASSED"; else echo "SMOKE FAILED ($FAILS)"; exit 1; fi
