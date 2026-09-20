ALTER TABLE ownership_transfers DROP CONSTRAINT ownership_transfers_status_check;

ALTER TABLE ownership_transfers
    ADD CONSTRAINT ownership_transfers_status_check
    CHECK (status IN ('pending', 'accepted', 'rejected', 'cancelled'));
