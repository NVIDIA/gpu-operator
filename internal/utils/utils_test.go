package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetStringHash(t *testing.T) {
	type test struct {
		input    string
		expected string
	}

	testcases := []test{
		{
			input:    "2269c984-db9a-4b0e-9fd5-86df0ad269f7",
			expected: "7c6d7bd86b",
		},
		{
			input:    "2269c984-db9a-4b0e-9fd5-86df0ad269f7-5.15.0-1041-azure",
			expected: "79d6bd954f",
		},
		{
			input:    "2269c984-db9a-4b0e-9fd5-86df0ad269f7-rhcos4.14-414.92.202309282257",
			expected: "646cdfdb96",
		},
		{
			input:    "rhcos4.14-414.92.202309282257",
			expected: "5bbdb464cb",
		},
		{
			input:    "nvidia-gpu-driver-2269c984-db9a-4b0e-9fd5-86df0ad269f7-rhcos4.14-414.92.202309282257",
			expected: "7bf6859b6d",
		},
		{
			input:    "nvidia-vgpu-driver-2269c984-db9a-4b0e-9fd5-868df0ad269f7-rhcos4.14-414.92.202309282257",
			expected: "7469f59898",
		},
	}

	for _, tc := range testcases {
		actual := GetStringHash(tc.input)
		assert.Equal(t, tc.expected, actual)
	}
}
