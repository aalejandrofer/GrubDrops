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

func TestParseKickUsername(t *testing.T) {
	cases := []struct {
		name   string
		body   string
		want   string
		wantOK bool
	}{
		{"flat", `{"username":"alice","id":1}`, "alice", true},
		{"data wrapper", `{"data":{"username":"bob","id":2}}`, "bob", true},
		{"user wrapper", `{"user":{"username":"carol","id":3}}`, "carol", true},
		{"empty flat", `{"username":""}`, "", false},
		{"missing", `{"id":1}`, "", false},
		{"not json", `<html>cf</html>`, "", false},
		{"empty wrapped", `{"data":{"username":""}}`, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseKickUsername([]byte(tc.body))
			assert.Equal(t, tc.wantOK, ok)
			assert.Equal(t, tc.want, got)
		})
	}
}
