-- remote_adopted_account is the station account a confirmation belongs to
-- (ADR 0091, W-0021 5F.3 commit 4b1): a hex HMAC-SHA256 keyed by the account's
-- token over the archive UUID and the account's URL. It is written with
-- remote_adopted_at in one durable write. A confirmation counts only for the
-- account whose fingerprint matches; any other account (a replacement, a
-- rotated token, an offline edit) needs a fresh confirmation. It is a verifier
-- derived from the token, so it is never served.

ALTER TABLE logbook_destination ADD COLUMN remote_adopted_account TEXT;
