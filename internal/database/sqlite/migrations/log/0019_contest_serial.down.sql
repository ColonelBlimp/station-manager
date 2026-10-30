-- Reverse of 0019. The serials logged stay on the QSOs (STX); only the record
-- of allocations goes, so a later re-upgrade restarts each sequence at 1.

DROP TABLE contest_serial_allocation;
