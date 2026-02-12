# Security Policy

## Supported Versions

Only the latest release is supported with security updates.

| Version | Supported |
|---------|-----------|
| Latest  | Yes       |
| Older   | No        |

## Reporting a Vulnerability

**Please do not report security vulnerabilities through public GitHub issues.**

Use [GitHub Private Vulnerability Reporting](https://github.com/fabienpiette/goscribe/security/advisories/new) to submit a report. This allows us to collaborate on a fix privately before public disclosure.

When reporting, please include:

- Description of the vulnerability
- Steps to reproduce
- Potential impact
- Suggested fix (if any)

You should receive an initial response within 72 hours. If the vulnerability is confirmed, a fix will be released as soon as possible and you will be credited in the advisory (unless you prefer otherwise).

## Security Considerations

goscribe is a local CLI tool that interacts with third-party AI APIs. Users should be aware of the following:

**API keys** are stored in plaintext in `~/.goscribe/config.yml`. Protect this file with appropriate filesystem permissions and avoid committing it to version control.

**Audio data** is sent to OpenAI and/or Google APIs for processing. Do not use goscribe with sensitive or classified audio without reviewing your organization's data handling policies for these services.

**Dependencies** are managed via Go modules with pinned versions. Run `go mod verify` to confirm module integrity.
