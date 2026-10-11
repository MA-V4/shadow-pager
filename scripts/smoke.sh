#!/usr/bin/env bash
# Smoke test for a deployed Shadow-Pager that works in Git Bash and on Linux and needs only bash and curl.
#
# Settings come from the environment:
#   API_URL      the address of the API, for example https://shadow-pager-api.onrender.com (required)
#   WEB_ORIGIN   the address of the web app, for example https://shadow-pager.vercel.app (optional)
#   ADMIN_TOKEN  the admin token of the server (optional, and never printed)
#
# Without WEB_ORIGIN the CORS check is skipped, and without ADMIN_TOKEN nothing is injected.
# The script asks the server if public drills are on and expects the matching behaviour.
# The script exits with 0 when every check passed and with 1 when at least one failed.

set -u

API_URL="${API_URL:-}"
WEB_ORIGIN="${WEB_ORIGIN:-}"
ADMIN_TOKEN="${ADMIN_TOKEN:-}"
WAKE_SECONDS="${WAKE_SECONDS:-90}"

if [ -z "$API_URL" ]; then
  echo "API_URL is not set. Example: API_URL=https://shadow-pager-api.onrender.com bash scripts/smoke.sh" >&2
  exit 2
fi
if ! command -v curl >/dev/null 2>&1; then
  echo "curl is needed and was not found." >&2
  exit 2
fi

# Trailing slashes are removed, so the paths below never start with two of them.
API_URL="${API_URL%/}"
WEB_ORIGIN="${WEB_ORIGIN%/}"
API="$API_URL/api/v1"

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
BODY="$WORK/body"
HEAD="$WORK/head"

failed=0
pass() { echo "PASS  $1"; }
fail() {
  echo "FAIL  $1"
  failed=1
}
skip() { echo "SKIP  $1"; }

# request runs curl, stores the headers and the body, and prints the status code, which is 000 for a server that cannot be reached.
request() {
  : >"$BODY"
  : >"$HEAD"
  local code
  code="$(curl --silent --show-error --max-time 15 --output "$BODY" --dump-header "$HEAD" --write-out '%{http_code}' "$@" 2>"$WORK/err")" || code="000"
  echo "${code:-000}"
}

# header prints the value of one answer header, without caring about capital letters.
header() {
  tr -d '\r' <"$HEAD" | awk -v want="$1" 'BEGIN { IGNORECASE = 1; want = tolower(want) } { split($0, parts, ":"); if (tolower(parts[1]) == want) { sub(/^[^:]*:[ ]*/, ""); print; exit } }'
}

# body_has tells us if the stored body contains a piece of text.
body_has() { grep -q -- "$1" "$BODY"; }

# json_string prints the first string value of a key in the stored body, which is the top level one in the answers we read.
json_string() {
  tr -d '\n' <"$BODY" | grep -o "\"$1\"[ ]*:[ ]*\"[^\"]*\"" | head -n 1 | sed 's/^[^:]*:[ ]*"\(.*\)"$/\1/'
}

echo "Smoke test against $API_URL"

# WAKE UP

# A free Render instance sleeps after 15 minutes, and its first answer can take most of a minute.
woke=0
started="$(date +%s)"
while :; do
  code="$(request "$API_URL/healthz")"
  if [ "$code" = "200" ]; then
    woke=1
    break
  fi
  waited=$(($(date +%s) - started))
  if [ "$waited" -ge "$WAKE_SECONDS" ]; then
    break
  fi
  echo "      waiting for the server to wake up (${waited}s of ${WAKE_SECONDS}s, last answer $code)"
  sleep 5
done

if [ "$woke" -ne 1 ]; then
  fail "the server did not answer /healthz with 200 within ${WAKE_SECONDS}s"
  echo
  echo "RESULT: FAIL"
  exit 1
fi
pass "the server is awake after $(($(date +%s) - started))s"

# PROBES

code="$(request "$API_URL/healthz")"
if [ "$code" = "200" ] && body_has '"status":"ok"'; then
  pass "GET /healthz is 200 and says ok"
else
  fail "GET /healthz gave $code, want 200 with status ok"
fi

code="$(request "$API_URL/readyz")"
if [ "$code" = "200" ]; then
  pass "GET /readyz is 200, so the engine runs and the database answers"
else
  fail "GET /readyz gave $code, want 200. Answer: $(head -c 300 "$BODY")"
fi

code="$(request "$API/status")"
level="$(json_string status)"
case "$level" in
  operational | degraded | major_outage) known=1 ;;
  *) known=0 ;;
esac
if [ "$code" = "200" ] && [ "$known" -eq 1 ]; then
  pass "GET /api/v1/status is 200 and reports $level"
else
  fail "GET /api/v1/status gave $code with status \"$level\", want 200 and a known level"
fi

if [ "$(header X-Content-Type-Options)" = "nosniff" ] && [ "$(header X-Frame-Options)" = "DENY" ] && [ -n "$(header Content-Security-Policy)" ]; then
  pass "the API sends its security headers"
else
  fail "the API answer is missing a security header (nosniff, frame options or content security policy)"
fi

# Browsers are told to insist on HTTPS, which the server only does in production and only matters over HTTPS.
case "$API_URL" in
  https://*)
    if header Strict-Transport-Security | grep -q "max-age=31536000"; then
      pass "the API sends Strict-Transport-Security for one year"
    else
      fail "the API does not send Strict-Transport-Security. Is APP_ENV set to production on the server?"
    fi
    ;;
  *) skip "Strict-Transport-Security, because API_URL is not an https address" ;;
