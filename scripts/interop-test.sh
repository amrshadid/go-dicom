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
#   GODICOM       path to the go-dicom binary (default: ./go-dicom)
#   PYNETDICOM_BIN directory holding pynetdicom's console scripts
#   DCMTK_BIN      directory holding dcmtk's tools (default: /usr/bin)
#
# Either peer may be absent; its tests are skipped with a notice. The script
# fails if no peer is available at all, so a broken install cannot masquerade
# as a pass.

set -euo pipefail

GODICOM="${GODICOM:-./go-dicom}"
# Resolve to an absolute path: servers are started from a scratch directory, so
# a relative path would not resolve there.
if [ ! -x "$GODICOM" ]; then
  echo "go-dicom binary not found or not executable: $GODICOM" >&2
  echo "Build it first (go build -o go-dicom .) or set GODICOM." >&2
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

# send_classes sends objects the library's default proposal leaves out — RT
# Dose, RT Plan, a Structured Report and an ECG — in one storescu invocation,
# and checks the peer holds all four. storescu proposed that default whatever
# it was sending, so each of these failed with "not among the presentation
# contexts proposed" (#116). It now proposes what the files hold.
#
# Usage: send_classes <aec> <port> <receive dir>
send_classes() {
  local aec=$1 port=$2 recv=$3 src="$WORKDIR/classes"
  mkdir -p "$src"
  python3 - "$src" <<'PY'
import shutil, sys
from pydicom.data import get_testdata_file
for name in ("rtdose.dcm", "rtplan.dcm", "test-SR.dcm", "waveform_ecg.dcm"):
    shutil.copy(get_testdata_file(name), sys.argv[1])
PY
  find "$recv" -type f -delete
  if ! "$GODICOM" storescu -aec "$aec" "127.0.0.1:$port" "$src"/*.dcm >/dev/null 2>&1; then
    fail "RT, SR and ECG in one storescu run — storescu reported failures"
    return
  fi
  if python3 - "$src" "$recv" <<'PY'
import glob, os, sys, pydicom
sent = {pydicom.dcmread(f).SOPInstanceUID for f in glob.glob(os.path.join(sys.argv[1], "*.dcm"))}
held = set()
for root, _, names in os.walk(sys.argv[2]):
    for n in names:
        try:
            held.add(pydicom.dcmread(os.path.join(root, n)).SOPInstanceUID)
        except Exception:
            pass
missing = sent - held
if missing:
    print(f"      the peer holds {len(sent) - len(missing)} of {len(sent)}", file=sys.stderr)
    sys.exit(1)
PY
  then
    pass "RT Dose, RT Plan, SR and ECG in one storescu run — all four stored"
  else
    fail "RT, SR and ECG in one storescu run — not all were stored"
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

    send_classes PYSCP 11151 "$WORKDIR/py_recv"

    # -events (#260): the PDUs storescu logs are decoded by pynetdicom's own
    # PDU classes, so the bytes in the log are what crossed the connection
    # and the log's account of them is what another implementation reads.
    events="$WORKDIR/events.jsonl"
    if "$GODICOM" storescu -aet EVTSCU -aec PYSCP -events "$events" 127.0.0.1:11151 "$FIXTURE" >/dev/null 2>&1 &&
       checked=$(python3 - "$events" <<'PY'
import json, sys
from pynetdicom.pdu import A_ASSOCIATE_RQ, A_ASSOCIATE_AC, P_DATA_TF, A_RELEASE_RQ, A_RELEASE_RP
kinds = {"A-ASSOCIATE-RQ": A_ASSOCIATE_RQ, "A-ASSOCIATE-AC": A_ASSOCIATE_AC, "P-DATA-TF": P_DATA_TF,
         "A-RELEASE-RQ": A_RELEASE_RQ, "A-RELEASE-RP": A_RELEASE_RP}
records = [json.loads(line) for line in open(sys.argv[1])]
pdus = dimse = 0
for r in records:
    if r["event"] not in ("EVT_PDU_SENT", "EVT_PDU_RECV"):
        dimse += r["event"].startswith("EVT_DIMSE")
        continue
    log = r["pdu"]
    raw = bytes.fromhex(log["bytes"])
    pdu = kinds[log["type"]]()
    pdu.decode(raw)
    pdus += 1
    if pdu.pdu_length != log["length"] or len(raw) != log["length"] + 6:
        sys.exit("%s: length %d in the log, %d in the bytes" % (log["type"], log["length"], pdu.pdu_length))
    if log["type"] == "A-ASSOCIATE-RQ":
        if pdu.calling_ae_title.strip() != "EVTSCU" or pdu.called_ae_title.strip() != "PYSCP":
            sys.exit("the request names %r and %r" % (pdu.calling_ae_title, pdu.called_ae_title))
        theirs = sorted((c.context_id, str(c.abstract_syntax)) for c in pdu.presentation_context)
        ours = sorted((c["id"], c["abstractSyntax"]) for c in log["proposedContexts"])
        if theirs != ours:
            sys.exit("proposed contexts differ: %r and %r" % (theirs, ours))
    if log["type"] == "A-ASSOCIATE-AC":
        theirs = sorted((c.context_id, c.result) for c in pdu.presentation_context)
        ours = sorted((c["id"], c["result"]) for c in log["answeredContexts"])
        if theirs != ours:
            sys.exit("answered contexts differ: %r and %r" % (theirs, ours))
    if log["type"] == "P-DATA-TF":
        theirs = [(v.context_id, len(v.presentation_data_value) - 1) for v in pdu.presentation_data_value_items]
        ours = [(v["contextId"], v["length"]) for v in log["pdvs"]]
        if theirs != ours:
            sys.exit("PDVs differ: %r and %r" % (theirs, ours))
names = [r["event"] for r in records]
for need in ("EVT_REQUESTED", "EVT_ACCEPTED", "EVT_ESTABLISHED", "EVT_DIMSE_SENT", "EVT_DIMSE_RECV", "EVT_RELEASED"):
    if need not in names:
        sys.exit("no %s in the log" % need)
status = [r["dimse"]["status"] for r in records if r["event"] == "EVT_DIMSE_RECV"]
if status != [0]:
    sys.exit("the C-STORE response's status is logged as %r" % status)
print("%d PDUs and %d DIMSE messages" % (pdus, dimse))
PY
    ); then
      pass "-events: pynetdicom decodes every logged PDU to what the log says — $checked"
    else
      fail "-events: the log does not match what pynetdicom reads from its bytes: $checked"
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

    # The same instance again, sent as Implicit VR Little Endian — DICOM's
    # default, and what most modalities send. storescp writes Explicit VR, so
    # this is the conversion that put "OB or OW" in the VR field and lost the
    # pixel data (#118). The fixture is Explicit VR, so the check above never
    # made it.
    find "$WORKDIR/go_recv2" -type f -delete
    if "$DCMTK_BIN/storescu" -xi -aec GODICOM 127.0.0.1 11153 "$FIXTURE" >/dev/null 2>&1; then
      received=$(find "$WORKDIR/go_recv2" -type f | head -1)
      if [ -n "$received" ]; then
        verify_transfer "$received" "C-STORE as Implicit VR"
      else
        fail "C-STORE as Implicit VR — peer reported success but no file was written"
      fi
    else
      fail "C-STORE as Implicit VR"
    fi

    # A compressed instance, sent in its own syntax as a modality storing
    # natively would. storescp wrote every instance as Explicit VR Little Endian,
    # so this one became a file declaring native pixels and holding JPEG
    # fragments (#126). The comparison is of the encapsulated bytes and the
    # declared syntax, so it needs no JPEG decoder.
    find "$WORKDIR/go_recv2" -type f -delete
    compressed="$WORKDIR/compressed.dcm"
    python3 -c "
from pydicom.data import get_testdata_file
import shutil
shutil.copy(get_testdata_file('SC_rgb_jpeg_gdcm.dcm'), '$compressed')
"
    if "$DCMTK_BIN/storescu" -xs -aec GODICOM 127.0.0.1 11153 "$compressed" >/dev/null 2>&1; then
      received=$(find "$WORKDIR/go_recv2" -type f | head -1)
      if [ -z "$received" ]; then
        fail "C-STORE compressed — peer reported success but no file was written"
      elif ! "$DCMTK_BIN/dcmdump" "$received" >/dev/null 2>&1; then
        fail "C-STORE compressed — dcmtk cannot read the stored file"
      elif python3 - "$compressed" "$received" <<'PY'
import sys, pydicom
sent, kept = pydicom.dcmread(sys.argv[1]), pydicom.dcmread(sys.argv[2])
problems = []
if kept.file_meta.TransferSyntaxUID != sent.file_meta.TransferSyntaxUID:
    problems.append(f"stored as {kept.file_meta.TransferSyntaxUID.name}, "
                    f"sent as {sent.file_meta.TransferSyntaxUID.name}")
if kept.PixelData != sent.PixelData:
    problems.append("the encapsulated pixel data differs")
if problems:
    print("      " + "\n      ".join(problems), file=sys.stderr)
    sys.exit(1)
PY
      then
        pass "C-STORE compressed — stored in the syntax it was sent in, fragments intact"
      else
        fail "C-STORE compressed — the stored file does not match what was sent"
      fi
    else
      fail "C-STORE compressed"
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

    send_classes DCMTKSCP 11154 "$WORKDIR/dcmtk_recv"
  else
    fail "dcmtk storescp did not start listening"
  fi
  stop_server "$dcmtk_scp_pid"

  # storescu -jpeg-quality (#252): an 8-bit image compressed by this
  # library's JPEG Baseline encoder on the way out. dcmtk receives it and
  # dcmdjpeg decodes it, so neither end of the check is this library's.
  if [ -x "$DCMTK_BIN/dcmdjpeg" ] && python3 -c "import numpy, pydicom" >/dev/null 2>&1; then
    note "go-dicom storescu -jpeg-quality -> dcmtk"
    mkdir -p "$WORKDIR/dcmtk_jpeg"
    gray="$WORKDIR/gray8.dcm"
    python3 - "$gray" <<'PY'
import sys, numpy, pydicom
from pydicom.data import get_testdata_file
ds = pydicom.dcmread(get_testdata_file("CT_small.dcm"))
ds.BitsAllocated, ds.BitsStored, ds.HighBit, ds.PixelRepresentation = 8, 8, 7, 0
y, x = numpy.mgrid[0:ds.Rows, 0:ds.Columns]
ds.PixelData = ((x + y) * 255 // (ds.Rows + ds.Columns - 2)).astype(numpy.uint8).tobytes()
ds.save_as(sys.argv[1])
PY
    start_server "$WORKDIR/dcmtk_jpeg" "$WORKDIR/dcmtk_jpeg_scp.log" \
      "$DCMTK_BIN/storescp" 11199 -aet DCMTKJPEG +xa
    dcmtk_jpeg_pid=$SERVER_PID
    if wait_for_port 11199; then
      if "$GODICOM" storescu -aec DCMTKJPEG -jpeg-quality 90 127.0.0.1:11199 "$gray" >/dev/null 2>&1; then
        received=$(find "$WORKDIR/dcmtk_jpeg" -type f | head -1)
        if [ -n "$received" ] && "$DCMTK_BIN/dcmdjpeg" "$received" "$WORKDIR/gray8_decoded.dcm" >/dev/null 2>&1 &&
           python3 - "$gray" "$received" "$WORKDIR/gray8_decoded.dcm" <<'PY'
import sys, numpy, pydicom
sent = pydicom.dcmread(sys.argv[1])
received = pydicom.dcmread(sys.argv[2], stop_before_pixels=True)
decoded = pydicom.dcmread(sys.argv[3])
if received.file_meta.TransferSyntaxUID != "1.2.840.10008.1.2.4.50":
    sys.exit("received as %s, not JPEG Baseline" % received.file_meta.TransferSyntaxUID)
if received.LossyImageCompression != "01":
    sys.exit("Lossy Image Compression is %r" % received.LossyImageCompression)
worst = int(numpy.abs(decoded.pixel_array.astype(int) - sent.pixel_array.astype(int)).max())
if worst > 6:
    sys.exit("decoded samples differ from the original by up to %d" % worst)
PY
        then
          pass "8-bit image sent as JPEG Baseline — dcmtk received it and dcmdjpeg decodes the image"
        else
          fail "storescu -jpeg-quality — dcmtk did not receive an image dcmdjpeg decodes to the original"
        fi
      else
        fail "storescu -jpeg-quality — the send failed"
      fi
    else
      fail "dcmtk storescp +xa did not start listening"
    fi
    stop_server "$dcmtk_jpeg_pid"
  fi

  # go-dicom sending compressed pixel data, which is where #128 was: PS3.5 A.4
  # requires undefined length, and dcmtk aborts the association on anything
  # else. pynetdicom accepts both, so the checks above never saw it. qrscp
  # serves the instance it stored, so dcmtk supplies both ends.
  if [ -x "$DCMTK_BIN/getscu" ]; then
    note "C-GET compressed: dcmtk getscu <- go-dicom qrscp"
    mkdir -p "$WORKDIR/qr_jpeg" "$WORKDIR/dcmtk_got"
    compressed="$WORKDIR/compressed_get.dcm"
    python3 -c "
from pydicom.data import get_testdata_file
import shutil
shutil.copy(get_testdata_file('SC_rgb_jpeg_gdcm.dcm'), '$compressed')
"
    patient_id=$(python3 -c "import pydicom, sys; print(pydicom.dcmread(sys.argv[1]).PatientID)" "$compressed")
    start_server "$WORKDIR" "$WORKDIR/qr_jpeg.log" \
      "$GODICOM" qrscp -port 11157 -aet GOQRJ -output "$WORKDIR/qr_jpeg"
    qr_jpeg_pid=$SERVER_PID
    if wait_for_port 11157 &&
      "$DCMTK_BIN/storescu" -xs -aec GOQRJ 127.0.0.1 11157 "$compressed" >/dev/null 2>&1; then
      ( cd "$WORKDIR/dcmtk_got" && "$DCMTK_BIN/getscu" +xs -aec GOQRJ -P \
          -k QueryRetrieveLevel=PATIENT -k PatientID="$patient_id" \
          127.0.0.1 11157 >"$WORKDIR/dcmtk_getscu.log" 2>&1 ) || true
      received=$(find "$WORKDIR/dcmtk_got" -type f | head -1)
      if [ -z "$received" ]; then
        fail "C-GET compressed — dcmtk retrieved nothing"
        grep -E '^E:' "$WORKDIR/dcmtk_getscu.log" | head -3 >&2
      elif python3 - "$compressed" "$received" <<'PY'
import sys, pydicom
sent, got = pydicom.dcmread(sys.argv[1]), pydicom.dcmread(sys.argv[2])
if got.file_meta.TransferSyntaxUID != sent.file_meta.TransferSyntaxUID or got.PixelData != sent.PixelData:
    print(f"      retrieved as {got.file_meta.TransferSyntaxUID.name}; fragments "
          f"{'identical' if got.PixelData == sent.PixelData else 'differ'}", file=sys.stderr)
    sys.exit(1)
PY
      then
        pass "C-GET compressed — dcmtk retrieved the JPEG instance, fragments intact"
      else
        fail "C-GET compressed — the retrieved instance does not match what was stored"
      fi
    else
      fail "C-GET compressed — could not store the instance in qrscp"
    fi
    stop_server "$qr_jpeg_pid"
  fi
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
      # commitscu ships with go-dicom. A failure here is a failure: this used to
      # report a skip, which read as "not applicable" whatever went wrong.
      fail "commitscu could not send the storage commitment request"
      sed -n '1,20p' "$WORKDIR/commit_scu.log" >&2
    fi
  else
    fail "pynetdicom storage commitment SCP did not start listening"
  fi
  stop_server "$commit_pid"

  # The other direction: pynetdicom asks go-dicom's archive to commit what it
  # holds. qrscp refused the Push Model context until #113, so commitscu, which
  # ships here, had no go-dicom peer. One instance was stored, one never was.
  note "Storage Commitment: pynetdicom -> go-dicom qrscp"
  commit_req_py="$WORKDIR/commit_req.py"
  cat > "$commit_req_py" <<'PYEOF'
import sys, threading
import pydicom
from pydicom.dataset import Dataset
from pynetdicom import AE, evt
from pynetdicom.sop_class import StorageCommitmentPushModel

port, fixture = int(sys.argv[1]), sys.argv[2]
stored = pydicom.dcmread(fixture, stop_before_pixels=True)
done, report = threading.Event(), {}

def handle_report(event):
    info = event.event_information
    report["type"] = event.event_type
    report["transaction"] = info.TransactionUID
    report["committed"] = [i.ReferencedSOPInstanceUID for i in info.get("ReferencedSOPSequence", [])]
    report["failed"] = [(i.ReferencedSOPInstanceUID, i.FailureReason) for i in info.get("FailedSOPSequence", [])]
    done.set()
    return 0x0000, None

ae = AE(ae_title="PYREQ")
ae.add_requested_context(StorageCommitmentPushModel)
assoc = ae.associate("127.0.0.1", port, ae_title="GOQRC",
                     evt_handlers=[(evt.EVT_N_EVENT_REPORT, handle_report)])
if not assoc.is_established:
    sys.exit("no association")

req = Dataset()
req.TransactionUID = "1.2.826.0.1.3680043.8.498.113"
req.ReferencedSOPSequence = []
for inst in (stored.SOPInstanceUID, "1.2.3.404"):
    item = Dataset()
    item.ReferencedSOPClassUID = stored.SOPClassUID
    item.ReferencedSOPInstanceUID = inst
    req.ReferencedSOPSequence.append(item)

status, _ = assoc.send_n_action(req, 1, StorageCommitmentPushModel, "1.2.840.10008.1.20.1.1")
done.wait(10)
assoc.release()

problems = []
if not status or status.Status != 0x0000:
    problems.append(f"N-ACTION status {status.Status if status else None}")
if report.get("type") != 2:
    problems.append(f"event type {report.get('type')}, want 2 (one failure)")
if report.get("transaction") != req.TransactionUID:
    problems.append(f"transaction {report.get('transaction')}")
if report.get("committed") != [stored.SOPInstanceUID]:
    problems.append(f"committed {report.get('committed')}")
if report.get("failed") != [("1.2.3.404", 0x0112)]:
    problems.append(f"failed {report.get('failed')}")
if problems:
    sys.exit("; ".join(problems))
PYEOF

  mkdir -p "$WORKDIR/qr_commit"
  start_server "$WORKDIR" "$WORKDIR/qr_commit.log" \
    "$GODICOM" qrscp -port 11703 -aet GOQRC -output "$WORKDIR/qr_commit"
  qr_commit_pid=$SERVER_PID
  if wait_for_port 11703 &&
    "$GODICOM" storescu -aec GOQRC 127.0.0.1:11703 "$FIXTURE" >/dev/null 2>&1; then
    if "$PYNETDICOM_BIN/python" "$commit_req_py" 11703 "$FIXTURE" > "$WORKDIR/commit_req.log" 2>&1; then
      pass "Storage Commitment — qrscp committed what it held and failed the unknown instance with 0112H"
    else
      fail "Storage Commitment — pynetdicom's request to qrscp was not answered correctly"
      grep -v '^[IW]:' "$WORKDIR/commit_req.log" | sed -n '1,5p' >&2
    fi
  else
    fail "Storage Commitment — could not store the fixture in qrscp"
  fi
  stop_server "$qr_commit_pid"
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
# JPEG 2000: OpenJPEG encodes, pydicom supplies the answer.
#
# dcmtk's JPEG 2000 encoder is not in the free distribution, so the encoder here
# is OpenJPEG's opj_compress, driven directly and wrapped into a data set by
# pydicom. That is the point of the exercise either way: this library's decoder
# was written from ISO 15444-1, and a fixture it encoded itself would only show
# the two halves of one implementation agreeing.
#
# The ground truth is pydicom's reading of the *uncompressed* original, which
# needs no plugin — asking pydicom to decode the compressed fixture would test
# whichever of pylibjpeg or gdcm happened to be installed, or fail on a machine
# with neither while appearing to say the pixels disagree.
#
# The fixture is checked before go-dicom is asked anything (#322). It was
# written as PGX, and OpenJPEG's PGX reader takes the precision from the
# largest sample rather than from the header's depth: MR_small's samples reach
# 2145, so the codestream came out signed 12-bit and every sample over 2047 was
# clipped. go-dicom then decoded exactly what OpenJPEG had written and failed
# for it. Now opj_compress is given the data set's own little endian samples as
# raw input with the depth stated (-F), the codestream's SIZ marker must say
# Bits Stored and the data set's sign, and opj_decompress must return the
# original bytes. A fixture that is not lossless fails as a fixture.
#
# Raw input also needs no numpy, which the CI venv does not have: building the
# PGX called pixel_array, and that failure was reported as a skip, so the
# section passed in CI without ever running.
if command -v opj_compress >/dev/null 2>&1 && command -v opj_decompress >/dev/null 2>&1 &&
  command -v python3 >/dev/null 2>&1; then
  note "JPEG 2000: OpenJPEG encoder, pydicom ground truth"

  j2k_dir="$WORKDIR/jpeg2000"
  mkdir -p "$j2k_dir"

  if python3 - "$j2k_dir" <<'PYEOF' 2>"$WORKDIR/j2k-fixture.log"
import os, subprocess, sys

import pydicom, pydicom.data
from pydicom.encaps import encapsulate
from pydicom.uid import JPEG2000Lossless

out = sys.argv[1]
corpus = os.path.join(os.path.dirname(pydicom.data.__file__), "test_files")
ds = pydicom.dcmread(os.path.join(corpus, "MR_small.dcm"))
rows, cols = int(ds.Rows), int(ds.Columns)
stored, allocated = int(ds.BitsStored), int(ds.BitsAllocated)
signed = int(ds.PixelRepresentation) == 1
if not ds.file_meta.TransferSyntaxUID.is_little_endian or int(ds.SamplesPerPixel) != 1 \
        or stored != allocated or allocated != 16:
    sys.exit("MR_small.dcm is no longer one 16-bit little endian plane; the raw input below assumes it")

# .rawl is little endian raw samples, which is what this Pixel Data already is.
raw = os.path.join(out, "orig.rawl")
j2k = os.path.join(out, "orig.j2k")
back = os.path.join(out, "back.rawl")
with open(raw, "wb") as fh:
    fh.write(ds.PixelData)
subprocess.run(["opj_compress", "-i", raw, "-o", j2k, "-r", "1",
                "-F", "%d,%d,1,%d,%s" % (cols, rows, stored, "s" if signed else "u")],
               capture_output=True, check=True)
codestream = open(j2k, "rb").read()

# SIZ (ISO 15444-1 A.5.1): the first component's Ssiz is the sign in its top
# bit and the precision minus one below it, at byte 42 of the codestream.
if codestream[2:4] != b"\xff\x51":
    sys.exit("opj_compress wrote no SIZ marker after SOC")
ssiz = codestream[42]
if (ssiz & 0x7F) + 1 != stored or bool(ssiz & 0x80) != signed:
    sys.exit("the fixture's codestream is %s %d-bit; the data set is %s %d-bit" % (
        "signed" if ssiz & 0x80 else "unsigned", (ssiz & 0x7F) + 1,
        "signed" if signed else "unsigned", stored))
subprocess.run(["opj_decompress", "-i", j2k, "-o", back], capture_output=True, check=True)
if open(back, "rb").read() != bytes(ds.PixelData):
    sys.exit("OpenJPEG does not return the original samples from the fixture it wrote")

with open(os.path.join(out, "orig.pixels"), "wb") as fh:
    fh.write(ds.PixelData)

ds.file_meta.TransferSyntaxUID = JPEG2000Lossless
ds.PixelData = encapsulate([codestream])
ds["PixelData"].is_undefined_length = True
ds.save_as(os.path.join(out, "openjpeg.dcm"))

# Leave only the data set behind, so the checker sees one fixture.
for leftover in (raw, j2k, back):
    os.remove(leftover)
PYEOF
  then
    if go run ./scripts/jpeglossless-check "$j2k_dir" "$j2k_dir/orig.pixels" > "$WORKDIR/j2k.log" 2>&1; then
      pass "JPEG 2000 — an OpenJPEG-encoded frame decodes to the original pixels"
      RAN_ANY=1
    else
      fail "go-dicom did not reproduce the original pixels from JPEG 2000"
      sed -n '1,20p' "$WORKDIR/j2k.log" >&2
    fi
  else
    # Not a skip: OpenJPEG and pydicom are both here, so a fixture that cannot
    # be built is a fault, and as a skip it passed CI unseen (#322).
    fail "could not build a lossless JPEG 2000 fixture with OpenJPEG"
    sed -n '1,10p' "$WORKDIR/j2k-fixture.log" >&2
  fi
fi

# ---------------------------------------------------------------------------
# Round trip: go-dicom writes, dcmtk judges.
#
# Writing was only ever checked by reading the output back with this library,
# which cannot catch a file that is self-consistent and non-conformant. dcmtk
# refused 34 of pydicom's 69 corpus files written this way, every one of them
# compressed: encapsulated pixel data carried an explicit length where PS3.5 A.4
# requires an undefined one. pydicom read them all.
if [ -x "$DCMTK_BIN/dcmdump" ] && command -v python3 >/dev/null 2>&1; then
  note "Round trip: go-dicom writer, dcmtk reader"

  rt_src="$WORKDIR/rt-src"
  rt_out="$WORKDIR/rt-out"
  mkdir -p "$rt_src" "$rt_out"

  if python3 - "$rt_src" <<'PYEOF'
import os, shutil, sys
import pydicom.data
corpus = os.path.join(os.path.dirname(pydicom.data.__file__), "test_files")
copied = 0
for name in sorted(os.listdir(corpus)):
    if name.endswith(".dcm"):
        shutil.copy(os.path.join(corpus, name), os.path.join(sys.argv[1], name))
        copied += 1
raise SystemExit(0 if copied else 1)
PYEOF
  then
    if go run ./scripts/roundtrip-check "$rt_src" "$rt_out" > "$WORKDIR/rt.log" 2>&1; then
      # Each file is judged against its own source: one dcmtk reads as supplied
      # and not as written is a regression, whatever the rest do. This used to
      # tolerate two rejections, on the grounds that two fixtures are malformed.
      # dcmtk reads one of the two, meta_missing_tsyntax.dcm, as supplied. Its
      # rewrite was refused because the writer put "OB or OW" in its VR field
      # (#118).
      rt_total=0
      rt_regressed=0
      rt_malformed=0
      for written in "$rt_out"/*.dcm; do
        [ -f "$written" ] || continue
        rt_total=$((rt_total + 1))
        "$DCMTK_BIN/dcmdump" "$written" >/dev/null 2>&1 && continue
        if "$DCMTK_BIN/dcmdump" "$rt_src/$(basename "$written")" >/dev/null 2>&1; then
          rt_regressed=$((rt_regressed + 1))
          echo "   dcmtk reads $(basename "$written") as supplied, and rejects it as written" >&2
        else
          rt_malformed=$((rt_malformed + 1))
        fi
      done

      if [ "$rt_regressed" -eq 0 ]; then
        pass "round trip — dcmtk reads $((rt_total - rt_malformed)) of $rt_total files this writer produced, and rejects none it reads as supplied"
        RAN_ANY=1
      else
        fail "dcmtk rejects $rt_regressed files this writer produced that it reads as supplied"
      fi
    else
      fail "the round trip harness failed"
      sed -n '1,20p' "$WORKDIR/rt.log" >&2
    fi
  else
    skip "pydicom is not available to supply the corpus"
  fi
fi

# ---------------------------------------------------------------------------
# RLE encoding: go-dicom compresses, dcmtk decompresses, pydicom compares.
#
# RLE is the only compressed syntax this library writes, and an encoder is worth
# only what another implementation makes of it. Encoding and decoding with our
# own codec proves nothing — that is how a frame that decodes one byte longer
# than its image went unnoticed, tolerated here and rejected by pydicom.
#
# dcmtk decompresses the result rather than pydicom, because the interoperability
# venv has no numpy and pydicom cannot produce a pixel array without it. The raw
# pixel data compares just as well and needs neither.
if [ -x "$DCMTK_BIN/dcmdrle" ] && command -v python3 >/dev/null 2>&1; then
  note "RLE encoding: go-dicom encoder, dcmtk decoder"

  rle_src="$WORKDIR/rle-src"
  rle_out="$WORKDIR/rle-out"
  rle_back="$WORKDIR/rle-back"
  mkdir -p "$rle_src" "$rle_out" "$rle_back"

  if python3 - "$rle_src" <<'PYEOF'
import os, shutil, sys
import pydicom, pydicom.data

corpus = os.path.join(os.path.dirname(pydicom.data.__file__), "test_files")
# Native little-endian sources only, so the comparison at the end is between
# like and like: the source's stored pixel data is exactly what dcmtk should
# hand back after decompressing ours.
#
# The exclusions are all cases where the bytes legitimately differ rather than
# cases the encoder gets wrong. Big-endian files are normalized to little endian
# on read; a compressed source stores a codestream, not pixels, so comparing it
# against decompressed output is meaningless; and YBR_FULL_422 is expanded from
# its subsampled form.
native = {"1.2.840.10008.1.2", "1.2.840.10008.1.2.1"}
copied = 0
for name in sorted(os.listdir(corpus)):
    if not name.endswith(".dcm"):
        continue
    try:
        ds = pydicom.dcmread(os.path.join(corpus, name), stop_before_pixels=True)
        if str(ds.file_meta.TransferSyntaxUID) not in native:
            continue
        if str(ds.get("PhotometricInterpretation", "")) == "YBR_FULL_422":
            continue
        if int(ds.get("BitsAllocated", 0)) not in (8, 16, 32):
            continue
    except Exception:
        continue
    shutil.copy(os.path.join(corpus, name), os.path.join(sys.argv[1], name))
    copied += 1
raise SystemExit(0 if copied else 1)
PYEOF
  then
    if go run ./scripts/roundtrip-check -rle "$rle_src" "$rle_out" > "$WORKDIR/rle.log" 2>&1; then
      rle_total=0
      rle_bad=0
      for written in "$rle_out"/*.dcm; do
        [ -f "$written" ] || continue
        rle_total=$((rle_total + 1))
        name="$(basename "$written")"

        # dcmtk has to read it, and to read it without complaining: an ordering
        # or structural fault is reported on stderr while the file still parses.
        if ! "$DCMTK_BIN/dcmdump" "$written" >/dev/null 2>"$WORKDIR/rle-dump.err"; then
          rle_bad=$((rle_bad + 1))
          echo "   dcmtk rejects $name" >&2
          continue
        fi
        # A warning only counts when the source did not already produce it.
        # Several of pydicom's own RLE fixtures carry OW where the standard
        # wants OB for encapsulated pixel data, and passing that through
        # faithfully is not a fault of this encoder.
        if [ -s "$WORKDIR/rle-dump.err" ]; then
          "$DCMTK_BIN/dcmdump" "$rle_src/$name" >/dev/null 2>"$WORKDIR/rle-src.err" || true
          if ! diff -q "$WORKDIR/rle-dump.err" "$WORKDIR/rle-src.err" >/dev/null 2>&1; then
            rle_bad=$((rle_bad + 1))
            echo "   dcmtk warns on $name but not on its source: $(head -1 "$WORKDIR/rle-dump.err")" >&2
            continue
          fi
        fi

        # And decompressing it has to give back the pixels that went in.
        if ! "$DCMTK_BIN/dcmdrle" "$written" "$rle_back/$name" >/dev/null 2>&1; then
          rle_bad=$((rle_bad + 1))
          echo "   dcmtk cannot decompress $name" >&2
        fi
      done

      if [ "$rle_bad" -eq 0 ] && python3 - "$rle_src" "$rle_back" <<'PYEOF'
import os, sys
import pydicom

src, back = sys.argv[1], sys.argv[2]
bad = []
for name in sorted(os.listdir(back)):
    if not name.endswith(".dcm"):
        continue
    try:
        got = pydicom.dcmread(os.path.join(back, name), force=True)
        want = pydicom.dcmread(os.path.join(src, name), force=True)
    except Exception:
        continue
    if "PixelData" not in got or "PixelData" not in want:
        continue
    # Compare the image bytes, not the stored bytes. A file may store more than
    # the image needs — MR_small_padded carries 128 bytes past the last pixel —
    # and this encoder compresses the image and nothing else, so the padding is
    # absent afterward by design. Comparing stored lengths would report that as
    # a mismatch.
    try:
        n = (int(want.Rows) * int(want.Columns) * int(want.SamplesPerPixel)
             * int(want.BitsAllocated) // 8 * int(getattr(want, "NumberOfFrames", 1) or 1))
    except Exception:
        continue  # geometry this malformed says nothing about the encoder
    if n <= 0 or len(want.PixelData) < n or len(got.PixelData) < n:
        bad.append(name)
        continue
    # RLE is lossless and the source is uncompressed, so these are identical.
    if bytes(want.PixelData)[:n] != bytes(got.PixelData)[:n]:
        bad.append(name)
for name in bad:
    print("   pixels differ after an RLE round trip:", name, file=sys.stderr)
raise SystemExit(1 if bad else 0)
PYEOF
      then
        pass "RLE encoding — dcmtk reads $rle_total files and decompresses them to the original pixels"
        RAN_ANY=1
      else
        fail "this encoder's output does not survive a round trip through dcmtk"
      fi
    else
      fail "the RLE encoding harness failed"
      sed -n '1,20p' "$WORKDIR/rle.log" >&2
    fi
  else
    skip "pydicom is not available to supply the corpus"
  fi
fi

# ---------------------------------------------------------------------------
# NIfTI export: go-dicom converts, nibabel reads.
#
# This existed and produced a file nibabel refused outright — the header was
# 348 zero bytes with the magic string at the end, so sizeof_hdr was 0. Checking
# the header fields from Go would not have caught that, because the fields were
# all there and all wrong in the same way; only another implementation opening
# the file does.
# ---------------------------------------------------------------------------
if python3 -c "import nibabel, pydicom" >/dev/null 2>&1; then
  note "NIfTI export: go-dicom converter, nibabel reader"
  RAN_ANY=1

  nifti_out="$WORKDIR/out.nii"
  nifti_src=$(python3 -c "from pydicom.data import get_testdata_file; print(get_testdata_file('CT_small.dcm'))" 2>/dev/null || true)

  if [ -z "$nifti_src" ] || [ ! -f "$nifti_src" ]; then
    skip "CT_small.dcm is not in the corpus"
  elif ! "$GODICOM" convert -format nifti "$nifti_src" "$nifti_out" >/dev/null 2>&1; then
    fail "the converter did not produce a NIfTI file"
  elif python3 - "$nifti_out" "$nifti_src" <<'PYEOF'
import sys
import warnings

warnings.filterwarnings("ignore")
import nibabel
import numpy
import pydicom

img = nibabel.load(sys.argv[1])
ds = pydicom.dcmread(sys.argv[2])

# nibabel indexes [x, y] where x is the fastest-varying axis, which for
# row-major DICOM pixel data is the column. pydicom indexes [row, column]. So
# the two arrays are transposes of one another, which is what dcm2niix produces
# as well; comparing them the other way round would report a difference that is
# only a convention.
raw = img.dataobj.get_unscaled().squeeze()
if not numpy.array_equal(raw.T, ds.pixel_array):
    print("   the pixels differ from pydicom's reading of the source", file=sys.stderr)
    raise SystemExit(1)

# And the rescale has to survive, or a CT arrives without its Hounsfield units.
want = ds.pixel_array * float(ds.RescaleSlope) + float(ds.RescaleIntercept)
if not numpy.allclose(img.get_fdata().squeeze().T, want):
    print("   the rescale did not survive the conversion", file=sys.stderr)
    raise SystemExit(1)
PYEOF
  then
    pass "NIfTI export — nibabel reads it and the pixels match pydicom"
  else
    fail "nibabel cannot read the converted file, or its pixels differ"
  fi
fi

# ---------------------------------------------------------------------------
# PNG export: go-dicom renders, pydicom and numpy say what it should look like.
#
# A rendering is a chain of four steps, the sign of the samples, the rescale,
# the window and the rounding, and getting any one wrong still gives a
# picture. Only the same picture worked out another way shows it.
# ---------------------------------------------------------------------------
if python3 -c "import numpy, pydicom, PIL" >/dev/null 2>&1; then
  note "PNG export: go-dicom renderer, pydicom reference"
  RAN_ANY=1

  png_out="$WORKDIR/out.png"
  png_src=$(python3 -c "from pydicom.data import get_testdata_file; print(get_testdata_file('CT_small.dcm'))" 2>/dev/null || true)

  if [ -z "$png_src" ] || [ ! -f "$png_src" ]; then
    skip "CT_small.dcm is not in the corpus"
  elif ! "$GODICOM" convert -format png -window 40,400 "$png_src" "$png_out" >/dev/null 2>&1; then
    fail "the converter did not produce a PNG"
  elif python3 - "$png_out" "$png_src" <<'PYEOF'
import sys
import warnings

warnings.filterwarnings("ignore")
import numpy
import pydicom
from PIL import Image

got = numpy.asarray(Image.open(sys.argv[1])).astype(int)
ds = pydicom.dcmread(sys.argv[2])

# Signed 16-bit samples, rescaled to Hounsfield units, then the window the
# converter was given, by PS3.3 C.11.2.1.2, to 8 bits. The file has no window
# of its own.
x = ds.pixel_array.astype(float) * float(ds.RescaleSlope) + float(ds.RescaleIntercept)
c, w = 40.0, 400.0
y = ((x - (c - 0.5)) / (w - 1) + 0.5) * 255
y[x <= c - 0.5 - (w - 1) / 2] = 0
y[x > c - 0.5 + (w - 1) / 2] = 255
want = numpy.floor(numpy.clip(y, 0, 255) + 0.5).astype(int)

if got.shape != want.shape:
    print(f"   the PNG is {got.shape}, the image {want.shape}", file=sys.stderr)
    raise SystemExit(1)
# One level of tolerance: a value exactly between two levels rounds either way.
if numpy.abs(got - want).max() > 1:
    print(f"   the PNG differs from the windowed image by up to {numpy.abs(got - want).max()} levels", file=sys.stderr)
    raise SystemExit(1)
PYEOF
  then
    pass "PNG export — the rendering matches pydicom's pixels, rescaled and windowed"
  else
    fail "the PNG is not the image the file's rescale and window describe"
  fi
fi

# ---------------------------------------------------------------------------
# Tier-2 SOP classes: Relevant Patient Information Query, Display System, and a
# non-patient object information model.
#
# CONFORMANCE.md 8.5 used to say of these: "Nothing verifies them against a peer."
# They had UID constants, presentation-context helpers, and no proof. The four
# N-DIMSE defects found in 1.4.0 were in exactly that condition — implemented,
# plausible, and never driven against anything but themselves, so go-dicom's SCP
# read back the same wrong tag its SCU wrote.
#
# pynetdicom is the peer here, and it reports what it received, so a request that
# names the wrong SOP class or the wrong instance shows up as a mismatch rather
# than as agreement.
if [ -n "$PYNETDICOM_BIN" ] && [ -x "$PYNETDICOM_BIN/python" ]; then
  note "Tier-2 SOP classes against pynetdicom"

  tier2_scp_py="$WORKDIR/tier2_scp.py"
  cat > "$tier2_scp_py" <<'PYEOF'
import sys
from pynetdicom import AE, evt, ALL_TRANSFER_SYNTAXES
from pynetdicom.sop_class import (
    GeneralRelevantPatientInformationQuery,
    HangingProtocolInformationModelFind,
    DisplaySystem,
)
from pydicom.dataset import Dataset


def handle_find(event):
    # Report the abstract syntax the request arrived on, and whether the
    # identifier carried a QueryRetrieveLevel. A non-patient model must send none.
    ctx = event.context
    identifier = event.identifier
    has_level = "QueryRetrieveLevel" in identifier
    print(f"FIND_ON: {ctx.abstract_syntax}", flush=True)
    print(f"FIND_HAS_LEVEL: {has_level}", flush=True)
    for elem in identifier:
        print(f"FIND_KEY: {elem.tag}", flush=True)

    match = Dataset()
    if ctx.abstract_syntax == HangingProtocolInformationModelFind:
        match.HangingProtocolName = "INTEROP_HP"
    else:
        match.PatientID = "P1"
    yield 0xFF00, match


def handle_nget(event):
    print(f"NGET_ON: {event.context.abstract_syntax}", flush=True)
    print(f"NGET_INSTANCE: {event.request.RequestedSOPInstanceUID}", flush=True)

    ds = Dataset()
    ds.Manufacturer = "PYNETDICOM"
    return 0x0000, ds


ae = AE(ae_title="PYTIER2")
for sop_class in (
    GeneralRelevantPatientInformationQuery,
    HangingProtocolInformationModelFind,
    DisplaySystem,
):
    ae.add_supported_context(sop_class, ALL_TRANSFER_SYNTAXES)

ae.start_server(
    ("127.0.0.1", int(sys.argv[1])),
    evt_handlers=[(evt.EVT_C_FIND, handle_find), (evt.EVT_N_GET, handle_nget)],
)
PYEOF

  tier2_port=11702
  "$PYNETDICOM_BIN/python" "$tier2_scp_py" "$tier2_port" > "$WORKDIR/tier2_scp.log" 2>&1 &
  tier2_pid=$!

  if wait_for_port "$tier2_port"; then
    # Hanging Protocol Information Model FIND: a non-patient model, so the
    # identifier must carry the model's own key and no QueryRetrieveLevel.
    if "$GODICOM" findscu -aec PYTIER2 \
        -sop-class 1.2.840.10008.5.1.4.38.2 \
        -key 0072,0002 \
        "127.0.0.1:$tier2_port" > "$WORKDIR/tier2_hp.log" 2>&1; then

      if grep -q "FIND_ON: 1.2.840.10008.5.1.4.38.2" "$WORKDIR/tier2_scp.log" &&
         grep -q "FIND_HAS_LEVEL: False" "$WORKDIR/tier2_scp.log"; then
        pass "Hanging Protocol FIND — pynetdicom saw the model and no QueryRetrieveLevel"
        RAN_ANY=1
      else
        fail "pynetdicom did not receive a well-formed Hanging Protocol query"
        sed -n '1,20p' "$WORKDIR/tier2_scp.log" >&2
      fi
    else
      fail "go-dicom findscu could not query the Hanging Protocol model"
      sed -n '1,20p' "$WORKDIR/tier2_hp.log" >&2
    fi

    # Relevant Patient Information Query: a C-FIND with a patient and a template,
    # and no level either.
    if "$GODICOM" findscu -aec PYTIER2 \
        -sop-class 1.2.840.10008.5.1.4.37.1 \
        -key 0010,0020=P1 -key 0040,E001 \
        "127.0.0.1:$tier2_port" > "$WORKDIR/tier2_rpi.log" 2>&1; then

      if grep -q "FIND_ON: 1.2.840.10008.5.1.4.37.1" "$WORKDIR/tier2_scp.log"; then
        pass "Relevant Patient Information Query — pynetdicom saw the right SOP class"
        RAN_ANY=1
      else
        fail "pynetdicom did not receive the Relevant Patient Information query"
        sed -n '1,30p' "$WORKDIR/tier2_scp.log" >&2
      fi
    else
      fail "go-dicom findscu could not issue a Relevant Patient Information query"
      sed -n '1,20p' "$WORKDIR/tier2_rpi.log" >&2
    fi
  else
    fail "the pynetdicom tier-2 SCP did not start listening"
  fi
  stop_server "$tier2_pid"
fi

# ---------------------------------------------------------------------------
# Media Creation Management and the two event logging classes (#370).
#
# They are N-CREATE, N-ACTION and N-GET against pynetdicom, in both directions,
# and no command line drives them, so the check is the Go tests that do: each
# fails on what pynetdicom read of our bytes, or on what go-dicom read of its.
# GODICOM_REQUIRE_INTEROP turns a test that cannot find pynetdicom into a
# failure, since here it was found.
if [ -n "$PYNETDICOM_BIN" ] && [ -x "$PYNETDICOM_BIN/python" ]; then
  note "Media Creation Management and event logging against pynetdicom"
  if PATH="$PYNETDICOM_BIN:$PATH" GODICOM_REQUIRE_INTEROP=1 go test ./network/ -count=1 -v \
    -run 'TestMediaCreationAgainstPynetdicom|TestMediaCreationSCPAgainstPynetdicom|TestEventLoggingAgainstPynetdicom|TestEventLoggingSCPAgainstPynetdicom' \
    > "$WORKDIR/mediaevent.log" 2>&1; then
    passed=$(grep -c '^--- PASS' "$WORKDIR/mediaevent.log" || true)
    if [ "$passed" -eq 4 ]; then
      pass "Media Creation Management, Procedural Event Logging, Substance Administration Logging — both directions, $passed tests"
      RAN_ANY=1
    else
      fail "expected 4 media creation and event logging tests to pass, $passed did"
      grep -E '^(---|=== RUN)' "$WORKDIR/mediaevent.log" >&2 || true
    fi
  else
    fail "media creation or event logging does not agree with pynetdicom"
    grep -vE '^=== (RUN|PAUSE|CONT)' "$WORKDIR/mediaevent.log" | sed -n '1,30p' >&2
  fi
fi

# ---------------------------------------------------------------------------
# TLS, both directions. Until SCUConfig.TLS an SCU could not associate over TLS
# at all, and TLSConfig.CAFile was never read, so a server that was meant to
# require client certificates accepted anyone (#180, #182). The PKI is made by
# OpenSSL rather than by this library, and the peer is Python's ssl module.
if [ -n "$PYNETDICOM_BIN" ] && [ -x "$PYNETDICOM_BIN/python" ] && command -v openssl >/dev/null; then
  note "TLS: pynetdicom <-> go-dicom, mutual authentication"

  pki="$WORKDIR/pki"
  mkdir -p "$pki"
  openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -days 1 \
    -keyout "$pki/ca.key" -out "$pki/ca.crt" -subj /CN=interop-ca 2>/dev/null
  openssl req -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes \
    -keyout "$pki/node.key" -out "$pki/node.csr" -subj /CN=node 2>/dev/null
  printf 'subjectAltName=IP:127.0.0.1\nextendedKeyUsage=serverAuth,clientAuth\n' > "$pki/node.ext"
  openssl x509 -req -in "$pki/node.csr" -CA "$pki/ca.crt" -CAkey "$pki/ca.key" -CAcreateserial \
    -days 1 -extfile "$pki/node.ext" -out "$pki/node.crt" 2>/dev/null
  go_tls=(-tls -tls-cert "$pki/node.crt" -tls-key "$pki/node.key" -tls-ca "$pki/ca.crt")

  tls_client_py="$WORKDIR/tls_client.py"
  cat > "$tls_client_py" <<'PYEOF'
import ssl, sys
from pydicom import dcmread
from pynetdicom import AE
from pynetdicom.sop_class import CTImageStorage
pki, port, fixture, with_cert = sys.argv[1], int(sys.argv[2]), sys.argv[3], sys.argv[4] == "1"
ctx = ssl.create_default_context(ssl.Purpose.SERVER_AUTH, cafile=pki + "/ca.crt")
if with_cert:
    ctx.load_cert_chain(pki + "/node.crt", pki + "/node.key")
ae = AE(ae_title="PYTLS")
ae.add_requested_context(CTImageStorage)
assoc = ae.associate("127.0.0.1", port, ae_title="STORESCP", tls_args=(ctx, "127.0.0.1"))
if not assoc.is_established:
    print("NOT ASSOCIATED", flush=True)
    sys.exit(2)
status = assoc.send_c_store(dcmread(fixture))
assoc.release()
print("STATUS 0x%04X" % status.Status, flush=True)
sys.exit(0 if status.Status == 0 else 1)
PYEOF

  tls_dir="$WORKDIR/tls_in"
  mkdir -p "$tls_dir"
  start_server "$tls_dir" "$WORKDIR/tls_storescp.log" "$GODICOM" storescp -port 11720 -output "$tls_dir" "${go_tls[@]}"
  tls_scp=$SERVER_PID
  if wait_for_port 11720; then
    if "$PYNETDICOM_BIN/python" "$tls_client_py" "$pki" 11720 "$FIXTURE" 1 > "$WORKDIR/tls_client.log" 2>&1; then
      received=$(find "$tls_dir" -name '*.dcm' | head -1)
      if [ -n "$received" ]; then
        verify_transfer "$received" "TLS C-STORE, pynetdicom -> go-dicom storescp -tls"
        RAN_ANY=1
      else
        fail "go-dicom storescp -tls accepted the C-STORE but wrote nothing"
      fi
    else
      fail "pynetdicom could not store to go-dicom storescp -tls"
      sed -n '1,20p' "$WORKDIR/tls_client.log" "$WORKDIR/tls_storescp.log" >&2
    fi
    if "$PYNETDICOM_BIN/python" "$tls_client_py" "$pki" 11720 "$FIXTURE" 0 > "$WORKDIR/tls_anon.log" 2>&1; then
      fail "go-dicom storescp -tls -tls-ca accepted a client with no certificate"
    else
      pass "TLS — go-dicom storescp -tls-ca refuses a pynetdicom client with no certificate"
    fi
  else
    fail "go-dicom storescp -tls did not start listening"
  fi
  stop_server "$tls_scp"

  tls_scp_py="$WORKDIR/tls_scp.py"
  cat > "$tls_scp_py" <<'PYEOF'
import os, ssl, sys
from pynetdicom import AE, evt, AllStoragePresentationContexts
pki, port, out = sys.argv[1], int(sys.argv[2]), sys.argv[3]
ctx = ssl.create_default_context(ssl.Purpose.CLIENT_AUTH, cafile=pki + "/ca.crt")
ctx.load_cert_chain(pki + "/node.crt", pki + "/node.key")
ctx.verify_mode = ssl.CERT_REQUIRED
def on_store(event):
    with open(os.path.join(out, event.request.AffectedSOPInstanceUID + ".dcm"), "wb") as f:
        f.write(event.encoded_dataset())
    return 0
ae = AE(ae_title="PYTLSSCP")
ae.supported_contexts = AllStoragePresentationContexts
ae.start_server(("127.0.0.1", port), ssl_context=ctx, evt_handlers=[(evt.EVT_C_STORE, on_store)])
PYEOF
  py_tls_dir="$WORKDIR/tls_py"
  mkdir -p "$py_tls_dir"
  "$PYNETDICOM_BIN/python" "$tls_scp_py" "$pki" 11721 "$py_tls_dir" > "$WORKDIR/tls_scp.log" 2>&1 &
  py_tls_pid=$!
  PIDS+=("$py_tls_pid")
  if wait_for_port 11721; then
    if "$GODICOM" storescu -aec PYTLSSCP "${go_tls[@]}" 127.0.0.1:11721 "$FIXTURE" > "$WORKDIR/tls_storescu.log" 2>&1; then
      received=$(find "$py_tls_dir" -name '*.dcm' | head -1)
      if [ -n "$received" ]; then
        verify_transfer "$received" "TLS C-STORE, go-dicom storescu -tls -> pynetdicom"
        RAN_ANY=1
      else
        fail "pynetdicom accepted go-dicom's TLS association but holds no file"
      fi
    else
      fail "go-dicom storescu -tls could not store to pynetdicom"
      sed -n '1,20p' "$WORKDIR/tls_storescu.log" "$WORKDIR/tls_scp.log" >&2
    fi
  else
    fail "the pynetdicom TLS SCP did not start listening"
  fi
  stop_server "$py_tls_pid"
else
  skip "TLS needs pynetdicom and openssl"
fi

# ---------------------------------------------------------------------------
# JPEG 2000 encoding: go-dicom compresses, OpenJPEG decompresses (#308)
#
# The encoder is lossless, so the check is equality, and the decoder has to
# be one that does not share this library's reading of the standard. Each
# uncompressed corpus file with an image is written as JPEG 2000 Lossless,
# its frames are taken out of the file and given to opj_decompress, and what
# comes back is held against the source's pixel data, sample for sample.
# dcmtk has to read the file as well: it does not decode JPEG 2000, and it
# does say when the structure around the codestream is wrong.
#
# The samples are compared as numbers and without numpy, which the
# interoperability venv does not have.
# ---------------------------------------------------------------------------
if command -v opj_decompress >/dev/null 2>&1 && python3 -c "import pydicom" >/dev/null 2>&1; then
  note "JPEG 2000 encoding: go-dicom encoder, OpenJPEG decoder"
  RAN_ANY=1
  j2k_src="$WORKDIR/j2k-src"
  j2k_out="$WORKDIR/j2k-out"
  mkdir -p "$j2k_src" "$j2k_out" "$WORKDIR/j2k-frames"
  if python3 - "$j2k_src" <<'PYEOF'
import os, shutil, sys
import pydicom
import pydicom.data

corpus = os.path.join(os.path.dirname(pydicom.data.__file__), "test_files")
copied = 0
for name in sorted(os.listdir(corpus)):
    if not name.endswith(".dcm"):
        continue
    path = os.path.join(corpus, name)
    try:
        ds = pydicom.dcmread(path)
        if "PixelData" not in ds or ds.file_meta.TransferSyntaxUID.is_compressed:
            continue
        if int(ds.BitsAllocated) not in (8, 16):
            continue
    except Exception:
        continue
    shutil.copy(path, sys.argv[1])
    copied += 1
raise SystemExit(0 if copied else 1)
PYEOF
  then
    if go run ./scripts/roundtrip-check -jpeg2000 "$j2k_src" "$j2k_out" > "$WORKDIR/j2k.log" 2>&1; then
      j2k_unread=0
      for written in "$j2k_out"/*.dcm; do
        [ -f "$written" ] || continue
        if ! "$DCMTK_BIN/dcmdump" "$written" >/dev/null 2>"$WORKDIR/j2k-dump.err" || [ -s "$WORKDIR/j2k-dump.err" ]; then
          # A complaint the source also draws is the source's, not the encoder's.
          "$DCMTK_BIN/dcmdump" "$j2k_src/$(basename "$written")" >/dev/null 2>"$WORKDIR/j2k-src.err" || true
          if ! diff -q "$WORKDIR/j2k-dump.err" "$WORKDIR/j2k-src.err" >/dev/null 2>&1; then
            j2k_unread=$((j2k_unread + 1))
            echo "   dcmtk on $(basename "$written"): $(head -1 "$WORKDIR/j2k-dump.err")" >&2
          fi
        fi
      done
      if j2k=$(python3 - "$j2k_src" "$j2k_out" "$WORKDIR/j2k-frames" <<'PYEOF'
import os, struct, subprocess, sys
import pydicom
from pydicom.encaps import generate_pixel_data_frame

src, out, work = sys.argv[1:4]

def pgx(path):
    """One component as OpenJPEG writes it: a text header, then big endian samples."""
    with open(path, "rb") as f:
        header = f.readline().split()
        data = f.read()
    signed, depth, width, height = header[2] == b"-", int(header[3]), int(header[4]), int(header[5])
    size = 1 if depth <= 8 else 2
    code = ">" + str(width * height) + ("b" if size == 1 else "h") if signed else \
           ">" + str(width * height) + ("B" if size == 1 else "H")
    return struct.unpack(code, data[:width * height * size])

def source_samples(ds):
    """The source's samples as numbers, frame by frame, pixel by pixel."""
    rows, cols, spp = int(ds.Rows), int(ds.Columns), int(ds.SamplesPerPixel)
    frames = int(getattr(ds, "NumberOfFrames", 1) or 1)
    size = int(ds.BitsAllocated) // 8
    signed = int(ds.PixelRepresentation) == 1
    order = "<" if ds.file_meta.TransferSyntaxUID.is_little_endian else ">"
    n = rows * cols * spp * frames
    code = order + str(n) + ({1: "b", 2: "h"} if signed else {1: "B", 2: "H"})[size]
    raw = bytes(ds.PixelData)
    if size == 1 and order == ">" and ds["PixelData"].VR == "OW":
        # 8-bit samples in 16-bit words, big endian: each pair is swapped
        # (PS3.5 8.2 and Annex D).
        swapped = bytearray(raw)
        swapped[0::2], swapped[1::2] = raw[1::2], raw[0:len(raw) - len(raw) % 2:2]
        raw = bytes(swapped)
    values = struct.unpack(code, raw[:n * size])
    per = rows * cols * spp
    for f in range(frames):
        frame = values[f * per:(f + 1) * per]
        if spp > 1 and int(getattr(ds, "PlanarConfiguration", 0)) == 1:
            plane = rows * cols
            frame = tuple(frame[c * plane + i] for i in range(plane) for c in range(spp))
        yield frame

checked, frames_checked, bad = 0, 0, []
for name in sorted(os.listdir(out)):
    if not name.endswith(".dcm"):
        continue
    want = pydicom.dcmread(os.path.join(src, name))
    got = pydicom.dcmread(os.path.join(out, name))
    spp = int(want.SamplesPerPixel)
    frames = int(getattr(want, "NumberOfFrames", 1) or 1)
    # A source that holds fewer bytes than its own header describes says
    # nothing about the encoder.
    if len(want.PixelData) < int(want.Rows) * int(want.Columns) * spp * frames * int(want.BitsAllocated) // 8:
        continue
    if got.file_meta.TransferSyntaxUID != "1.2.840.10008.1.2.4.90":
        bad.append("%s was written as %s" % (name, got.file_meta.TransferSyntaxUID))
        continue
    # What the data set must say of an image coded this way (PS3.5 8.2.4).
    if spp == 3 and want.PhotometricInterpretation == "RGB" and got.PhotometricInterpretation != "YBR_RCT":
        bad.append("%s: an RGB image is described as %s" % (name, got.PhotometricInterpretation))
        continue
    if spp > 1 and int(got.PlanarConfiguration) != 0:
        bad.append("%s: Planar Configuration is %s" % (name, got.PlanarConfiguration))
        continue
    coded = list(generate_pixel_data_frame(got.PixelData, frames))
    if len(coded) != frames:
        bad.append("%s holds %d frames of %d" % (name, len(coded), frames))
        continue
    ok = True
    for index, (codestream, original) in enumerate(zip(coded, source_samples(want))):
        base = os.path.join(work, "frame")
        for stale in os.listdir(work):
            os.remove(os.path.join(work, stale))
        with open(base + ".j2k", "wb") as f:
            f.write(codestream)
        run = subprocess.run(["opj_decompress", "-i", base + ".j2k", "-o", base + ".pgx"], capture_output=True)
        if run.returncode != 0:
            bad.append("%s frame %d: OpenJPEG refuses it: %s" % (name, index, run.stderr.decode()[-200:]))
            ok = False
            break
        files = [base + ".pgx"] if spp == 1 and os.path.exists(base + ".pgx") else \
                [base + "_%d.pgx" % c for c in range(spp)]
        planes = [pgx(p) for p in files]
        decoded = tuple(planes[c][i] for i in range(len(planes[0])) for c in range(spp))
        if decoded != tuple(original):
            bad.append("%s frame %d: OpenJPEG decodes other samples" % (name, index))
            ok = False
            break
        frames_checked += 1
    checked += ok
for line in bad:
    print("   " + line, file=sys.stderr)
print("%d files, %d frames" % (checked, frames_checked))
raise SystemExit(1 if bad or not checked else 0)
PYEOF
      ) && [ "$j2k_unread" -eq 0 ]; then
        pass "JPEG 2000 encoding — OpenJPEG decodes every frame to the original samples, and dcmtk reads each file — $j2k"

        # And over the network: a file this encoder wrote, sent to dcmtk,
        # which takes it in JPEG 2000 Lossless and keeps the codestream.
        if [ -x "$DCMTK_BIN/storescp" ]; then
          mkdir -p "$WORKDIR/j2k-recv"
          start_server "$WORKDIR/j2k-recv" "$WORKDIR/j2k_scp.log" \
            "$DCMTK_BIN/storescp" +xv -od "$WORKDIR/j2k-recv" 11750
          J2K_SCP_PID=$SERVER_PID
          j2k_sent="$j2k_out/CT_small.dcm"
          if wait_for_port 11750 && "$GODICOM" storescu 127.0.0.1:11750 "$j2k_sent" >"$WORKDIR/j2k_scu.log" 2>&1 &&
            python3 - "$j2k_sent" "$WORKDIR/j2k-recv" <<'PYEOF'
import os, sys
import pydicom

sent = pydicom.dcmread(sys.argv[1])
received = [os.path.join(sys.argv[2], n) for n in os.listdir(sys.argv[2]) if not n.endswith(".log")]
if len(received) != 1:
    sys.exit("dcmtk kept %d files" % len(received))
got = pydicom.dcmread(received[0])
if got.file_meta.TransferSyntaxUID != "1.2.840.10008.1.2.4.90":
    sys.exit("dcmtk received it as %s" % got.file_meta.TransferSyntaxUID)
if bytes(got.PixelData) != bytes(sent.PixelData):
    sys.exit("the codestream dcmtk kept is not the one that was sent")
PYEOF
          then
            pass "JPEG 2000 over the network — dcmtk storescp receives the file in 1.2.840.10008.1.2.4.90 with its codestream intact"
          else
            fail "dcmtk did not receive a JPEG 2000 Lossless file as sent: $(tail -2 "$WORKDIR/j2k_scu.log")"
          fi
          stop_server "$J2K_SCP_PID"
        fi
      else
        fail "this encoder's JPEG 2000 does not survive OpenJPEG or dcmtk: $j2k"
      fi
    else
      fail "the JPEG 2000 encoding harness failed"
      sed -n '1,20p' "$WORKDIR/j2k.log" >&2
    fi
  else
    skip "pydicom is not available to supply the corpus"
  fi
else
  skip "JPEG 2000 encoding needs opj_decompress (OpenJPEG) and pydicom"
fi

# ---------------------------------------------------------------------------
# Lossless JPEG encoding: go-dicom compresses, dcmtk decompresses (#336)
#
# Each uncompressed corpus file with an image of 8 or 16 bits is written as
# .57 (the predictor chosen per frame) and as .70 (predictor 1), dcmdjpeg
# decodes it, and what dcmtk returns is held against the source's samples,
# each masked to Bits Stored. dcmtk's lossless JPEG decoder shares no code
# with this library. pydicom is not the second decoder here: its call into
# pylibjpeg-libjpeg converts YCbCr to RGB whenever the data set says RGB,
# which is wrong for lossless JPEG and misreads dcmtk's own RGB output too.
# ---------------------------------------------------------------------------
if [ -x "$DCMTK_BIN/dcmdjpeg" ] && python3 -c "import pydicom" >/dev/null 2>&1; then
  note "Lossless JPEG encoding: go-dicom encoder, dcmtk decoder"
  RAN_ANY=1
  jl_src="$WORKDIR/jl-enc-src"
  mkdir -p "$jl_src"
  if python3 - "$jl_src" <<'PYEOF'
import os, shutil, sys
import pydicom
import pydicom.data

corpus = os.path.join(os.path.dirname(pydicom.data.__file__), "test_files")
copied = 0
for name in sorted(os.listdir(corpus)):
    if not name.endswith(".dcm"):
        continue
    path = os.path.join(corpus, name)
    try:
        ds = pydicom.dcmread(path)
        if "PixelData" not in ds or ds.file_meta.TransferSyntaxUID.is_compressed:
            continue
        if int(ds.BitsAllocated) not in (8, 16):
            continue
    except Exception:
        continue
    shutil.copy(path, sys.argv[1])
    copied += 1
raise SystemExit(0 if copied else 1)
PYEOF
  then
    for jl_mode in jpeg-lossless jpeg-lossless-sv1; do
      jl_out="$WORKDIR/jl-enc-$jl_mode"
      mkdir -p "$jl_out" "$jl_out-decoded"
      if go run ./scripts/roundtrip-check "-$jl_mode" "$jl_src" "$jl_out" > "$WORKDIR/jl-enc.log" 2>&1; then
        for written in "$jl_out"/*.dcm; do
          [ -f "$written" ] || continue
          "$DCMTK_BIN/dcmdjpeg" +cn "$written" "$jl_out-decoded/$(basename "$written")" \
            2>>"$WORKDIR/jl-dcmdjpeg.log" || true
        done
        if jl=$(python3 - "$jl_src" "$jl_out" "$jl_out-decoded" <<'PYEOF'
import os, struct, sys
import pydicom

src, out, decoded = sys.argv[1:4]

def samples(ds, stored):
    """Every sample as a number masked to Bits Stored, pixel by pixel."""
    rows, cols, spp = int(ds.Rows), int(ds.Columns), int(ds.SamplesPerPixel)
    frames = int(getattr(ds, "NumberOfFrames", 1) or 1)
    size = int(ds.BitsAllocated) // 8
    order = "<" if ds.file_meta.TransferSyntaxUID.is_little_endian else ">"
    n = rows * cols * spp * frames
    raw = bytes(ds.PixelData)
    if size == 1 and order == ">" and ds["PixelData"].VR == "OW":
        swapped = bytearray(raw)
        swapped[0::2], swapped[1::2] = raw[1::2], raw[0:len(raw) - len(raw) % 2:2]
        raw = bytes(swapped)
    values = struct.unpack(order + str(n) + ("B" if size == 1 else "H"), raw[:n * size])
    mask = (1 << stored) - 1
    if spp > 1 and int(getattr(ds, "PlanarConfiguration", 0)) == 1:
        plane, per = rows * cols, rows * cols * spp
        values = tuple(values[f * per + c * plane + i] for f in range(frames)
                       for i in range(plane) for c in range(spp))
    return [v & mask for v in values]

checked, bad = 0, []
for name in sorted(os.listdir(out)):
    if not name.endswith(".dcm"):
        continue
    want = pydicom.dcmread(os.path.join(src, name))
    frames = int(getattr(want, "NumberOfFrames", 1) or 1)
    if len(want.PixelData) < int(want.Rows) * int(want.Columns) * int(want.SamplesPerPixel) * \
            frames * int(want.BitsAllocated) // 8:
        continue
    path = os.path.join(decoded, name)
    if not os.path.exists(path):
        bad.append("%s: dcmdjpeg could not decode it" % name)
        continue
    got = pydicom.dcmread(path)
    stored = int(want.BitsStored)
    if samples(got, stored) != samples(want, stored):
        bad.append("%s: dcmtk decodes other samples" % name)
        continue
    checked += 1
for line in bad:
    print("   " + line, file=sys.stderr)
print("%d files" % checked)
raise SystemExit(1 if bad or not checked else 0)
PYEOF
        ); then
          pass "Lossless JPEG encoding ($jl_mode) — dcmdjpeg decodes every file to the original samples — $jl"
        else
          fail "this encoder's lossless JPEG ($jl_mode) does not survive dcmtk: $jl"
        fi
      else
        fail "the lossless JPEG encoding harness failed"
        sed -n '1,20p' "$WORKDIR/jl-enc.log" >&2
      fi
    done
  else
    skip "pydicom is not available to supply the corpus"
  fi
else
  skip "lossless JPEG encoding needs dcmdjpeg (dcmtk) and pydicom"
fi

# ---------------------------------------------------------------------------
# JPEG-LS Near-Lossless encoding: go-dicom compresses, dcmtk decompresses (#337)
#
# Each uncompressed, unsigned corpus image of 8 or 16 bits is written as .81
# with an allowed error of 2, dcmdjpls decodes it, and every sample dcmtk
# returns has to be within 2 of the source's. dcmtk's JPEG-LS decoder is
# CharLS, which shares no code with this library. A signed image is refused
# at NEAR above 0, by this library as by dcmtk, and is not part of the check.
# ---------------------------------------------------------------------------
if [ -x "$DCMTK_BIN/dcmdjpls" ] && python3 -c "import pydicom" >/dev/null 2>&1; then
  note "JPEG-LS Near-Lossless encoding: go-dicom encoder, dcmtk decoder"
  RAN_ANY=1
  jn_src="$WORKDIR/jn-src"
  jn_out="$WORKDIR/jn-out"
  mkdir -p "$jn_src" "$jn_out" "$jn_out-decoded"
  if python3 - "$jn_src" <<'PYEOF'
import os, shutil, sys
import pydicom
import pydicom.data

corpus = os.path.join(os.path.dirname(pydicom.data.__file__), "test_files")
copied = 0
for name in sorted(os.listdir(corpus)):
    if not name.endswith(".dcm"):
        continue
    path = os.path.join(corpus, name)
    try:
        ds = pydicom.dcmread(path)
        if "PixelData" not in ds or ds.file_meta.TransferSyntaxUID.is_compressed:
            continue
        if int(ds.BitsAllocated) not in (8, 16) or int(ds.PixelRepresentation) != 0:
            continue
    except Exception:
        continue
    shutil.copy(path, sys.argv[1])
    copied += 1
raise SystemExit(0 if copied else 1)
PYEOF
  then
    if go run ./scripts/roundtrip-check -jpegls-near 2 "$jn_src" "$jn_out" > "$WORKDIR/jn.log" 2>&1; then
      for written in "$jn_out"/*.dcm; do
        [ -f "$written" ] || continue
        "$DCMTK_BIN/dcmdjpls" "$written" "$jn_out-decoded/$(basename "$written")" 2>>"$WORKDIR/jn-dcmdjpls.log" || true
      done
      if jn=$(python3 - "$jn_src" "$jn_out" "$jn_out-decoded" <<'PYEOF'
import os, struct, sys
import pydicom

src, out, decoded = sys.argv[1:4]
NEAR = 2

def samples(ds, stored):
    """Every sample as a number masked to Bits Stored, pixel by pixel."""
    rows, cols, spp = int(ds.Rows), int(ds.Columns), int(ds.SamplesPerPixel)
    frames = int(getattr(ds, "NumberOfFrames", 1) or 1)
    size = int(ds.BitsAllocated) // 8
    order = "<" if ds.file_meta.TransferSyntaxUID.is_little_endian else ">"
    n = rows * cols * spp * frames
    raw = bytes(ds.PixelData)
    if size == 1 and order == ">" and ds["PixelData"].VR == "OW":
        swapped = bytearray(raw)
        swapped[0::2], swapped[1::2] = raw[1::2], raw[0:len(raw) - len(raw) % 2:2]
        raw = bytes(swapped)
    values = struct.unpack(order + str(n) + ("B" if size == 1 else "H"), raw[:n * size])
    mask = (1 << stored) - 1
    if spp > 1 and int(getattr(ds, "PlanarConfiguration", 0)) == 1:
        plane, per = rows * cols, rows * cols * spp
        values = tuple(values[f * per + c * plane + i] for f in range(frames)
                       for i in range(plane) for c in range(spp))
    return [v & mask for v in values]

checked, bad = 0, []
for name in sorted(os.listdir(out)):
    if not name.endswith(".dcm"):
        continue
    want = pydicom.dcmread(os.path.join(src, name))
    frames = int(getattr(want, "NumberOfFrames", 1) or 1)
    if len(want.PixelData) < int(want.Rows) * int(want.Columns) * int(want.SamplesPerPixel) * \
            frames * int(want.BitsAllocated) // 8:
        continue
    written = pydicom.dcmread(os.path.join(out, name))
    if written.file_meta.TransferSyntaxUID != "1.2.840.10008.1.2.4.81":
        bad.append("%s was written as %s" % (name, written.file_meta.TransferSyntaxUID))
        continue
    if str(getattr(written, "LossyImageCompression", "")) != "01":
        bad.append("%s is not marked lossy" % name)
        continue
    path = os.path.join(decoded, name)
    if not os.path.exists(path):
        bad.append("%s: dcmdjpls could not decode it" % name)
        continue
    stored = int(want.BitsStored)
    a, b = samples(pydicom.dcmread(path), stored), samples(want, stored)
    worst = max(abs(x - y) for x, y in zip(a, b)) if a else 0
    if len(a) != len(b) or worst > NEAR:
        bad.append("%s: dcmtk decodes a sample %d away" % (name, worst))
        continue
    checked += 1
for line in bad:
    print("   " + line, file=sys.stderr)
print("%d files" % checked)
raise SystemExit(1 if bad or not checked else 0)
PYEOF
      ); then
        pass "JPEG-LS Near-Lossless encoding — dcmdjpls decodes every file to within NEAR 2 of the original — $jn"
      else
        fail "this encoder's near-lossless JPEG-LS does not survive dcmtk: $jn"
      fi
    else
      fail "the near-lossless JPEG-LS encoding harness failed"
      sed -n '1,20p' "$WORKDIR/jn.log" >&2
    fi
  else
    skip "pydicom is not available to supply the corpus"
  fi
else
  skip "JPEG-LS Near-Lossless encoding needs dcmdjpls (dcmtk) and pydicom"
fi

# ---------------------------------------------------------------------------
# DICOMweb against a server that is not this library (#285)
#
# The client had its own test server and nothing else in CI. Checked by hand
# against Orthanc for 1.8.0, it turned out that Orthanc answers 400, not 409
# or 202, for a store it kept part of, which no test server written from the
# standard would have done. So: stow, qido and wado against Orthanc, and what
# they return held against what Python reads from the same server.
#
# ORTHANC_URL is the service's DICOMweb root, http://127.0.0.1:8042/dicom-web
# in CI. Without one the step is skipped, as for every other peer.
# ---------------------------------------------------------------------------
if [ -n "${ORTHANC_URL:-}" ] && python3 -c "import urllib.request, sys; urllib.request.urlopen(sys.argv[1] + '/studies', timeout=5)" "$ORTHANC_URL" >/dev/null 2>&1; then
  note "DICOMweb: go-dicom stow, qido and wado against Orthanc"
  RAN_ANY=1
  mkdir -p "$WORKDIR/web_src" "$WORKDIR/web_back"
  if web=$(python3 - "$GODICOM" "$ORTHANC_URL" "$WORKDIR/web_src" "$WORKDIR/web_back" <<'PY'
import json, os, shutil, subprocess, sys, urllib.request
import pydicom
from pydicom.data import get_testdata_file

go, url, src, back = sys.argv[1:5]

def run(*args):
    return subprocess.run([go, *args], capture_output=True, text=True)

# Three transfer syntaxes, and a file that is not DICOM, which stow passes over.
names = ["CT_small.dcm", "MR_small.dcm", "SC_rgb_rle.dcm"]
for name in names:
    shutil.copy(get_testdata_file(name), src)
with open(os.path.join(src, "notes.txt"), "w") as f:
    f.write("not an image")

stored = run("stow", url, src)
if stored.returncode != 0 or "Stored 3 instance(s)" not in stored.stdout:
    sys.exit("stow: %s%s" % (stored.stdout[-300:], stored.stderr[-300:]))

for name in names:
    path = os.path.join(src, name)
    ds = pydicom.dcmread(path, stop_before_pixels=True)
    study, series, sop = str(ds.StudyInstanceUID), str(ds.SeriesInstanceUID), str(ds.SOPInstanceUID)

    # The search, as this library makes it and as Python does.
    found = run("qido", "-level", "instance", "-study", study, "-series", series,
                "-match", "SOPInstanceUID=" + sop, "-json", url)
    if found.returncode != 0:
        sys.exit("qido %s: %s" % (name, found.stderr[-300:]))
    ours = json.loads(found.stdout)
    request = urllib.request.Request(
        "%s/studies/%s/series/%s/instances?SOPInstanceUID=%s" % (url, study, series, sop),
        headers={"Accept": "application/dicom+json"})
    theirs = json.loads(urllib.request.urlopen(request, timeout=30).read())
    uid = lambda results: [r["00080018"]["Value"][0] for r in results]
    if uid(ours) != [sop] or uid(ours) != uid(theirs):
        sys.exit("qido %s: found %r, Python found %r" % (name, uid(ours), uid(theirs)))

    # The retrieve: the file that was stored, byte for byte.
    got = run("wado", "-study", study, "-series", series, "-instance", sop, url, back)
    if got.returncode != 0:
        sys.exit("wado %s: %s" % (name, got.stderr[-300:]))
    with open(path, "rb") as a, open(os.path.join(back, sop + ".dcm"), "rb") as b:
        if a.read() != b.read():
            sys.exit("wado %s: the file that came back is not the file that was stored" % name)

# A store the server refuses fails the command, and says which instance.
refused = run("stow", "-study", "1.2.3.4.5", url, os.path.join(src, names[0]))
if refused.returncode == 0 or "refused" not in refused.stdout + refused.stderr:
    sys.exit("a store into the wrong study was reported as stored: %s" % refused.stdout[-200:])
print("%d files stored, found and retrieved byte for byte; a refusal reported" % len(names))
PY
  ); then
    pass "stow, qido and wado agree with Orthanc and with Python — $web"
  else
    fail "DICOMweb against Orthanc: $web"
  fi
else
  skip "DICOMweb needs an Orthanc: set ORTHANC_URL to its DICOMweb root"
fi

# ---------------------------------------------------------------------------
# The archive over DICOMweb (#294)
#
# qrscp -web serves the store a DIMSE peer writes to. So: pynetdicom stores
# three files over DIMSE, and a client that is not this library (urllib)
# searches for them over HTTP. What each search returns is held against what
# pydicom reads from the file, against what pynetdicom finds by C-FIND on the
# same archive, and, when there is an Orthanc holding the same files, against
# what Orthanc answers. The token is checked the same way: from outside.
# ---------------------------------------------------------------------------
if python3 -c "import pydicom, pynetdicom, numpy" >/dev/null 2>&1; then
  note "DICOMweb: the archive searched, retrieved from and stored to over HTTP by a client that is not this library"
  RAN_ANY=1
  mkdir -p "$WORKDIR/webarc" "$WORKDIR/webarc_guarded" "$WORKDIR/webarc_back"
  start_server "$WORKDIR" "$WORKDIR/webarc.log" \
    "$GODICOM" qrscp -port 11740 -web 18740 -output "$WORKDIR/webarc"
  WEBARC_PID=$SERVER_PID
  start_server "$WORKDIR" "$WORKDIR/webarc_guarded.log" \
    "$GODICOM" qrscp -port 11741 -web 18741 -web-token interop-token -output "$WORKDIR/webarc_guarded"
  WEBARC_GUARDED_PID=$SERVER_PID
  if wait_for_port 11740 && wait_for_port 18740 && wait_for_port 18741; then
    if webarc=$(python3 - "${ORTHANC_URL:-}" "$GODICOM" "$WORKDIR/webarc_back" <<'PY'
import io, json, os, re, subprocess, sys, urllib.error, urllib.parse, urllib.request
import numpy
import pydicom
from pydicom.data import get_testdata_file
from pydicom.dataset import Dataset
from pynetdicom import AE
from pynetdicom.sop_class import StudyRootQueryRetrieveInformationModelFind as FIND

orthanc = sys.argv[1].rstrip("/")
go, back = sys.argv[2], sys.argv[3]
ours = "http://127.0.0.1:18740/dicom-web"
EXPLICIT = "1.2.840.10008.1.2.1"
DICOM = 'multipart/related; type="application/dicom"'

def retrieve(base, path, accept):
    # The parts of a multipart answer, taken apart by hand: this is the
    # check that the body is what PS3.18 8.6 says, not what our reader takes.
    request = urllib.request.Request(base + path, headers={"Accept": accept})
    with urllib.request.urlopen(request, timeout=60) as r:
        content_type, body = r.headers["Content-Type"], r.read()
    boundary = re.search(r'boundary="?([^";]+)"?', content_type).group(1).encode()
    parts = []
    for chunk in body.split(b"--" + boundary)[1:]:
        if chunk.startswith(b"--"):
            break
        header, _, data = chunk.partition(b"\r\n\r\n")
        parts.append((header.decode(), data[:-2] if data.endswith(b"\r\n") else data))
    return parts

def get(base, path, query=None, token=None):
    url = base + path + ("?" + urllib.parse.urlencode(query) if query else "")
    headers = {"Accept": "application/dicom+json"}
    if token:
        headers["Authorization"] = "Bearer " + token
    try:
        with urllib.request.urlopen(urllib.request.Request(url, headers=headers), timeout=30) as r:
            body = r.read()
            return r.status, json.loads(body) if body.strip() else []
    except urllib.error.HTTPError as e:
        return e.code, []

def first(result, tag):
    values = result.get(tag, {}).get("Value") or [None]
    v = values[0]
    return v.get("Alphabetic") if isinstance(v, dict) else v

# Stored over DIMSE, by pynetdicom.
names = ["CT_small.dcm", "MR_small.dcm", "SC_rgb_rle.dcm"]
files = [pydicom.dcmread(get_testdata_file(n)) for n in names]
ae = AE()
# Each file in the transfer syntax it is in: the archive keeps it as sent.
for ds in files:
    ae.add_requested_context(ds.SOPClassUID, ds.file_meta.TransferSyntaxUID)
ae.add_requested_context(FIND)
assoc = ae.associate("127.0.0.1", 11740, ae_title="QRSCP")
if not assoc.is_established:
    sys.exit("pynetdicom could not associate with qrscp")
for ds in files:
    status = assoc.send_c_store(ds)
    if not status or status.Status != 0:
        sys.exit("pynetdicom could not store %s" % ds.SOPInstanceUID)

# Each file, found at each level over HTTP, with the values pydicom reads.
for name, ds in zip(names, files):
    study, series, sop = str(ds.StudyInstanceUID), str(ds.SeriesInstanceUID), str(ds.SOPInstanceUID)
    status, studies = get(ours, "/studies", {"StudyInstanceUID": study})
    if status != 200 or len(studies) != 1:
        sys.exit("%s: /studies answered %d with %d results" % (name, status, len(studies)))
    want = {"00100010": str(ds.PatientName), "00100020": str(ds.PatientID),
            "00080020": str(ds.get("StudyDate", "")) or None}
    for tag, value in want.items():
        if value and first(studies[0], tag) != value:
            sys.exit("%s: study %s is %r, pydicom reads %r" % (name, tag, first(studies[0], tag), value))
    if first(studies[0], "00201208") != 1 or first(studies[0], "00080061") != str(ds.Modality):
        sys.exit("%s: the study's counts are %r" % (name, studies[0]))

    status, serieses = get(ours, "/studies/%s/series" % study)
    if [first(s, "0020000E") for s in serieses] != [series] or first(serieses[0], "00080060") != str(ds.Modality):
        sys.exit("%s: /series answered %r" % (name, serieses))

    status, instances = get(ours, "/studies/%s/series/%s/instances" % (study, series))
    if [first(i, "00080018") for i in instances] != [sop]:
        sys.exit("%s: /instances answered %r" % (name, instances))
    got = instances[0]
    size = (first(got, "00280010"), first(got, "00280011"), first(got, "00280100"), first(got, "00080016"))
    if size != (int(ds.Rows), int(ds.Columns), int(ds.BitsAllocated), str(ds.SOPClassUID)):
        sys.exit("%s: the instance is %r, pydicom reads %r" % (
            name, size, (ds.Rows, ds.Columns, ds.BitsAllocated, ds.SOPClassUID)))

    # Retrieved as stored: one part, the same instance, in the syntax the
    # archive keeps it in, with the pixel data it was sent.
    path = "/studies/%s/series/%s/instances/%s" % (study, series, sop)
    stored = retrieve(ours, path, DICOM + "; transfer-syntax=*")
    if len(stored) != 1:
        sys.exit("%s: retrieved %d parts" % (name, len(stored)))
    kept = pydicom.dcmread(io.BytesIO(stored[0][1]))
    sent = ds.file_meta.TransferSyntaxUID
    want_syntax = sent if sent.is_compressed else EXPLICIT
    if kept.SOPInstanceUID != sop or kept.file_meta.TransferSyntaxUID != want_syntax:
        sys.exit("%s: retrieved %s in %s" % (name, kept.SOPInstanceUID, kept.file_meta.TransferSyntaxUID))
    if "transfer-syntax=%s" % want_syntax not in stored[0][0]:
        sys.exit("%s: the part is labeled %r" % (name, stored[0][0]))
    if kept.PixelData != ds.PixelData:
        sys.exit("%s: the pixel data retrieved is not the pixel data stored" % name)

    # Retrieved with no syntax named, which asks for Explicit VR Little
    # Endian: decoded by the archive, and the same pixels as pydicom decodes.
    plain = retrieve(ours, path, DICOM)
    decoded = pydicom.dcmread(io.BytesIO(plain[0][1]))
    if decoded.file_meta.TransferSyntaxUID != EXPLICIT:
        sys.exit("%s: the default syntax came back as %s" % (name, decoded.file_meta.TransferSyntaxUID))
    if not numpy.array_equal(decoded.pixel_array, ds.pixel_array):
        sys.exit("%s: decoded for the default syntax, the pixels differ from pydicom's" % name)

    # The whole study by this library's own command: the same bytes.
    out = os.path.join(back, sop)
    got = subprocess.run([go, "wado", "-study", study, ours, out], capture_output=True, text=True)
    if got.returncode != 0:
        sys.exit("%s: wado: %s" % (name, got.stderr[-300:]))
    with open(os.path.join(out, sop + ".dcm"), "rb") as f:
        if f.read() != stored[0][1]:
            sys.exit("%s: wado wrote a file that is not the part the server sent" % name)

    # Metadata: the attributes without the pixel data, as pydicom reads them.
    status, metadata = get(ours, path + "/metadata")
    if status != 200 or len(metadata) != 1:
        sys.exit("%s: metadata answered %d with %d results" % (name, status, len(metadata)))
    described = pydicom.Dataset.from_json(metadata[0])
    for keyword in ("PatientName", "Rows", "Columns", "SOPClassUID", "PhotometricInterpretation"):
        if described[keyword].value != ds[keyword].value:
            sys.exit("%s: metadata %s is %r, the file has %r" % (name, keyword, described[keyword].value, ds[keyword].value))
    if set(metadata[0].get("7FE00010", {})) - {"vr"}:
        sys.exit("%s: the metadata carries the pixel data" % name)
    if any(key.startswith("0002") for key in metadata[0]):
        sys.exit("%s: the metadata carries the file's meta information" % name)
    if orthanc:
        _, theirs = get(orthanc, path + "/metadata")
        # A DS or an IS may be a JSON number or a string (PS3.18 F.2.3.1),
        # and the two servers choose differently, so numbers are compared
        # by their value.
        def plain(values):
            if values is None or any(isinstance(v, dict) for v in values):
                return values
            out = []
            for v in values:
                try:
                    out.append(float(v))
                except (TypeError, ValueError):
                    out.append(v)
            return out
        # Specific Character Set is left out: DICOM JSON is UTF-8, which this
        # server says (ISO_IR 192), where Orthanc repeats what the file said.
        for tag in set(metadata[0]) & set(theirs[0]) - {"00080005"}:
            a, b = plain(metadata[0][tag].get("Value")), plain(theirs[0][tag].get("Value"))
            if a is not None and b is not None and a != b:
                sys.exit("%s: metadata %s is %r against Orthanc's %r" % (name, tag, a, b))
        missing = [t for t in theirs[0] if t not in metadata[0]]
        if missing:
            sys.exit("%s: Orthanc's metadata has %s and ours does not" % (name, missing[:5]))

    # Against Orthanc, which the earlier step gave the same files: every
    # attribute both return has the same value.
    if orthanc:
        for path in ("/studies?StudyInstanceUID=" + study, "/studies/%s/series" % study,
                     "/studies/%s/series/%s/instances" % (study, series)):
            _, mine = get(ours, path)
            _, theirs = get(orthanc, path)
            if len(mine) != 1 or len(theirs) != 1:
                sys.exit("%s %s: %d results against Orthanc's %d" % (name, path, len(mine), len(theirs)))
            for tag in set(mine[0]) & set(theirs[0]) - {"00081190"}:
                a, b = mine[0][tag].get("Value"), theirs[0][tag].get("Value")
                if a != b:
                    sys.exit("%s %s %s: %r against Orthanc's %r" % (name, path, tag, a, b))

# Stored over HTTP by urllib, the multipart written by hand, and then found
# over DIMSE: the other direction through the same archive.
def stow(base, path, blobs):
    boundary = b"interop-boundary"
    body = b"".join(b"--" + boundary + b"\r\nContent-Type: application/dicom\r\n\r\n" + blob + b"\r\n"
                    for blob in blobs) + b"--" + boundary + b"--\r\n"
    request = urllib.request.Request(base + path, data=body, method="POST", headers={
        "Content-Type": 'multipart/related; type="application/dicom"; boundary=' + boundary.decode(),
        "Accept": "application/dicom+json"})
    try:
        with urllib.request.urlopen(request, timeout=60) as r:
            return r.status, json.loads(r.read())
    except urllib.error.HTTPError as e:
        text = e.read()
        return e.code, json.loads(text) if text.strip().startswith(b"{") else {}

extra_path = get_testdata_file("JPEG2000.dcm")
extra = pydicom.dcmread(extra_path)
with open(extra_path, "rb") as f:
    extra_bytes = f.read()
status, answer = stow(ours, "/studies", [extra_bytes])
kept = [i["00081155"]["Value"][0] for i in answer.get("00081199", {}).get("Value", [])]
if status != 200 or kept != [str(extra.SOPInstanceUID)]:
    sys.exit("a store over HTTP answered %d and kept %r" % (status, kept))
status, answer = stow(ours, "/studies/1.2.3.4.5", [extra_bytes])
reasons = [i["00081197"]["Value"][0] for i in answer.get("00081198", {}).get("Value", [])]
if status != 409 or reasons != [0xA900]:
    sys.exit("a store into the wrong study answered %d with %r" % (status, reasons))
status, answer = stow(ours, "/studies", [b"not an image", extra_bytes])
if status != 202 or len(answer.get("00081199", {}).get("Value", [])) != 1:
    sys.exit("a store of one instance and one stray part answered %d" % status)

find = Dataset()
find.QueryRetrieveLevel = "IMAGE"
find.StudyInstanceUID = str(extra.StudyInstanceUID)
find.SeriesInstanceUID = str(extra.SeriesInstanceUID)
find.SOPInstanceUID = str(extra.SOPInstanceUID)
found = [i for s, i in assoc.send_c_find(find, FIND) if i is not None]
if len(found) != 1:
    sys.exit("what was stored over HTTP was found %d times over DIMSE" % len(found))
came_back = retrieve(ours, "/studies/%s/series/%s/instances/%s" % (
    extra.StudyInstanceUID, extra.SeriesInstanceUID, extra.SOPInstanceUID), DICOM + "; transfer-syntax=*")
if pydicom.dcmread(io.BytesIO(came_back[0][1])).PixelData != extra.PixelData:
    sys.exit("what was stored over HTTP came back with other pixel data")
files.append(extra)

# And by this library's own command, which reports what the server kept.
sent = subprocess.run([go, "stow", ours, get_testdata_file("MR_small.dcm")], capture_output=True, text=True)
if sent.returncode != 0 or "Stored 1 instance(s)" not in sent.stdout:
    sys.exit("go-dicom stow to the archive: %s%s" % (sent.stdout[-200:], sent.stderr[-200:]))

# The same archive by C-FIND and by HTTP: the same studies, the same names.
query = Dataset()
query.QueryRetrieveLevel = "STUDY"
query.StudyInstanceUID = ""
query.PatientName = ""
by_find = sorted((str(i.StudyInstanceUID), str(i.PatientName))
                 for s, i in assoc.send_c_find(query, FIND) if i is not None)
assoc.release()
_, studies = get(ours, "/studies")
by_http = sorted((first(s, "0020000D"), first(s, "00100010")) for s in studies)
if by_find != by_http or len(by_http) != len({str(d.StudyInstanceUID) for d in files}):
    sys.exit("C-FIND found %r and HTTP found %r" % (by_find, by_http))

# A name is matched whatever its case, and nothing found is 204.
name = str(files[0].PatientName)
status, found = get(ours, "/studies", {"PatientName": name[:3].lower() + "*"})
if status != 200 or name not in [first(s, "00100010") for s in found]:
    sys.exit("a search for %r in lower case did not find it" % name)
status, found = get(ours, "/studies", {"PatientName": "NOBODY^AT^ALL"})
if status != 204:
    sys.exit("a search that matches nothing answered %d" % status)
status, _ = get(ours, "/studies", {"PatientNmae": "x"})
if status != 400:
    sys.exit("an attribute that does not exist answered %d" % status)

# The token: asked for, and nothing without it.
guarded = "http://127.0.0.1:18741/dicom-web"
if get(guarded, "/studies")[0] != 401 or get(guarded, "/studies", token="wrong")[0] != 401:
    sys.exit("the guarded archive answered a request with no token, or a wrong one")
if get(guarded, "/studies", token="interop-token")[0] != 204:
    sys.exit("the guarded archive refused its own token")

print("%d files stored over DIMSE and HTTP, found at three levels%s, retrieved as stored and decoded, and described; C-FIND and HTTP agree; a wrong study is refused; the token is enforced"
      % (len(files), ", as Orthanc answers" if orthanc else ""))
PY
    ); then
      pass "qrscp -web answers as pydicom, pynetdicom and Orthanc read the same files — $webarc"
    else
      fail "the archive over DICOMweb: $webarc"
    fi
  else
    fail "qrscp -web did not start: $(tail -3 "$WORKDIR/webarc.log")"
  fi
  stop_server "$WEBARC_PID"
  stop_server "$WEBARC_GUARDED_PID"
  # An address other machines can reach, with no token, is refused outright.
  if "$GODICOM" qrscp -port 11742 -web 0.0.0.0:18742 -output "$WORKDIR/webarc" >"$WORKDIR/webarc_open.log" 2>&1; then
    fail "qrscp served an archive on every interface with no token"
  elif grep -q -- "-web-token" "$WORKDIR/webarc_open.log"; then
    pass "qrscp -web refuses to listen beyond this machine without a token"
  else
    fail "qrscp -web 0.0.0.0 failed for another reason: $(tail -2 "$WORKDIR/webarc_open.log")"
  fi
else
  skip "the archive over DICOMweb needs pydicom and pynetdicom"
fi

# ---------------------------------------------------------------------------
# show --json: where each element sits in the file (#259)
#
# pydicom records where it found every value it read (file_tell). The
# offsets `show --json` prints are held against those, across pydicom's own
# corpus, so a wrong offset is caught by a reader that is not this one.
# ---------------------------------------------------------------------------
if python3 -c "import pydicom" >/dev/null 2>&1; then
  note "show --json: element offsets against pydicom's"
  RAN_ANY=1
  corpus=$(python3 -c "import os, pydicom.data; print(os.path.join(os.path.dirname(pydicom.data.__file__), 'test_files'))")
  if offsets=$(python3 - "$GODICOM" "$corpus" <<'PY'
import sys, json, subprocess, glob, os, warnings
import pydicom
warnings.simplefilter("ignore")
G, D = sys.argv[1], sys.argv[2]
files = ok = 0; elems = 0; bad = []
def walk(pds, gelems, path, name):
    global elems
    gmap = {e["tag"]: e for e in gelems}
    for pe in pds:
        t = "(%04X,%04X)" % (pe.tag.group, pe.tag.element)
        ge = gmap.get(t)
        if ge is None or pe.file_tell is None: continue
        elems += 1
        if ge["valueOffset"] != pe.file_tell:
            bad.append((name, path + t, ge["valueOffset"], pe.file_tell)); return
        # pydicom reads a sequence of stated length from a buffer of its own,
        # and its offsets inside one count from that buffer, not the file.
        if pe.VR == "SQ" and pe.is_undefined_length and "items" in ge and len(pe.value) == len(ge["items"]):
            for i, (pi, gi) in enumerate(zip(pe.value, ge["items"])):
                tell = getattr(pi, "seq_item_tell", None)
                if tell is not None and tell != gi["tagOffset"]:
                    bad.append((name, path + t + "[%d] item" % i, gi["tagOffset"], tell)); return
                walk(pi, gi["elements"], path + t + "[%d]" % i, name)
for f in sorted(glob.glob(os.path.join(D, "*.dcm"))):
    name = os.path.basename(f)
    r = subprocess.run([G, "show", "--json", f], capture_output=True, text=True)
    if r.returncode != 0: continue
    doc = json.loads(r.stdout)
    if doc["offsetsRelativeTo"] != "file": continue
    try: pds = pydicom.dcmread(f, force=True)
    except Exception: continue
    files += 1; before = len(bad)
    if hasattr(pds, "file_meta"): walk(pds.file_meta, doc["fileMeta"], "meta ", name)
    walk(pds, doc["dataSet"], "", name)
    ok += len(bad) == before
for b in bad[:12]: print("  DIFF", b)
if bad or files < 50: sys.exit("%d of %d files disagree" % (files - ok, files))
print("%d elements of %d files" % (elems, files))
PY
  ); then
    pass "value offsets agree with pydicom's — $offsets"
  else
    fail "show --json offsets differ from pydicom's: $offsets"
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
