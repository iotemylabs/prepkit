-- Core entities mirroring inventory.schema.json.
--
-- Conventions: TEXT UUIDs, ISO-8601 dates as TEXT (parsed in Go, never with
-- SQLite date functions), booleans as INTEGER 0/1, absent optional values as
-- NULL rather than ''.

CREATE TABLE instance (
    id          INTEGER PRIMARY KEY CHECK (id = 1),
    instance_id TEXT NOT NULL,
    created_at  TEXT NOT NULL
);

-- Single-row household profile; persons live in their own table.
CREATE TABLE household (
    id          INTEGER PRIMARY KEY CHECK (id = 1),
    label       TEXT,
    pets        INTEGER NOT NULL DEFAULT 0 CHECK (pets >= 0),
    climate     TEXT,
    environment TEXT
);

CREATE TABLE persons (
    id         TEXT PRIMARY KEY,
    label      TEXT NOT NULL,
    age_band   TEXT,
    -- SENSITIVE: JSON array of free-text planning notes. Never logged,
    -- excluded from unencrypted exports by default.
    needs_json TEXT
);

CREATE TABLE containers (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL,
    -- RESTRICT: deleting a container with children is refused, not cascaded.
    -- Cycle rejection cannot be expressed here; the store layer walks the
    -- ancestor chain inside the writing transaction.
    parent_id     TEXT REFERENCES containers(id) ON DELETE RESTRICT,
    type          TEXT,
    color_code    TEXT,
    location_note TEXT,
    qr_slug       TEXT UNIQUE,
    weight_g      REAL CHECK (weight_g >= 0)
);
CREATE INDEX idx_containers_parent ON containers(parent_id);

CREATE TABLE kits (
    id                 TEXT PRIMARY KEY,
    name               TEXT NOT NULL,
    template_id        TEXT,
    template_version   TEXT,
    layer              TEXT,
    duration           TEXT,
    evacuation_tier    TEXT,
    container_id       TEXT REFERENCES containers(id) ON DELETE RESTRICT,
    assigned_person_id TEXT REFERENCES persons(id) ON DELETE SET NULL,
    last_verified      TEXT,
    notes              TEXT
);

CREATE TABLE items (
    id               TEXT PRIMARY KEY,
    name             TEXT NOT NULL,
    category         TEXT NOT NULL,
    template_item_id TEXT,
    -- Deleting a kit orphans its items rather than destroying them.
    kit_id           TEXT REFERENCES kits(id) ON DELETE SET NULL,
    container_id     TEXT REFERENCES containers(id) ON DELETE RESTRICT,
    -- NULL whenever the item has lots; the store layer enforces the
    -- invariant so quantity and the lot sum can never disagree.
    quantity         REAL CHECK (quantity >= 0),
    unit             TEXT,
    product          TEXT,
    weight_g         REAL CHECK (weight_g >= 0),
    acquired         TEXT,
    -- SENSITIVE: cost and serial never reach logs and are excluded from
    -- unencrypted exports by default.
    cost             REAL CHECK (cost >= 0),
    photo_ids_json   TEXT,
    serial           TEXT,
    notes            TEXT,
    archived         INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_items_kit ON items(kit_id);
CREATE INDEX idx_items_template_item ON items(template_item_id);

CREATE TABLE lots (
    id           TEXT PRIMARY KEY,
    item_id      TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    quantity     REAL NOT NULL CHECK (quantity >= 0),
    unit         TEXT,
    expires      TEXT,
    expiry_basis TEXT,
    opened       TEXT,
    next_check   TEXT,
    container_id TEXT REFERENCES containers(id) ON DELETE RESTRICT,
    status       TEXT NOT NULL DEFAULT 'sealed',
    notes        TEXT
);
CREATE INDEX idx_lots_item ON lots(item_id);
CREATE INDEX idx_lots_expires ON lots(expires);
CREATE INDEX idx_lots_next_check ON lots(next_check);
