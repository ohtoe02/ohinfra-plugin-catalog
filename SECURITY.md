# Security

Do not open a public issue for a suspected signing-key compromise, malicious
catalog entry or sandbox escape. Report it privately through GitHub Security
Advisories for this repository.

The signing seed must exist only in the protected `catalog-production`
environment. Pull-request workflows receive no secrets and use a read-only
GitHub token.
