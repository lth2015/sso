# SMTP account notifications

`cmd/event-worker` supports two explicit modes:

- `SSO_MODE=local-accounts`: private local `/mailbox` delivery, unchanged.
- `SSO_MODE=kubernetes`: authenticated SMTP submission; verified STARTTLS is mandatory. No plaintext or local-file fallback.

The Kubernetes worker needs these environment variables:

| Variable | Value / source |
| --- | --- |
| SSO_MODE | `kubernetes` |
| SMTP_HOST | `smtp.office365.com` |
| SMTP_PORT | `587` |
| SMTP_FROM | Actual approved sender address |
| SMTP_USERNAME | SMTP mailbox login, from Secret |
| SMTP_PASSWORD | SMTP mailbox password, from Secret |
| MAIL_ACCOUNT_URL | `https://sso.nsstest.com/account` |
| IDENTITY_MAIL_KEY | Same encryption key as identity-api, from Secret |
| DB_HOST / DB_PORT / DB_NAME | Existing business DB host / `3306` / `sso_business` |
| DB_USER / DB_PASSWORD | Dedicated worker database user / Secret |
| DB_TLS_CA_FILE / DB_TLS_SERVER_NAME | Existing mounted CA / verified Cloud SQL certificate name |

Do not put passwords into ConfigMaps, source control, command arguments or logs. The SMTP SSL boolean from kubestar is not used here: port 587 explicitly upgrades using STARTTLS and validates the system trust chain and server hostname. SMTP LOGIN is preferred when advertised; PLAIN is supported only after TLS. Microsoft 365 tenant/mailbox policies must permit the chosen password authentication; OAuth is not implemented by this adapter.

Deploy one internal worker replica with the existing event-worker image target, a non-root read-only container, CA Secret volume, DB host aliases as needed and ConfigMap/Secret environment references. It does not require Service, Ingress or public DNS. Permit outbound DNS, database TCP 3306 and SMTP TCP 587. A dedicated worker DB account needs SELECT and UPDATE on `sso_business.account_notifications`; it does not need account/password tables. This worker does not process other business event queues.

Notifications are decrypted only at delivery, with authenticated metadata checked. Links use `/account#purpose=...&token=...` to match the existing UI; proof tokens are not placed into URL query strings or logs. Expired pending messages are discarded; failed delivery uses the existing database backoff. A notification is acknowledged only after the SMTP server accepts DATA. This confirms SMTP acceptance, not final inbox delivery. Rejected/undeliverable recipients and spam placement need mailbox/provider monitoring.

SMTP acceptance and database commit are not atomic. A crash or lost acknowledgement can cause duplicate email with the same proof and stable Message-ID. Proof consumption remains one-time. Network delivery has a 15-second timeout. Worker logs contain controlled failure categories only, without raw server errors, recipients, passwords or proof links.

Before production: create the credentials Secret privately, publish the worker image, deploy the worker, then request a new registration using an approved test inbox. Verify arrival, correct HTTPS link, one-time completion, and the delivered queue state. Reset-password delivery should be checked separately. No email is sent merely by running the unit tests.

Run `event-worker --check-smtp` with the same Kubernetes environment to verify database access, mail key, verified TLS and SMTP authentication without submitting a message. This check exits on success and must not be used as the long-running Deployment command.
