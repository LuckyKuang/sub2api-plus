-- Channel-level Ultra Fast multiplier.
--
-- NULL keeps the model-owned default (currently 6x for models that support the
-- ultrafast service tier). A positive value overrides it for this channel only,
-- so an operator can price ultrafast independently of Fast/priority.
ALTER TABLE channel_model_pricing
    ADD COLUMN IF NOT EXISTS ultrafast_multiplier NUMERIC(12,6);

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'channel_model_pricing_ultrafast_multiplier_positive'
          AND conrelid = 'channel_model_pricing'::regclass
    ) THEN
        ALTER TABLE channel_model_pricing
            ADD CONSTRAINT channel_model_pricing_ultrafast_multiplier_positive
            CHECK (ultrafast_multiplier IS NULL OR ultrafast_multiplier > 0);
    END IF;
END $$;

COMMENT ON COLUMN channel_model_pricing.ultrafast_multiplier IS
    'Optional channel-specific price multiplier for the OpenAI ultrafast service tier; NULL uses the model default';
