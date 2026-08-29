package chatpipeline

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
)

func TestPrepareMessagesFingerprintsStablePrefixBeforeDynamicContext(t *testing.T) {
	first := &types.ChatManage{PipelineRequest: types.PipelineRequest{
		Query: "first question", Language: "Chinese",
		SummaryConfig: types.SummaryConfig{Prompt: "Stable rules in {{language}}.\n{{contexts}}\nQuestion: {{query}}"},
	}, PipelineState: types.PipelineState{RenderedContexts: "first retrieved context"}}
	second := first.Clone()
	second.Query = "second question"
	second.RenderedContexts = "different retrieved context"

	firstMessages := prepareMessagesWithHistory(first)
	secondMessages := prepareMessagesWithHistory(second)

	assert.NotEqual(t, firstMessages[0].Content, secondMessages[0].Content)
	assert.Equal(t,
		chat.PromptPrefixFingerprint(firstMessages, nil),
		chat.PromptPrefixFingerprint(secondMessages, nil),
	)
	assert.NotContains(t, *firstMessages[0].CachePrefix, "retrieved context")
}
