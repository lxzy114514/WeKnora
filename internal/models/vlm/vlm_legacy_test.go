package vlm

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLegacyVLMUsesStableCredentialFreeModelID(t *testing.T) {
	t.Setenv("SSRF_WHITELIST", "127.0.0.1")
	config := types.VLMConfig{
		ModelName: "vision-model", BaseURL: "http://127.0.0.1:12345/v1",
		APIKey: "secret-a", InterfaceType: "openai",
	}
	first, err := NewVLMFromLegacyConfig(config, nil)
	require.NoError(t, err)
	require.NotEmpty(t, first.GetModelID())

	config.APIKey = "rotated-secret"
	rotated, err := NewVLMFromLegacyConfig(config, nil)
	require.NoError(t, err)
	assert.Equal(t, first.GetModelID(), rotated.GetModelID())

	config.ModelName = "vision-model-v2"
	changed, err := NewVLMFromLegacyConfig(config, nil)
	require.NoError(t, err)
	assert.NotEqual(t, first.GetModelID(), changed.GetModelID())
}
