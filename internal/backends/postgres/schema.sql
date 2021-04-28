CREATE TABLE IF NOT EXISTS poll(
    id INTEGER UNIQUE NOT NULL,
    stopped BOOLEAN NOT NULL,

    -- user_ids is managed by the application. It stores all user ids in a way
    -- that makes it impossible to see the sequence in which the users have
    -- voted.
    user_ids BYTEA
);

CREATE TABLE IF NOT EXISTS objects (
    id SERIAL PRIMARY KEY,

    -- There are many raws per poll.
    poll_id INTEGER NOT NULL REFERENCES poll(id) ON DELETE CASCADE,

    -- The vote object.
    vote BYTEA
);
