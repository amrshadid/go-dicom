#!/usr/bin/env python3
"""Run the same jobs with go-dicom, dcmtk and pydicom and print a table.

The jobs are the ones a command line is used for:

  read    read a file and print every element
  decode  decode the pixel data of a compressed file and write the pixels
  send    store a file to a Storage SCP over loopback

Every job is a whole process, from the command line to its exit, timed by the
wall clock, so what is measured is what a person waits for: the program's
start, its work and its end. Python's import of pydicom is part of that for
pydicom, and is the larger part of its small jobs, so the pydicom columns are
given twice: the process, and the work after the imports (timed inside the
process, from after the imports to after the job).

Each job is run once to warm the file cache and then RUNS times (default 11);
the table gives the median in milliseconds. Nothing is estimated: a number is
the median of the timings this script took, and --verbose prints them all.

Before anything is timed, every lossless decode is checked to give the
samples of the image that was encoded, whichever program decoded it, so the
programs are doing the same job. A program that cannot do a job (dcmtk has no
JPEG 2000 decoder) is shown as n/a, not left out.

Needs: a go-dicom binary (GODICOM, default ./go-dicom), dcmtk (DCMTK_BIN),
OpenJPEG's opj_compress and opj_decompress (the same directory or PATH),
and Python with numpy, pydicom and pynetdicom, plus the codec plugins pydicom
needs for JPEG, JPEG-LS and JPEG 2000 (pylibjpeg with libjpeg and openjpeg).

  GODICOM=./go-dicom DCMTK_BIN=/opt/homebrew/bin python3 scripts/benchmark.py
"""

import argparse
import datetime
import os
import platform
import re
import shutil
import socket
import statistics
import subprocess
import sys
import tempfile
import time

import numpy as np

SIDE = 2048  # a 2048 x 2048, 8-bit image: 4 MiB of pixels
BIG_SIDE = 8192  # 64 MiB, for the network job


def free_port():
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


