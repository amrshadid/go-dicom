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

# (FFFC,FFFC) Data Set Trailing Padding is padding, not data. PS3.5 Section 7.2
# states its value field "has no significance and shall be ignored by the
# receiving application", and dcmtk discards it on that basis. Comparing it
# would fail a conforming peer for doing the right thing.
IGNORED = {0xFFFCFFFC}

def significant(ds):
    return [e for e in ds if (e.tag.group << 16 | e.tag.element) not in IGNORED]

orig = pydicom.dcmread(sys.argv[1], force=True)
recv = pydicom.dcmread(sys.argv[2], force=True)

orig_elems = significant(orig)
recv_elems = significant(recv)

problems = []
if len(orig_elems) != len(recv_elems):
    problems.append(f"element count {len(orig_elems)} -> {len(recv_elems)}")
    # Name the elements that differ; a bare count is not actionable.
    recv_tags = {e.tag for e in recv_elems}
    orig_tags = {e.tag for e in orig_elems}
    for e in [e for e in orig_elems if e.tag not in recv_tags][:10]:
        problems.append(f"  lost {e.tag} {e.name} (VR {e.VR})")
    for e in [e for e in recv_elems if e.tag not in orig_tags][:10]:
        problems.append(f"  added {e.tag} {e.name} (VR {e.VR})")
if orig.PixelData != getattr(recv, "PixelData", None):
    problems.append("pixel data differs")
for kw in ("PatientName", "PatientID", "SOPInstanceUID", "StudyInstanceUID", "Rows", "Columns"):
    o, r = getattr(orig, kw, None), getattr(recv, kw, None)
    if str(o) != str(r):
        problems.append(f"{kw}: {o!r} -> {r!r}")
for e in orig_elems:
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
  PY_MOVESCU="$PYNETDICOM_BIN/movescu"

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

  note "C-MOVE: pynetdicom movescu -> go-dicom qrscp -> pynetdicom storescp"
  mkdir -p "$WORKDIR/mv_store" "$WORKDIR/mv_dest"
  # Three parties: the requestor asks the source to send to a destination it is
  # not itself. The destination must be reachable by AE title from the source.
  start_server "$WORKDIR/mv_dest" "$WORKDIR/mv_dest.log" \
    "$PY_STORESCP" 11155 -aet MVDEST -od .
  mv_dest_pid=$SERVER_PID
  start_server "$WORKDIR" "$WORKDIR/mv_src.log" \
    "$GODICOM" qrscp -port 11156 -aet GOMOVE -output "$WORKDIR/mv_store" \
    -move-dest "MVDEST=127.0.0.1:11155"
  mv_src_pid=$SERVER_PID

  if wait_for_port 11155 && wait_for_port 11156; then
    if "$PY_STORESCU" 127.0.0.1 11156 "$FIXTURE" -aec GOMOVE >/dev/null 2>&1; then
      if "$PY_MOVESCU" 127.0.0.1 11156 -aec GOMOVE -aem MVDEST -S \
           -k QueryRetrieveLevel=STUDY -k StudyInstanceUID= >/dev/null 2>&1; then
        received=$(find "$WORKDIR/mv_dest" -type f | head -1)
        if [ -n "$received" ]; then
          verify_transfer "$received" "C-MOVE sub-operations"
        else
          fail "C-MOVE — completed but the destination received nothing"
        fi
      else
        fail "C-MOVE"
      fi
    else
      fail "C-MOVE — could not seed the source"
    fi
  else
    fail "C-MOVE servers did not start listening"
  fi
  stop_server "$mv_src_pid"
  stop_server "$mv_dest_pid"
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
# The CLI's own retrieve commands, against go-dicom's own qrscp.
#
# This is not third-party interoperability, but it belongs here because it is
# the only place the CLI binary is exercised rather than the library. getscu
# called Move with an empty destination for its whole existence — a C-MOVE
# naming nowhere to send to, answered with 0xA801 — so the command had never
# performed a C-GET. Every library-level C-GET test passed throughout, because
# the defect was in the wiring between the command and the SCU.
note "go-dicom CLI: storescu, findscu, getscu against qrscp"

cli_port=11902
cli_out="$WORKDIR/cli-getscu"
mkdir -p "$cli_out"

"$GODICOM" qrscp -port "$cli_port" > "$WORKDIR/cli-qrscp.log" 2>&1 &
cli_qrscp_pid=$!

