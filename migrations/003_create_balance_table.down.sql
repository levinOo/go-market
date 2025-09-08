CREATE TABLE balance (
    user_id INTEGER PRIMARY KEY REFERENCES users(id),
    current NUMERIC DEFAULT 0,
    withdraw NUMERIC DEFAULT 0
);