CREATE TABLE IF NOT EXISTS users (
    id SERIAL PRIMARY KEY,
    login TEXT NOT NULL UNIQUE,
    password TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS orders (
    user_id BIGINT NOT NULL REFERENCES users(id),
    order_number BIGINT NOT NULL UNIQUE,
    status TEXT DEFAULT 'NEW',
    accrual NUMERIC,
    uploaded_at TIMESTAMP
);

CREATE TABLE IF NOT EXISTS balance (
    user_id BIGINT PRIMARY KEY REFERENCES users(id),
    current NUMERIC DEFAULT 0,
    withdraw NUMERIC DEFAULT 0
);

CREATE TABLE IF NOT EXISTS withdraw (
    user_id BIGINT NOT NULL REFERENCES users(id),
    order_number BIGINT NOT NULL,
    amount NUMERIC,
    processed_at TIMESTAMP
);