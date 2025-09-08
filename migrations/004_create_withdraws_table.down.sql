CREATE TABLE withdrawals (
    user_id INTEGER NOT NULL REFERENCES users(id),
    order_number INTEGER NOT NULL,
    amount NUMERIC ,
    processed_at TIMESTAMP
);