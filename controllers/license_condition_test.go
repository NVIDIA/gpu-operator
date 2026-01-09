package controllers

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/NVIDIA/gpu-operator/internal/conditions"
	"github.com/NVIDIA/gpu-operator/internal/licenseinfo"
)

func TestSummarizeLicenseSnapshots_AllLicensed(t *testing.T) {
	now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	expiry := now.Add(30 * 24 * time.Hour)
	snapshots := []nodeLicenseSnapshot{
		{
			node:              "node-a",
			annotationPresent: true,
			snapshot: &licenseinfo.Snapshot{
				Devices: []licenseinfo.DeviceStatus{
					{ID: "0000:00:01.0", Licensed: true, Expiry: &expiry},
				},
			},
		},
	}

	cond := summarizeLicenseSnapshots(snapshots, now)
	require.Equal(t, metav1.ConditionTrue, cond.Status)
	require.Equal(t, conditions.LicenseOK, cond.Reason)
	require.Contains(t, cond.Message, "All 1 vGPU device(s)")
}

func TestSummarizeLicenseSnapshots_ExpiringSoon(t *testing.T) {
	now := time.Now()
	expiry := now.Add(12 * time.Hour)
	snapshots := []nodeLicenseSnapshot{
		{
			node:              "node-b",
			annotationPresent: true,
			snapshot: &licenseinfo.Snapshot{
				Devices: []licenseinfo.DeviceStatus{
					{ID: "0000:00:02.0", Licensed: true, Expiry: &expiry},
				},
			},
		},
	}

	cond := summarizeLicenseSnapshots(snapshots, now)
	require.Equal(t, metav1.ConditionFalse, cond.Status)
	require.Equal(t, conditions.LicenseExpiringSoon, cond.Reason)
}

func TestSummarizeLicenseSnapshots_Unlicensed(t *testing.T) {
	now := time.Now()
	snapshots := []nodeLicenseSnapshot{
		{
			node:              "node-c",
			annotationPresent: true,
			snapshot: &licenseinfo.Snapshot{
				Devices: []licenseinfo.DeviceStatus{
					{ID: "0000:00:03.0", Licensed: false, Status: "Unlicensed"},
				},
			},
		},
	}

	cond := summarizeLicenseSnapshots(snapshots, now)
	require.Equal(t, metav1.ConditionFalse, cond.Status)
	require.Equal(t, conditions.LicenseNotReady, cond.Reason)
}

func TestSummarizeLicenseSnapshots_MissingAnnotation(t *testing.T) {
	cond := summarizeLicenseSnapshots([]nodeLicenseSnapshot{
		{node: "node-d", annotationPresent: false},
	}, time.Now())
	require.Equal(t, metav1.ConditionUnknown, cond.Status)
	require.Equal(t, conditions.LicenseInfoMissing, cond.Reason)
}
