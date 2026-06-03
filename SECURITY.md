# Security

Do not include FootagePal API tokens, signed download URLs, signed upload URLs, Azure account keys, or private media in issues, pull requests, logs, or examples.

The CLI redacts API tokens in normal command output and does not print signed download or upload URLs. Signed storage URLs are bearer credentials until they expire.

Report security issues directly to Troy Anderson rather than filing a public issue with sensitive details.
