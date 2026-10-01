# Security

## Reporting a vulnerability

Please do not open a public issue for a security problem. Use GitHub's
private vulnerability reporting on this repository ("Report a
vulnerability" under the Security tab), which reaches the maintainers
only. Say what you found, how to reproduce it, and what you think the
impact is. You will get an acknowledgement within five working days and
a fix or a plan within thirty.

## What counts

Cartograph serves a vault over HTTP. Problems worth reporting include:
a way to read or write a manifest the authorizer refused; a way to make
the server read a file outside the vault; a way to run a command through
the PDF printer; a working-copy or draft write that corrupts a version;
a crash a client can cause on demand.

The proxy authenticator trusts an identity header by design; a report
that a client reaching the port directly can set that header is not a
vulnerability, it is the documented reason the port must sit behind the
proxy (`docs/DEPLOYMENT.md`).

## Supported versions

The latest release. Security fixes are not backported.
