-- adoption_reserved_at reserves an SM Cloud binding's cloud name for its
-- adoption (ADR 0091, W-0021 5F.3 commit 4a). It is written, and committed,
-- before the adoption request is sent, and never cleared automatically: from
-- then on adoption may have occurred, so no other binding may take the name.
-- The name is the CloudName of the row's credentials, which the adoption-key
-- lock fixes from the reservation onward. It is archive-local protection, not
-- proof that any server accepted adoption: that is remote_adopted_at.

ALTER TABLE logbook_destination ADD COLUMN adoption_reserved_at DATETIME;
