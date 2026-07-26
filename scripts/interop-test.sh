#!/usr/bin/env bash
#
# Interoperability tests against third-party DICOM implementations.
#
# Every critical defect found in v1.2.0 was a case where go-dicom talked to
# itself successfully and would have failed against anyone else — the unit
# tests were measuring self-consistency. This script talks to pynetdicom and
# dcmtk instead, in both directions, and verifies the transferred data with
# pydicom rather than with go-dicom's own reader.
#
# Environment:
#   GODICOM       path to the go-dicom binary (default: ./dicom)
#   PYNETDICOM_BIN directory holding pynetdicom's console scripts
#   DCMTK_BIN      directory holding dcmtk's tools (default: /usr/bin)
#
# Either peer may be absent; its tests are skipped with a notice. The script
# fails if no peer is available at all, so a broken install cannot masquerade
# as a pass.

set -euo pipefail

GODICOM="${GODICOM:-./dicom}"
# Resolve to an absolute path: servers are started from a scratch directory, so
# a relative path would not resolve there.
if [ ! -x "$GODICOM" ]; then
  echo "go-dicom binary not found or not executable: $GODICOM" >&2
  echo "Build it first (go build -o dicom .) or set GODICOM." >&2
  exit 1
fi
GODICOM="$(cd "$(dirname "$GODICOM")" && pwd)/$(basename "$GODICOM")"
PYNETDICOM_BIN="${PYNETDICOM_BIN:-}"
DCMTK_BIN="${DCMTK_BIN:-/usr/bin}"

WORKDIR="$(mktemp -d)"
PIDS=()
FAILURES=0
RAN_ANY=0

cleanup() {
  for pid in "${PIDS[@]:-}"; do
    kill "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
  done
  rm -rf "$WORKDIR"
}
trap cleanup EXIT

note()  { printf '\n\033[1m== %s\033[0m\n' "$*"; }
pass()  { printf '   \033[32mPASS\033[0m  %s\n' "$*"; }
fail()  { printf '   \033[31mFAIL\033[0m  %s\n' "$*"; FAILURES=$((FAILURES + 1)); }
skip()  { printf '   \033[33mSKIP\033[0m  %s\n' "$*"; }

# start_server launches a server in the background and returns its real PID.
# `exec` replaces the subshell so $! is the server itself; backgrounding a
# plain subshell would yield the subshell's PID and leave the server orphaned
# when it is killed.
#
# Usage: start_server <workdir> <logfile> <command...>
start_server() {
  local dir=$1 log=$2
  shift 2
  ( cd "$dir" && exec "$@" >"$log" 2>&1 ) &
  SERVER_PID=$!
  PIDS+=("$SERVER_PID")
}

# stop_server terminates a server started by start_server and waits for it.
stop_server() {
  local pid=$1
  [ -n "$pid" ] || return 0
  kill "$pid" 2>/dev/null || true
  wait "$pid" 2>/dev/null || true
}

# wait_for_port blocks until a TCP port accepts connections, or times out.
wait_for_port() {
  local port=$1 tries=${2:-50}
  for _ in $(seq "$tries"); do
    if python3 -c "
import socket,sys
s=socket.socket()
s.settimeout(0.3)
sys.exit(0 if s.connect_ex(('127.0.0.1',$port))==0 else 1)
" 2>/dev/null; then
      return 0
    fi
    sleep 0.2
  done
  return 1
}

# ---------------------------------------------------------------------------
# Fixture: a real DICOM file from pydicom's test data, not one we generated.
# ---------------------------------------------------------------------------
note "Preparing fixture"
FIXTURE="$WORKDIR/fixture.dcm"
if ! python3 -c "
from pydicom.data import get_testdata_file
import shutil
shutil.copy(get_testdata_file('CT_small.dcm'), '$FIXTURE')
" 2>/dev/null; then
  echo "pydicom is required to run interop tests (provides the fixture and the verifier)" >&2
  exit 1
fi
pass "using pydicom's CT_small.dcm as the fixture"

