# Security Policy

## Supported Versions

| Version | Supported          |
|---------|--------------------|
| 1.x     | Yes                |
| < 1.0   | No                 |

## Reporting a Vulnerability

Report vulnerabilities privately through GitHub:

**[Open a private security advisory →](https://github.com/amrshadid/go-dicom/security/advisories/new)**

The form is visible only to you and the maintainers. It needs no email address
from either side, and it is the same mechanism used to publish the advisory and
request a CVE once a fix is ready.

Please include:

1. A description of the vulnerability
2. Steps to reproduce it
3. What an attacker gains, and what access they need to start
4. A suggested fix, if you have one

**Please do not open a public issue for a vulnerability.** The reason is
specific to what this library does: it parses files from untrusted sources and
listens on a network port, often inside a clinical network where upgrades are
slow and deployments are not centrally controlled. A public report is a working
exploit recipe available to everyone before any operator has patched. That
window is the whole risk — not the disclosure itself, which we want, but its
ordering relative to a fix.

This is a small project, so treat the response times as intent rather than a
guarantee: acknowledgement within a few days, and an assessment with a fix or a
timeline once the report has been reproduced. If a report goes unanswered for
two weeks, opening a public issue that says a private report is outstanding —
without the details — is a reasonable escalation.

If you believe a vulnerability is being actively exploited, say so in the first
line of the report.

## Protected Health Information (PHI)

DICOM files frequently contain Protected Health Information (PHI) as defined by HIPAA and similar regulations worldwide. When using go-dicom:

- **Never include real patient data** in bug reports, issues, or pull requests
- **Always use synthetic or anonymized DICOM data** for testing and examples
- Use the `anonymize` package to de-identify DICOM files before sharing
- Be aware that DICOM files may contain PHI in unexpected locations (burned-in annotations, private tags, structured reports)

## Security Considerations

When deploying applications built with go-dicom:

- Validate all DICOM input from untrusted sources
- Use the `config` package validation modes (`RAISE` or `WARN`) in production
- Review private tags carefully as they may contain sensitive data
- Implement appropriate access controls for DICOM data at the application level
- DICOM over TCP is unauthenticated and unencrypted by default. Use `network.DialTLS`
  and `network.ListenTLS` for transport security, and set `SCPConfig.MaxAssociations`
  to bound concurrency on an exposed server. Checking the called AE title is a naming
  convention, not authentication.

### Limits enforced by the library

These bound what a peer or a crafted file can cost, and are enforced rather than
advisory:

| Limit | Value | Guards against |
|-------|-------|----------------|
| `network.MaxPDULengthLimit` | 128 MiB | A PDU declaring a huge length before sending any payload |
| `network.MaxInflatedDatasetSize` | 256 MiB | Decompression bombs in a deflated data set received over an association |
| `filereader.MaxInflatedDatasetSize` | 256 MiB | Decompression bombs in a deflated file on disk |
| `compress.MaxDecompressedSize` | 256 MiB | Decompression bombs in stored pixel data |
| `filereader.MaxSequenceDepth` | 64 | Unbounded recursion from deeply nested sequences |
| `compress.MaxInflateRatio` / `MinInflateAllowance` | 1000:1, 8 MiB floor | A *small* input claiming a large expansion |
| Element length verification | Every element | An element declaring more bytes than the stream holds |

The two inflate bounds work together, and the second exists because the first is
not enough on its own. An absolute ceiling lets an attacker choose the cost of
the attack: a 5 KB file was permitted to allocate 256 MiB before being rejected,
and since `io.ReadAll` grows its buffer by doubling, the real peak was close to
twice that — roughly a 50,000x amplification from a file that costs nothing to
send. Scaling the allowance to the compressed size ties the cost of rejection to
the effort of construction. The 8 MiB floor keeps that safe for legitimate
files: DEFLATE reaches its theoretical maximum ratio on genuinely blank medical
images, so a ratio alone would reject an all-black frame.

The element length check applies to every element rather than only large ones.
An earlier version skipped it below 16 MiB, reasoning that a small allocation is
harmless; a 200-byte file declaring a 15 MiB element still allocated 15 MiB
before discovering the stream was short, which is cheap once and ruinous in a
loop. The stream size is measured once and cached, so checking every element
costs nothing.

The parsers that consume untrusted input — PDU decoding, data set decoding, DIMSE
command decoding, and file reading — have fuzz targets that run in CI.

## Disclosure Policy

- We follow a coordinated disclosure process
- Security fixes will be released as patch versions
- Security advisories will be published via GitHub Security Advisories