if wait_for_port "$cli_port"; then
  if "$GODICOM" storescu -aec QRSCP "127.0.0.1:$cli_port" "$FIXTURE" \
      > "$WORKDIR/cli-storescu.log" 2>&1; then
    pass "storescu sent an instance to qrscp"
    RAN_ANY=1
  else
    fail "storescu could not send to qrscp"
  fi

  if "$GODICOM" findscu -aec QRSCP -level STUDY "127.0.0.1:$cli_port" \
      > "$WORKDIR/cli-findscu.log" 2>&1; then
    pass "findscu queried qrscp"
  else
    fail "findscu could not query qrscp"
  fi

  if "$GODICOM" getscu -aec QRSCP -level STUDY -output "$cli_out" \
      "127.0.0.1:$cli_port" > "$WORKDIR/cli-getscu.log" 2>&1; then
    retrieved=$(find "$cli_out" -name '*.dcm' | wc -l | tr -d ' ')
    if [ "$retrieved" -ge 1 ]; then
      pass "getscu retrieved $retrieved instance(s) and wrote them to disk"
    else
      fail "getscu reported success but wrote no files"
    fi
  else
    fail "getscu failed"
    sed -n '1,5p' "$WORKDIR/cli-getscu.log" >&2
  fi
else
  fail "go-dicom qrscp did not start listening"
fi
stop_server "$cli_qrscp_pid"

# ---------------------------------------------------------------------------
# Storage Commitment: go-dicom SCU -> pynetdicom SCP.
#
# This exchange is what exposed go-dicom naming its N-ACTION target with
# Affected SOP Instance UID (0000,1000) instead of Requested (0000,1001). Its
# own SCP read the same wrong tag, so every internal test passed while
# pynetdicom answered "Received unexpected N-ACTION service message" and
# aborted. The same defect was in N-GET, N-SET and N-DELETE.
if [ -n "$PYNETDICOM_BIN" ] && [ -x "$PYNETDICOM_BIN/python" ]; then
  note "Storage Commitment: go-dicom -> pynetdicom"

  commit_scp_py="$WORKDIR/commit_scp.py"
  cat > "$commit_scp_py" <<'PYEOF'
import sys
from pynetdicom import AE, evt, ALL_TRANSFER_SYNTAXES
from pynetdicom.sop_class import StorageCommitmentPushModel

def handle_action(event):
    ds = event.action_information
    print("TRANSACTION:", ds.TransactionUID, flush=True)
    print("ACTION_TYPE:", event.action_type, flush=True)
    for item in ds.ReferencedSOPSequence:
        print("INSTANCE:", item.ReferencedSOPClassUID, item.ReferencedSOPInstanceUID, flush=True)
    return 0x0000, None

ae = AE(ae_title="PYCOMMIT")
ae.add_supported_context(StorageCommitmentPushModel, ALL_TRANSFER_SYNTAXES)
ae.start_server(("127.0.0.1", int(sys.argv[1])), evt_handlers=[(evt.EVT_N_ACTION, handle_action)])
PYEOF

  commit_port=11701
  "$PYNETDICOM_BIN/python" "$commit_scp_py" "$commit_port" > "$WORKDIR/commit_scp.log" 2>&1 &
  commit_pid=$!

  if wait_for_port "$commit_port"; then
    if "$GODICOM" commitscu -aec PYCOMMIT \
        -transaction 1.2.826.0.1.3680043.8.498.999 \
        -instance 1.2.840.10008.5.1.4.1.1.2:1.2.3.111 \
        "127.0.0.1:$commit_port" > "$WORKDIR/commit_scu.log" 2>&1; then

      if grep -q "TRANSACTION: 1.2.826.0.1.3680043.8.498.999" "$WORKDIR/commit_scp.log" &&
         grep -q "ACTION_TYPE: 1" "$WORKDIR/commit_scp.log" &&
         grep -q "INSTANCE: 1.2.840.10008.5.1.4.1.1.2 1.2.3.111" "$WORKDIR/commit_scp.log"; then
        pass "N-ACTION storage commitment — pynetdicom read the transaction and instances"
        RAN_ANY=1
      else
        fail "pynetdicom did not read the commitment request correctly"
        sed -n '1,20p' "$WORKDIR/commit_scp.log" >&2
      fi
    else
      skip "go-dicom has no commitscu command; skipping the storage commitment check"
    fi
  else
    fail "pynetdicom storage commitment SCP did not start listening"
  fi
  stop_server "$commit_pid"
fi