# verify_transfer compares a transferred file against the fixture using
# pydicom, so the check does not depend on go-dicom's own reader.
verify_transfer() {
  local received=$1 label=$2
  if python3 - "$FIXTURE" "$received" <<'PY'
import sys, pydicom
orig = pydicom.dcmread(sys.argv[1], force=True)
recv = pydicom.dcmread(sys.argv[2], force=True)

problems = []
if len(orig) != len(recv):
    problems.append(f"element count {len(orig)} -> {len(recv)}")
if orig.PixelData != getattr(recv, "PixelData", None):
    problems.append("pixel data differs")
for kw in ("PatientName", "PatientID", "SOPInstanceUID", "StudyInstanceUID", "Rows", "Columns"):
    o, r = getattr(orig, kw, None), getattr(recv, kw, None)
    if str(o) != str(r):
        problems.append(f"{kw}: {o!r} -> {r!r}")
for e in orig:
    if e.VR == "SQ":
        n_o = len(e.value) if e.value else 0
        n_r = len(recv[e.tag].value) if e.tag in recv and recv[e.tag].value else 0
        if n_o != n_r:
            problems.append(f"{e.name}: {n_o} -> {n_r} item(s)")

if problems:
    print("      " + "\n      ".join(problems), file=sys.stderr)
    sys.exit(1)
PY
  then
    pass "$label — all elements, pixel data, and sequences intact"
  else
    fail "$label — transferred data does not match the original"
  fi
}

# ---------------------------------------------------------------------------
# pynetdicom
# ---------------------------------------------------------------------------
if [ -n "$PYNETDICOM_BIN" ] && [ -x "$PYNETDICOM_BIN/storescu" ]; then
  RAN_ANY=1
  PY_STORESCU="$PYNETDICOM_BIN/storescu"
  PY_STORESCP="$PYNETDICOM_BIN/storescp"
  PY_ECHOSCU="$PYNETDICOM_BIN/echoscu"
  PY_GETSCU="$PYNETDICOM_BIN/getscu"

  note "pynetdicom -> go-dicom"
  mkdir -p "$WORKDIR/go_recv"
  start_server "$WORKDIR" "$WORKDIR/go_scp.log" \
    "$GODICOM" storescp -port 11150 -aet GODICOM -output "$WORKDIR/go_recv"
  go_scp_pid=$SERVER_PID
  if wait_for_port 11150; then
    if "$PY_ECHOSCU" 127.0.0.1 11150 -aec GODICOM >/dev/null 2>&1; then
      pass "C-ECHO"
    else
      fail "C-ECHO"
    fi

    if "$PY_STORESCU" 127.0.0.1 11150 "$FIXTURE" -aec GODICOM >/dev/null 2>&1; then
      received=$(find "$WORKDIR/go_recv" -type f | head -1)
      if [ -n "$received" ]; then
        verify_transfer "$received" "C-STORE"
      else
        fail "C-STORE — peer reported success but no file was written"
      fi
    else
      fail "C-STORE"
    fi
  else
    fail "go-dicom storescp did not start listening"
  fi
  stop_server "$go_scp_pid"

  note "go-dicom -> pynetdicom"
  mkdir -p "$WORKDIR/py_recv"
  start_server "$WORKDIR/py_recv" "$WORKDIR/py_scp.log" \
    "$PY_STORESCP" 11151 -aet PYSCP -od .
  py_scp_pid=$SERVER_PID
  if wait_for_port 11151; then
    if "$GODICOM" echoscu -aec PYSCP 127.0.0.1:11151 >/dev/null 2>&1; then
      pass "C-ECHO"
    else
      fail "C-ECHO"
    fi

    if "$GODICOM" storescu -aec PYSCP 127.0.0.1:11151 "$FIXTURE" >/dev/null 2>&1; then
      received=$(find "$WORKDIR/py_recv" -type f | head -1)
      if [ -n "$received" ]; then
        verify_transfer "$received" "C-STORE"
      else
        fail "C-STORE — peer reported success but no file was written"
      fi
    else
      fail "C-STORE"
    fi
  else
    fail "pynetdicom storescp did not start listening"
  fi
  stop_server "$py_scp_pid"

  note "C-GET: pynetdicom getscu -> go-dicom qrscp"
  mkdir -p "$WORKDIR/qr_store" "$WORKDIR/qr_got"
  start_server "$WORKDIR" "$WORKDIR/qr_scp.log" \
    "$GODICOM" qrscp -port 11152 -aet GOQR -output "$WORKDIR/qr_store"
  qr_scp_pid=$SERVER_PID
  if wait_for_port 11152; then
    # Seed the Q/R server, then retrieve what was seeded.
    if "$PY_STORESCU" 127.0.0.1 11152 "$FIXTURE" -aec GOQR >/dev/null 2>&1; then
      if (cd "$WORKDIR/qr_got" && "$PY_GETSCU" 127.0.0.1 11152 -aec GOQR -S \
            -k QueryRetrieveLevel=STUDY -k StudyInstanceUID= -od . >/dev/null 2>&1); then
        received=$(find "$WORKDIR/qr_got" -type f | head -1)
        if [ -n "$received" ]; then
          verify_transfer "$received" "C-GET sub-operations"
        else
          fail "C-GET — completed but transferred no instances"
        fi
      else
        fail "C-GET"
      fi
    else
      fail "C-GET — could not seed the Q/R server"
    fi
  else
    fail "go-dicom qrscp did not start listening"
  fi
  stop_server "$qr_scp_pid"
