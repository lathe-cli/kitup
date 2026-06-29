# Security Policy

## Supported Versions

Only the latest released minor line receives security fixes.

| Version | Supported |
| ------- | --------- |
| 0.1.x   | Yes       |
| < 0.1   | No        |

## Reporting a Vulnerability

Do not open a public issue for a vulnerability.

Use GitHub's private vulnerability reporting flow:

https://github.com/samzong/kitup/security/advisories/new

If that flow is unavailable, open a minimal public issue asking for maintainer contact without including exploit details.

## Expected Response

- Initial maintainer response: within 7 days.
- Status update: at least every 14 days until resolution.
- Fix target: the smallest release that preserves installer safety and cross-language parity.

## Security Scope

In scope:

- Unsafe overwrite, update, or uninstall behavior.
- Incorrect `.kitup.json` ownership checks.
- Host path traversal or unexpected absolute path handling.
- Skill validation bypasses.
- Release or supply-chain integrity problems.

Out of scope:

- Vulnerabilities in downstream agent hosts.
- Arbitrary behavior inside user-authored skill content.
- Requests to execute skill scripts. `kitup` does not execute files from a skill directory.

