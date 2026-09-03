const ipGeoMocks = vi.hoisted(() => ({
  getEntry: vi.fn(() => ({ status: 'idle' as const })),
  fetchOne: vi.fn(),
  fetchBatch: vi.fn(),
}))

const appStoreMocks = vi.hoisted(() => ({
  showSuccess: vi.fn(),
  showError: vi.fn(),
}))

vi.mock('@/utils/ipGeoLookup', () => ipGeoMocks)
vi.mock('@/stores/app', () => ({ useAppStore: () => appStoreMocks }))

import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'

import UsageTable from '../UsageTable.vue'

const messages: Record<string, string> = {
  'admin.usage.userDeletedBadge': 'Deleted',
  'usage.costDetails': 'Cost Breakdown',
  'admin.usage.inputCost': 'Input Cost',
  'admin.usage.outputCost': 'Output Cost',
  'admin.usage.cacheCreationCost': 'Cache Creation Cost',
  'admin.usage.cacheReadCost': 'Cache Read Cost',
  'usage.inputTokenPrice': 'Input price',
  'usage.outputTokenPrice': 'Output price',
  'usage.perMillionTokens': '/ 1M tokens',
  'usage.serviceTier': 'Service tier',
  'usage.serviceTierPriority': 'Fast',
  'usage.serviceTierFlex': 'Flex',
  'usage.serviceTierStandard': 'Standard',
  'usage.rate': 'Rate',
  'usage.accountMultiplier': 'Account rate',
  'usage.original': 'Original',
  'usage.userBilled': 'User billed',
  'usage.accountBilled': 'Account billed',
  'usage.imageUnit': ' images',
  'usage.imageCount': 'Image count',
  'usage.imageBillingSize': 'Billing size',
  'usage.imageInputSize': 'Input size',
  'usage.imageOutputSize': 'Output size',
  'usage.imageSizeSource': 'Size source',
  'usage.imageSizeBreakdown': 'Size breakdown',
  'usage.imageSizeSourceOutput': 'Upstream output',
  'usage.imageSizeSourceInput': 'Request input',
  'usage.imageSizeSourceDefault': 'Default billing tier',
  'usage.imageSizeSourceLegacy': 'Legacy record',
  'usage.imageSizeSourceMissing': 'Not recorded',
  'usage.imageSizeNotRecorded': 'not recorded',
  'usage.imageSizeLegacyUnstandardized': 'legacy unstandardized',
  'usage.imageSizeUnknown': 'unknown',
  'usage.imageUnitPrice': 'Per-image price',
  'usage.imageTotalPrice': 'Image total price',
	'usage.latencyFirstToken': 'First Token',
	'usage.latencyLegacyFirstEvent': 'First Event (Legacy)',
	'usage.latencyFirstOutput': 'First Output',
	'usage.latencyFirstOutputKind': 'First Output Kind',
	'usage.latencyOutputKindText': 'Text',
	'usage.latencyFirstImage': 'First Image Data',
	'usage.latencyFirstAudio': 'First Audio Data',
	'usage.latencyFirstReasoning': 'First Reasoning',
	'usage.latencyFirstTool': 'First Tool Output',
	'usage.latencyDetails': 'Latency Details',
	'usage.latencyLegacyFirstEventHint': 'Legacy first event; not comparable to strict first-token TTFT.',
	'usage.latencyMediaOnlyHint': 'Media first output only; no strict first-token sample.',
	'usage.latencyMixedModalityHint': 'First output and first token differ; an earlier non-text or aggregate output arrived first.',
	'usage.latencyNonTextFirstHint': 'First token-like output was reasoning or a tool call, not necessarily final answer text.',
	'usage.latencyDuration': 'Total',
	'usage.latencyTps': 'TPS',
	'usage.latencyTpsHint': 'Estimated average text output rate: text output tokens ÷ (last token − first token). Complete stream/ws requests only. Sample too small (short window or few text tokens) shows "-". Values below 1 or above 1000 show as "< 1" / "> 1000".',
		'usage.incomplete': 'Incomplete',
		'usage.incompleteHint': 'The request ended before a complete terminal result.',
		'usage.clientDisconnected': 'Client disconnected',
		'usage.clientDisconnectedHint': 'The client disconnected after upstream acceptance.',
		'usage.stream': 'Stream',
		'usage.sync': 'Sync',
		'usage.nativeCompactionV2': 'Compaction',
  'admin.usage.billingModeToken': 'Token',
  'admin.usage.billingModePerRequest': 'Per request',
  'admin.usage.billingModeImage': 'Image',
	'admin.usage.requestIdCopied': 'Request ID copied',
	'admin.usage.userAgentCopied': 'User-Agent copied',
	'keys.copied': 'Copied',
	'keys.copyToClipboard': 'Copy to clipboard',
	'common.copyFailed': 'Copy failed',
	'usage.requestedModel': 'Requested',
	'usage.sentUpstreamModel': 'Sent upstream',
	'usage.upstreamResponseModel': 'Upstream response',
	'usage.modelVariant': 'Possible version variant',
	'usage.modelMismatch': 'Different model',
}

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => messages[key] ?? key,
    }),
  }
})