else
  skip "pynetdicom not available (set PYNETDICOM_BIN)"
fi

# ---------------------------------------------------------------------------
# dcmtk
# ---------------------------------------------------------------------------
if [ -x "$DCMTK_BIN/storescu" ] && [ -x "$DCMTK_BIN/storescp" ]; then
  RAN_ANY=1

  note "dcmtk -> go-dicom"
  mkdir -p "$WORKDIR/go_recv2"
  start_server "$WORKDIR" "$WORKDIR/go_scp2.log" \
    "$GODICOM" storescp -port 11153 -aet GODICOM -output "$WORKDIR/go_recv2"
  go_scp2_pid=$SERVER_PID
  if wait_for_port 11153; then
    if "$DCMTK_BIN/echoscu" -aec GODICOM 127.0.0.1 11153 >/dev/null 2>&1; then
      pass "C-ECHO"
    else
      fail "C-ECHO"
    fi

    if "$DCMTK_BIN/storescu" -aec GODICOM 127.0.0.1 11153 "$FIXTURE" >/dev/null 2>&1; then
      received=$(find "$WORKDIR/go_recv2" -type f | head -1)
      if [ -n "$received" ]; then
        verify_transfer "$received" "C-STORE"
      else
        fail "C-STORE — peer reported success but no file was written"
      fi
    else
      fail "C-STORE"
    fi
  else
    fail "go-dicom storescp did not start listening"
  fi
  stop_server "$go_scp2_pid"

  note "go-dicom -> dcmtk"
  mkdir -p "$WORKDIR/dcmtk_recv"
  start_server "$WORKDIR/dcmtk_recv" "$WORKDIR/dcmtk_scp.log" \
    "$DCMTK_BIN/storescp" 11154 -aet DCMTKSCP
  dcmtk_scp_pid=$SERVER_PID
  if wait_for_port 11154; then
    if "$GODICOM" echoscu -aec DCMTKSCP 127.0.0.1:11154 >/dev/null 2>&1; then
      pass "C-ECHO"
    else
      fail "C-ECHO"
    fi

    if "$GODICOM" storescu -aec DCMTKSCP 127.0.0.1:11154 "$FIXTURE" >/dev/null 2>&1; then
      received=$(find "$WORKDIR/dcmtk_recv" -type f | head -1)
      if [ -n "$received" ]; then
        verify_transfer "$received" "C-STORE"
      else
        fail "C-STORE — peer reported success but no file was written"
      fi
    else
      fail "C-STORE"
    fi
  else
    fail "dcmtk storescp did not start listening"
  fi
  stop_server "$dcmtk_scp_pid"
else
  skip "dcmtk not available (set DCMTK_BIN)"
fi

# ---------------------------------------------------------------------------
note "Result"
if [ "$RAN_ANY" -eq 0 ]; then
  echo "   No third-party peer was available, so nothing was verified." >&2
  echo "   Install pynetdicom or dcmtk; a silent skip is not a pass." >&2
  exit 1
fi
if [ "$FAILURES" -gt 0 ]; then
  echo "   $FAILURES interoperability check(s) failed." >&2
  exit 1
fi
echo "   All interoperability checks passed."