def make_image(side):
    """A deterministic picture with edges, gradients and noise, as pixels
    usually are: not a constant, not white noise."""
    rng = np.random.default_rng(20261118)
    y, x = np.mgrid[0:side, 0:side].astype(np.float64)
    img = 110 + 60 * np.sin(x / 97.0) * np.cos(y / 131.0) + 25 * np.sin((x + y) / 17.0)
    img += ((x // 256 + y // 256) % 2) * 30
    disc = (x - side * 0.5) ** 2 + (y - side * 0.4) ** 2 < (side * 0.18) ** 2
    img[disc] += 45
    img += rng.normal(0, 3.0, img.shape)
    return np.clip(img, 0, 255).astype(np.uint8)


def write_base(path, pixels):
    """An uncompressed secondary capture instance."""
    import pydicom
    from pydicom.dataset import FileDataset, FileMetaDataset
    from pydicom.uid import ExplicitVRLittleEndian, SecondaryCaptureImageStorage, generate_uid

    meta = FileMetaDataset()
    meta.MediaStorageSOPClassUID = SecondaryCaptureImageStorage
    meta.MediaStorageSOPInstanceUID = generate_uid()
    meta.TransferSyntaxUID = ExplicitVRLittleEndian
    meta.ImplementationClassUID = pydicom.uid.PYDICOM_IMPLEMENTATION_UID
    ds = FileDataset(path, {}, file_meta=meta, preamble=b"\0" * 128)
    ds.SOPClassUID = meta.MediaStorageSOPClassUID
    ds.SOPInstanceUID = meta.MediaStorageSOPInstanceUID
    ds.PatientName = "Benchmark^Synthetic"
    ds.PatientID = "BENCH0001"
    ds.StudyInstanceUID = generate_uid()
    ds.SeriesInstanceUID = generate_uid()
    ds.Modality = "OT"
    ds.StudyDate = "20261118"
    ds.SamplesPerPixel = 1
    ds.PhotometricInterpretation = "MONOCHROME2"
    ds.Rows, ds.Columns = pixels.shape
    ds.BitsAllocated = ds.BitsStored = 8
    ds.HighBit = 7
    ds.PixelRepresentation = 0
    ds.PixelData = pixels.tobytes()
    ds.save_as(path, write_like_original=False)


def encapsulated(src, dst, syntax_uid, frames):
    """src with its pixel data replaced by encapsulated frames."""
    import pydicom
    from pydicom.encaps import encapsulate

    ds = pydicom.dcmread(src)
    ds.PixelData = encapsulate(frames)
    ds["PixelData"].is_undefined_length = True
    ds.file_meta.TransferSyntaxUID = pydicom.uid.UID(syntax_uid)
    ds.is_little_endian, ds.is_implicit_VR = True, False
    ds.save_as(dst, write_like_original=False)


def run(cmd, **kw):
    kw.setdefault("check", True)
    kw.setdefault("capture_output", True)
    return subprocess.run(cmd, **kw)


class Rig:
    def __init__(self, args):
        self.args = args
        self.godicom = os.path.abspath(args.godicom)
        self.dcmtk = args.dcmtk
        self.python = args.python
        self.work = args.workdir
        self.timings = {}
        self.servers = []

    def tool(self, name):
        path = os.path.join(self.dcmtk, name) if self.dcmtk else name
        found = shutil.which(path)
        if not found:
            sys.exit("missing: %s (set DCMTK_BIN)" % name)
        return found

    def opj(self, name):
        found = shutil.which(os.path.join(self.dcmtk, name)) or shutil.which(name)
        if not found:
            sys.exit("missing: %s" % name)
        return found

    # ---------------------------------------------------------------- fixtures

    def fixtures(self):
        w = self.work
        pixels = make_image(SIDE)
        np.save(os.path.join(w, "pixels.npy"), pixels)
        base = os.path.join(w, "plain.dcm")
        write_base(base, pixels)

        small = os.path.join(w, "small.dcm")
        write_base(small, pixels[:128, :128].copy())

        big = os.path.join(w, "big.dcm")
        write_base(big, make_image(BIG_SIDE))

        # The encoders are not go-dicom's, nor pydicom's decoders': every
        # codec's input is made by another program, so that no program is
        # timed on its own output.
        rle = os.path.join(w, "rle.dcm")
        run([self.tool("dcmcrle"), base, rle])
        jpeg = os.path.join(w, "jpeg.dcm")
        run([self.tool("dcmcjpeg"), "+eb", base, jpeg])
        jpegll = os.path.join(w, "jpegll.dcm")
        run([self.tool("dcmcjpeg"), "+e1", base, jpegll])
        jls = os.path.join(w, "jls.dcm")
        run([self.tool("dcmcjpls"), base, jls])

        from PIL import Image
        pgm = os.path.join(w, "plain.pgm")
        Image.fromarray(pixels).save(pgm)
        j2k = os.path.join(w, "image.j2k")
        run([self.opj("opj_compress"), "-i", pgm, "-o", j2k])
        with open(j2k, "rb") as f:
            codestream = f.read()
        encapsulated(base, os.path.join(w, "j2k.dcm"), "1.2.840.10008.1.2.4.90", [codestream])
        self.j2k_codestream = j2k
        self.pixels = pixels

    # ----------------------------------------------------------------- timing

    def command(self, cmd, **kw):
        """A job that is one command: returns how long the process took."""
        def job():
            t0 = time.perf_counter()
            run(cmd, **kw)
            return (time.perf_counter() - t0) * 1000, None
        return job

    def python_job(self, code):
        """A pydicom job: the whole process, and the work after the imports,
        which the process reports itself."""
        prelude = "import time, pydicom, numpy\nfrom pydicom import dcmread\nt0 = time.perf_counter()\n"
        post = "\nprint((time.perf_counter() - t0) * 1000)\n"
        cmd = [self.python, "-c", prelude + code + post]

        def job():
            t0 = time.perf_counter()
            out = run(cmd, text=True).stdout.strip().splitlines()[-1]
            return (time.perf_counter() - t0) * 1000, float(out)
        return job

    def race(self, label, jobs):
        """Time the jobs of one row, one after another in turn, round after
        round, with the order rotated each round. The machine's load changes
        while this runs; taking the programs in turn gives each the same
        share of it, which running one program's eleven runs and then the
        next's does not. A job that is None is shown as n/a."""
        names = [n for n, j in jobs.items() if j is not None]
        for n in names:
            jobs[n]()  # warms the file cache, and fails early
        walls = {n: [] for n in names}
        inners = {n: [] for n in names}
        for r in range(self.args.runs):
            for n in names[r % len(names):] + names[:r % len(names)]:
                wall, inner = jobs[n]()
                walls[n].append(wall)
                if inner is not None:
                    inners[n].append(inner)
        row = {n: None for n in jobs}
        for n in names:
            row[n] = statistics.median(walls[n])
            if inners[n]:
                row[n + " (after import)"] = statistics.median(inners[n])
            if self.args.verbose:
                print("%-44s %s" % (label + " " + n, " ".join("%.0f" % s for s in walls[n])), file=sys.stderr)
                if inners[n]:
                    print("%-44s %s" % (label + " " + n + " (after import)", " ".join("%.0f" % s for s in inners[n])),
                          file=sys.stderr)
        return row

    # ------------------------------------------------------------------ check

    def check_decodes(self):
        """Every program must decode each lossless file to the samples that were
        encoded. JPEG Baseline is lossy: the programs are compared with each
        other, and the largest difference is reported."""
        w, px = self.work, self.pixels
        flat = px.tobytes()
        problems = []
        results = {}
        for codec in ("rle", "jpegll", "jls", "j2k", "jpeg"):
            src = os.path.join(w, codec + ".dcm")
            outs = {}
            # go-dicom: a NIfTI file, 352 bytes of header and then the voxels.
            out = os.path.join(w, "check-go.nii")
            run([self.godicom, "convert", "--format", "nifti", src, out])
            data = open(out, "rb").read()
            outs["go-dicom"] = data[len(data) - len(flat):]
            # dcmtk
            tool = {"rle": "dcmdrle", "jls": "dcmdjpls", "jpeg": "dcmdjpeg", "jpegll": "dcmdjpeg"}.get(codec)
            if tool:
                out = os.path.join(w, "check-dcmtk.dcm")
                run([self.tool(tool), src, out])
                outs["dcmtk"] = self.py_pixels(out)
            # pydicom
            outs["pydicom"] = self.py_pixels(src)
            for name, got in outs.items():
                if len(got) != len(flat):
                    problems.append("%s %s: %d bytes, want %d" % (name, codec, len(got), len(flat)))
                    continue
                if codec == "jpeg":
                    diff = np.abs(np.frombuffer(got, np.uint8).astype(int) - px.reshape(-1).astype(int))
                    results[(name, codec)] = int(diff.max())
                elif got != flat:
                    problems.append("%s decodes %s to other samples than were encoded" % (name, codec))
        # The OpenJPEG tool on the same codestream.
        out = os.path.join(w, "check-opj.pgm")
        run([self.opj("opj_decompress"), "-i", self.j2k_codestream, "-o", out])
        from PIL import Image
        if np.asarray(Image.open(out)).tobytes() != flat:
            problems.append("opj_decompress decodes the codestream to other samples")
        if problems:
            sys.exit("the programs are not doing the same job:\n  " + "\n  ".join(problems))
        self.jpeg_differences = results

    def py_pixels(self, path):
        code = "import sys,pydicom;sys.stdout.buffer.write(pydicom.dcmread(sys.argv[1]).pixel_array.tobytes())"
        return run([self.python, "-c", code, path]).stdout

    # ------------------------------------------------------------------- jobs

    def read_job(self, name, path):
        return self.race("read " + name, {
            "go-dicom": self.command([self.godicom, "show", path]),
            "dcmtk": self.command([self.tool("dcmdump"), path]),
            "pydicom": self.python_job("ds = dcmread(%r)\nprint(ds)" % path),
        })

    def decode_job(self, codec, tool):
        src = os.path.join(self.work, codec + ".dcm")
        out = os.path.join(self.work, "out-" + codec)
        return self.race("decode " + codec, {
            "go-dicom": self.command([self.godicom, "convert", "--format", "nifti", src, out + ".nii"]),
            "dcmtk": self.command([self.tool(tool), src, out + ".dcm"]) if tool else None,
            "pydicom": self.python_job("ds = dcmread(%r)\nds.pixel_array.tofile(%r)" % (src, out + ".raw")),
            # OpenJPEG's own tool, on the codestream of the same image, for the
            # codec dcmtk does not decode.
            "opj_decompress": self.command([self.opj("opj_decompress"), "-i", self.j2k_codestream,
                                            "-o", out + ".pgm"]) if codec == "j2k" else None,
        })

    def start_server(self, cmd, port):
        p = subprocess.Popen(cmd, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        self.servers.append(p)
        for _ in range(100):
            try:
                socket.create_connection(("127.0.0.1", port), 0.1).close()
                return
            except OSError:
                time.sleep(0.1)
        sys.exit("a server did not start: %s" % cmd)

    def send_job(self, name, path):
        """Each client sends to the dcmtk storescp, so the three are timed
        against the same peer, and then to the server of its own project."""
        w = self.work
        py_send = ("from pynetdicom import AE\n"
                   "from pynetdicom.sop_class import SecondaryCaptureImageStorage as SC\n"
                   "ae = AE(ae_title='PYCLIENT')\nae.add_requested_context(SC)\n"
                   "a = ae.associate('127.0.0.1', %d, ae_title='PEER')\n"
                   "assert a.is_established\n"
                   "s = a.send_c_store(dcmread(%r))\nassert s.Status == 0, s\na.release()")
        dcmtk_send = lambda port: [self.tool("storescu"), "-aet", "DCMCLIENT", "-aec", "PEER", "127.0.0.1", str(port), path]
        go_send = lambda port: [self.godicom, "storescu", "-aec", "PEER", "127.0.0.1:%d" % port, path]

        port = free_port()
        self.start_server([self.tool("storescp"), "-aet", "PEER", "-od", tempfile.mkdtemp(dir=w), str(port)], port)
        peer = self.race("send %s to dcmtk storescp" % name, {
            "go-dicom": self.command(go_send(port)),
            "dcmtk": self.command(dcmtk_send(port)),
            "pydicom": self.python_job(py_send % (port, path)),
        })

        p_go, p_dcmtk, p_py = free_port(), free_port(), free_port()
        self.start_server([self.godicom, "storescp", "-aet", "PEER", "-port", str(p_go),
                           "-output", tempfile.mkdtemp(dir=w)], p_go)
        self.start_server([self.tool("storescp"), "-aet", "PEER", "-od", tempfile.mkdtemp(dir=w), str(p_dcmtk)], p_dcmtk)
        server = ("from pynetdicom import AE, evt, AllStoragePresentationContexts\n"
                  "def h(event):\n    return 0x0000\n"
                  "ae = AE(ae_title='PEER')\nae.supported_contexts = AllStoragePresentationContexts\n"
                  "ae.start_server(('127.0.0.1', %d), evt_handlers=[(evt.EVT_C_STORE, h)])" % p_py)
        self.start_server([self.python, "-c", server], p_py)
        own = self.race("send %s to its own kind of server" % name, {
            "go-dicom": self.command(go_send(p_go)),
            "dcmtk": self.command(dcmtk_send(p_dcmtk)),
            "pydicom": self.python_job(py_send % (p_py, path)),
        })
        return {"to one peer (dcmtk storescp)": peer, "to its own kind of server": own}

    def stop(self):
        for p in self.servers:
            p.terminate()
        for p in self.servers:
            try:
                p.wait(5)
            except subprocess.TimeoutExpired:
                p.kill()


def versions(rig):
    def first_line(cmd):
        try:
            return run(cmd, text=True).stdout.strip().splitlines()[0]
        except Exception as e:  # a version that cannot be had is said so
            return "unknown (%s)" % e
    import pydicom, pynetdicom
    opj = run([rig.opj("opj_compress"), "-h"], text=True, check=False)
    opj_version = (opj.stdout + opj.stderr).strip().splitlines()
    opj_version = next((l.strip().rstrip(".").replace("It has been compiled against ", "OpenJPEG, ") for l in opj_version if "compiled against" in l), "unknown")
    sysctl = lambda k: run(["sysctl", "-n", k], text=True, check=False).stdout.strip()
    cpu = sysctl("machdep.cpu.brand_string") or platform.processor()
    mem = sysctl("hw.memsize")
    return {
        "go-dicom": first_line([rig.godicom, "--version"]).split()[-1],
        "dcmtk": re.search(r"dcmdump v[0-9.]+", first_line([rig.tool("dcmdump"), "--version"])).group(0).replace("dcmdump ", "dcmtk "),
        "pydicom": "pydicom %s, pynetdicom %s, numpy %s, Python %s" % (
            pydicom.__version__, pynetdicom.__version__, np.__version__, platform.python_version()),
        "openjpeg": opj_version,
        "machine": "%s, %s cores, %s GiB, %s %s" % (
            cpu, os.cpu_count(), round(int(mem) / 2 ** 30) if mem else "?", platform.system(), platform.release()),
    }


def table(title, rows, columns):
    lines = ["| %s | %s |" % (title, " | ".join(columns)), "|---|%s|" % "|".join("---:" for _ in columns)]
    for label, row in rows:
        cells = []
        for c in columns:
            v = row.get(c)
            cells.append("n/a" if v is None else ("%.0f" % v if v >= 10 else "%.1f" % v))
        lines.append("| %s | %s |" % (label, " | ".join(cells)))
    return "\n".join(lines)


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--runs", type=int, default=11, help="timed runs of each job (default 11)")
    ap.add_argument("--godicom", default=os.environ.get("GODICOM", "./go-dicom"))
    ap.add_argument("--dcmtk", default=os.environ.get("DCMTK_BIN", ""), help="directory of dcmtk's tools")
    ap.add_argument("--python", default=sys.executable, help="the Python that has pydicom and pynetdicom")
    ap.add_argument("--workdir", help="where the fixtures go (default: a temporary directory, removed after)")
    ap.add_argument("--skip-send", action="store_true", help="leave out the network jobs")
    ap.add_argument("--verbose", action="store_true", help="print every timing to standard error")
    args = ap.parse_args()
    if not os.access(args.godicom, os.X_OK):
        sys.exit("go-dicom binary not found: %s (go build -o go-dicom . or set GODICOM)" % args.godicom)

    cleanup = args.workdir is None
    args.workdir = args.workdir or tempfile.mkdtemp(prefix="godicom-bench-")
    os.makedirs(args.workdir, exist_ok=True)
    rig = Rig(args)
    try:
        rig.fixtures()
        rig.check_decodes()
        ver = versions(rig)
        load_before = os.getloadavg()[0]

        cols = ["go-dicom", "dcmtk", "pydicom", "pydicom (after import)"]
        out = []
        reads = [("a 128x128 image, 16 KiB", run_read(rig, "small")),
                 ("a 2048x2048 image, 4 MiB", run_read(rig, "plain")),
                 ("an 8192x8192 image, 64 MiB", run_read(rig, "big"))]
        out.append(table("Read a file and print every element (ms)", reads, cols))

        decodes = [("RLE Lossless", rig.decode_job("rle", "dcmdrle")),
                   ("JPEG Baseline (lossy)", rig.decode_job("jpeg", "dcmdjpeg")),
                   ("JPEG Lossless", rig.decode_job("jpegll", "dcmdjpeg")),
                   ("JPEG-LS Lossless", rig.decode_job("jls", "dcmdjpls")),
                   ("JPEG 2000 Lossless", rig.decode_job("j2k", None))]
        out.append(table("Decode a 2048x2048 image and write the pixels (ms)", decodes, cols + ["opj_decompress"]))
        out.append("`opj_decompress` is OpenJPEG's own tool, run on the codestream of the JPEG 2000 file: "
                   "dcmtk has no JPEG 2000 decoder, and this is the reference one.")

        if not args.skip_send:
            sends = []
            for name, path in (("4 MiB", os.path.join(rig.work, "plain.dcm")), ("64 MiB", os.path.join(rig.work, "big.dcm"))):
                for label, row in rig.send_job(name, path).items():
                    sends.append(("%s, %s" % (name, label), row))
            out.append(table("Send a file over loopback with C-STORE (ms)", sends, cols))

        print("## Results\n")
        print("Date: %s  " % datetime.date.today().isoformat())
        print("Machine: %s  " % ver["machine"])
        for k in ("go-dicom", "dcmtk", "pydicom", "openjpeg"):
            print("%s: %s  " % (k, ver[k]))
        print("Runs: 1 warm-up, then %d timed; each cell is the median. One-minute load average of the machine "
              "%.1f before the jobs and %.1f after, on %s cores.\n" % (args.runs, load_before, os.getloadavg()[0], os.cpu_count()))
        print("\n\n".join(out))
        if rig.jpeg_differences:
            worst = {n: d for (n, c), d in rig.jpeg_differences.items()}
            print("\nJPEG Baseline is lossy, so the programs were compared with the original samples: the largest "
                  "difference of any sample is %s." % ", ".join("%s %d" % kv for kv in sorted(worst.items())))
    finally:
        rig.stop()
        if cleanup:
            shutil.rmtree(args.workdir, ignore_errors=True)


def run_read(rig, name):
    return rig.read_job(name, os.path.join(rig.work, name + ".dcm"))


if __name__ == "__main__":
    main()