const DataTableStub = {
  props: ['data'],
  template: `
    <div>
      <div v-for="row in data" :key="row.request_id">
        <slot name="cell-model" :row="row" :value="row.model" />
        <slot name="cell-reasoning_effort" :row="row" :value="row.reasoning_effort" />
        <slot name="cell-billing_mode" :row="row" />
        <slot name="cell-tokens" :row="row" />
        <slot name="cell-cost" :row="row" />
        <slot name="cell-latency" :row="row" />
        <slot name="cell-session_id" :row="row" />
        <slot name="cell-request_id" :row="row" />
        <slot name="cell-user_agent" :row="row" />
      </div>
    </div>
  `,
}

const baseImageRow = {
  request_id: 'req-admin-image',
  model: 'gpt-image-2',
  actual_cost: 0.4,
  total_cost: 0.4,
  account_rate_multiplier: 1,
  rate_multiplier: 1,
  service_tier: null,
  input_cost: 0,
  output_cost: 0,
  cache_creation_cost: 0,
  cache_read_cost: 0,
  input_tokens: 0,
  output_tokens: 0,
  image_output_tokens: 0,
  audio_output_tokens: 0,
  is_complete: true,
  last_token_ms: null,
  cache_creation_tokens: 0,
  cache_read_tokens: 0,
  cache_creation_5m_tokens: 0,
  cache_creation_1h_tokens: 0,
  cache_ttl_overridden: false,
  billing_mode: 'image',
  image_count: 2,
  image_size: '2K',
  image_input_size: null,
  image_output_size: null,
  image_size_source: null,
  image_size_breakdown: null,
}

