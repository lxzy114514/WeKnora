import assert from 'node:assert/strict'
import test from 'node:test'

import {
  normalizeModelPricing,
  serializeModelPricing,
  supportsModelPricing,
  usesTokenPricing,
} from './modelPricing'

test('VLLM supports the same input/output pricing round trip as chat', () => {
  assert.equal(supportsModelPricing('vllm'), true)
  assert.equal(usesTokenPricing('vllm'), true)
  assert.equal(supportsModelPricing('asr'), false)

  const hydrated = normalizeModelPricing({
    currency: 'USD', input_per_million: 1.25, output_per_million: 5,
  })
  const saved = serializeModelPricing(hydrated)
  assert.equal(saved.currency, 'USD')
  assert.equal(saved.input_per_million, 1.25)
  assert.equal(saved.output_per_million, 5)
})
