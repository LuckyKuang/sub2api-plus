ALTER TABLE redeem_codes
    ADD COLUMN generate_order boolean NOT NULL DEFAULT false,
    ADD COLUMN order_currency varchar(3) NOT NULL DEFAULT 'CNY',
    ADD COLUMN order_amount numeric(20,2) NOT NULL DEFAULT 0,
    ADD COLUMN order_plan_id bigint,
    ADD COLUMN order_plan_name varchar(255) NOT NULL DEFAULT '';
ALTER TABLE payment_orders
    ADD COLUMN order_source varchar(20) NOT NULL DEFAULT 'payment',
    ADD COLUMN currency varchar(3) NOT NULL DEFAULT '',
    ADD COLUMN plan_name varchar(255) NOT NULL DEFAULT '',
    ADD COLUMN redeem_code_id bigint;
CREATE UNIQUE INDEX paymentorder_redeem_code_id ON payment_orders (redeem_code_id);
CREATE INDEX paymentorder_order_source_paid_at ON payment_orders (order_source, paid_at);
