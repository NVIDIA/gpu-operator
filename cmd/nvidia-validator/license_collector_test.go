package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseVGPULicenseOutput(t *testing.T) {
	sample := `
GPU 00000000:02:00.0
    Product Name                    : NVIDIA A16
    License Status                  : Licensed (Expiry: 2025-6-26 21:46:51 GMT)
    License Expiry                  : 2025-6-26 21:46:51 GMT

GPU 00000000:82:00.0
    Product Name                    : NVIDIA A16
    License Status                  : Unlicensed
    License Expiry                  : N/A
`
	devices, err := parseVGPULicenseOutput(sample)
	require.NoError(t, err)
	require.Len(t, devices, 2)
	require.Equal(t, "00000000:02:00.0", devices[0].ID)
	require.True(t, devices[0].Licensed)
	require.NotNil(t, devices[0].Expiry)
	require.Equal(t, "Licensed", devices[0].Status)

	require.Equal(t, "00000000:82:00.0", devices[1].ID)
	require.False(t, devices[1].Licensed)
	require.Nil(t, devices[1].Expiry)
}