describe('admin UsageTable tooltip', () => {
  beforeEach(() => {
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue({
      x: 0,
      y: 0,
      top: 20,
      left: 20,
      right: 120,
      bottom: 40,
      width: 100,
      height: 20,
      toJSON: () => ({}),
    } as DOMRect)
  })

	it('shows an incomplete usage badge only for explicitly incomplete records', () => {
		const wrapper = mount(UsageTable, {
			props: {
				data: [
					{ ...baseImageRow, request_id: 'req-incomplete', is_complete: false },
					{ ...baseImageRow, request_id: 'req-complete', is_complete: true },
				],
				loading: false,
				columns: [{ key: 'model', label: 'Model' }],
			},
			global: { stubs: { DataTable: DataTableStub, Icon: true } },
		})

		expect(wrapper.findAll('[data-testid="usage-incomplete-badge"]')).toHaveLength(1)
		expect(wrapper.get('[data-testid="usage-incomplete-badge"]').text()).toBe('Incomplete')
	})

	it('distinguishes a client disconnect from a generic incomplete record', () => {
		const wrapper = mount(UsageTable, {
			props: {
				data: [{
					...baseImageRow,
					request_id: 'req-client-disconnected',
					is_complete: false,
					completion_status: 'client_disconnected',
					usage_source: 'partial',
				}],
				loading: false,
				columns: [{ key: 'model', label: 'Model' }],
			},
			global: { stubs: { DataTable: DataTableStub, Icon: true } },
		})

		const badge = wrapper.get('[data-testid="usage-incomplete-badge"]')
		expect(badge.text()).toBe('Client disconnected')
		expect(badge.attributes('title')).toBe('The client disconnected after upstream acceptance.')
	})

  it('shows the original session ID and marks missing values with a dash', () => {
    const wrapper = mount(UsageTable, {
      props: {
        data: [
          { ...baseImageRow, request_id: 'req-session-present', session_id: '=audit-session-001' },
          { ...baseImageRow, request_id: 'req-session-absent', session_id: null },
        ],
        loading: false,
        columns: [{ key: 'session_id', label: 'Session ID' }],
      },
      global: {
        stubs: {
          DataTable: DataTableStub,
          EmptyState: true,
          Icon: true,
          Teleport: true,
        },
      },
    })

    const sessionID = wrapper.get('[data-testid="usage-session-id"]')
    expect(sessionID.text()).toBe('=audit-session-001')
    expect(sessionID.attributes('title')).toBe('=audit-session-001')
    expect(wrapper.text()).toContain('-')
  })

  it('keeps primary latency as first token and hides modality details behind an info icon', async () => {
    const rows = [
      {
        ...baseImageRow,
        request_id: 'req-latency-token',
        first_token_ms: 120,
        first_output_ms: 40,
        first_output_kind: 'image',
        duration_ms: 500,
      },
      {
        ...baseImageRow,
        request_id: 'req-latency-image',
        first_token_ms: null,
        first_output_ms: 75,
        first_output_kind: 'image',
        duration_ms: 600,
      },
      {
        ...baseImageRow,
        request_id: 'req-latency-audio',
        first_token_ms: null,
        first_output_ms: 80,
        first_output_kind: 'audio',
        duration_ms: 700,
      },
      {
        ...baseImageRow,
        request_id: 'req-latency-reasoning',
        first_token_ms: 85,
        first_output_ms: 85,
        first_output_kind: 'reasoning',
        duration_ms: 750,
      },
      {
        ...baseImageRow,
        request_id: 'req-latency-tool',
        first_token_ms: 88,
        first_output_ms: 88,
        first_output_kind: 'tool',
        duration_ms: 760,
      },
      {
        ...baseImageRow,
        request_id: 'req-latency-text-metadata',
        first_token_ms: 120,
        first_output_ms: 40,
        first_output_kind: 'text',
        duration_ms: 780,
      },
      {
        ...baseImageRow,
        request_id: 'req-latency-aggregate',
        first_token_ms: null,
        first_output_ms: 90,
        first_output_kind: 'text',
        duration_ms: 800,
      },
      {
        ...baseImageRow,
        request_id: 'req-latency-legacy',
        first_token_ms: 55,
        first_output_ms: null,
        first_output_kind: null,
        duration_ms: 900,
      },
      {
        ...baseImageRow,
        request_id: 'req-latency-plain-text',
        first_token_ms: 100,
        first_output_ms: 100,
        first_output_kind: 'text',
        duration_ms: 1_000,
      },
    ]

    const wrapper = mount(UsageTable, {
      props: { data: rows, loading: false, columns: [] },
      global: {
        stubs: {
          DataTable: DataTableStub,
          EmptyState: true,
          Icon: true,
          Teleport: true,
        },
      },
    })

    const text = wrapper.text()
    // Primary column is always First Token / Legacy, never modality labels.
    expect(text).toContain('First Token 120ms')
    expect(text).toContain('First Token 85ms')
    expect(text).toContain('First Token 88ms')
    expect(text).toContain('First Event (Legacy) 55ms')
    expect(text).not.toContain('First Image Data')
    expect(text).not.toContain('First Audio Data')
    expect(text).not.toContain('First Reasoning')
    expect(text).not.toContain('First Tool Output')

    const values = wrapper.findAll('[data-testid="first-latency-value"]')
    expect(values[0].text()).toBe('120ms')
    expect(values[0].classes()).toContain('text-emerald-600')
    expect(values[1].text()).toBe('-')
    expect(values[7].text()).toBe('55ms')
    expect(values[7].classes()).toContain('text-gray-600')

    // Detail icon for non-plain cases; plain text with matching times has no icon.
    const triggers = wrapper.findAll('[data-testid="latency-details-trigger"]')
    expect(triggers).toHaveLength(8)

    await triggers[0].trigger('mouseenter')
    await nextTick()
    const tooltip = wrapper.get('[data-testid="latency-details-tooltip"]')
    expect(tooltip.text()).toContain('Latency Details')
    expect(tooltip.text()).toContain('First Output Kind')
    expect(tooltip.text()).toContain('First Image Data')
    expect(tooltip.text()).toContain('40ms')
    expect(tooltip.text()).toContain('First Token')
    expect(tooltip.text()).toContain('120ms')
    expect(tooltip.text()).toContain('First output and first token differ')
  })

  it('shows estimated TPS from last-first token time, clamped outside [1, 1000]', () => {
    const rows = [
      {
        ...baseImageRow,
        request_id: 'req-tps-stream',
        request_type: 'stream',
        stream: true,
        billing_mode: 'token',
        image_count: 0,
        output_tokens: 375,
        image_output_tokens: 0,
        first_token_ms: 721,
        last_token_ms: 10_860,
        first_output_ms: 721,
        first_output_kind: 'text',
        duration_ms: 10_860,
      },
      {
        ...baseImageRow,
        request_id: 'req-tps-websocket',
        request_type: 'ws_v2',
        stream: true,
        billing_mode: 'token',
        image_count: 0,
        output_tokens: 50,
        image_output_tokens: 0,
        first_token_ms: 1_000,
        last_token_ms: 2_000,
        first_output_ms: 1_000,
        first_output_kind: 'text',
        duration_ms: 2_000,
      },
      {
        ...baseImageRow,
        request_id: 'req-tps-mixed-image',
        request_type: 'stream',
        stream: true,
        billing_mode: 'token',
        image_count: 1,
        output_tokens: 105,
        image_output_tokens: 5,
        first_token_ms: 100,
        last_token_ms: 1_100,
        first_output_ms: 40,
        first_output_kind: 'image',
        duration_ms: 1_100,
      },
      {
        ...baseImageRow,
        request_id: 'req-tps-mixed-audio',
        request_type: 'stream',
        stream: true,
        billing_mode: 'token',
        image_count: 0,
        output_tokens: 150,
        audio_output_tokens: 50,
        first_token_ms: 100,
        last_token_ms: 1_100,
        first_output_ms: 20,
        first_output_kind: 'audio',
        duration_ms: 1_100,
      },
      {
        ...baseImageRow,
        request_id: 'req-tps-incomplete',
        request_type: 'stream',
        stream: true,
        output_tokens: 100,
        first_token_ms: 100,
        last_token_ms: 1_100,
        first_output_ms: 100,
        first_output_kind: 'text',
        is_complete: false,
        duration_ms: 1_100,
      },
      {
        ...baseImageRow,
        request_id: 'req-tps-unknown-completion',
        request_type: 'stream',
        stream: true,
        output_tokens: 100,
        first_token_ms: 100,
        last_token_ms: 1_100,
        first_output_ms: 100,
        first_output_kind: 'text',
        is_complete: null,
        duration_ms: 1_100,
      },
      {
        ...baseImageRow,
        request_id: 'req-tps-sync',
        request_type: 'sync',
        stream: false,
        output_tokens: 100,
        first_token_ms: 100,
        last_token_ms: 1_000,
        first_output_ms: 100,
        first_output_kind: 'text',
        duration_ms: 1_000,
      },
      {
        ...baseImageRow,
        request_id: 'req-tps-legacy',
        request_type: 'stream',
        stream: true,
        output_tokens: 100,
        first_token_ms: 100,
        last_token_ms: 1_000,
        first_output_ms: null,
        first_output_kind: null,
        duration_ms: 1_000,
      },
      {
        ...baseImageRow,
        request_id: 'req-tps-zero-window',
        request_type: 'stream',
        stream: true,
        output_tokens: 100,
        first_token_ms: 100,
        last_token_ms: 100,
        first_output_ms: 100,
        first_output_kind: 'text',
        duration_ms: 100,
      },
      {
        ...baseImageRow,
        request_id: 'req-tps-image-only',
        request_type: 'stream',
        stream: true,
        output_tokens: 5,
        image_output_tokens: 5,
        first_token_ms: 100,
        last_token_ms: 1_000,
        first_output_ms: 40,
        first_output_kind: 'image',
        duration_ms: 1_000,
      },
      {
        ...baseImageRow,
        request_id: 'req-tps-missing-last',
        request_type: 'stream',
        stream: true,
        output_tokens: 100,
        first_token_ms: 100,
        last_token_ms: null,
        first_output_ms: 100,
        first_output_kind: 'text',
        duration_ms: 1_100,
      },
      {
        // generationMs = 250 < 300 → dash
        ...baseImageRow,
        request_id: 'req-tps-short-generation',
        request_type: 'stream',
        stream: true,
        output_tokens: 100,
        first_token_ms: 100,
        last_token_ms: 350,
        first_output_ms: 100,
        first_output_kind: 'text',
        duration_ms: 350,
      },
      {
        // text tokens = 7 < 8 → dash
        ...baseImageRow,
        request_id: 'req-tps-few-tokens',
        request_type: 'stream',
        stream: true,
        output_tokens: 7,
        image_output_tokens: 0,
        first_token_ms: 100,
        last_token_ms: 1_100,
        first_output_ms: 100,
        first_output_kind: 'text',
        duration_ms: 1_100,
      },
      {
        // 1000 tokens / 500ms = 2000 → > 1000
        ...baseImageRow,
        request_id: 'req-tps-unrealistically-high',
        request_type: 'stream',
        stream: true,
        output_tokens: 1_000,
        image_output_tokens: 0,
        first_token_ms: 100,
        last_token_ms: 600,
        first_output_ms: 100,
        first_output_kind: 'text',
        duration_ms: 600,
      },
      {
        // 8 tokens / 10000ms = 0.8 → < 1
        ...baseImageRow,
        request_id: 'req-tps-below-one',
        request_type: 'stream',
        stream: true,
        output_tokens: 8,
        image_output_tokens: 0,
        first_token_ms: 100,
        last_token_ms: 10_100,
        first_output_ms: 100,
        first_output_kind: 'text',
        duration_ms: 10_100,
      },
      {
        // boundary: generationMs = 300, tokens = 8, TPS = 26.7
        ...baseImageRow,
        request_id: 'req-tps-min-gates-pass',
        request_type: 'stream',
        stream: true,
        output_tokens: 8,
        image_output_tokens: 0,
        first_token_ms: 100,
        last_token_ms: 400,
        first_output_ms: 100,
        first_output_kind: 'text',
        duration_ms: 400,
      },
      {
        // 150 * 1000 / 300 = 500
        ...baseImageRow,
        request_id: 'req-tps-mid-band',
        request_type: 'stream',
        stream: true,
        output_tokens: 150,
        image_output_tokens: 0,
        first_token_ms: 100,
        last_token_ms: 400,
        first_output_ms: 100,
        first_output_kind: 'text',
        duration_ms: 400,
      },
      {
        // 300 * 1000 / 300 = 1000 → show 1000
        ...baseImageRow,
        request_id: 'req-tps-max-boundary',
        request_type: 'stream',
        stream: true,
        output_tokens: 300,
        image_output_tokens: 0,
        first_token_ms: 100,
        last_token_ms: 400,
        first_output_ms: 100,
        first_output_kind: 'text',
        duration_ms: 400,
      },
    ]

    const wrapper = mount(UsageTable, {
      props: { data: rows, loading: false, columns: [] },
      global: {
        stubs: {
          DataTable: DataTableStub,
          EmptyState: true,
          Icon: true,
          Teleport: true,
        },
      },
    })

    expect(wrapper.findAll('[data-testid="latency-tps"]').map((node) => node.text())).toEqual([
      '37',
      '50',
      '100',
      '100',
      '-',
      '-',
      '-',
      '-',
      '-',
      '-',
      '-',
      '-',
      '-',
      '> 1000',
      '< 1',
      '26.7',
      '500',
      '1000',
    ])
    expect(wrapper.text()).toContain('First Token 721msTotal10.86sTPS37')
    expect(wrapper.text()).toContain('First Token 100msTotal1.10sTPS100')
    expect(wrapper.text()).not.toContain('First Image Data')
    expect(wrapper.text()).not.toContain('First Audio Data')
  })

  it('renders missing duration and media first output with neutral latency styles', () => {
    const wrapper = mount(UsageTable, {
      props: {
        data: [{
          ...baseImageRow,
          request_id: 'req-latency-null-duration',
          first_token_ms: null,
          first_output_ms: 75,
          first_output_kind: 'image',
          duration_ms: null,
        }],
        loading: false,
        columns: [],
      },
      global: {
        stubs: {
          DataTable: DataTableStub,
          EmptyState: true,
          Icon: true,
          Teleport: true,
        },
      },
    })

    expect(wrapper.get('[data-testid="first-latency-value"]').text()).toBe('-')
    expect(wrapper.get('[data-testid="first-latency-value"]').classes()).toContain('text-gray-400')
    expect(wrapper.get('[data-testid="latency-bar"]').classes()).toContain('bg-gray-300')
    expect(wrapper.get('[data-testid="latency-duration"]').classes()).toContain('text-gray-400')
    expect(wrapper.get('[data-testid="latency-duration"]').text()).toBe('-')
    expect(wrapper.find('[data-testid="latency-details-trigger"]').exists()).toBe(true)
  })

  it('marks only usage rows that actually applied long-context billing', () => {
    const wrapper = mount(UsageTable, {
      props: {
        data: [
          {
            ...baseImageRow,
            request_id: 'req-long-context-enabled',
            long_context_billing_applied: true,
          },
          {
            ...baseImageRow,
            request_id: 'req-long-context-disabled',
            long_context_billing_applied: false,
          },
        ],
        loading: false,
        columns: [],
      },
      global: {
        stubs: {
          DataTable: DataTableStub,
          EmptyState: true,
          Icon: true,
          Teleport: true,
        },
      },
    })

    expect(wrapper.findAll('[data-testid="long-context-billing-marker"]')).toHaveLength(1)
    expect(wrapper.get('[data-testid="long-context-billing-marker"]').text()).toBe('x2')
  })

  it('keeps the request type badge and adds a separate badge only for native compaction rows', () => {
    const DataTableStreamStub = {
      props: ['data'],
      template: `
        <div>
          <div v-for="row in data" :key="row.request_id">
            <slot name="cell-stream" :row="row" />
          </div>
        </div>
      `,
    }
    const wrapper = mount(UsageTable, {
      props: {
        data: [
          {
            ...baseImageRow,
            request_id: 'req-compaction-stream',
            request_type: 'stream',
            stream: true,
            native_compaction_v2: true,
          },
          {
            ...baseImageRow,
            request_id: 'req-historical-sync',
            request_type: 'sync',
            stream: false,
            native_compaction_v2: false,
          },
        ],
        loading: false,
        columns: [],
      },
      global: {
        stubs: {
          DataTable: DataTableStreamStub,
          EmptyState: true,
          Icon: true,
          Teleport: true,
        },
      },
    })

    const requestBadges = wrapper.findAll('[data-testid="request-type-badge"]')
    expect(requestBadges).toHaveLength(2)
    expect(requestBadges[0].text()).toBe('Stream')
    expect(requestBadges[1].text()).toBe('Sync')
    expect(wrapper.findAll('[data-testid="native-compaction-badge"]')).toHaveLength(1)
    expect(wrapper.get('[data-testid="native-compaction-badge"]').text()).toBe('Compaction')
  })

  it('shows service tier and billing breakdown in cost tooltip', async () => {
    const row = {
      request_id: 'req-admin-1',
      actual_cost: 0.092883,
      total_cost: 0.092883,
      account_rate_multiplier: 1,
      rate_multiplier: 1,
      service_tier: 'priority',
      input_cost: 0.020285,
      output_cost: 0.00303,
      cache_creation_cost: 0,
      cache_read_cost: 0.069568,
      input_tokens: 4057,
      output_tokens: 101,
    }

    const wrapper = mount(UsageTable, {
      props: {
        data: [row],
        loading: false,
        columns: [],
      },
      global: {
        stubs: {
          DataTable: DataTableStub,
          EmptyState: true,
          Icon: true,
          Teleport: true,
        },
      },
    })

    const tooltipTriggers = wrapper.findAll('.group.relative')
    await tooltipTriggers[tooltipTriggers.length - 1].trigger('mouseenter')
    await nextTick()

    const text = wrapper.text()
    expect(text).toContain('Service tier')
    expect(text).toContain('Fast')
    expect(text).toContain('Rate')
    expect(text).toContain('1.00x')
    expect(text).toContain('Account rate')
    expect(text).toContain('User billed')
    expect(text).toContain('Account billed')
    expect(text).toContain('$0.092883')
    expect(text).toContain('$5.0000 / 1M tokens')
    expect(text).toContain('$30.0000 / 1M tokens')
    expect(text).toContain('$0.069568')
  })

  it('shows requested and upstream models separately for admin rows', () => {
    const row = {
      request_id: 'req-admin-model-1',
      model: 'claude-sonnet-4',
      upstream_model: 'claude-sonnet-4-20250514',
      actual_cost: 0,
      total_cost: 0,
      account_rate_multiplier: 1,
      rate_multiplier: 1,
      input_cost: 0,
      output_cost: 0,
      cache_creation_cost: 0,
      cache_read_cost: 0,
      input_tokens: 0,
      output_tokens: 0,
    }

    const wrapper = mount(UsageTable, {
      props: {
        data: [row],
        loading: false,
        columns: [],
      },
      global: {
        stubs: {
          DataTable: DataTableStub,
          EmptyState: true,
          Icon: true,
          Teleport: true,
        },
      },
    })

    const text = wrapper.text()
    expect(text).toContain('claude-sonnet-4')
    expect(text).toContain('claude-sonnet-4-20250514')
  })

  it('shows requested and forwarded reasoning effort separately when they differ', () => {
    const wrapper = mount(UsageTable, {
      props: {
        data: [{
          request_id: 'req-admin-effort-1',
          model: 'gpt-5.4',
          reasoning_effort: 'max',
          upstream_reasoning_effort: 'xhigh',
        }],
        loading: false,
        columns: [],
      },
      global: {
        stubs: {
          DataTable: DataTableStub,
          EmptyState: true,
          Icon: true,
          Teleport: true,
        },
      },
    })

    const text = wrapper.text()
    expect(text).toContain('Max')
    expect(text).toContain('XHigh')
    expect(text).toContain('↳')
  })

  it('shows a single reasoning effort when requested matches forwarded', () => {
    const wrapper = mount(UsageTable, {
      props: {
        data: [{
          request_id: 'req-admin-effort-2',
          model: 'gpt-5.6-sol',
          reasoning_effort: 'max',
        }],
        loading: false,
        columns: [],
      },
      global: {
        stubs: {
          DataTable: DataTableStub,
          EmptyState: true,
          Icon: true,
          Teleport: true,
        },
      },
    })

    const text = wrapper.text()
    expect(text).toContain('Max')
    expect(text).not.toContain('↳')
  })

  it('hides mapped reasoning effort for user rows that only have the requested value', () => {
    const wrapper = mount(UsageTable, {
      props: {
        data: [{
          request_id: 'req-user-effort-1',
          model: 'gpt-5.4',
          reasoning_effort: 'max',
        }],
        loading: false,
        columns: [],
      },
      global: {
        stubs: {
          DataTable: DataTableStub,
          EmptyState: true,
          Icon: true,
          Teleport: true,
        },
      },
    })

    expect(wrapper.text()).toContain('Max')
    expect(wrapper.text()).not.toContain('XHigh')
    expect(wrapper.text()).not.toContain('↳')
  })

	it.each([
		{
			name: 'possible version variant',
			responseModel: 'gpt-5.5-2026-08-01',
			expectedBadge: 'Possible version variant',
		},
		{
			name: 'different upstream model',
			responseModel: 'gpt-5.4',
			expectedBadge: 'Different model',
		},
	])('shows a compact upstream response audit marker for $name', ({ responseModel, expectedBadge }) => {
		const wrapper = mount(UsageTable, {
			props: {
				data: [{
					request_id: `req-${responseModel}`,
					model: 'gpt-5.6-sol',
					upstream_model: 'gpt-5.5',
					model_mapping_chain: 'gpt-5.6-sol→gpt-5.5',
					upstream_response_model: responseModel,
					upstream_model_mismatch: true,
				}],
				loading: false,
				columns: [],
			},
			global: {
				stubs: {
					DataTable: DataTableStub,
					EmptyState: true,
					Icon: true,
					Teleport: true,
				},
			},
		})

		const text = wrapper.text()
		expect(text).toContain('gpt-5.6-sol')
		expect(text).toContain('gpt-5.5')
		expect(text).toContain(responseModel)
		expect(text).toContain(expectedBadge)
	})

  it.each([
    {
      name: 'defaulted row',
      row: {
        ...baseImageRow,
        request_id: 'req-admin-default-image',
        image_size: '2K',
        image_input_size: 'auto',
        image_output_size: null,
        image_size_source: 'default',
      },
      expected: ['2K', 'Default billing tier', 'auto', 'unknown'],
    },
    {
      name: 'output-sourced row',
      row: {
        ...baseImageRow,
        request_id: 'req-admin-output-image',
        image_size: '4K',
        image_input_size: '1024x1024',
        image_output_size: '3840x2160',
        image_size_source: 'output',
        image_size_breakdown: { '4K': 1 },
      },
      expected: ['4K', 'Upstream output', '1024x1024', '3840x2160', '4K x 1'],
    },
    {
      name: 'input-sourced row',
      row: {
        ...baseImageRow,
        request_id: 'req-admin-input-image',
        image_size: '1K',
        image_input_size: '1024x1024',
        image_output_size: null,
        image_size_source: 'input',
      },
      expected: ['1K', 'Request input', '1024x1024', 'unknown'],
    },
    {
      name: 'legacy unstandardized row',
      row: {
        ...baseImageRow,
        request_id: 'req-admin-legacy-unstandardized-image',
        image_size: '512x512',
        image_input_size: null,
        image_output_size: null,
        image_size_source: null,
      },
      expected: ['legacy unstandardized: 512x512', 'Legacy record', 'unknown'],
    },
  ])('shows image usage metadata for $name', async ({ row, expected }) => {
    const wrapper = mount(UsageTable, {
      props: {
        data: [row],
        loading: false,
        columns: [],
      },
      global: {
        stubs: {
          DataTable: DataTableStub,
          EmptyState: true,
          Icon: true,
          Teleport: true,
        },
      },
    })

    await wrapper.find('.group.relative').trigger('mouseenter')
    await nextTick()

    const text = wrapper.text()
    expect(text).toContain('Image count')
    expect(text).toContain('Billing size')
    expect(text).toContain('Size source')
    expect(text).toContain('Input size')
    expect(text).toContain('Output size')
    expect(text).toContain('Per-image price')
    expect(text).toContain('Image total price')
    for (const value of expected) {
      expect(text).toContain(value)
    }
  })

  it('displays historical image rows with missing billing_mode as image usage without a 2K fallback', async () => {
    const wrapper = mount(UsageTable, {
      props: {
        data: [
          {
            ...baseImageRow,
            request_id: 'req-admin-legacy-missing-image',
            billing_mode: null,
            image_size: null,
            image_input_size: null,
            image_output_size: null,
            image_size_source: null,
            image_size_breakdown: null,
          },
        ],
        loading: false,
        columns: [],
      },
      global: {
        stubs: {
          DataTable: DataTableStub,
          EmptyState: true,
          Icon: true,
          Teleport: true,
        },
      },
    })

    await wrapper.find('.group.relative').trigger('mouseenter')
    await nextTick()

    const text = wrapper.text()
    expect(text).toContain('Image')
    expect(text).toContain('Image count')
    expect(text).toContain('Per-image price')
    expect(text).toContain('not recorded')
    expect(text).not.toContain('(2K)')
  })
})

