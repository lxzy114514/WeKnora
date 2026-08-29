package metric

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefinitionsHaveUniqueNamesAndRoundTripRuntimeFields(t *testing.T) {
	seen := make(map[string]struct{})
	result := &types.MetricResult{}
	for index, definition := range Definitions() {
		require.NotEmpty(t, definition.Name)
		_, duplicate := seen[definition.Name]
		assert.Falsef(t, duplicate, "duplicate metric name %s", definition.Name)
		seen[definition.Name] = struct{}{}
		want := float64(index + 1)
		definition.Set(result, want)
		assert.Equal(t, want, definition.Get(result))
	}
	assert.Len(t, seen, 12)
}
