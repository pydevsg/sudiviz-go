package fix

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseSelection(t *testing.T) {
	sel, err := ParseSelection("1,3", 5)
	require.NoError(t, err)
	assert.True(t, sel[1])
	assert.True(t, sel[3])
	assert.False(t, sel[2])

	sel, err = ParseSelection("1-3", 5)
	require.NoError(t, err)
	assert.Equal(t, 3, len(sel))

	_, err = ParseSelection("9", 2)
	require.Error(t, err)

	sel, err = ParseSelection("", 3)
	require.NoError(t, err)
	assert.Nil(t, sel)

	_, err = ParseSelection("nope", 3)
	require.Error(t, err)
}