describe('admin UsageTable request ID column', () => {
  beforeEach(() => {
    appStoreMocks.showSuccess.mockReset()
    appStoreMocks.showError.mockReset()
  })

  it('renders and copies the request ID', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('navigator', { clipboard: { writeText } })

    const wrapper = mount(UsageTable, {
      props: {
        data: [{ ...baseImageRow, request_id: 'req-admin-visible-id' }],
        loading: false,
        columns: [{ key: 'request_id', label: 'Request ID' }],
      },
      global: {
        stubs: {
          DataTable: DataTableStub,
          EmptyState: true,
          Icon: true,
          Teleport: true,
        },
      },
    })

    expect(wrapper.text()).toContain('req-admin-visible-id')
    await wrapper.get('button[title="Copy to clipboard"]').trigger('click')

    expect(writeText).toHaveBeenCalledWith('req-admin-visible-id')
    expect(appStoreMocks.showSuccess).toHaveBeenCalledWith('Request ID copied')
  })

  it('renders and copies the user agent', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('navigator', { clipboard: { writeText } })
    const UserAgentTableStub = {
      props: ['data'],
      template: `
        <div>
          <div v-for="row in data" :key="row.request_id">
            <slot name="cell-user_agent" :row="row" />
          </div>
        </div>
      `,
    }

    const wrapper = mount(UsageTable, {
      props: {
        data: [{ ...baseImageRow, user_agent: 'Mozilla/5.0 TestAgent' }],
        loading: false,
        columns: [{ key: 'user_agent', label: 'User-Agent' }],
      },
      global: {
        stubs: {
          DataTable: UserAgentTableStub,
          EmptyState: true,
          Icon: true,
          Teleport: true,
        },
      },
    })

    expect(wrapper.text()).toContain('Mozilla/5.0 TestAgent')
    await wrapper.get('button[title="Copy to clipboard"]').trigger('click')

    expect(writeText).toHaveBeenCalledWith('Mozilla/5.0 TestAgent')
    expect(appStoreMocks.showSuccess).toHaveBeenCalledWith('User-Agent copied')
  })
})