esac

# The server says if visitors may start drills, which changes what the checks below expect.
public_drills=0
code="$(request "$API/config")"
if [ "$code" = "200" ] && body_has '"public_drills":true'; then
  public_drills=1
  pass "GET /api/v1/config is 200 and says public drills are on"
elif [ "$code" = "200" ] && body_has '"public_drills":false'; then
  pass "GET /api/v1/config is 200 and says public drills are off"
else
  fail "GET /api/v1/config gave $code, want 200 with public_drills"
fi

# CORS

if [ -z "$WEB_ORIGIN" ]; then
  skip "CORS preflight, because WEB_ORIGIN is not set"
else
  code="$(request --request OPTIONS "$API/simulations" \
    --header "Origin: $WEB_ORIGIN" \
    --header "Access-Control-Request-Method: POST" \
    --header "Access-Control-Request-Headers: authorization, content-type")"
  allowed="$(header Access-Control-Allow-Origin)"
  if [ "$code" = "204" ] && [ "$allowed" = "$WEB_ORIGIN" ]; then
    pass "CORS preflight from $WEB_ORIGIN is allowed"
  else
    fail "CORS preflight from $WEB_ORIGIN gave $code and allowed \"$allowed\". Check CORS_ALLOWED_ORIGINS on the server."
  fi

  code="$(request --request OPTIONS "$API/simulations" \
    --header "Origin: https://smoke-test-stranger.invalid" \
    --header "Access-Control-Request-Method: POST")"
  if [ "$code" = "403" ]; then
    pass "CORS preflight from an unknown website is refused"
  else
    fail "CORS preflight from an unknown website gave $code, want 403"
  fi
fi

# DRILL

if [ -z "$ADMIN_TOKEN" ]; then
  skip "inject, list and halt, because ADMIN_TOKEN is not set"
else
  # The drill is tiny on purpose: at this intensity it stays inside every SLO, so it declares no incident.
  drill='{"service":"smoke-test","mode":"latency_spike","severity":"minor","intensity":0.05,"duration":"10s"}'
  code="$(request --request POST "$API/simulations" \
    --header "Authorization: Bearer $ADMIN_TOKEN" \
    --header "Content-Type: application/json" \
    --data "$drill")"
  id="$(json_string id)"
  if [ "$code" = "201" ] && [ -n "$id" ]; then
    pass "POST /api/v1/simulations started the drill $id"

    code="$(request "$API/simulations")"
    if [ "$code" = "200" ] && body_has "\"$id\""; then
      pass "GET /api/v1/simulations lists the drill"
    else
      fail "GET /api/v1/simulations gave $code and does not list $id"
    fi

    # A visitor must never be able to stop a drill that the admin started.
    code="$(request --request POST "$API/simulations/$id/halt")"
    if [ "$code" = "401" ]; then
      pass "halting the admin drill without the token is refused"
    else
      fail "halting the admin drill without the token gave $code, want 401"
    fi

    code="$(request --request POST "$API/simulations/$id/halt" --header "Authorization: Bearer $ADMIN_TOKEN")"
    state="$(json_string state)"
    if [ "$code" = "200" ] && [ "$state" = "halted" ]; then
      pass "POST /api/v1/simulations/$id/halt stopped the drill"
    else
      fail "halting $id gave $code with state \"$state\", want 200 and halted. The drill ends by itself after 10 seconds."
    fi
  elif [ "$code" = "401" ]; then
    fail "the server refused the admin token (401)"
  elif [ "$code" = "429" ]; then
    fail "the server is rate limiting writes (429). Wait $(header Retry-After) seconds and run again."
  else
    fail "POST /api/v1/simulations gave $code, want 201. Answer: $(head -c 300 "$BODY")"
  fi

  code="$(request --request POST "$API/simulations" --header "Content-Type: application/json" --data "$drill")"
  id="$(json_string id)"
  if [ "$public_drills" -eq 1 ]; then
    # With public drills on, a visitor gets a drill of their own, marked public, and may stop it again.
    if [ "$code" = "201" ] && body_has '"public":true'; then
      pass "a write without the token starts a public drill"
      code="$(request --request POST "$API/simulations/$id/halt")"
      if [ "$code" = "200" ]; then
        pass "the public drill can be halted without the token"
      else
        fail "halting the public drill without the token gave $code, want 200. It ends by itself after 10 seconds."
      fi
    elif [ "$code" = "429" ]; then
      fail "the public drill was rate limited or the public limit is full (429). Wait $(header Retry-After) seconds and run again."
    else
      [ -n "$id" ] && request --request POST "$API/simulations/$id/halt" --header "Authorization: Bearer $ADMIN_TOKEN" >/dev/null
      fail "a write without the token gave $code, want 201 with a public drill"
    fi
  else
    # With public drills off, a request with no token must be refused, or the token protects nothing.
    if [ "$code" = "401" ]; then
      pass "a write without the admin token is refused"
    else
      [ -n "$id" ] && request --request POST "$API/simulations/$id/halt" >/dev/null
      fail "a write without the admin token gave $code, want 401. Is ADMIN_TOKEN set on the server?"
    fi
  fi
fi

echo
if [ "$failed" -ne 0 ]; then
  echo "RESULT: FAIL"
  exit 1
fi
echo "RESULT: PASS"