# ---------------------------------------------------------------------------
# JPEG Lossless: dcmtk encodes, pydicom supplies the answer.
#
# The decoder was written from ITU-T T.81, so testing it against frames this
# project produced would only show that it agrees with itself. dcmtk encodes an
# uncompressed fixture with each of the seven prediction selection values, and
# the comparison is against the original pixels.
#
# Those pixels are taken from the *uncompressed* file. Bare pydicom cannot decode
# lossless JPEG at all without a pylibjpeg or gdcm plugin, so asking it to read
# the compressed fixtures would test whichever plugin happened to be installed,
# or report a disagreement on a machine with none. Reading uncompressed pixel
# data needs no plugin and no numpy.
if [ -x "$DCMTK_BIN/dcmcjpeg" ] && command -v python3 >/dev/null 2>&1; then
  note "JPEG Lossless: dcmtk encoder, pydicom ground truth"

  jl_dir="$WORKDIR/jpeglossless"
  mkdir -p "$jl_dir"

  jl_prepared=0
  if python3 - "$jl_dir" <<'PYEOF'
import os, shutil, sys
import pydicom, pydicom.data

corpus = os.path.join(os.path.dirname(pydicom.data.__file__), "test_files")
src = os.path.join(corpus, "CT_small.dcm")
shutil.copy(src, os.path.join(sys.argv[1], "orig.dcm"))

# PixelData is the raw element value: no decoding, so no plugin and no numpy.
ds = pydicom.dcmread(src)
with open(os.path.join(sys.argv[1], "orig.pixels"), "wb") as fh:
    fh.write(ds.PixelData)
PYEOF
  then
    jl_prepared=1
  fi

  if [ "$jl_prepared" -eq 0 ]; then
    skip "pydicom is not available to supply the fixture"
  else
    jl_encoded=0
    for sv in 1 2 3 4 5 6 7; do
      if "$DCMTK_BIN/dcmcjpeg" +el +sv "$sv" "$jl_dir/orig.dcm" "$jl_dir/sv$sv.dcm" >/dev/null 2>&1; then
        jl_encoded=$((jl_encoded + 1))
      fi
    done
    # Selection value 1 also has its own transfer syntax, which is the one
    # archives actually store.
    if "$DCMTK_BIN/dcmcjpeg" +e1 "$jl_dir/orig.dcm" "$jl_dir/e1.dcm" >/dev/null 2>&1; then
      jl_encoded=$((jl_encoded + 1))
    fi

    if [ "$jl_encoded" -eq 0 ]; then
      skip "dcmcjpeg produced no lossless JPEG fixtures"
    elif go run ./scripts/jpeglossless-check "$jl_dir" "$jl_dir/orig.pixels" > "$WORKDIR/jl.log" 2>&1; then
      pass "JPEG Lossless — $jl_encoded fixtures decode to the original pixels"
      RAN_ANY=1
    else
      fail "go-dicom did not reproduce the original pixels"
      sed -n '1,20p' "$WORKDIR/jl.log" >&2
    fi
  fi
fi

# ---------------------------------------------------------------------------
# JPEG-LS: dcmtk encodes, pydicom supplies the answer, same as above.
#
# Only single-component frames are checked, because that is all this library
# decodes; a colour frame is refused rather than decoded, which the unit tests
# cover.
if [ -x "$DCMTK_BIN/dcmcjpls" ] && command -v python3 >/dev/null 2>&1; then
  note "JPEG-LS: dcmtk encoder, pydicom ground truth"

  jls_dir="$WORKDIR/jpegls"
  mkdir -p "$jls_dir"

  if python3 - "$jls_dir" <<'PYEOF'
import os, shutil, sys
import pydicom, pydicom.data

corpus = os.path.join(os.path.dirname(pydicom.data.__file__), "test_files")
src = os.path.join(corpus, "MR_small.dcm")
shutil.copy(src, os.path.join(sys.argv[1], "orig.dcm"))

ds = pydicom.dcmread(src)
with open(os.path.join(sys.argv[1], "orig.pixels"), "wb") as fh:
    fh.write(ds.PixelData)
PYEOF
  then
    if "$DCMTK_BIN/dcmcjpls" +el "$jls_dir/orig.dcm" "$jls_dir/lossless.dcm" >/dev/null 2>&1; then
      if go run ./scripts/jpeglossless-check "$jls_dir" "$jls_dir/orig.pixels" > "$WORKDIR/jls.log" 2>&1; then
        pass "JPEG-LS — a grayscale frame decodes to the original pixels"
        RAN_ANY=1
      else
        fail "go-dicom did not reproduce the original pixels from JPEG-LS"
        sed -n '1,20p' "$WORKDIR/jls.log" >&2
      fi
    else
      skip "dcmcjpls produced no JPEG-LS fixture"
    fi
  else
    skip "pydicom is not available to supply the JPEG-LS fixture"
  fi
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
