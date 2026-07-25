-- Documents and the maintenance log.

CREATE TABLE documents (
    id                     TEXT PRIMARY KEY,
    title                  TEXT NOT NULL,
    kind                   TEXT NOT NULL,
    -- SENSITIVE: every document field is confidential by default; body never
    -- reaches a log line. Kinds identity/financial/insurance/medical are
    -- excluded from unencrypted exports unconditionally.
    body                   TEXT,
    file_id                TEXT,
    review_interval_months REAL CHECK (review_interval_months > 0),
    last_reviewed          TEXT,
    export_ok              INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE document_persons (
    document_id TEXT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    person_id   TEXT NOT NULL REFERENCES persons(id) ON DELETE CASCADE,
    PRIMARY KEY (document_id, person_id)
);

CREATE TABLE maintenance_events (
    id      TEXT PRIMARY KEY,
    date    TEXT NOT NULL,
    kind    TEXT NOT NULL,
    kit_id  TEXT REFERENCES kits(id) ON DELETE SET NULL,
    item_id TEXT REFERENCES items(id) ON DELETE SET NULL,
    lot_id  TEXT REFERENCES lots(id) ON DELETE SET NULL,
    notes   TEXT
);
CREATE INDEX idx_maint_kit ON maintenance_events(kit_id);
CREATE INDEX idx_maint_item ON maintenance_events(item_id);