describe('admin UsageTable IP geolocation batch toolbar', () => {
  const DataTableStubWithIp = {
    props: ['data'],
    template: `
      <div>
        <div v-for="row in data" :key="row.request_id">
          <slot name="cell-ip_address" :row="row" />
        </div>
      </div>
    `,
  }

  beforeEach(() => {
    ipGeoMocks.getEntry.mockReset()
    ipGeoMocks.fetchOne.mockReset()
    ipGeoMocks.fetchBatch.mockReset()
    ipGeoMocks.getEntry.mockReturnValue({ status: 'idle' })
  })

  it('does not render the batch toolbar when the ip_address column is not visible', () => {
    const wrapper = mount(UsageTable, {
      props: {
        data: [{ request_id: 'r1', ip_address: '8.8.8.8' }],
        loading: false,
        columns: [],
      },
      global: { stubs: { DataTable: DataTableStubWithIp, EmptyState: true, Teleport: true } },
    })
    expect(wrapper.text()).not.toContain('usage.ipGeo.batchFetch')
  })

  it('renders the batch toolbar with a pending count when the ip_address column is visible', () => {
    const wrapper = mount(UsageTable, {
      props: {
        data: [
          { request_id: 'r1', ip_address: '8.8.8.8' },
          { request_id: 'r2', ip_address: '8.8.8.8' },
          { request_id: 'r3', ip_address: '1.1.1.1' },
        ],
        loading: false,
        columns: [{ key: 'ip_address', label: 'IP' }],
      },
      global: { stubs: { DataTable: DataTableStubWithIp, EmptyState: true, Teleport: true } },
    })
    expect(wrapper.text()).toContain('usage.ipGeo.pending')
    const button = wrapper.find('button')
    expect(button.exists()).toBe(true)
    expect((button.element as HTMLButtonElement).disabled).toBe(false)
  })

  it('fetches deduplicated IPs from the current page when the batch button is clicked', async () => {
    ipGeoMocks.fetchBatch.mockResolvedValue(true)
    const wrapper = mount(UsageTable, {
      props: {
        data: [
          { request_id: 'r1', ip_address: '8.8.8.8' },
          { request_id: 'r2', ip_address: '8.8.8.8' },
          { request_id: 'r3', ip_address: '1.1.1.1' },
        ],
        loading: false,
        columns: [{ key: 'ip_address', label: 'IP' }],
      },
      global: { stubs: { DataTable: DataTableStubWithIp, EmptyState: true, Teleport: true } },
    })
    await wrapper.find('button').trigger('click')
    expect(ipGeoMocks.fetchBatch).toHaveBeenCalledWith(['8.8.8.8', '1.1.1.1'])
    expect(wrapper.emitted('ipGeoBatchFailed')).toBeUndefined()
  })

  it('emits ipGeoBatchFailed when the batch request reports a network-level failure', async () => {
    ipGeoMocks.fetchBatch.mockResolvedValue(false)
    const wrapper = mount(UsageTable, {
      props: {
        data: [{ request_id: 'r1', ip_address: '8.8.8.8' }],
        loading: false,
        columns: [{ key: 'ip_address', label: 'IP' }],
      },
      global: { stubs: { DataTable: DataTableStubWithIp, EmptyState: true, Teleport: true } },
    })
    await wrapper.find('button').trigger('click')
    expect(wrapper.emitted('ipGeoBatchFailed')).toHaveLength(1)
  })

  it('renders IpGeoCell content for ip_address cells', () => {
    ipGeoMocks.getEntry.mockReturnValue({ status: 'success', label: 'CN · Guangdong · Shenzhen', detail: {} })
    const wrapper = mount(UsageTable, {
      props: {
        data: [{ request_id: 'r1', ip_address: '121.35.47.43' }],
        loading: false,
        columns: [{ key: 'ip_address', label: 'IP' }],
      },
      global: { stubs: { DataTable: DataTableStubWithIp, EmptyState: true, Teleport: true } },
    })
    expect(wrapper.text()).toContain('121.35.47.43')
    expect(wrapper.text()).toContain('CN · Guangdong · Shenzhen')
  })
})

