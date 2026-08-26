# Security Policy

## Supported Versions

| Version | Supported |
|---------|-----------|
| latest (master) | Yes |
| older releases  | No — please upgrade |

## Reporting a Vulnerability

**Do not open a public GitHub issue for security vulnerabilities.**

Please report security issues by opening a **private** [GitHub Security Advisory](https://github.com/mintfary-oss/zapret2-may/security/advisories/new) on this repository. Describe:

- The vulnerability type (e.g., RCE, MITM, information disclosure).
- Steps to reproduce.
- Potential impact.

We aim to acknowledge reports within **72 hours** and provide a fix or mitigation within **14 days** for critical issues.

## Threat model

FreeNet is a **client-side** DPI bypass tool. It processes your own traffic. The primary trust boundary is the local machine. FreeNet does **not**:

- Store or transmit user credentials.
- Phone home or send telemetry.
- Require root or administrator privileges on Android.

The Telegram bot token (if configured) is a sensitive credential — treat it like a password and do not commit it to source control.
