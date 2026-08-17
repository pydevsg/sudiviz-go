package explain

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pydevsg/sudiviz-go/internal/diagnose"
)

func TestStreamNoIssues(t *testing.T) {
	var buf bytes.Buffer
	err := Stream(t.Context(), nil, Request{Diagnosis: &diagnose.Diagnosis{}}, &buf)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "nothing to explain")
}

func TestStreamNilDiagnosis(t *testing.T) {
	var buf bytes.Buffer
	err := Stream(t.Context(), nil, Request{}, &buf)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "nothing to explain")
}
