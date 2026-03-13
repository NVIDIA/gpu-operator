/**
# Copyright (c) NVIDIA CORPORATION.  All rights reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
**/

package controllers

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	gpuv1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	"github.com/NVIDIA/gpu-operator/internal/validator"
)

// FakeConditionUpdater implements conditions.Updater
// It always returns CustomError if set
type FakeConditionUpdater struct {
	CustomError error
}

// SetConditionsError always returns CustomError if set
func (f *FakeConditionUpdater) SetConditionsError(ctx context.Context, obj any, condType, msg string) error {
	return f.CustomError
}

// SetConditionsReady always returns CustomError if set
func (f *FakeConditionUpdater) SetConditionsReady(ctx context.Context, obj any, condType, msg string) error {
	return f.CustomError
}

// FakeNodeSelectorValidator always returns CustomError if set
type FakeNodeSelectorValidator struct {
	CustomError error
}

// Validate always returns CustomError if set
func (f *FakeNodeSelectorValidator) Validate(ctx context.Context, cr *nvidiav1alpha1.NVIDIADriver) error {
	return f.CustomError
}

// newTestLogger creates a zap.Logger that writes to an in-memory buffer for testing
func newTestLogger() (logr.Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}

	encoder := zapcore.NewConsoleEncoder(zapcore.EncoderConfig{
		MessageKey:   "msg",
		LevelKey:     "level",
		NameKey:      "logger",
		CallerKey:    "caller",
		EncodeLevel:  zapcore.CapitalLevelEncoder,
		EncodeCaller: zapcore.ShortCallerEncoder,
		EncodeName:   zapcore.FullNameEncoder,
	})

	core := zapcore.NewCore(encoder, zapcore.AddSync(buf), zapcore.DebugLevel)
	zapLogger := zap.New(core)

	return zapr.NewLogger(zapLogger), buf
}

// TestReconcile tests that reconciliation proceeds or skips based on the
// ClusterPolicy and NVIDIADriver. Since Reconcile() does in-memory updates,
// we just ensure it does not error out or the error is handled gracefully.
func TestReconcile(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, nvidiav1alpha1.AddToScheme(scheme))
	require.NoError(t, gpuv1.AddToScheme(scheme))

	tests := []struct {
		name        string
		useCRD      *bool
		spec        nvidiav1alpha1.NVIDIADriverSpec
		validator   validator.Validator
		error       error
		expectedLog string
	}{
		{
			name:   "ClusterPolicy has driver CRD false → reconciliation skips driver",
			useCRD: ptr.To(false),
			validator: &FakeNodeSelectorValidator{
				CustomError: errors.New("fake list error"),
			},
			error:       nil,
			expectedLog: "useNvidiaDriverCRD is not enabled in ClusterPolicy",
		},
		{
			name:   "ClusterPolicy has driver CRD true but validator errors",
			useCRD: ptr.To(true),
			validator: &FakeNodeSelectorValidator{
				CustomError: errors.New("fake list error"),
			},
			error:       nil,
			expectedLog: "nodeSelector validation failed",
		},
		{
			name:   "driver CRD true, no validator errors, use precompiled drivers and GDS enabled",
			useCRD: ptr.To(true),
			spec: nvidiav1alpha1.NVIDIADriverSpec{
				UsePrecompiled: ptr.To(true),
				GPUDirectStorage: &nvidiav1alpha1.GPUDirectStorageSpec{
					Enabled: ptr.To(true),
				},
			},
			validator: &FakeNodeSelectorValidator{
				CustomError: nil,
			},
			error:       nil,
			expectedLog: "GPUDirect Storage driver (nvidia-fs) and/or GDRCopy driver is not supported along with pre-compiled NVIDIA drivers",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			logger, buf := newTestLogger()
			ctx := ctrl.LoggerInto(context.Background(), logger)
			driver := &nvidiav1alpha1.NVIDIADriver{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-driver",
					Namespace: "default",
				},
				Spec: tc.spec,
			}

			cp := &gpuv1.ClusterPolicy{
				ObjectMeta: metav1.ObjectMeta{Name: "default"},
				Spec: gpuv1.ClusterPolicySpec{
					Driver: gpuv1.DriverSpec{
						UseNvidiaDriverCRD: tc.useCRD,
					},
				},
			}

			// Initialize fake client with ClusterPolicy (driver optional)
			clientBuilder := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cp, driver)
			client := clientBuilder.Build()

			updater := &FakeConditionUpdater{}

			reconciler := &NVIDIADriverReconciler{
				Client:                client,
				Scheme:                scheme,
				conditionUpdater:      updater,
				nodeSelectorValidator: tc.validator,
			}

			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      driver.Name,
					Namespace: driver.Namespace,
				},
			}

			_, err := reconciler.Reconcile(ctx, req)

			if tc.error != nil {
				require.Error(t, err)
				require.EqualError(t, err, tc.error.Error())
			} else {
				require.NoError(t, err)
			}

			logs := buf.String()
			if tc.expectedLog != "" {
				require.Contains(t, logs, tc.expectedLog)
			}
		})
	}
}
