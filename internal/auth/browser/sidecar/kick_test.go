package sidecar

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseInventoryNextData_Empty(t *testing.T) {
	out, err := parseInventoryNextData(`{"props":{"pageProps":{"drops":[]}}}`)
	require.NoError(t, err)
	assert.Empty(t, out)
}

func TestParseInventoryNextData_OneDrop(t *testing.T) {
	raw := `{"props":{"pageProps":{"drops":[
		{"id":"d1","minutesWatched":30,"claimed":false},
		{"id":"d2","minutesWatched":60,"claimed":true}
	]}}}`
	out, err := parseInventoryNextData(raw)
	require.NoError(t, err)
	require.Len(t, out, 2)
	assert.Equal(t, "d1", out[0].BenefitId)
	assert.Equal(t, int32(30), out[0].MinutesWatched)
	assert.False(t, out[0].Claimed)
	assert.True(t, out[1].Claimed)
}

func TestParseInventoryNextData_Malformed(t *testing.T) {
	_, err := parseInventoryNextData(`not json`)
	require.Error(t, err)
}
