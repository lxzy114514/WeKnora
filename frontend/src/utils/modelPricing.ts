import type { ModelPricing } from '@/api/model'

export type PricedModelType = 'chat' | 'embedding' | 'rerank' | 'vllm' | 'asr'

export const supportsModelPricing = (modelType: PricedModelType) =>
  ['chat', 'embedding', 'rerank', 'vllm'].includes(modelType)

export const usesTokenPricing = (modelType: PricedModelType) =>
  modelType === 'chat' || modelType === 'vllm'

export const normalizeModelPricing = (pricing?: ModelPricing): ModelPricing => ({
  currency: 'USD',
  ...(pricing || {}),
})

export const serializeModelPricing = (pricing?: ModelPricing): ModelPricing => ({
  currency: pricing?.currency || 'USD',
  input_per_million: Number(pricing?.input_per_million) || 0,
  output_per_million: Number(pricing?.output_per_million) || 0,
  cache_read_per_million: Number(pricing?.cache_read_per_million) || 0,
  cache_write_per_million: Number(pricing?.cache_write_per_million) || 0,
  embedding_per_million: Number(pricing?.embedding_per_million) || 0,
  rerank_per_thousand: Number(pricing?.rerank_per_thousand) || 0,
})
