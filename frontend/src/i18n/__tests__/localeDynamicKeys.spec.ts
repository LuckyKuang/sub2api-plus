import { describe, expect, it } from 'vitest'
import ts from 'typescript'
import apiTypes from '../../types/index.ts?raw'
import paymentTypes from '../../types/payment.ts?raw'
import pluginTypes from '../../api/admin/plugins.ts?raw'
import monitorTypes from '../../api/admin/channelMonitor.ts?raw'
import statusTypes from '../../api/channelMonitorV3.ts?raw'
import en from '../locales/en'
import zh from '../locales/zh'

// Read the actual frontend API contracts rather than duplicating enum values.
// Backend API key states also have an independent contract regression in
// api/__tests__/apiKeyStatusLocale.spec.ts, outside the frontend-only build gate.
function literalValues(source: string, name: string, property = ''): string[] {
  const file = ts.createSourceFile('contract.ts', source, ts.ScriptTarget.Latest, true)
  const declaration = file.statements.find(node =>
    (ts.isInterfaceDeclaration(node) || ts.isTypeAliasDeclaration(node)) && node.name.text === name
  )
  let type: ts.TypeNode | undefined
  if (declaration && ts.isTypeAliasDeclaration(declaration)) {
    type = declaration.type
  } else if (declaration && ts.isInterfaceDeclaration(declaration)) {
    const member = declaration.members.find(node =>
      ts.isPropertySignature(node) && node.name.getText(file) === property
    )
    if (member && ts.isPropertySignature(member)) type = member.type
  }
  if (!type) throw new Error(`Missing API contract: ${name}.${property}`)
  const values: string[] = []
  function visit(node: ts.Node): void {
    if (ts.isLiteralTypeNode(node) && ts.isStringLiteral(node.literal)) {
      values.push(node.literal.text)
    } else {
      ts.forEachChild(node, visit)
    }
  }
  visit(type)
  if (!values.length) throw new Error(`No literal values: ${name}.${property}`)
  return values
}

function message(messages: unknown, key: string): unknown {
  return key.split('.').reduce<unknown>((node, segment) =>
    node && typeof node === 'object' ? (node as Record<string, unknown>)[segment] : undefined,
  messages)
}

const contracts = [
  [apiTypes, 'ApiKey', 'status', 'keys.status'],
  [apiTypes, 'Proxy', 'status', 'admin.accounts.status'],
  [apiTypes, 'Account', 'status', 'admin.accounts.status'],
  [apiTypes, 'Group', 'status', 'admin.accounts.status'],
  [apiTypes, 'User', 'role', 'admin.users.roles'],
  [apiTypes, 'RedeemCode', 'status', 'admin.redeem.status'],
  [apiTypes, 'RedeemCodeType', '', 'admin.redeem.types'],
  [apiTypes, 'UserSubscription', 'status', 'userSubscriptions.status'],
  [apiTypes, 'UserSubscription', 'status', 'admin.subscriptions.status'],
  [apiTypes, 'UserAttributeType', '', 'admin.users.attributes.types'],
  [paymentTypes, 'OrderStatus', '', 'payment.status'],
  [paymentTypes, 'PaymentType', '', 'payment.methods'],
  [pluginTypes, 'PluginInstallation', 'state', 'admin.plugins'],
  [pluginTypes, 'PluginInstallation', 'signature_status', 'admin.plugins'],
  [pluginTypes, 'PluginCompatibility', 'status', 'admin.plugins'],
  [monitorTypes, 'Provider', '', 'monitorCommon.providers'],
  [monitorTypes, 'MonitorStatus', '', 'monitorCommon.status'],
  [monitorTypes, 'CheckMode', '', 'monitorCommon.checkMode'],
  [statusTypes, 'ServiceStatus', '', 'channelMonitorV3.status'],
  [statusTypes, 'ServiceStatus', '', 'channelMonitorV3.description'],
  [statusTypes, 'ServiceStatus', '', 'channelMonitorV3.overview'],
  [statusTypes, 'ServiceStatus', '', 'channelMonitorV3.overviewDescription'],
  [statusTypes, 'IncidentPhase', '', 'channelMonitorV3.phase'],
  [statusTypes, 'IncidentPhase', '', 'channelMonitorV3.phaseDescription'],
  [statusTypes, 'StatusRange', '', 'channelMonitorV3.ranges'],
] as const

const domains = contracts.map(([source, name, property, prefix]) => ({
  name: `${name}${property ? `.${property}` : ''} → ${prefix}`,
  keys: literalValues(source, name, property).map(value => `${prefix}.${value.toLowerCase()}`),
}))
describe.each([{ locale: 'en', messages: en }, { locale: 'zh', messages: zh }])(
  '$locale dynamic translations', ({ messages }) => {
    it.each(domains)('translates every value in $name', ({ keys }) => {
      const missing = keys.filter(key => {
        const value = message(messages, key)
        return typeof value !== 'string' || !value.trim() || value === key
      })
      expect(missing).toEqual([])
    })
  }
)
