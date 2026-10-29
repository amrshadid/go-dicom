#!/usr/bin/env python3
"""Write the lossless JPEG fixtures with restart intervals, checked by libjpeg.

Neither dcmtk nor pydicom's corpus has a lossless JPEG with restart markers, so
this encodes them from T.81 directly: 8 bits, a restart every two rows, at
selection values 1 and 7. Seven looks up as well as left, so it shows whether
the first line of each interval is predicted as a first line (H.1.2.1). They are
written here rather than in Go so that the encoder shares no code with the
decoder under test, and only once libjpeg (through pylibjpeg) decodes them to
the original samples.

    python3 scripts/jpeglossless-restart-fixture.py compress/testdata/jpeglossless
"""
import os
import struct
import sys

from libjpeg import decode

WIDTH, HEIGHT, PRECISION, ROWS_PER_INTERVAL = 16, 12, 8, 2
PREDICTORS = (1, 7)


def image():
    # Deterministic, with both smooth runs and jumps.
    return [[(x * 13 + y * 7 + (x * y) % 5 * 40) % 256 for x in range(WIDTH)] for y in range(HEIGHT)]


class Bits:
    def __init__(self):
        self.out, self.acc, self.n = bytearray(), 0, 0

    def put(self, value, count):
        for i in range(count - 1, -1, -1):
            self.acc = self.acc << 1 | (value >> i) & 1
            self.n += 1
            if self.n == 8:
                self.out.append(self.acc)
                if self.acc == 0xFF:
                    self.out.append(0x00)  # stuffing
                self.acc, self.n = 0, 0

    def pad(self):
        # F.1.2.3: pad the last byte with 1-bits.
        while self.n:
            self.put(1, 1)


def segment(marker, body):
    return struct.pack(">HH", 0xFF00 | marker, len(body) + 2) + body


def predict(sv, ra, rb, rc):
    return {1: ra, 2: rb, 3: rc, 4: ra + rb - rc, 5: ra + ((rb - rc) >> 1),
            6: rb + ((ra - rc) >> 1), 7: (ra + rb) >> 1}[sv]


def encode(pixels, sv):
    # Every category 0..16 has a 5-bit code: 17 codes, none all ones.
    counts = [0] * 16
    counts[4] = 17
    codes = {cat: cat for cat in range(17)}
    dht = bytes([0x00]) + bytes(counts) + bytes(range(17))

    sof = struct.pack(">BHHB", PRECISION, HEIGHT, WIDTH, 1) + bytes([1, 0x11, 0])
    interval = WIDTH * ROWS_PER_INTERVAL
    sos = bytes([1, 1, 0x00, sv, 0, 0])  # one component, table 0, Ss=sv, Se=0, Ah/Al=0

    bits = Bits()
    entropy = bytearray()
    restart = 0
    for y in range(HEIGHT):
        first_row = y % ROWS_PER_INTERVAL == 0  # H.1.2.1: each interval starts afresh
        for x in range(WIDTH):
            if first_row and x == 0:
                pred = 1 << (PRECISION - 1)
            elif first_row:
                pred = pixels[y][x - 1]
            elif x == 0:
                pred = pixels[y - 1][x]
            else:
                pred = predict(sv, pixels[y][x - 1], pixels[y - 1][x], pixels[y - 1][x - 1])
            diff = (pixels[y][x] - pred) % 65536
            if diff >= 32768:
                diff -= 65536
            cat = abs(diff).bit_length()
            bits.put(codes[cat], 5)
            if 0 < cat < 16:
                bits.put(diff if diff > 0 else diff - 1 + (1 << cat), cat)
        last = y == HEIGHT - 1
        if (y + 1) % ROWS_PER_INTERVAL == 0 and not last:
            bits.pad()
            entropy += bits.out + bytes([0xFF, 0xD0 + restart % 8])
            bits.out = bytearray()
            restart += 1
    bits.pad()
    entropy += bits.out

    return (b"\xff\xd8" + segment(0xC4, dht) + segment(0xC3, sof) +
            segment(0xDD, struct.pack(">H", interval)) + segment(0xDA, sos) +
            bytes(entropy) + b"\xff\xd9")


def main():
    pixels = image()
    out = sys.argv[1]
    for sv in PREDICTORS:
        data = encode(pixels, sv)
        if decode(data).tolist() != pixels:
            sys.exit(f"libjpeg does not decode the selection value {sv} fixture to the original samples")
        with open(os.path.join(out, f"restart_sv{sv}.jpgls"), "wb") as f:
            f.write(data)
        print(f"restart_sv{sv}.jpgls: {len(data)} bytes, {HEIGHT // ROWS_PER_INTERVAL} intervals; libjpeg agrees")
    with open(os.path.join(out, "restart_expected.raw"), "wb") as f:
        f.write(bytes(v for row in pixels for v in row))


if __name__ == "__main__":
    main()
