CREATE TABLE contacts (
    owner_id   TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    contact_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    PRIMARY KEY (owner_id, contact_id)
);

CREATE INDEX idx_contacts_contact ON contacts(contact_id);
