# Security Policy

## Supported Versions

| Version | Supported          |
|---------|--------------------|
| 1.x     | Yes                |
| < 1.0   | No                 |

## Reporting a Vulnerability

If you discover a security vulnerability in go-dicom, please report it responsibly.

**Do NOT open a public GitHub issue for security vulnerabilities.**

Instead, please send an email to the maintainers with:

1. A description of the vulnerability
2. Steps to reproduce the issue
3. Potential impact assessment
4. Any suggested fixes (optional)

We will acknowledge receipt within 48 hours and provide a detailed response within 7 days.

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
| `network.MaxInflatedDatasetSize` | 256 MiB | Decompression bombs over the Deflated transfer syntax |
| `compress.MaxDecompressedSize` | 256 MiB | Decompression bombs in stored pixel data |
| `filereader.MaxSequenceDepth` | 64 | Unbounded recursion from deeply nested sequences |
| Element length verification | 16 MiB threshold | An element declaring more bytes than the stream holds |

The parsers that consume untrusted input — PDU decoding, data set decoding, DIMSE
command decoding, and file reading — have fuzz targets that run in CI.

## Disclosure Policy

- We follow a coordinated disclosure process
- Security fixes will be released as patch versions
- Security advisories will be published via GitHub Security Advisories
