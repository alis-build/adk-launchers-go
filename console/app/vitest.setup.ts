import { vi } from 'vitest'

vi.mock('@/a2ui', () => ({
  getA2UICreateSurfaceId: vi.fn(() => undefined),
  useA2UISettingsStore: vi.fn(() => ({
    effectiveA2uiEnabled: false,
  })),
  ingestA2uiPayloadOnce: vi.fn(),
  resetA2uiProcessorScope: vi.fn(),
  setA2uiActionHandler: vi.fn(),
  a2uiSendActionKey: Symbol('a2uiSendAction'),
}))

vi.mock('@alis-build/a2ui-vuetify-renderer', () => ({
  CATALOG_ID: 'test-catalog',
  defaultRegistry: {},
  getCatalogSchema: () => ({}),
}))
