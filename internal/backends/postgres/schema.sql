CREATE TABLE IF NOT EXISTS poll(
    id SERIAL PRIMARY KEY,
    poll_id INTEGER UNIQUE NOT NULL,
    stopped BOOLEAN NOT NULL,

    -- user_ids is managed by the application. It stores all user ids in a way
    -- that makes it impossible to see the sequence in which the users have
    -- voted.
    user_ids BYTEA
);

CREATE TABLE IF NOT EXISTS objects (
    id SERIAL PRIMARY KEY,

    -- There are many raws per poll. This is not the poll_id from the datastore,
    -- but the poll from this database.
    poll_id INTEGER NOT NULL,

    -- The vote object.
    vote BYTEA,

    FOREIGN KEY (poll_id) REFERENCES poll(id) ON DELETE CASCADE
);
