// TODO(port): placeholder pending constants/ port
// Minimal placeholder exporting only the channel constant types imported by src/api.
// Values mirror backend service.Status*/BillingMode*/BillingModelSource* constants.

/** Channel status values (must match service.Status* constants in Go). */
export type ChannelStatus = 'active' | 'disabled'

/** Billing mode values (must match service.BillingMode* constants in Go). */
export type BillingMode = 'token' | 'per_request' | 'image' | 'video'

/** Billing-model-source values (must match service.BillingModelSource* constants in Go). */
export type BillingModelSource = 'requested' | 'upstream' | 'channel_mapped' | 'response_model'
