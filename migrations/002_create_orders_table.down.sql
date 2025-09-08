CREATE TABLE orders (
    user_id INTEGER NOT NULL REFERENCES users(id),
    order_number INTEGER NOT NULL UNIQUE,
    status TEXT DEFAULT 'NEW',
    accrual NUMERIC,
    uploaded_at TIMESTAMP
);