// A DataTable stub that also renders cell-user, so the deleted badge can be asserted.
const DataTableStubWithUser = {
  props: ['data'],
  template: `
    <div>
      <div v-for="row in data" :key="row.request_id">
        <slot name="cell-user" :row="row" />
        <slot name="cell-model" :row="row" :value="row.model" />
        <slot name="cell-reasoning_effort" :row="row" :value="row.reasoning_effort" />
        <slot name="cell-billing_mode" :row="row" />
        <slot name="cell-tokens" :row="row" />
        <slot name="cell-cost" :row="row" />
      </div>
    </div>
  `,
}

describe('admin UsageTable deleted-user badge', () => {
  it('renders deleted badge for a soft-deleted user row', () => {
    const row = {
      request_id: 'req-deleted-user-1',
      model: 'claude-3',
      user_id: 2,
      user: { id: 2, email: 'd@test.com', deleted_at: '2026-05-28T00:00:00Z' },
      actual_cost: 0,
      total_cost: 0,
      input_cost: 0,
      output_cost: 0,
      rate_multiplier: 1,
      input_tokens: 1,
      output_tokens: 1,
    }

    const wrapper = mount(UsageTable, {
      props: {
        data: [row],
        loading: false,
        columns: [{ key: 'user', label: 'User' }],
      },
      global: {
        stubs: {
          DataTable: DataTableStubWithUser,
          EmptyState: true,
          Icon: true,
          Teleport: true,
        },
      },
    })

    expect(wrapper.text()).toContain('Deleted')
    expect(wrapper.text()).toContain('d@test.com')
  })

  it('does NOT render deleted badge for an active user row', () => {
    const row = {
      request_id: 'req-active-user-1',
      model: 'claude-3',
      user_id: 3,
      user: { id: 3, email: 'active@test.com', deleted_at: null },
      actual_cost: 0,
      total_cost: 0,
      input_cost: 0,
      output_cost: 0,
      rate_multiplier: 1,
      input_tokens: 1,
      output_tokens: 1,
    }

    const wrapper = mount(UsageTable, {
      props: {
        data: [row],
        loading: false,
        columns: [{ key: 'user', label: 'User' }],
      },
      global: {
        stubs: {
          DataTable: DataTableStubWithUser,
          EmptyState: true,
          Icon: true,
          Teleport: true,
        },
      },
    })

    expect(wrapper.text()).not.toContain('Deleted')
    expect(wrapper.text()).toContain('active@test.com')
  })
})
